package services

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/livefire2015/ez-ledger/src/models"
	"github.com/shopspring/decimal"
)

// CashbackService handles cashback calculation, earning, and redemption
type CashbackService struct {
	db                     *sql.DB
	statementLedgerService *StatementLedgerService
}

// NewCashbackService creates a new cashback service
func NewCashbackService(db *sql.DB) *CashbackService {
	return &CashbackService{
		db:                     db,
		statementLedgerService: NewStatementLedgerService(db),
	}
}

// EarnCashbackRequest contains parameters for earning cashback on a transaction
type EarnCashbackRequest struct {
	TenantID          uuid.UUID
	CreditCard        *models.CreditCard
	TransactionAmount decimal.Decimal
	TransactionDate   time.Time
	StatementEntryID  uuid.UUID
	MerchantCategory  string
	Description       string
}

// EarnCashback records cashback earned from a qualifying transaction
func (s *CashbackService) EarnCashback(
	ctx context.Context,
	req EarnCashbackRequest,
) (*models.CashbackLedgerEntry, error) {
	// Check if cashback is enabled for this card
	if !req.CreditCard.CashbackEnabled {
		return nil, nil // No cashback for this card
	}

	// Get cashback earning rules
	rule, err := s.getEarningRule(ctx, req.CreditCard.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cashback rules: %w", err)
	}

	// Calculate cashback amount
	cashbackAmount, effectiveRate := rule.CalculateCashback(
		req.TransactionAmount,
		req.MerchantCategory,
		req.TransactionDate,
	)

	// Don't create entry if no cashback earned
	if cashbackAmount.LessThanOrEqual(decimal.Zero) {
		return nil, nil
	}

	// Check for category bonus
	var categoryBonus *decimal.Decimal
	if cat, ok := rule.CategoryRules[req.MerchantCategory]; ok {
		bonus := cat.BonusRate.Sub(rule.BaseRate)
		if bonus.GreaterThan(decimal.Zero) {
			categoryBonus = &bonus
		}
	}

	entry := &models.CashbackLedgerEntry{
		ID:                uuid.New(),
		TenantID:          req.TenantID,
		CreditCardID:      req.CreditCard.ID,
		StatementEntryID:  &req.StatementEntryID,
		EntryType:         models.CashbackEarned,
		EntryDate:         req.TransactionDate,
		Amount:            cashbackAmount,
		Description:       fmt.Sprintf("Cashback earned: %s", req.Description),
		TransactionAmount: &req.TransactionAmount,
		CashbackRate:      &effectiveRate,
		CategoryBonus:     categoryBonus,
		Metadata: map[string]interface{}{
			"merchant_category":   req.MerchantCategory,
			"transaction_id":      req.StatementEntryID.String(),
			"base_rate":           rule.BaseRate.String(),
		},
		CreatedAt: time.Now(),
	}

	if err := s.createEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create cashback entry: %w", err)
	}

	return entry, nil
}

// AdjustCashbackForRefund adjusts cashback when a transaction is refunded
type AdjustCashbackForRefundRequest struct {
	TenantID                  uuid.UUID
	CreditCard                *models.CreditCard
	RefundAmount              decimal.Decimal
	RefundDate                time.Time
	OriginalTransactionEntryID uuid.UUID
	RefundEntryID             uuid.UUID
}

