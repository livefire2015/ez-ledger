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

// CreditCardService handles all credit card operations including
// transactions, payments, refunds, and account management
type CreditCardService struct {
	db                     *sql.DB
	statementLedgerService *StatementLedgerService
	feeService             *FeeService
	cashbackService        *CashbackService
}

// NewCreditCardService creates a new credit card service
func NewCreditCardService(db *sql.DB) *CreditCardService {
	return &CreditCardService{
		db:                     db,
		statementLedgerService: NewStatementLedgerService(db),
		feeService:             NewFeeService(db),
		cashbackService:        NewCashbackService(db),
	}
}

// CreateCreditCardRequest contains parameters for creating a new credit card
type CreateCreditCardRequest struct {
	TenantID         uuid.UUID
	CardholderName   string
	CreditLimit      decimal.Decimal
	PurchaseAPR      decimal.Decimal
	BillingCycleType models.BillingCycleType
	BillingCycleDay  int
}

// CreateCreditCard creates a new credit card account
func (s *CreditCardService) CreateCreditCard(
	ctx context.Context,
	req CreateCreditCardRequest,
) (*models.CreditCard, error) {
	// Get defaults and apply request values
	card := models.CreditCardDefaults()
	card.ID = uuid.New()
	card.TenantID = req.TenantID
	card.CardholderName = req.CardholderName
	card.CreditLimit = req.CreditLimit
	card.AvailableCredit = req.CreditLimit
	card.PurchaseAPR = req.PurchaseAPR
	card.BillingCycleType = req.BillingCycleType
	card.BillingCycleDay = req.BillingCycleDay
	card.CreatedAt = time.Now()
	card.UpdatedAt = time.Now()

	// Calculate next statement date
	now := time.Now()
	startDate, _, _ := card.GetNextBillingPeriod(now)
	card.NextStatementDate = &startDate

	// Validate
	if err := card.Validate(); err != nil {
		return nil, fmt.Errorf("invalid credit card configuration: %w", err)
	}

	query := `
		INSERT INTO credit_cards (
			id, tenant_id, cardholder_name, credit_limit, available_credit,
			purchase_apr, cash_advance_apr, penalty_apr, introductory_apr,
			annual_fee, late_payment_fee, failed_payment_fee, international_fee_rate,
			cash_advance_fee, cash_advance_fee_rate, over_limit_fee,
			billing_cycle_type, billing_cycle_day, payment_due_days, grace_period_days,
			minimum_payment_percent, minimum_payment_amount,
			cashback_enabled, cashback_rate, cashback_redemption_min,
			status, next_statement_date, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29
		)
	`

	_, err := s.db.ExecContext(ctx, query,
		card.ID, card.TenantID, card.CardholderName, card.CreditLimit, card.AvailableCredit,
		card.PurchaseAPR, card.CashAdvanceAPR, card.PenaltyAPR, card.IntroductoryAPR,
		card.AnnualFee, card.LatePaymentFee, card.FailedPaymentFee, card.InternationalFeeRate,
		card.CashAdvanceFee, card.CashAdvanceFeeRate, card.OverLimitFee,
		card.BillingCycleType, card.BillingCycleDay, card.PaymentDueDays, card.GracePeriodDays,
		card.MinimumPaymentPercent, card.MinimumPaymentAmount,
		card.CashbackEnabled, card.CashbackRate, card.CashbackRedemptionMin,
		card.Status, card.NextStatementDate, card.CreatedAt, card.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create credit card: %w", err)
	}

	return &card, nil
}

// CCTransactionRequest contains parameters for recording a credit card transaction
type CCTransactionRequest struct {
	CreditCard       *models.CreditCard
	Amount           decimal.Decimal
	Description      string
	MerchantName     string
	MerchantCategory string // MCC code
	TransactionDate  time.Time
	PostingDate      time.Time
	ReferenceID      string
	IsInternational  bool
	CountryCode      string
	CurrencyCode     string
	ExchangeRate     decimal.Decimal
}

// TransactionResult contains the results of processing a transaction
type TransactionResult struct {
	TransactionEntry *models.StatementLedgerEntry
	InternationalFee *FeeAssessmentResult
	CashbackEntry    *models.CashbackLedgerEntry
	NewBalance       decimal.Decimal
	AvailableCredit  decimal.Decimal
}

