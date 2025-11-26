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

// FeeService handles fee calculation and assessment for credit card accounts
type FeeService struct {
	db                     *sql.DB
	statementLedgerService *StatementLedgerService
}

// NewFeeService creates a new fee service
func NewFeeService(db *sql.DB) *FeeService {
	return &FeeService{
		db:                     db,
		statementLedgerService: NewStatementLedgerService(db),
	}
}

// FeeType represents the type of fee being assessed
type FeeType string

const (
	FeeTypeLatePayment     FeeType = "late_payment"
	FeeTypeFailedPayment   FeeType = "failed_payment"
	FeeTypeInternational   FeeType = "international"
	FeeTypeOverLimit       FeeType = "over_limit"
	FeeTypeAnnual          FeeType = "annual"
	FeeTypeCashAdvance     FeeType = "cash_advance"
)

// FeeAssessmentResult contains the result of assessing a fee
type FeeAssessmentResult struct {
	FeeType         FeeType         `json:"fee_type"`
	FeeAmount       decimal.Decimal `json:"fee_amount"`
	EntryID         uuid.UUID       `json:"entry_id"`
	Description     string          `json:"description"`
	WaivedAmount    decimal.Decimal `json:"waived_amount,omitempty"`
	WaiverReason    string          `json:"waiver_reason,omitempty"`
	AssessedAt      time.Time       `json:"assessed_at"`
}

// FeeWaiverRequest represents a request to waive a fee
type FeeWaiverRequest struct {
	EntryID    uuid.UUID       `json:"entry_id"`
	WaiveAmount decimal.Decimal `json:"waive_amount"` // Can be partial waiver
	Reason      string          `json:"reason"`
	ApprovedBy  string          `json:"approved_by"`
}

// LatePaymentFeeRequest contains parameters for assessing a late payment fee
type LatePaymentFeeRequest struct {
	CreditCard     *models.CreditCard
	BillingCycle   *models.BillingCycle
	CurrentDate    time.Time
	DaysOverdue    int
}

// AssessLatePaymentFee assesses a late payment fee if payment is overdue
func (s *FeeService) AssessLatePaymentFee(
	ctx context.Context,
	req LatePaymentFeeRequest,
) (*FeeAssessmentResult, error) {
	// Check if payment is actually overdue
	if !req.BillingCycle.IsOverdue(req.CurrentDate) {
		return nil, nil // No fee to assess
	}

	// Check if minimum payment was met
	if req.BillingCycle.MinimumPaymentMet {
		return nil, nil // No late fee if minimum was paid
	}

	// Check if we've already assessed a late fee for this cycle
	existing, err := s.hasExistingFee(ctx, req.CreditCard.TenantID, req.BillingCycle.ID, models.EntryTypeFeeLate)
	if err != nil {
		return nil, err
	}
	if existing {
		return nil, nil // Already assessed
	}

	// Get the late payment fee amount from card config
	feeAmount := req.CreditCard.LatePaymentFee

	// Create the fee entry
	entry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		StatementID: &req.BillingCycle.ID,
		EntryType:   models.EntryTypeFeeLate,
		EntryDate:   req.CurrentDate,
		PostingDate: req.CurrentDate,
		Amount:      feeAmount,
		Description: fmt.Sprintf("Late payment fee - payment %d days overdue", req.DaysOverdue),
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"days_overdue":        req.DaysOverdue,
			"billing_cycle_id":    req.BillingCycle.ID.String(),
			"minimum_payment_due": req.BillingCycle.MinimumPayment.String(),
			"due_date":            req.BillingCycle.DueDate.Format(time.RFC3339),
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create late payment fee entry: %w", err)
	}

	return &FeeAssessmentResult{
		FeeType:     FeeTypeLatePayment,
		FeeAmount:   feeAmount,
		EntryID:     entry.ID,
		Description: entry.Description,
		AssessedAt:  time.Now(),
	}, nil
}