// AdjustCashbackForRefund deducts cashback when the original transaction is refunded
func (s *CashbackService) AdjustCashbackForRefund(
	ctx context.Context,
	req AdjustCashbackForRefundRequest,
) (*models.CashbackLedgerEntry, error) {
	// Find original cashback entry
	originalEntry, err := s.getEntryByStatementEntryID(ctx, req.OriginalTransactionEntryID)
	if err == sql.ErrNoRows {
		return nil, nil // No cashback was earned on original transaction
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find original cashback: %w", err)
	}

	// Calculate proportional cashback to deduct
	var deductAmount decimal.Decimal
	if originalEntry.TransactionAmount != nil && !originalEntry.TransactionAmount.IsZero() {
		// Proportional to refund amount
		ratio := req.RefundAmount.Div(*originalEntry.TransactionAmount)
		deductAmount = originalEntry.Amount.Mul(ratio).Round(2)
	} else {
		// Full reversal if no transaction amount recorded
		deductAmount = originalEntry.Amount
	}

	// Don't exceed original cashback
	if deductAmount.GreaterThan(originalEntry.Amount) {
		deductAmount = originalEntry.Amount
	}

	entry := &models.CashbackLedgerEntry{
		ID:                uuid.New(),
		TenantID:          req.TenantID,
		CreditCardID:      req.CreditCard.ID,
		StatementEntryID:  &req.RefundEntryID,
		EntryType:         models.CashbackEarnedRefund,
		EntryDate:         req.RefundDate,
		Amount:            deductAmount.Neg(), // Negative for deduction
		Description:       "Cashback adjustment for refund",
		TransactionAmount: &req.RefundAmount,
		CashbackRate:      originalEntry.CashbackRate,
		Metadata: map[string]interface{}{
			"original_cashback_id": originalEntry.ID.String(),
			"original_amount":      originalEntry.Amount.String(),
			"refund_entry_id":      req.RefundEntryID.String(),
		},
		CreatedAt: time.Now(),
	}

	if err := s.createEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create cashback adjustment: %w", err)
	}

	return entry, nil
}

// RedeemCashbackRequest contains parameters for redeeming cashback
type RedeemCashbackRequest struct {
	TenantID      uuid.UUID
	CreditCard    *models.CreditCard
	Amount        decimal.Decimal
	RedemptionDate time.Time
	RedeemAs      string // "statement_credit", "check", "direct_deposit"
}

// RedeemCashback redeems accumulated cashback
func (s *CashbackService) RedeemCashback(
	ctx context.Context,
	req RedeemCashbackRequest,
) (*models.CashbackLedgerEntry, *models.StatementLedgerEntry, error) {
	// Get current balance
	balance, err := s.GetBalance(ctx, req.CreditCard.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get cashback balance: %w", err)
	}

	// Validate redemption
	if req.Amount.GreaterThan(balance.AvailableBalance) {
		return nil, nil, fmt.Errorf("insufficient cashback balance: available $%.2f, requested $%.2f",
			balance.AvailableBalance.InexactFloat64(), req.Amount.InexactFloat64())
	}

	// Check minimum redemption amount
	if req.Amount.LessThan(req.CreditCard.CashbackRedemptionMin) {
		return nil, nil, fmt.Errorf("minimum redemption amount is $%.2f",
			req.CreditCard.CashbackRedemptionMin.InexactFloat64())
	}

	// Start transaction for atomicity
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	// Create cashback redemption entry (negative)
	cashbackEntry := &models.CashbackLedgerEntry{
		ID:           uuid.New(),
		TenantID:     req.TenantID,
		CreditCardID: req.CreditCard.ID,
		EntryType:    models.CashbackRedeemed,
		EntryDate:    req.RedemptionDate,
		Amount:       req.Amount.Neg(), // Negative for redemption
		Description:  fmt.Sprintf("Cashback redemption (%s)", req.RedeemAs),
		Metadata: map[string]interface{}{
			"redemption_type": req.RedeemAs,
		},
		CreatedAt: time.Now(),
	}

	if err := s.createEntry(ctx, cashbackEntry); err != nil {
		return nil, nil, fmt.Errorf("failed to create cashback redemption entry: %w", err)
	}

	var statementEntry *models.StatementLedgerEntry

	// If redeeming as statement credit, create corresponding statement entry
	if req.RedeemAs == "statement_credit" {
		statementEntry = &models.StatementLedgerEntry{
			ID:          uuid.New(),
			TenantID:    req.TenantID,
			EntryType:   models.EntryTypeCashbackRedeemed,
			EntryDate:   req.RedemptionDate,
			PostingDate: req.RedemptionDate,
			Amount:      req.Amount, // Credit amount (will be treated as credit)
			Description: fmt.Sprintf("Cashback redemption - $%.2f applied", req.Amount.InexactFloat64()),
			Status:      models.EntryStatusPending,
			Metadata: map[string]interface{}{
				"cashback_entry_id": cashbackEntry.ID.String(),
			},
			CreatedAt: time.Now(),
		}

		if err := s.statementLedgerService.CreateEntry(ctx, statementEntry); err != nil {
			return nil, nil, fmt.Errorf("failed to create statement credit entry: %w", err)
		}

		// Link the two entries
		cashbackEntry.StatementEntryID = &statementEntry.ID
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit redemption: %w", err)
	}

	return cashbackEntry, statementEntry, nil
}