// RecordTransaction records a purchase transaction on the credit card
func (s *CreditCardService) RecordTransaction(
	ctx context.Context,
	req CCTransactionRequest,
) (*TransactionResult, error) {
	// Validate card can transact
	if err := req.CreditCard.CanTransact(); err != nil {
		return nil, err
	}

	// Check available credit
	if err := req.CreditCard.HasAvailableCredit(req.Amount); err != nil {
		return nil, err
	}

	// Start transaction for atomicity
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result := &TransactionResult{}

	// Create main transaction entry
	transactionEntry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   models.EntryTypeTransaction,
		EntryDate:   req.TransactionDate,
		PostingDate: req.PostingDate,
		Amount:      req.Amount,
		Description: fmt.Sprintf("%s - %s", req.MerchantName, req.Description),
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"merchant_name":     req.MerchantName,
			"merchant_category": req.MerchantCategory,
			"is_international":  req.IsInternational,
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, transactionEntry); err != nil {
		return nil, fmt.Errorf("failed to create transaction entry: %w", err)
	}
	result.TransactionEntry = transactionEntry

	// Assess international fee if applicable
	if req.IsInternational && req.CreditCard.InternationalFeeRate.GreaterThan(decimal.Zero) {
		feeResult, err := s.feeService.AssessInternationalFee(ctx, InternationalFeeRequest{
			CreditCard:          req.CreditCard,
			TransactionAmount:   req.Amount,
			TransactionCurrency: req.CurrencyCode,
			ExchangeRate:        req.ExchangeRate,
			MerchantCountry:     req.CountryCode,
			TransactionDate:     req.TransactionDate,
			ReferenceID:         req.ReferenceID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to assess international fee: %w", err)
		}
		result.InternationalFee = feeResult
	}

	// Earn cashback if enabled
	if req.CreditCard.CashbackEnabled {
		cashbackEntry, err := s.cashbackService.EarnCashback(ctx, EarnCashbackRequest{
			TenantID:          req.CreditCard.TenantID,
			CreditCard:        req.CreditCard,
			TransactionAmount: req.Amount,
			TransactionDate:   req.TransactionDate,
			StatementEntryID:  transactionEntry.ID,
			MerchantCategory:  req.MerchantCategory,
			Description:       req.Description,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to earn cashback: %w", err)
		}
		result.CashbackEntry = cashbackEntry
	}

	// Update available credit
	totalCharged := req.Amount
	if result.InternationalFee != nil {
		totalCharged = totalCharged.Add(result.InternationalFee.FeeAmount)
	}

	newAvailableCredit := req.CreditCard.AvailableCredit.Sub(totalCharged)
	if err := s.updateAvailableCredit(ctx, req.CreditCard.ID, newAvailableCredit); err != nil {
		return nil, fmt.Errorf("failed to update available credit: %w", err)
	}

	result.AvailableCredit = newAvailableCredit
	result.NewBalance = req.CreditCard.CreditLimit.Sub(newAvailableCredit)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return result, nil
}

// CashAdvanceRequest contains parameters for a cash advance
type CashAdvanceRequest struct {
	CreditCard      *models.CreditCard
	Amount          decimal.Decimal
	ATMLocation     string
	TransactionDate time.Time
	ReferenceID     string
}

// CashAdvanceResult contains the result of processing a cash advance
type CashAdvanceResult struct {
	CashAdvanceEntry *models.StatementLedgerEntry
	FeeEntry         *FeeAssessmentResult
	NewBalance       decimal.Decimal
	AvailableCredit  decimal.Decimal
}

// RecordCashAdvance records a cash advance (ATM withdrawal)
// Note: Cash advances typically don't earn cashback and accrue interest immediately
func (s *CreditCardService) RecordCashAdvance(
	ctx context.Context,
	req CashAdvanceRequest,
) (*CashAdvanceResult, error) {
	if err := req.CreditCard.CanTransact(); err != nil {
		return nil, err
	}

	if err := req.CreditCard.HasAvailableCredit(req.Amount); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result := &CashAdvanceResult{}

	// Create cash advance entry
	advanceEntry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   models.EntryTypeCashAdvance,
		EntryDate:   req.TransactionDate,
		PostingDate: req.TransactionDate,
		Amount:      req.Amount,
		Description: fmt.Sprintf("Cash advance - %s", req.ATMLocation),
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"atm_location":     req.ATMLocation,
			"cash_advance_apr": req.CreditCard.CashAdvanceAPR.String(),
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, advanceEntry); err != nil {
		return nil, fmt.Errorf("failed to create cash advance entry: %w", err)
	}
	result.CashAdvanceEntry = advanceEntry

	// Assess cash advance fee
	feeResult, err := s.feeService.AssessCashAdvanceFee(ctx, CashAdvanceFeeRequest{
		CreditCard:        req.CreditCard,
		CashAdvanceAmount: req.Amount,
		TransactionDate:   req.TransactionDate,
		ATMLocation:       req.ATMLocation,
		ReferenceID:       req.ReferenceID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to assess cash advance fee: %w", err)
	}
	result.FeeEntry = feeResult

	// Calculate total charged
	totalCharged := req.Amount
	if feeResult != nil {
		totalCharged = totalCharged.Add(feeResult.FeeAmount)
	}

	// Update available credit
	newAvailableCredit := req.CreditCard.AvailableCredit.Sub(totalCharged)
	if err := s.updateAvailableCredit(ctx, req.CreditCard.ID, newAvailableCredit); err != nil {
		return nil, fmt.Errorf("failed to update available credit: %w", err)
	}

	result.AvailableCredit = newAvailableCredit
	result.NewBalance = req.CreditCard.CreditLimit.Sub(newAvailableCredit)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit cash advance: %w", err)
	}

	return result, nil
}

