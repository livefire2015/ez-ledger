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

// InterestService handles interest calculation and accrual
// Implements GAAP-compliant Average Daily Balance (ADB) method
type InterestService struct {
	db *sql.DB
}

// NewInterestService creates a new interest service
func NewInterestService(db *sql.DB) *InterestService {
	return &InterestService{db: db}
}

// InterestCalculationMethod represents the method used to calculate interest
type InterestCalculationMethod string

const (
	// AverageDailyBalanceMethod - Standard credit card interest calculation
	// Interest = ADB × DPR × Days in billing cycle
	AverageDailyBalanceMethod InterestCalculationMethod = "average_daily_balance"

	// DailyBalanceMethod - Interest calculated on each day's balance
	// Interest = Sum of (Daily Balance × DPR)
	DailyBalanceMethod InterestCalculationMethod = "daily_balance"

	// AdjustedBalanceMethod - Balance after payments (less common)
	// Interest = (Previous Balance - Payments) × Monthly Rate
	AdjustedBalanceMethod InterestCalculationMethod = "adjusted_balance"
)

// InterestCalculationResult contains the results of an interest calculation
type InterestCalculationResult struct {
	CreditCardID         uuid.UUID       `json:"credit_card_id"`
	BillingCycleID       uuid.UUID       `json:"billing_cycle_id"`
	CalculationMethod    InterestCalculationMethod `json:"calculation_method"`
	AverageDailyBalance  decimal.Decimal `json:"average_daily_balance"`
	APRUsed              decimal.Decimal `json:"apr_used"`
	DailyPeriodicRate    decimal.Decimal `json:"daily_periodic_rate"`
	DaysInCycle          int             `json:"days_in_cycle"`
	InterestCharge       decimal.Decimal `json:"interest_charge"`
	MinimumInterestCharge decimal.Decimal `json:"minimum_interest_charge"`
	WaivedDueToGracePeriod bool          `json:"waived_due_to_grace_period"`
	CalculatedAt         time.Time       `json:"calculated_at"`
}

// InterestConfig holds configuration for interest calculation
type InterestConfig struct {
	Method                InterestCalculationMethod
	MinimumInterestCharge decimal.Decimal // Minimum interest to charge (e.g., $0.50)
	RoundingPrecision     int32           // Decimal places for rounding
	CompoundDaily         bool            // Whether to compound interest daily
}

// DefaultInterestConfig returns standard interest configuration
func DefaultInterestConfig() InterestConfig {
	return InterestConfig{
		Method:                AverageDailyBalanceMethod,
		MinimumInterestCharge: decimal.NewFromFloat(0.50),
		RoundingPrecision:     2,
		CompoundDaily:         false,
	}
}

// CalculateInterest calculates interest for a billing cycle
// Uses the Average Daily Balance method per GAAP standards
func (s *InterestService) CalculateInterest(
	ctx context.Context,
	card *models.CreditCard,
	cycle *models.BillingCycle,
	config InterestConfig,
) (*InterestCalculationResult, error) {
	result := &InterestCalculationResult{
		CreditCardID:      card.ID,
		BillingCycleID:    cycle.ID,
		CalculationMethod: config.Method,
		CalculatedAt:      time.Now(),
	}

	// Get effective APR for this cycle
	apr := card.GetEffectiveAPR(cycle.CycleStartDate)
	result.APRUsed = apr

	// Calculate Daily Periodic Rate (DPR)
	// DPR = APR / 365
	dpr := card.GetDailyPeriodicRate(apr)
	result.DailyPeriodicRate = dpr

	// Calculate days in billing cycle
	daysInCycle := int(cycle.CycleEndDate.Sub(cycle.CycleStartDate).Hours() / 24) + 1
	result.DaysInCycle = daysInCycle

	// Get daily balances for the billing cycle
	dailyBalances, err := s.getDailyBalances(ctx, card.ID, cycle.CycleStartDate, cycle.CycleEndDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get daily balances: %w", err)
	}

	// Calculate Average Daily Balance
	adb := models.CalculateAverageDailyBalance(dailyBalances)
	result.AverageDailyBalance = adb

	// Check if grace period applies (paid previous balance in full)
	if s.qualifiesForGracePeriod(ctx, card, cycle) {
		result.WaivedDueToGracePeriod = true
		result.InterestCharge = decimal.Zero
		return result, nil
	}

	// Calculate interest based on method
	var interestCharge decimal.Decimal
	switch config.Method {
	case AverageDailyBalanceMethod:
		interestCharge = s.calculateADBInterest(adb, dpr, daysInCycle)
	case DailyBalanceMethod:
		interestCharge = s.calculateDailyBalanceInterest(dailyBalances, dpr)
	case AdjustedBalanceMethod:
		interestCharge = s.calculateAdjustedBalanceInterest(cycle.PreviousBalance, cycle.PaymentsReceived, apr)
	default:
		interestCharge = s.calculateADBInterest(adb, dpr, daysInCycle)
	}

	// Apply minimum interest charge if applicable
	result.MinimumInterestCharge = config.MinimumInterestCharge
	if interestCharge.GreaterThan(decimal.Zero) && interestCharge.LessThan(config.MinimumInterestCharge) {
		interestCharge = config.MinimumInterestCharge
	}

	// Round to specified precision
	result.InterestCharge = interestCharge.Round(config.RoundingPrecision)

	return result, nil
}