// FailedPaymentFeeRequest contains parameters for assessing a failed payment fee
type FailedPaymentFeeRequest struct {
	CreditCard      *models.CreditCard
	PaymentAmount   decimal.Decimal
	PaymentDate     time.Time
	FailureReason   string
	PaymentMethod   string
	ReferenceID     string
}

// AssessFailedPaymentFee assesses a fee for a returned/failed payment
func (s *FeeService) AssessFailedPaymentFee(
	ctx context.Context,
	req FailedPaymentFeeRequest,
) (*FeeAssessmentResult, error) {
	feeAmount := req.CreditCard.FailedPaymentFee

	entry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   models.EntryTypeFeeFailed,
		EntryDate:   req.PaymentDate,
		PostingDate: req.PaymentDate,
		Amount:      feeAmount,
		Description: fmt.Sprintf("Failed/returned payment fee - %s", req.FailureReason),
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"failed_payment_amount": req.PaymentAmount.String(),
			"failure_reason":        req.FailureReason,
			"payment_method":        req.PaymentMethod,
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create failed payment fee entry: %w", err)
	}

	return &FeeAssessmentResult{
		FeeType:     FeeTypeFailedPayment,
		FeeAmount:   feeAmount,
		EntryID:     entry.ID,
		Description: entry.Description,
		AssessedAt:  time.Now(),
	}, nil
}

// InternationalFeeRequest contains parameters for assessing an international transaction fee
type InternationalFeeRequest struct {
	CreditCard          *models.CreditCard
	TransactionAmount   decimal.Decimal
	TransactionCurrency string
	ExchangeRate        decimal.Decimal
	MerchantCountry     string
	TransactionDate     time.Time
	ReferenceID         string
}

// AssessInternationalFee assesses a foreign transaction fee
func (s *FeeService) AssessInternationalFee(
	ctx context.Context,
	req InternationalFeeRequest,
) (*FeeAssessmentResult, error) {
	// Calculate fee as percentage of transaction
	feeAmount := req.CreditCard.CalculateInternationalFee(req.TransactionAmount)

	// Don't assess fee if it rounds to zero
	if feeAmount.LessThanOrEqual(decimal.Zero) {
		return nil, nil
	}

	entry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   models.EntryTypeFeeInternational,
		EntryDate:   req.TransactionDate,
		PostingDate: req.TransactionDate,
		Amount:      feeAmount,
		Description: fmt.Sprintf("Foreign transaction fee (%.2f%%) - %s",
			req.CreditCard.InternationalFeeRate.InexactFloat64(), req.MerchantCountry),
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"transaction_amount":   req.TransactionAmount.String(),
			"transaction_currency": req.TransactionCurrency,
			"exchange_rate":        req.ExchangeRate.String(),
			"merchant_country":     req.MerchantCountry,
			"fee_rate_percent":     req.CreditCard.InternationalFeeRate.String(),
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create international fee entry: %w", err)
	}

	return &FeeAssessmentResult{
		FeeType:     FeeTypeInternational,
		FeeAmount:   feeAmount,
		EntryID:     entry.ID,
		Description: entry.Description,
		AssessedAt:  time.Now(),
	}, nil
}

// OverLimitFeeRequest contains parameters for assessing an over-limit fee
type OverLimitFeeRequest struct {
	CreditCard     *models.CreditCard
	CurrentBalance decimal.Decimal
	TransactionDate time.Time
}