// CCPaymentRequest contains parameters for recording a credit card payment
type CCPaymentRequest struct {
	CreditCard    *models.CreditCard
	Amount        decimal.Decimal
	PaymentDate   time.Time
	PostingDate   time.Time
	PaymentMethod string
	ReferenceID   string
	Description   string
}

// CCPaymentResult contains the result of processing a payment
type CCPaymentResult struct {
	PaymentEntry    *models.StatementLedgerEntry
	NewBalance      decimal.Decimal
	AvailableCredit decimal.Decimal
}

// RecordPayment records a payment on the credit card
// Note: Payments do NOT affect cashback or points balance
func (s *CreditCardService) RecordPayment(
	ctx context.Context,
	req CCPaymentRequest,
) (*CCPaymentResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result := &CCPaymentResult{}

	// Create payment entry
	paymentEntry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   models.EntryTypePayment,
		EntryDate:   req.PaymentDate,
		PostingDate: req.PostingDate,
		Amount:      req.Amount,
		Description: fmt.Sprintf("Payment received - %s", req.Description),
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending, // Will be cleared when ACH settles
		Metadata: map[string]interface{}{
			"payment_method": req.PaymentMethod,
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, paymentEntry); err != nil {
		return nil, fmt.Errorf("failed to create payment entry: %w", err)
	}
	result.PaymentEntry = paymentEntry

	// Update available credit (increase by payment amount)
	newAvailableCredit := req.CreditCard.AvailableCredit.Add(req.Amount)
	// Cap at credit limit
	if newAvailableCredit.GreaterThan(req.CreditCard.CreditLimit) {
		newAvailableCredit = req.CreditCard.CreditLimit
	}

	if err := s.updateAvailableCredit(ctx, req.CreditCard.ID, newAvailableCredit); err != nil {
		return nil, fmt.Errorf("failed to update available credit: %w", err)
	}

	result.AvailableCredit = newAvailableCredit
	result.NewBalance = req.CreditCard.CreditLimit.Sub(newAvailableCredit)

	// Update last payment info
	if err := s.updateLastPayment(ctx, req.CreditCard.ID, req.PaymentDate, req.Amount); err != nil {
		return nil, fmt.Errorf("failed to update last payment: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit payment: %w", err)
	}

	return result, nil
}

// FailedPaymentRequest contains parameters for recording a failed payment
type FailedPaymentRequest struct {
	CreditCard      *models.CreditCard
	OriginalPayment *models.StatementLedgerEntry
	FailureReason   string
	FailureDate     time.Time
}

