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

// LedgerReconciliationService coordinates between Statement and Points ledgers
// This service ensures both ledgers stay in sync and handles cross-ledger transactions
type LedgerReconciliationService struct {
	db                     *sql.DB
	statementLedgerService *StatementLedgerService
	pointsLedgerService    *PointsLedgerService
	defaultPointsRule      models.PointsEarningRule
}

// NewLedgerReconciliationService creates a new reconciliation service
func NewLedgerReconciliationService(
	db *sql.DB,
	defaultPointsRule models.PointsEarningRule,
) *LedgerReconciliationService {
	return &LedgerReconciliationService{
		db:                     db,
		statementLedgerService: NewStatementLedgerService(db),
		pointsLedgerService:    NewPointsLedgerService(db),
		defaultPointsRule:      defaultPointsRule,
	}
}

// TransactionRequest represents a request to record a transaction
type TransactionRequest struct {
	TenantID      uuid.UUID
	Amount        decimal.Decimal
	Description   string
	ReferenceID   string
	TransactionDate time.Time
	PostingDate   time.Time
	EarnPoints    bool // Whether this transaction earns points
	PointsRule    *models.PointsEarningRule // Optional custom points rule
}

// RecordTransaction records a transaction in both ledgers (if points earning)
// This is the main reconciliation point between the two ledgers
func (s *LedgerReconciliationService) RecordTransaction(
	ctx context.Context,
	req TransactionRequest,
) (*models.StatementLedgerEntry, *models.PointsLedgerEntry, error) {
	// Start a database transaction for atomicity
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	// 1. Create statement ledger entry (the charge)
	statementEntry := &models.StatementLedgerEntry{
		TenantID:    req.TenantID,
		EntryType:   models.EntryTypeTransaction,
		EntryDate:   req.TransactionDate,
		PostingDate: req.PostingDate,
		Amount:      req.Amount,
		Description: req.Description,
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
	}

	if err := s.statementLedgerService.CreateEntry(ctx, statementEntry); err != nil {
		return nil, nil, fmt.Errorf("failed to create statement entry: %w", err)
	}

	var pointsEntry *models.PointsLedgerEntry

	// 2. Create points ledger entry (if earning points)
	if req.EarnPoints {
		pointsRule := s.defaultPointsRule
		if req.PointsRule != nil {
			pointsRule = *req.PointsRule
		}

		pointsEarned := pointsRule.CalculatePointsEarned(req.Amount)

		if pointsEarned > 0 {
			pointsEntry = &models.PointsLedgerEntry{
				TenantID:          req.TenantID,
				StatementEntryID:  &statementEntry.ID,
				EntryType:         models.PointsEarnedTransaction,
				EntryDate:         req.TransactionDate,
				Points:            pointsEarned,
				Description:       fmt.Sprintf("Points earned from transaction: %s", req.Description),
				TransactionAmount: &req.Amount,
				PointsRate:        &pointsRule.PointsPerDollar,
			}

			if err := s.pointsLedgerService.CreateEntry(ctx, pointsEntry); err != nil {
				return nil, nil, fmt.Errorf("failed to create points entry: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return statementEntry, pointsEntry, nil
}

// PaymentRequest represents a request to record a payment
type PaymentRequest struct {
	TenantID    uuid.UUID
	Amount      decimal.Decimal
	Description string
	ReferenceID string
	PaymentDate time.Time
	PostingDate time.Time
}

// RecordPayment records a payment in the statement ledger
// IMPORTANT: Payments DO NOT affect points balance!
// Points are earned on transactions (spending), not affected by payments
func (s *LedgerReconciliationService) RecordPayment(
	ctx context.Context,
	req PaymentRequest,
) (*models.StatementLedgerEntry, error) {
	statementEntry := &models.StatementLedgerEntry{
		TenantID:    req.TenantID,
		EntryType:   models.EntryTypePayment,
		EntryDate:   req.PaymentDate,
		PostingDate: req.PostingDate,
		Amount:      req.Amount,
		Description: req.Description,
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
	}

	if err := s.statementLedgerService.CreateEntry(ctx, statementEntry); err != nil {
		return nil, fmt.Errorf("failed to create payment entry: %w", err)
	}

	return statementEntry, nil
}

// RefundRequest represents a request to record a refund
type RefundRequest struct {
	TenantID              uuid.UUID
	Amount                decimal.Decimal
	Description           string
	ReferenceID           string
	OriginalTransactionID uuid.UUID // Original statement entry that's being refunded
	RefundDate            time.Time
	PostingDate           time.Time
	AdjustPoints          bool // Whether to adjust points for this refund
}

// RecordRefund records a refund in both ledgers
// Refunds credit the statement balance and may adjust points earned
func (s *LedgerReconciliationService) RecordRefund(
	ctx context.Context,
	req RefundRequest,
) (*models.StatementLedgerEntry, *models.PointsLedgerEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	// 1. Create statement ledger entry (credit)
	statementEntry := &models.StatementLedgerEntry{
		TenantID:    req.TenantID,
		EntryType:   models.EntryTypeRefund,
		EntryDate:   req.RefundDate,
		PostingDate: req.PostingDate,
		Amount:      req.Amount,
		Description: req.Description,
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
	}

	if err := s.statementLedgerService.CreateEntry(ctx, statementEntry); err != nil {
		return nil, nil, fmt.Errorf("failed to create refund entry: %w", err)
	}

	var pointsEntry *models.PointsLedgerEntry

	// 2. Adjust points if applicable
	if req.AdjustPoints {
		// Find original points entry
		var originalPoints int
		var originalPointsRate decimal.Decimal

		queryPoints := `
			SELECT points, COALESCE(points_rate, 0)
			FROM points_ledger_entries
			WHERE statement_entry_id = $1 AND entry_type = 'earned_transaction'
		`

		err := s.db.QueryRowContext(ctx, queryPoints, req.OriginalTransactionID).Scan(&originalPoints, &originalPointsRate)
		if err == nil && originalPoints > 0 {
			// Calculate points to deduct based on refund amount
			pointsToDeduct := int(req.Amount.Mul(originalPointsRate).IntPart())
			if pointsToDeduct > originalPoints {
				pointsToDeduct = originalPoints
			}

			pointsEntry = &models.PointsLedgerEntry{
				TenantID:          req.TenantID,
				StatementEntryID:  &statementEntry.ID,
				EntryType:         models.PointsEarnedRefund,
				EntryDate:         req.RefundDate,
				Points:            -pointsToDeduct, // Negative to deduct points
				Description:       fmt.Sprintf("Points adjustment for refund: %s", req.Description),
				TransactionAmount: &req.Amount,
				PointsRate:        &originalPointsRate,
			}

			if err := s.pointsLedgerService.CreateEntry(ctx, pointsEntry); err != nil {
				return nil, nil, fmt.Errorf("failed to create points adjustment entry: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit refund transaction: %w", err)
	}

	return statementEntry, pointsEntry, nil
}

// RewardRedemptionRequest represents a request to redeem points as statement credit
type RewardRedemptionRequest struct {
	TenantID            uuid.UUID
	PointsToRedeem      int
	CreditAmount        decimal.Decimal // How much credit to apply to statement
	Description         string
	ExternalPlatform    string // e.g., "keystone"
	ExternalReferenceID string
	RedemptionDate      time.Time
	PostingDate         time.Time
}

// RecordRewardRedemption records points redemption and applies credit to statement
// This is a KEY reconciliation point: points ledger -> statement ledger
func (s *LedgerReconciliationService) RecordRewardRedemption(
	ctx context.Context,
	req RewardRedemptionRequest,
) (*models.StatementLedgerEntry, *models.PointsLedgerEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	// 1. Validate tenant has enough points
	if err := s.pointsLedgerService.ValidateRedemption(ctx, req.TenantID, req.PointsToRedeem); err != nil {
		return nil, nil, fmt.Errorf("redemption validation failed: %w", err)
	}

	// 2. Create points ledger entry (redemption/deduction)
	pointsEntry := &models.PointsLedgerEntry{
		TenantID:            req.TenantID,
		EntryType:           models.PointsRedeemedSpent,
		EntryDate:           req.RedemptionDate,
		Points:              -req.PointsToRedeem, // Negative for redemption
		Description:         req.Description,
		ExternalPlatform:    &req.ExternalPlatform,
		ExternalReferenceID: &req.ExternalReferenceID,
	}

	if err := s.pointsLedgerService.CreateEntry(ctx, pointsEntry); err != nil {
		return nil, nil, fmt.Errorf("failed to create points redemption entry: %w", err)
	}

	// 3. Create statement ledger entry (reward credit)
	statementEntry := &models.StatementLedgerEntry{
		TenantID:    req.TenantID,
		EntryType:   models.EntryTypeReward,
		EntryDate:   req.RedemptionDate,
		PostingDate: req.PostingDate,
		Amount:      req.CreditAmount,
		Description: fmt.Sprintf("%s (redeemed %d points)", req.Description, req.PointsToRedeem),
		ReferenceID: &req.ExternalReferenceID,
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"points_redeemed":     req.PointsToRedeem,
			"points_entry_id":     pointsEntry.ID.String(),
			"external_platform":   req.ExternalPlatform,
			"external_reference":  req.ExternalReferenceID,
		},
	}

	if err := s.statementLedgerService.CreateEntry(ctx, statementEntry); err != nil {
		return nil, nil, fmt.Errorf("failed to create reward statement entry: %w", err)
	}

	// 4. Link the two entries
	pointsEntry.StatementEntryID = &statementEntry.ID

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("failed to commit reward redemption: %w", err)
	}

	return statementEntry, pointsEntry, nil
}

// GetReconciliationReport generates a report showing both ledgers for a tenant
type ReconciliationReport struct {
	TenantID              uuid.UUID
	StatementBalance      decimal.Decimal
	PointsBalance         int
	LastStatementActivity time.Time
	LastPointsActivity    time.Time
	ReportGeneratedAt     time.Time
}

// GenerateReconciliationReport creates a report of both ledgers
func (s *LedgerReconciliationService) GenerateReconciliationReport(
	ctx context.Context,
	tenantID uuid.UUID,
) (*ReconciliationReport, error) {
	// Get statement balance
	stmtBalance, err := s.statementLedgerService.GetBalance(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get statement balance: %w", err)
	}

	// Get points balance
	pointsBalance, err := s.pointsLedgerService.GetBalance(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get points balance: %w", err)
	}

	report := &ReconciliationReport{
		TenantID:              tenantID,
		StatementBalance:      stmtBalance.CurrentBalance,
		PointsBalance:         pointsBalance.AvailablePoints,
		LastStatementActivity: stmtBalance.LastActivityDate,
		LastPointsActivity:    pointsBalance.LastActivityDate,
		ReportGeneratedAt:     time.Now(),
	}

	return report, nil
}