// AssessOverLimitFee assesses a fee when balance exceeds credit limit
func (s *FeeService) AssessOverLimitFee(
	ctx context.Context,
	req OverLimitFeeRequest,
) (*FeeAssessmentResult, error) {
	// Check if actually over limit
	if req.CurrentBalance.LessThanOrEqual(req.CreditCard.CreditLimit) {
		return nil, nil
	}

	// Check if card even has over-limit fee configured
	if req.CreditCard.OverLimitFee.LessThanOrEqual(decimal.Zero) {
		return nil, nil
	}

	// Calculate how much over limit
	overLimitAmount := req.CurrentBalance.Sub(req.CreditCard.CreditLimit)

	entry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   models.EntryTypeFeeOverLimit,
		EntryDate:   req.TransactionDate,
		PostingDate: req.TransactionDate,
		Amount:      req.CreditCard.OverLimitFee,
		Description: fmt.Sprintf("Over credit limit fee - exceeded by $%.2f", overLimitAmount.InexactFloat64()),
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"credit_limit":        req.CreditCard.CreditLimit.String(),
			"current_balance":     req.CurrentBalance.String(),
			"over_limit_amount":   overLimitAmount.String(),
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create over-limit fee entry: %w", err)
	}

	return &FeeAssessmentResult{
		FeeType:     FeeTypeOverLimit,
		FeeAmount:   req.CreditCard.OverLimitFee,
		EntryID:     entry.ID,
		Description: entry.Description,
		AssessedAt:  time.Now(),
	}, nil
}

// AnnualFeeRequest contains parameters for assessing an annual fee
type AnnualFeeRequest struct {
	CreditCard       *models.CreditCard
	AnniversaryDate  time.Time
	BillingCycleID   *uuid.UUID
}

// AssessAnnualFee assesses the yearly membership fee
func (s *FeeService) AssessAnnualFee(
	ctx context.Context,
	req AnnualFeeRequest,
) (*FeeAssessmentResult, error) {
	// Check if card has an annual fee
	if req.CreditCard.AnnualFee.LessThanOrEqual(decimal.Zero) {
		return nil, nil
	}

	// Check if we've already assessed an annual fee recently (within 11 months)
	existing, err := s.hasRecentAnnualFee(ctx, req.CreditCard.TenantID, 11)
	if err != nil {
		return nil, err
	}
	if existing {
		return nil, nil // Already assessed recently
	}

	entry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		StatementID: req.BillingCycleID,
		EntryType:   models.EntryTypeFeeAnnual,
		EntryDate:   req.AnniversaryDate,
		PostingDate: req.AnniversaryDate,
		Amount:      req.CreditCard.AnnualFee,
		Description: "Annual membership fee",
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"anniversary_date":  req.AnniversaryDate.Format("2006-01-02"),
			"credit_card_id":    req.CreditCard.ID.String(),
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create annual fee entry: %w", err)
	}

	return &FeeAssessmentResult{
		FeeType:     FeeTypeAnnual,
		FeeAmount:   req.CreditCard.AnnualFee,
		EntryID:     entry.ID,
		Description: entry.Description,
		AssessedAt:  time.Now(),
	}, nil
}

// CashAdvanceFeeRequest contains parameters for assessing a cash advance fee
type CashAdvanceFeeRequest struct {
	CreditCard        *models.CreditCard
	CashAdvanceAmount decimal.Decimal
	TransactionDate   time.Time
	ATMLocation       string
	ReferenceID       string
}

// AssessCashAdvanceFee assesses a cash advance fee
func (s *FeeService) AssessCashAdvanceFee(
	ctx context.Context,
	req CashAdvanceFeeRequest,
) (*FeeAssessmentResult, error) {
	// Calculate fee - greater of flat fee or percentage
	feeAmount := req.CreditCard.CalculateCashAdvanceFee(req.CashAdvanceAmount)

	entry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    req.CreditCard.TenantID,
		EntryType:   models.EntryTypeFeeCashAdvance,
		EntryDate:   req.TransactionDate,
		PostingDate: req.TransactionDate,
		Amount:      feeAmount,
		Description: fmt.Sprintf("Cash advance fee - $%.2f advance", req.CashAdvanceAmount.InexactFloat64()),
		ReferenceID: &req.ReferenceID,
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"cash_advance_amount": req.CashAdvanceAmount.String(),
			"flat_fee":            req.CreditCard.CashAdvanceFee.String(),
			"fee_rate_percent":    req.CreditCard.CashAdvanceFeeRate.String(),
			"atm_location":        req.ATMLocation,
		},
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create cash advance fee entry: %w", err)
	}

	return &FeeAssessmentResult{
		FeeType:     FeeTypeCashAdvance,
		FeeAmount:   feeAmount,
		EntryID:     entry.ID,
		Description: entry.Description,
		AssessedAt:  time.Now(),
	}, nil
}