// FailedPaymentResult contains the result of processing a failed payment
type FailedPaymentResult struct {
	ReversalEntry *models.StatementLedgerEntry
	FeeEntry      *FeeAssessmentResult
	NewBalance    decimal.Decimal
}

// RecordFailedPayment records a returned/failed payment
func (s *CreditCardService) RecordFailedPayment(
	ctx context.Context,
	req FailedPaymentRequest,
) (*FailedPaymentResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result := &FailedPaymentResult{}

	// Create reversal entry (adds back the payment amount as a charge)
	reversalEntry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   models.EntryTypeAdjustment,
		EntryDate:   req.FailureDate,
		PostingDate: req.FailureDate,
		Amount:      req.OriginalPayment.Amount, // Positive to add back
		Description: fmt.Sprintf("Payment returned - %s", req.FailureReason),
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"original_payment_id": req.OriginalPayment.ID.String(),
			"failure_reason":      req.FailureReason,
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, reversalEntry); err != nil {
		return nil, fmt.Errorf("failed to create reversal entry: %w", err)
	}
	result.ReversalEntry = reversalEntry

	// Assess failed payment fee
	refID := req.OriginalPayment.ID.String()
	feeResult, err := s.feeService.AssessFailedPaymentFee(ctx, FailedPaymentFeeRequest{
		CreditCard:    req.CreditCard,
		PaymentAmount: req.OriginalPayment.Amount,
		PaymentDate:   req.FailureDate,
		FailureReason: req.FailureReason,
		PaymentMethod: "ACH",
		ReferenceID:   refID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to assess failed payment fee: %w", err)
	}
	result.FeeEntry = feeResult

	// Update available credit (reduce by reversed amount + fee)
	totalReduction := req.OriginalPayment.Amount
	if feeResult != nil {
		totalReduction = totalReduction.Add(feeResult.FeeAmount)
	}

	newAvailableCredit := req.CreditCard.AvailableCredit.Sub(totalReduction)
	if err := s.updateAvailableCredit(ctx, req.CreditCard.ID, newAvailableCredit); err != nil {
		return nil, fmt.Errorf("failed to update available credit: %w", err)
	}

	result.NewBalance = req.CreditCard.CreditLimit.Sub(newAvailableCredit)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit failed payment: %w", err)
	}

	return result, nil
}

// CCRefundRequest contains parameters for recording a credit card refund
type CCRefundRequest struct {
	CreditCard            *models.CreditCard
	OriginalTransactionID uuid.UUID
	RefundAmount          decimal.Decimal
	RefundDate            time.Time
	PostingDate           time.Time
	MerchantName          string
	ReferenceID           string
	Description           string
}

// RefundResult contains the result of processing a refund
type RefundResult struct {
	RefundEntry     *models.StatementLedgerEntry
	CashbackAdjust  *models.CashbackLedgerEntry
	NewBalance      decimal.Decimal
	AvailableCredit decimal.Decimal
}

// RecordRefund records a merchant refund/credit
func (s *CreditCardService) RecordRefund(
	ctx context.Context,
	req CCRefundRequest,
) (*RefundResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result := &RefundResult{}

	// Create refund entry
	refundEntry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   models.EntryTypeRefund,
		EntryDate:   req.RefundDate,
		PostingDate: req.PostingDate,
		Amount:      req.RefundAmount,
		Description: fmt.Sprintf("Refund from %s - %s", req.MerchantName, req.Description),
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"original_transaction_id": req.OriginalTransactionID.String(),
			"merchant_name":           req.MerchantName,
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, refundEntry); err != nil {
		return nil, fmt.Errorf("failed to create refund entry: %w", err)
	}
	result.RefundEntry = refundEntry

	// Adjust cashback if applicable
	if req.CreditCard.CashbackEnabled {
		cashbackAdj, err := s.cashbackService.AdjustCashbackForRefund(ctx, AdjustCashbackForRefundRequest{
			TenantID:                   req.CreditCard.TenantID,
			CreditCard:                 req.CreditCard,
			RefundAmount:               req.RefundAmount,
			RefundDate:                 req.RefundDate,
			OriginalTransactionEntryID: req.OriginalTransactionID,
			RefundEntryID:              refundEntry.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to adjust cashback: %w", err)
		}
		result.CashbackAdjust = cashbackAdj
	}

	// Update available credit (increase by refund amount)
	newAvailableCredit := req.CreditCard.AvailableCredit.Add(req.RefundAmount)
	// Cap at credit limit
	if newAvailableCredit.GreaterThan(req.CreditCard.CreditLimit) {
		newAvailableCredit = req.CreditCard.CreditLimit
	}

	if err := s.updateAvailableCredit(ctx, req.CreditCard.ID, newAvailableCredit); err != nil {
		return nil, fmt.Errorf("failed to update available credit: %w", err)
	}

	result.AvailableCredit = newAvailableCredit
	result.NewBalance = req.CreditCard.CreditLimit.Sub(newAvailableCredit)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit refund: %w", err)
	}

	return result, nil
}