// calculateADBInterest calculates interest using Average Daily Balance method
// Formula: ADB × DPR × Days in cycle
func (s *InterestService) calculateADBInterest(adb, dpr decimal.Decimal, daysInCycle int) decimal.Decimal {
	if adb.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	days := decimal.NewFromInt(int64(daysInCycle))
	return adb.Mul(dpr).Mul(days).Round(2)
}

// calculateDailyBalanceInterest calculates interest daily and sums
// Formula: Sum of (Daily Balance × DPR)
func (s *InterestService) calculateDailyBalanceInterest(balances []models.DailyBalanceRecord, dpr decimal.Decimal) decimal.Decimal {
	var totalInterest decimal.Decimal
	for _, record := range balances {
		if record.Balance.GreaterThan(decimal.Zero) {
			dailyInterest := record.Balance.Mul(dpr)
			totalInterest = totalInterest.Add(dailyInterest)
		}
	}
	return totalInterest.Round(2)
}

// calculateAdjustedBalanceInterest calculates using adjusted balance method
// Formula: (Previous Balance - Payments) × Monthly Rate
func (s *InterestService) calculateAdjustedBalanceInterest(previousBalance, payments, apr decimal.Decimal) decimal.Decimal {
	adjustedBalance := previousBalance.Sub(payments)
	if adjustedBalance.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	// Convert APR to monthly rate
	monthlyRate := apr.Div(decimal.NewFromInt(12)).Div(decimal.NewFromInt(100))
	return adjustedBalance.Mul(monthlyRate).Round(2)
}