// WaiveFee creates a credit entry to offset a previously assessed fee
func (s *FeeService) WaiveFee(
	ctx context.Context,
	req FeeWaiverRequest,
) (*models.StatementLedgerEntry, error) {
	// Get the original fee entry
	originalFee, err := s.getFeeEntry(ctx, req.EntryID)
	if err != nil {
		return nil, fmt.Errorf("failed to get original fee: %w", err)
	}

	// Validate waiver amount
	if req.WaiveAmount.GreaterThan(originalFee.Amount) {
		return nil, fmt.Errorf("waiver amount cannot exceed original fee amount")
	}

	// Create credit entry to offset the fee
	entry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    originalFee.TenantID,
		StatementID: originalFee.StatementID,
		EntryType:   models.EntryTypeCredit,
		EntryDate:   time.Now(),
		PostingDate: time.Now(),
		Amount:      req.WaiveAmount,
		Description: fmt.Sprintf("Fee waiver: %s", req.Reason),
		Status:      models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"original_fee_id":     req.EntryID.String(),
			"original_fee_type":   string(originalFee.EntryType),
			"original_fee_amount": originalFee.Amount.String(),
			"waiver_reason":       req.Reason,
			"approved_by":         req.ApprovedBy,
		},
		CreatedBy: &req.ApprovedBy,
		CreatedAt: time.Now(),
	}

	if err := s.statementLedgerService.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create fee waiver entry: %w", err)
	}

	return entry, nil
}

// GetFeeSummary returns a summary of fees assessed for a tenant
type FeeSummary struct {
	TenantID            uuid.UUID       `json:"tenant_id"`
	Period              string          `json:"period"`
	TotalLatePaymentFees decimal.Decimal `json:"total_late_payment_fees"`
	TotalFailedPaymentFees decimal.Decimal `json:"total_failed_payment_fees"`
	TotalInternationalFees decimal.Decimal `json:"total_international_fees"`
	TotalOverLimitFees   decimal.Decimal `json:"total_over_limit_fees"`
	TotalAnnualFees      decimal.Decimal `json:"total_annual_fees"`
	TotalCashAdvanceFees decimal.Decimal `json:"total_cash_advance_fees"`
	TotalInterestCharges decimal.Decimal `json:"total_interest_charges"`
	GrandTotal           decimal.Decimal `json:"grand_total"`
	FeeCount             int             `json:"fee_count"`
}