// AdjustmentRequest contains parameters for a manual adjustment
type AdjustmentRequest struct {
	CreditCard     *models.CreditCard
	Amount         decimal.Decimal // Positive = charge, Negative = credit
	AdjustmentDate time.Time
	Reason         string
	ApprovedBy     string
	ReferenceID    string
}

// RecordAdjustment records a manual adjustment (credit or debit)
func (s *CreditCardService) RecordAdjustment(
	ctx context.Context,
	req AdjustmentRequest,
) (*models.StatementLedgerEntry, error) {
	entryType := models.EntryTypeAdjustment
	if req.Amount.LessThan(decimal.Zero) {
		entryType = models.EntryTypeCredit
	}

	entry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   entryType,
		EntryDate:   req.AdjustmentDate,
		PostingDate: req.AdjustmentDate,
		Amount:      req.Amount.Abs(),
		Description: fmt.Sprintf("Manual adjustment: %s", req.Reason),
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"adjustment_reason": req.Reason,
			"approved_by":       req.ApprovedBy,
		},
		CreatedBy: &req.ApprovedBy,
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create adjustment entry: %w", err)
	}

	// Update available credit
	var newAvailableCredit decimal.Decimal
	if req.Amount.LessThan(decimal.Zero) {
		// Credit adjustment - increase available
		newAvailableCredit = req.CreditCard.AvailableCredit.Add(req.Amount.Abs())
	} else {
		// Debit adjustment - decrease available
		newAvailableCredit = req.CreditCard.AvailableCredit.Sub(req.Amount)
	}

	if err := s.updateAvailableCredit(ctx, req.CreditCard.ID, newAvailableCredit); err != nil {
		return nil, fmt.Errorf("failed to update available credit: %w", err)
	}

	return entry, nil
}

// GetCreditCard retrieves a credit card by ID
func (s *CreditCardService) GetCreditCard(ctx context.Context, cardID uuid.UUID) (*models.CreditCard, error) {
	query := `
		SELECT id, tenant_id, cardholder_name, credit_limit, available_credit,
		       purchase_apr, cash_advance_apr, penalty_apr, introductory_apr,
		       introductory_end_date, annual_fee, late_payment_fee, failed_payment_fee,
		       international_fee_rate, cash_advance_fee, cash_advance_fee_rate, over_limit_fee,
		       billing_cycle_type, billing_cycle_day, payment_due_days, grace_period_days,
		       minimum_payment_percent, minimum_payment_amount,
		       cashback_enabled, cashback_rate, cashback_redemption_min,
		       status, last_statement_date, next_statement_date,
		       last_payment_date, last_payment_amount, consecutive_late_count,
		       created_at, updated_at, closed_at
		FROM credit_cards
		WHERE id = $1
	`

	card := &models.CreditCard{}
	err := s.db.QueryRowContext(ctx, query, cardID).Scan(
		&card.ID, &card.TenantID, &card.CardholderName, &card.CreditLimit, &card.AvailableCredit,
		&card.PurchaseAPR, &card.CashAdvanceAPR, &card.PenaltyAPR, &card.IntroductoryAPR,
		&card.IntroductoryEndDate, &card.AnnualFee, &card.LatePaymentFee, &card.FailedPaymentFee,
		&card.InternationalFeeRate, &card.CashAdvanceFee, &card.CashAdvanceFeeRate, &card.OverLimitFee,
		&card.BillingCycleType, &card.BillingCycleDay, &card.PaymentDueDays, &card.GracePeriodDays,
		&card.MinimumPaymentPercent, &card.MinimumPaymentAmount,
		&card.CashbackEnabled, &card.CashbackRate, &card.CashbackRedemptionMin,
		&card.Status, &card.LastStatementDate, &card.NextStatementDate,
		&card.LastPaymentDate, &card.LastPaymentAmount, &card.ConsecutiveLateCount,
		&card.CreatedAt, &card.UpdatedAt, &card.ClosedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("credit card not found: %s", cardID)
	}
	if err != nil {
		return nil, err
	}

	return card, nil
}