// getDailyBalances retrieves daily balance snapshots for interest calculation
func (s *InterestService) getDailyBalances(
	ctx context.Context,
	creditCardID uuid.UUID,
	startDate, endDate time.Time,
) ([]models.DailyBalanceRecord, error) {
	query := `
		WITH RECURSIVE dates AS (
			SELECT $2::date as date
			UNION ALL
			SELECT date + interval '1 day'
			FROM dates
			WHERE date < $3::date
		),
		daily_entries AS (
			SELECT
				d.date,
				COALESCE(
					SUM(
						CASE
							WHEN sle.entry_type IN ('transaction', 'fee_late', 'fee_failed', 'fee_international', 'returned_reward')
								THEN sle.amount
							WHEN sle.entry_type IN ('payment', 'refund', 'reward', 'credit')
								THEN -sle.amount
							WHEN sle.entry_type = 'adjustment'
								THEN sle.amount
							ELSE 0
						END
					) OVER (ORDER BY d.date ROWS UNBOUNDED PRECEDING),
					0
				) as running_balance
			FROM dates d
			LEFT JOIN statement_ledger_entries sle ON
				sle.posting_date <= d.date
				AND EXISTS (
					SELECT 1 FROM credit_cards cc
					WHERE cc.id = $1 AND cc.tenant_id = sle.tenant_id
				)
				AND sle.status = 'cleared'
		)
		SELECT DISTINCT ON (date) date, running_balance
		FROM daily_entries
		ORDER BY date, running_balance DESC
	`

	rows, err := s.db.QueryContext(ctx, query, creditCardID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []models.DailyBalanceRecord
	for rows.Next() {
		var record models.DailyBalanceRecord
		if err := rows.Scan(&record.Date, &record.Balance); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

// qualifiesForGracePeriod checks if the cardholder qualifies for grace period
// Grace period applies if previous statement balance was paid in full before due date
func (s *InterestService) qualifiesForGracePeriod(
	ctx context.Context,
	card *models.CreditCard,
	currentCycle *models.BillingCycle,
) bool {
	// Get previous billing cycle
	query := `
		SELECT new_balance, payments_made
		FROM billing_cycles
		WHERE credit_card_id = $1
		  AND cycle_number = $2
		  AND status IN ('paid_full')
		LIMIT 1
	`

	var prevBalance, prevPayments decimal.Decimal
	err := s.db.QueryRowContext(ctx, query, card.ID, currentCycle.CycleNumber-1).Scan(&prevBalance, &prevPayments)
	if err == sql.ErrNoRows {
		// No previous cycle, grace period applies for new accounts
		return true
	}
	if err != nil {
		return false
	}

	// Grace period applies if previous balance was paid in full
	return prevPayments.GreaterThanOrEqual(prevBalance)
}

// AccrueInterest creates an interest charge entry in the statement ledger
func (s *InterestService) AccrueInterest(
	ctx context.Context,
	tenantID uuid.UUID,
	cycle *models.BillingCycle,
	result *InterestCalculationResult,
) (*models.StatementLedgerEntry, error) {
	if result.InterestCharge.LessThanOrEqual(decimal.Zero) {
		return nil, nil // No interest to accrue
	}

	entry := &models.StatementLedgerEntry{
		ID:          uuid.New(),
		TenantID:    tenantID,
		StatementID: &cycle.ID,
		EntryType:   models.EntryTypeFeeInterest,
		EntryDate:   time.Now(),
		PostingDate: cycle.CycleEndDate,
		Amount:      result.InterestCharge,
		Description: fmt.Sprintf("Interest charge (APR: %.2f%%, ADB: $%.2f)",
			result.APRUsed.InexactFloat64(),
			result.AverageDailyBalance.InexactFloat64()),
		Status: models.EntryStatusPending,
		Metadata: map[string]interface{}{
			"calculation_method":     string(result.CalculationMethod),
			"apr_used":               result.APRUsed.String(),
			"daily_periodic_rate":    result.DailyPeriodicRate.String(),
			"average_daily_balance":  result.AverageDailyBalance.String(),
			"days_in_cycle":          result.DaysInCycle,
			"billing_cycle_id":       cycle.ID.String(),
		},
		CreatedAt: time.Now(),
	}

	statementService := NewStatementLedgerService(s.db)
	if err := statementService.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to create interest entry: %w", err)
	}

	return entry, nil
}

// ProjectedInterest calculates projected interest if balance remains unchanged
// Useful for "what-if" scenarios shown to customers
type ProjectedInterest struct {
	CurrentBalance        decimal.Decimal `json:"current_balance"`
	ProjectedDays         int             `json:"projected_days"`
	APR                   decimal.Decimal `json:"apr"`
	ProjectedInterest     decimal.Decimal `json:"projected_interest"`
	TotalIfUnpaid         decimal.Decimal `json:"total_if_unpaid"`
}

// CalculateProjectedInterest estimates future interest charges
func (s *InterestService) CalculateProjectedInterest(
	currentBalance decimal.Decimal,
	apr decimal.Decimal,
	days int,
) *ProjectedInterest {
	if currentBalance.LessThanOrEqual(decimal.Zero) {
		return &ProjectedInterest{
			CurrentBalance:    currentBalance,
			ProjectedDays:     days,
			APR:               apr,
			ProjectedInterest: decimal.Zero,
			TotalIfUnpaid:     currentBalance,
		}
	}

	// DPR = APR / 365 / 100
	dpr := apr.Div(decimal.NewFromInt(365)).Div(decimal.NewFromInt(100))

	// Simple interest projection: Balance × DPR × Days
	projectedInterest := currentBalance.Mul(dpr).Mul(decimal.NewFromInt(int64(days))).Round(2)

	return &ProjectedInterest{
		CurrentBalance:    currentBalance,
		ProjectedDays:     days,
		APR:               apr,
		ProjectedInterest: projectedInterest,
		TotalIfUnpaid:     currentBalance.Add(projectedInterest),
	}
}

// InterestAccrualSchedule represents scheduled interest accrual
type InterestAccrualSchedule struct {
	CreditCardID  uuid.UUID `json:"credit_card_id"`
	NextAccrual   time.Time `json:"next_accrual"`
	AccrualMethod InterestCalculationMethod `json:"accrual_method"`
	CurrentAPR    decimal.Decimal `json:"current_apr"`
}

// GetAccrualSchedules returns interest accrual schedules for all active cards
func (s *InterestService) GetAccrualSchedules(ctx context.Context) ([]InterestAccrualSchedule, error) {
	query := `
		SELECT
			cc.id,
			COALESCE(bc.cycle_end_date, cc.next_statement_date) as next_accrual,
			cc.purchase_apr as current_apr
		FROM credit_cards cc
		LEFT JOIN billing_cycles bc ON bc.credit_card_id = cc.id AND bc.status = 'open'
		WHERE cc.status = 'active'
		ORDER BY next_accrual
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []InterestAccrualSchedule
	for rows.Next() {
		var schedule InterestAccrualSchedule
		schedule.AccrualMethod = AverageDailyBalanceMethod
		if err := rows.Scan(&schedule.CreditCardID, &schedule.NextAccrual, &schedule.CurrentAPR); err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}

	return schedules, rows.Err()
}