// GetFeeSummary retrieves fee summary for a tenant within a date range
func (s *FeeService) GetFeeSummary(
	ctx context.Context,
	tenantID uuid.UUID,
	startDate, endDate time.Time,
) (*FeeSummary, error) {
	query := `
		SELECT
			COUNT(*) as fee_count,
			COALESCE(SUM(CASE WHEN entry_type = 'fee_late' THEN amount ELSE 0 END), 0) as late_fees,
			COALESCE(SUM(CASE WHEN entry_type = 'fee_failed' THEN amount ELSE 0 END), 0) as failed_fees,
			COALESCE(SUM(CASE WHEN entry_type = 'fee_international' THEN amount ELSE 0 END), 0) as intl_fees,
			COALESCE(SUM(CASE WHEN entry_type = 'fee_over_limit' THEN amount ELSE 0 END), 0) as over_limit_fees,
			COALESCE(SUM(CASE WHEN entry_type = 'fee_annual' THEN amount ELSE 0 END), 0) as annual_fees,
			COALESCE(SUM(CASE WHEN entry_type = 'fee_cash_advance' THEN amount ELSE 0 END), 0) as cash_advance_fees,
			COALESCE(SUM(CASE WHEN entry_type = 'fee_interest' THEN amount ELSE 0 END), 0) as interest_charges
		FROM statement_ledger_entries
		WHERE tenant_id = $1
		  AND posting_date >= $2
		  AND posting_date <= $3
		  AND entry_type LIKE 'fee_%'
		  AND status != 'reversed'
	`

	summary := &FeeSummary{
		TenantID: tenantID,
		Period:   fmt.Sprintf("%s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02")),
	}

	err := s.db.QueryRowContext(ctx, query, tenantID, startDate, endDate).Scan(
		&summary.FeeCount,
		&summary.TotalLatePaymentFees,
		&summary.TotalFailedPaymentFees,
		&summary.TotalInternationalFees,
		&summary.TotalOverLimitFees,
		&summary.TotalAnnualFees,
		&summary.TotalCashAdvanceFees,
		&summary.TotalInterestCharges,
	)

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get fee summary: %w", err)
	}

	// Calculate grand total
	summary.GrandTotal = summary.TotalLatePaymentFees.
		Add(summary.TotalFailedPaymentFees).
		Add(summary.TotalInternationalFees).
		Add(summary.TotalOverLimitFees).
		Add(summary.TotalAnnualFees).
		Add(summary.TotalCashAdvanceFees).
		Add(summary.TotalInterestCharges)

	return summary, nil
}

// hasExistingFee checks if a fee of the given type already exists for the cycle
func (s *FeeService) hasExistingFee(
	ctx context.Context,
	tenantID uuid.UUID,
	cycleID uuid.UUID,
	feeType models.StatementEntryType,
) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM statement_ledger_entries
			WHERE tenant_id = $1
			  AND statement_id = $2
			  AND entry_type = $3
			  AND status != 'reversed'
		)
	`

	var exists bool
	err := s.db.QueryRowContext(ctx, query, tenantID, cycleID, feeType).Scan(&exists)
	return exists, err
}

// hasRecentAnnualFee checks if an annual fee was assessed within the specified months
func (s *FeeService) hasRecentAnnualFee(
	ctx context.Context,
	tenantID uuid.UUID,
	withinMonths int,
) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM statement_ledger_entries
			WHERE tenant_id = $1
			  AND entry_type = 'fee_annual'
			  AND status != 'reversed'
			  AND posting_date >= NOW() - INTERVAL '%d months'
		)
	`

	var exists bool
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(query, withinMonths), tenantID).Scan(&exists)
	return exists, err
}

// getFeeEntry retrieves a specific fee entry by ID
func (s *FeeService) getFeeEntry(ctx context.Context, entryID uuid.UUID) (*models.StatementLedgerEntry, error) {
	query := `
		SELECT id, tenant_id, statement_id, entry_type, entry_date, posting_date,
		       amount, description, reference_id, metadata, status, cleared_at,
		       created_at, created_by
		FROM statement_ledger_entries
		WHERE id = $1
	`

	entry := &models.StatementLedgerEntry{}
	err := s.db.QueryRowContext(ctx, query, entryID).Scan(
		&entry.ID,
		&entry.TenantID,
		&entry.StatementID,
		&entry.EntryType,
		&entry.EntryDate,
		&entry.PostingDate,
		&entry.Amount,
		&entry.Description,
		&entry.ReferenceID,
		&entry.Metadata,
		&entry.Status,
		&entry.ClearedAt,
		&entry.CreatedAt,
		&entry.CreatedBy,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("fee entry not found: %s", entryID)
	}
	if err != nil {
		return nil, err
	}

	return entry, nil
}