// GetCreditCardByTenant retrieves a credit card by tenant ID
func (s *CreditCardService) GetCreditCardByTenant(ctx context.Context, tenantID uuid.UUID) (*models.CreditCard, error) {
	query := `
		SELECT id FROM credit_cards WHERE tenant_id = $1 AND status != 'closed' LIMIT 1
	`

	var cardID uuid.UUID
	err := s.db.QueryRowContext(ctx, query, tenantID).Scan(&cardID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active credit card found for tenant: %s", tenantID)
	}
	if err != nil {
		return nil, err
	}

	return s.GetCreditCard(ctx, cardID)
}

// UpdateCreditCardAPR updates the APR for a credit card
func (s *CreditCardService) UpdateCreditCardAPR(
	ctx context.Context,
	cardID uuid.UUID,
	newAPR decimal.Decimal,
	aprType string, // "purchase", "cash_advance", "penalty"
) error {
	var column string
	switch aprType {
	case "purchase":
		column = "purchase_apr"
	case "cash_advance":
		column = "cash_advance_apr"
	case "penalty":
		column = "penalty_apr"
	default:
		return fmt.Errorf("invalid APR type: %s", aprType)
	}

	query := fmt.Sprintf(`UPDATE credit_cards SET %s = $1, updated_at = $2 WHERE id = $3`, column)
	_, err := s.db.ExecContext(ctx, query, newAPR, time.Now(), cardID)
	return err
}

// FreezeCard freezes a credit card account
func (s *CreditCardService) FreezeCard(ctx context.Context, cardID uuid.UUID) error {
	query := `UPDATE credit_cards SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := s.db.ExecContext(ctx, query, models.CreditCardStatusFrozen, time.Now(), cardID)
	return err
}

// UnfreezeCard unfreezes a credit card account
func (s *CreditCardService) UnfreezeCard(ctx context.Context, cardID uuid.UUID) error {
	query := `UPDATE credit_cards SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := s.db.ExecContext(ctx, query, models.CreditCardStatusActive, time.Now(), cardID)
	return err
}

// CloseCard closes a credit card account
func (s *CreditCardService) CloseCard(ctx context.Context, cardID uuid.UUID) error {
	now := time.Now()
	query := `UPDATE credit_cards SET status = $1, closed_at = $2, updated_at = $2 WHERE id = $3`
	_, err := s.db.ExecContext(ctx, query, models.CreditCardStatusClosed, now, cardID)
	return err
}

// updateAvailableCredit updates the available credit for a card
func (s *CreditCardService) updateAvailableCredit(
	ctx context.Context,
	cardID uuid.UUID,
	newCredit decimal.Decimal,
) error {
	query := `UPDATE credit_cards SET available_credit = $1, updated_at = $2 WHERE id = $3`
	_, err := s.db.ExecContext(ctx, query, newCredit, time.Now(), cardID)
	return err
}

// updateLastPayment updates the last payment information
func (s *CreditCardService) updateLastPayment(
	ctx context.Context,
	cardID uuid.UUID,
	paymentDate time.Time,
	amount decimal.Decimal,
) error {
	query := `
		UPDATE credit_cards
		SET last_payment_date = $1, last_payment_amount = $2, updated_at = $3
		WHERE id = $4
	`
	_, err := s.db.ExecContext(ctx, query, paymentDate, amount, time.Now(), cardID)
	return err
}