// GetBalance returns the current cashback balance for a credit card
func (s *CashbackService) GetBalance(
	ctx context.Context,
	creditCardID uuid.UUID,
) (*models.CashbackBalance, error) {
	query := `
		SELECT
			tenant_id,
			credit_card_id,
			COALESCE(SUM(CASE WHEN entry_type = 'earned' THEN amount ELSE 0 END), 0) as earned_total,
			COALESCE(SUM(CASE WHEN entry_type = 'redeemed' THEN ABS(amount) ELSE 0 END), 0) as redeemed_total,
			COALESCE(SUM(CASE WHEN entry_type = 'expired' THEN ABS(amount) ELSE 0 END), 0) as expired_total,
			COALESCE(SUM(amount), 0) as available_balance,
			COUNT(*) as total_entries,
			MAX(entry_date) as last_activity_date
		FROM cashback_ledger_entries
		WHERE credit_card_id = $1
		GROUP BY tenant_id, credit_card_id
	`

	balance := &models.CashbackBalance{CreditCardID: creditCardID}
	err := s.db.QueryRowContext(ctx, query, creditCardID).Scan(
		&balance.TenantID,
		&balance.CreditCardID,
		&balance.EarnedTotal,
		&balance.RedeemedTotal,
		&balance.ExpiredTotal,
		&balance.AvailableBalance,
		&balance.TotalEntries,
		&balance.LastActivityDate,
	)

	if err == sql.ErrNoRows {
		return &models.CashbackBalance{CreditCardID: creditCardID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cashback balance: %w", err)
	}

	return balance, nil
}

// GetCashbackStatement generates a cashback statement for a billing cycle
func (s *CashbackService) GetCashbackStatement(
	ctx context.Context,
	creditCardID uuid.UUID,
	startDate, endDate time.Time,
) (*models.CashbackStatement, error) {
	statement := &models.CashbackStatement{
		CreditCardID:   creditCardID,
		CycleStartDate: startDate,
		CycleEndDate:   endDate,
	}

	// Get summary totals
	summaryQuery := `
		SELECT
			COALESCE(SUM(CASE WHEN entry_type = 'earned' THEN amount ELSE 0 END), 0) as earned,
			COALESCE(SUM(CASE WHEN entry_type = 'redeemed' THEN ABS(amount) ELSE 0 END), 0) as redeemed,
			COALESCE(SUM(CASE WHEN entry_type = 'earned' THEN transaction_amount ELSE 0 END), 0) as total_purchases
		FROM cashback_ledger_entries
		WHERE credit_card_id = $1
		  AND entry_date >= $2
		  AND entry_date <= $3
	`

	err := s.db.QueryRowContext(ctx, summaryQuery, creditCardID, startDate, endDate).Scan(
		&statement.CashbackEarned,
		&statement.CashbackRedeemed,
		&statement.TotalPurchases,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get cashback summary: %w", err)
	}

	// Calculate effective rate
	if statement.TotalPurchases.GreaterThan(decimal.Zero) {
		statement.EffectiveRate = statement.CashbackEarned.
			Div(statement.TotalPurchases).
			Mul(decimal.NewFromInt(100)).
			Round(2)
	}

	// Get ending balance
	balance, err := s.GetBalance(ctx, creditCardID)
	if err != nil {
		return nil, err
	}
	statement.EndingBalance = balance.AvailableBalance

	// Get category breakdown
	categoryQuery := `
		SELECT
			COALESCE(metadata->>'merchant_category', 'unknown') as category_code,
			COUNT(*) as tx_count,
			COALESCE(SUM(transaction_amount), 0) as total_spent,
			COALESCE(SUM(amount), 0) as cashback_earned
		FROM cashback_ledger_entries
		WHERE credit_card_id = $1
		  AND entry_date >= $2
		  AND entry_date <= $3
		  AND entry_type = 'earned'
		GROUP BY metadata->>'merchant_category'
		ORDER BY cashback_earned DESC
	`

	rows, err := s.db.QueryContext(ctx, categoryQuery, creditCardID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get category breakdown: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var summary models.CategoryCashbackSummary
		err := rows.Scan(
			&summary.CategoryCode,
			&summary.TransactionCount,
			&summary.TotalSpent,
			&summary.CashbackEarned,
		)
		if err != nil {
			return nil, err
		}

		// Calculate effective rate for category
		if summary.TotalSpent.GreaterThan(decimal.Zero) {
			summary.EffectiveRate = summary.CashbackEarned.
				Div(summary.TotalSpent).
				Mul(decimal.NewFromInt(100)).
				Round(2)
		}

		statement.CategoryBreakdown = append(statement.CategoryBreakdown, summary)
	}

	return statement, rows.Err()
}

// GetRecentEarnings returns recent cashback earnings for a card
func (s *CashbackService) GetRecentEarnings(
	ctx context.Context,
	creditCardID uuid.UUID,
	limit int,
) ([]*models.CashbackLedgerEntry, error) {
	query := `
		SELECT id, tenant_id, credit_card_id, statement_entry_id, entry_type,
		       entry_date, amount, description, transaction_amount, cashback_rate,
		       category_bonus, metadata, created_at, created_by
		FROM cashback_ledger_entries
		WHERE credit_card_id = $1
		ORDER BY entry_date DESC, created_at DESC
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, creditCardID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*models.CashbackLedgerEntry
	for rows.Next() {
		entry := &models.CashbackLedgerEntry{}
		err := rows.Scan(
			&entry.ID,
			&entry.TenantID,
			&entry.CreditCardID,
			&entry.StatementEntryID,
			&entry.EntryType,
			&entry.EntryDate,
			&entry.Amount,
			&entry.Description,
			&entry.TransactionAmount,
			&entry.CashbackRate,
			&entry.CategoryBonus,
			&entry.Metadata,
			&entry.CreatedAt,
			&entry.CreatedBy,
		)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// SetCategoryBonusRate sets a bonus cashback rate for a merchant category
func (s *CashbackService) SetCategoryBonusRate(
	ctx context.Context,
	creditCardID uuid.UUID,
	categoryCode string,
	categoryName string,
	bonusRate decimal.Decimal,
	maxBonus *decimal.Decimal,
	startDate, endDate *time.Time,
) (*models.CashbackCategory, error) {
	category := &models.CashbackCategory{
		ID:           uuid.New(),
		CreditCardID: creditCardID,
		CategoryCode: categoryCode,
		CategoryName: categoryName,
		BonusRate:    bonusRate,
		MaxBonus:     maxBonus,
		IsActive:     true,
		StartDate:    startDate,
		EndDate:      endDate,
	}

	query := `
		INSERT INTO cashback_categories (
			id, credit_card_id, category_code, category_name, bonus_rate,
			max_bonus, is_active, start_date, end_date
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (credit_card_id, category_code)
		DO UPDATE SET
			category_name = EXCLUDED.category_name,
			bonus_rate = EXCLUDED.bonus_rate,
			max_bonus = EXCLUDED.max_bonus,
			is_active = EXCLUDED.is_active,
			start_date = EXCLUDED.start_date,
			end_date = EXCLUDED.end_date
	`

	_, err := s.db.ExecContext(ctx, query,
		category.ID,
		category.CreditCardID,
		category.CategoryCode,
		category.CategoryName,
		category.BonusRate,
		category.MaxBonus,
		category.IsActive,
		category.StartDate,
		category.EndDate,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to set category bonus: %w", err)
	}

	return category, nil
}

// getEarningRule retrieves the cashback earning rule for a card
func (s *CashbackService) getEarningRule(
	ctx context.Context,
	creditCardID uuid.UUID,
) (*models.CashbackEarningRule, error) {
	// Get card's base cashback rate
	cardQuery := `SELECT cashback_rate FROM credit_cards WHERE id = $1`
	var baseRate decimal.Decimal
	err := s.db.QueryRowContext(ctx, cardQuery, creditCardID).Scan(&baseRate)
	if err != nil {
		return nil, err
	}

	rule := &models.CashbackEarningRule{
		BaseRate:       baseRate,
		MinTransaction: decimal.NewFromFloat(0.01),
		CategoryRules:  make(map[string]models.CashbackCategory),
	}

	// Get category bonuses
	categoryQuery := `
		SELECT id, credit_card_id, category_code, category_name, bonus_rate,
		       max_bonus, is_active, start_date, end_date
		FROM cashback_categories
		WHERE credit_card_id = $1 AND is_active = true
	`

	rows, err := s.db.QueryContext(ctx, categoryQuery, creditCardID)
	if err != nil {
		return rule, nil // Return base rule if categories fail
	}
	defer rows.Close()

	for rows.Next() {
		var cat models.CashbackCategory
		err := rows.Scan(
			&cat.ID,
			&cat.CreditCardID,
			&cat.CategoryCode,
			&cat.CategoryName,
			&cat.BonusRate,
			&cat.MaxBonus,
			&cat.IsActive,
			&cat.StartDate,
			&cat.EndDate,
		)
		if err != nil {
			continue
		}
		rule.CategoryRules[cat.CategoryCode] = cat
	}

	return rule, nil
}

// getEntryByStatementEntryID finds a cashback entry by its linked statement entry
func (s *CashbackService) getEntryByStatementEntryID(
	ctx context.Context,
	statementEntryID uuid.UUID,
) (*models.CashbackLedgerEntry, error) {
	query := `
		SELECT id, tenant_id, credit_card_id, statement_entry_id, entry_type,
		       entry_date, amount, description, transaction_amount, cashback_rate,
		       category_bonus, metadata, created_at, created_by
		FROM cashback_ledger_entries
		WHERE statement_entry_id = $1
		LIMIT 1
	`

	entry := &models.CashbackLedgerEntry{}
	err := s.db.QueryRowContext(ctx, query, statementEntryID).Scan(
		&entry.ID,
		&entry.TenantID,
		&entry.CreditCardID,
		&entry.StatementEntryID,
		&entry.EntryType,
		&entry.EntryDate,
		&entry.Amount,
		&entry.Description,
		&entry.TransactionAmount,
		&entry.CashbackRate,
		&entry.CategoryBonus,
		&entry.Metadata,
		&entry.CreatedAt,
		&entry.CreatedBy,
	)

	return entry, err
}

// createEntry inserts a new cashback ledger entry
func (s *CashbackService) createEntry(ctx context.Context, entry *models.CashbackLedgerEntry) error {
	query := `
		INSERT INTO cashback_ledger_entries (
			id, tenant_id, credit_card_id, statement_entry_id, entry_type,
			entry_date, amount, description, reference_id, transaction_amount,
			cashback_rate, category_bonus, metadata, created_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	_, err := s.db.ExecContext(ctx, query,
		entry.ID,
		entry.TenantID,
		entry.CreditCardID,
		entry.StatementEntryID,
		entry.EntryType,
		entry.EntryDate,
		entry.Amount,
		entry.Description,
		entry.ReferenceID,
		entry.TransactionAmount,
		entry.CashbackRate,
		entry.CategoryBonus,
		entry.Metadata,
		entry.CreatedAt,
		entry.CreatedBy,
	)

	return err
}
