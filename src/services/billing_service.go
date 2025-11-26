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

// BillingService handles billing cycle management and statement generation
type BillingService struct {
	db                     *sql.DB
	creditCardService      *CreditCardService
	statementLedgerService *StatementLedgerService
	interestService        *InterestService
	feeService             *FeeService
	cashbackService        *CashbackService
}

// NewBillingService creates a new billing service
func NewBillingService(db *sql.DB) *BillingService {
	return &BillingService{
		db:                     db,
		creditCardService:      NewCreditCardService(db),
		statementLedgerService: NewStatementLedgerService(db),
		interestService:        NewInterestService(db),
		feeService:             NewFeeService(db),
		cashbackService:        NewCashbackService(db),
	}
}

// GenerateStatementRequest contains parameters for generating a billing statement
type GenerateStatementRequest struct {
	CreditCard *models.CreditCard
	CycleEnd   time.Time // End of the billing period
}

// StatementGenerationResult contains the result of generating a statement
type StatementGenerationResult struct {
	BillingCycle     *models.BillingCycle
	InterestResult   *InterestCalculationResult
	FeeSummary       *FeeSummary
	CashbackStatement *models.CashbackStatement
	StatementPDF     []byte // Optional PDF representation
}

// GenerateStatement generates a billing statement for a credit card
func (s *BillingService) GenerateStatement(
	ctx context.Context,
	req GenerateStatementRequest,
) (*StatementGenerationResult, error) {
	result := &StatementGenerationResult{}

	// Get the previous billing cycle to determine previous balance
	previousCycle, err := s.getPreviousCycle(ctx, req.CreditCard.ID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get previous cycle: %w", err)
	}

	var previousBalance decimal.Decimal
	if previousCycle != nil {
		previousBalance = previousCycle.NewBalance.Sub(previousCycle.PaymentsMade)
	}

	// Determine billing period dates
	startDate := req.CreditCard.LastStatementDate
	if startDate == nil {
		// First statement - use card creation date
		startDate = &req.CreditCard.CreatedAt
	}

	cycleNumber := 1
	if previousCycle != nil {
		cycleNumber = previousCycle.CycleNumber + 1
	}

	// Calculate due date and grace period
	dueDate := req.CycleEnd.AddDate(0, 0, req.CreditCard.PaymentDueDays)
	gracePeriodEnd := req.CycleEnd.AddDate(0, 0, req.CreditCard.GracePeriodDays)

	// Get the effective APR
	apr := req.CreditCard.GetEffectiveAPR(req.CycleEnd)

	// Build the billing cycle
	cycle := models.NewBillingCycleBuilder().
		WithCreditCard(req.CreditCard).
		WithCycleNumber(cycleNumber).
		WithDateRange(*startDate, req.CycleEnd, dueDate, gracePeriodEnd).
		WithPreviousBalance(previousBalance).
		WithAPR(apr).
		Build()

	// Get all transactions for this billing period
	if err := s.populateCycleAmounts(ctx, cycle, req.CreditCard.TenantID); err != nil {
		return nil, fmt.Errorf("failed to populate cycle amounts: %w", err)
	}

	// Calculate interest if applicable
	interestConfig := DefaultInterestConfig()
	interestResult, err := s.interestService.CalculateInterest(ctx, req.CreditCard, cycle, interestConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate interest: %w", err)
	}
	result.InterestResult = interestResult

	// Add interest to cycle if applicable
	if interestResult != nil && !interestResult.WaivedDueToGracePeriod {
		cycle.InterestAmount = interestResult.InterestCharge

		// Create interest entry
		_, err := s.interestService.AccrueInterest(ctx, req.CreditCard.TenantID, cycle, interestResult)
		if err != nil {
			return nil, fmt.Errorf("failed to accrue interest: %w", err)
		}
	}

	// Calculate new balance
	cycle.NewBalance = cycle.CalculateNewBalance()

	// Calculate minimum payment
	cycle.MinimumPayment = req.CreditCard.CalculateMinimumPayment(cycle.NewBalance)

	// Calculate average daily balance
	cycle.AverageDailyBalance = interestResult.AverageDailyBalance

	// Set statement date
	cycle.StatementDate = time.Now()

	// Save the billing cycle
	if err := s.saveBillingCycle(ctx, cycle); err != nil {
		return nil, fmt.Errorf("failed to save billing cycle: %w", err)
	}
	result.BillingCycle = cycle

	// Get fee summary
	feeSummary, err := s.feeService.GetFeeSummary(ctx, req.CreditCard.TenantID, *startDate, req.CycleEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to get fee summary: %w", err)
	}
	result.FeeSummary = feeSummary

	// Get cashback statement
	cashbackStatement, err := s.cashbackService.GetCashbackStatement(ctx, req.CreditCard.ID, *startDate, req.CycleEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to get cashback statement: %w", err)
	}
	result.CashbackStatement = cashbackStatement

	// Update credit card with new statement date
	if err := s.updateCardStatementDates(ctx, req.CreditCard.ID, req.CycleEnd); err != nil {
		return nil, fmt.Errorf("failed to update card statement dates: %w", err)
	}

	// Close the billing cycle
	now := time.Now()
	cycle.Status = models.BillingCycleStatusClosed
	cycle.ClosedAt = &now
	if err := s.updateBillingCycleStatus(ctx, cycle.ID, models.BillingCycleStatusClosed); err != nil {
		return nil, fmt.Errorf("failed to close billing cycle: %w", err)
	}

	return result, nil
}

// ProcessPaymentTowardsBillingCycle applies a payment to a billing cycle
func (s *BillingService) ProcessPaymentTowardsBillingCycle(
	ctx context.Context,
	cycleID uuid.UUID,
	paymentAmount decimal.Decimal,
	paymentDate time.Time,
) error {
	// Get the billing cycle
	cycle, err := s.getBillingCycle(ctx, cycleID)
	if err != nil {
		return fmt.Errorf("failed to get billing cycle: %w", err)
	}

	// Add payment to cycle
	newPaymentsMade := cycle.PaymentsMade.Add(paymentAmount)

	// Check if minimum payment is now met
	minimumPaymentMet := newPaymentsMade.GreaterThanOrEqual(cycle.MinimumPayment)

	// Determine new status
	var newStatus models.BillingCycleStatus
	if newPaymentsMade.GreaterThanOrEqual(cycle.NewBalance) {
		newStatus = models.BillingCycleStatusPaidFull
	} else if minimumPaymentMet {
		newStatus = models.BillingCycleStatusPaid
	} else {
		newStatus = cycle.Status // Keep current status
	}

	// Update the cycle
	query := `
		UPDATE billing_cycles
		SET payments_made = $1,
		    last_payment_date = $2,
		    last_payment_amount = $3,
		    minimum_payment_met = $4,
		    status = $5,
		    updated_at = $6
		WHERE id = $7
	`

	_, err = s.db.ExecContext(ctx, query,
		newPaymentsMade,
		paymentDate,
		paymentAmount,
		minimumPaymentMet,
		newStatus,
		time.Now(),
		cycleID,
	)

	return err
}

// CheckAndAssessLatePaymentFees checks all overdue cycles and assesses late fees
func (s *BillingService) CheckAndAssessLatePaymentFees(ctx context.Context) ([]*FeeAssessmentResult, error) {
	currentDate := time.Now()

	// Find all cycles that are past due and haven't had minimum payment
	query := `
		SELECT bc.id, bc.credit_card_id
		FROM billing_cycles bc
		JOIN credit_cards cc ON cc.id = bc.credit_card_id
		WHERE bc.status = 'closed'
		  AND bc.minimum_payment_met = false
		  AND bc.due_date < $1
		  AND NOT EXISTS (
		      SELECT 1 FROM statement_ledger_entries sle
		      WHERE sle.statement_id = bc.id
		        AND sle.entry_type = 'fee_late'
		        AND sle.status != 'reversed'
		  )
	`

	rows, err := s.db.QueryContext(ctx, query, currentDate)
	if err != nil {
		return nil, fmt.Errorf("failed to find overdue cycles: %w", err)
	}
	defer rows.Close()

	var results []*FeeAssessmentResult
	for rows.Next() {
		var cycleID, cardID uuid.UUID
		if err := rows.Scan(&cycleID, &cardID); err != nil {
			continue
		}

		// Get the credit card and cycle
		card, err := s.creditCardService.GetCreditCard(ctx, cardID)
		if err != nil {
			continue
		}

		cycle, err := s.getBillingCycle(ctx, cycleID)
		if err != nil {
			continue
		}

		// Assess late fee
		feeResult, err := s.feeService.AssessLatePaymentFee(ctx, LatePaymentFeeRequest{
			CreditCard:   card,
			BillingCycle: cycle,
			CurrentDate:  currentDate,
			DaysOverdue:  cycle.DaysOverdue(currentDate),
		})
		if err != nil {
			continue
		}

		if feeResult != nil {
			results = append(results, feeResult)

			// Update cycle status to past due
			s.updateBillingCycleStatus(ctx, cycleID, models.BillingCycleStatusPastDue)

			// Increment consecutive late count
			s.incrementLateCounts(ctx, cardID)
		}
	}

	return results, rows.Err()
}

// GetCurrentBillingCycle returns the current open billing cycle for a card
func (s *BillingService) GetCurrentBillingCycle(
	ctx context.Context,
	creditCardID uuid.UUID,
) (*models.BillingCycle, error) {
	query := `
		SELECT id, credit_card_id, tenant_id, cycle_number, cycle_type,
		       cycle_start_date, cycle_end_date, statement_date, due_date, grace_period_end,
		       previous_balance, payments_received, purchases_amount, cash_advances_amount,
		       refunds_amount, fees_amount, interest_amount, adjustments_amount,
		       cashback_earned, cashback_redeemed, new_balance, minimum_payment,
		       average_daily_balance, days_in_cycle, apr_applied,
		       payments_made, last_payment_date, last_payment_amount, minimum_payment_met,
		       status, created_at, updated_at, closed_at
		FROM billing_cycles
		WHERE credit_card_id = $1 AND status = 'open'
		ORDER BY cycle_number DESC
		LIMIT 1
	`

	cycle := &models.BillingCycle{}
	err := s.db.QueryRowContext(ctx, query, creditCardID).Scan(
		&cycle.ID, &cycle.CreditCardID, &cycle.TenantID, &cycle.CycleNumber, &cycle.CycleType,
		&cycle.CycleStartDate, &cycle.CycleEndDate, &cycle.StatementDate, &cycle.DueDate, &cycle.GracePeriodEnd,
		&cycle.PreviousBalance, &cycle.PaymentsReceived, &cycle.PurchasesAmount, &cycle.CashAdvancesAmount,
		&cycle.RefundsAmount, &cycle.FeesAmount, &cycle.InterestAmount, &cycle.AdjustmentsAmount,
		&cycle.CashbackEarned, &cycle.CashbackRedeemed, &cycle.NewBalance, &cycle.MinimumPayment,
		&cycle.AverageDailyBalance, &cycle.DaysInCycle, &cycle.APRApplied,
		&cycle.PaymentsMade, &cycle.LastPaymentDate, &cycle.LastPaymentAmount, &cycle.MinimumPaymentMet,
		&cycle.Status, &cycle.CreatedAt, &cycle.UpdatedAt, &cycle.ClosedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No current cycle
	}
	return cycle, err
}

// GetBillingHistory returns billing cycle history for a card
func (s *BillingService) GetBillingHistory(
	ctx context.Context,
	creditCardID uuid.UUID,
	limit int,
) ([]*models.BillingCycleSummary, error) {
	query := `
		SELECT id, cycle_number, cycle_type, cycle_start_date, cycle_end_date,
		       due_date, new_balance, minimum_payment, payments_made, status
		FROM billing_cycles
		WHERE credit_card_id = $1
		ORDER BY cycle_number DESC
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, creditCardID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	currentDate := time.Now()
	var summaries []*models.BillingCycleSummary

	for rows.Next() {
		cycle := &models.BillingCycle{}
		err := rows.Scan(
			&cycle.ID, &cycle.CycleNumber, &cycle.CycleType, &cycle.CycleStartDate, &cycle.CycleEndDate,
			&cycle.DueDate, &cycle.NewBalance, &cycle.MinimumPayment, &cycle.PaymentsMade, &cycle.Status,
		)
		if err != nil {
			return nil, err
		}

		summary := cycle.ToSummary(currentDate)
		summaries = append(summaries, &summary)
	}

	return summaries, rows.Err()
}

// StartNewBillingCycle creates a new billing cycle for a card
func (s *BillingService) StartNewBillingCycle(
	ctx context.Context,
	creditCard *models.CreditCard,
) (*models.BillingCycle, error) {
	// Get the previous cycle
	previousCycle, err := s.getPreviousCycle(ctx, creditCard.ID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get previous cycle: %w", err)
	}

	var previousBalance decimal.Decimal
	var cycleNumber int = 1
	var startDate time.Time

	if previousCycle != nil {
		previousBalance = previousCycle.GetRemainingBalance()
		cycleNumber = previousCycle.CycleNumber + 1
		startDate = previousCycle.CycleEndDate.AddDate(0, 0, 1)
	} else {
		startDate = creditCard.CreatedAt
	}

	// Calculate end date based on billing cycle type
	var endDate time.Time
	switch creditCard.BillingCycleType {
	case models.BillingCycleQuarterly:
		endDate = startDate.AddDate(0, 3, -1)
	default:
		endDate = startDate.AddDate(0, 1, -1)
	}

	dueDate := endDate.AddDate(0, 0, creditCard.PaymentDueDays)
	gracePeriodEnd := endDate.AddDate(0, 0, creditCard.GracePeriodDays)

	cycle := models.NewBillingCycleBuilder().
		WithCreditCard(creditCard).
		WithCycleNumber(cycleNumber).
		WithDateRange(startDate, endDate, dueDate, gracePeriodEnd).
		WithPreviousBalance(previousBalance).
		WithAPR(creditCard.GetEffectiveAPR(startDate)).
		Build()

	if err := s.saveBillingCycle(ctx, cycle); err != nil {
		return nil, fmt.Errorf("failed to save new billing cycle: %w", err)
	}

	return cycle, nil
}

// populateCycleAmounts aggregates transaction amounts for a billing period
func (s *BillingService) populateCycleAmounts(
	ctx context.Context,
	cycle *models.BillingCycle,
	tenantID uuid.UUID,
) error {
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN entry_type = 'transaction' THEN amount ELSE 0 END), 0) as purchases,
			COALESCE(SUM(CASE WHEN entry_type = 'cash_advance' THEN amount ELSE 0 END), 0) as cash_advances,
			COALESCE(SUM(CASE WHEN entry_type = 'refund' THEN amount ELSE 0 END), 0) as refunds,
			COALESCE(SUM(CASE WHEN entry_type = 'payment' THEN amount ELSE 0 END), 0) as payments,
			COALESCE(SUM(CASE WHEN entry_type LIKE 'fee_%' THEN amount ELSE 0 END), 0) as fees,
			COALESCE(SUM(CASE WHEN entry_type IN ('adjustment', 'credit') THEN
				CASE WHEN entry_type = 'credit' THEN -amount ELSE amount END
			ELSE 0 END), 0) as adjustments,
			COALESCE(SUM(CASE WHEN entry_type = 'cashback_earned' THEN amount ELSE 0 END), 0) as cashback_earned,
			COALESCE(SUM(CASE WHEN entry_type = 'cashback_redeemed' THEN amount ELSE 0 END), 0) as cashback_redeemed
		FROM statement_ledger_entries
		WHERE tenant_id = $1
		  AND posting_date >= $2
		  AND posting_date <= $3
		  AND status IN ('pending', 'cleared')
	`

	err := s.db.QueryRowContext(ctx, query, tenantID, cycle.CycleStartDate, cycle.CycleEndDate).Scan(
		&cycle.PurchasesAmount,
		&cycle.CashAdvancesAmount,
		&cycle.RefundsAmount,
		&cycle.PaymentsReceived,
		&cycle.FeesAmount,
		&cycle.AdjustmentsAmount,
		&cycle.CashbackEarned,
		&cycle.CashbackRedeemed,
	)

	return err
}

// saveBillingCycle saves a billing cycle to the database
func (s *BillingService) saveBillingCycle(ctx context.Context, cycle *models.BillingCycle) error {
	query := `
		INSERT INTO billing_cycles (
			id, credit_card_id, tenant_id, cycle_number, cycle_type,
			cycle_start_date, cycle_end_date, statement_date, due_date, grace_period_end,
			previous_balance, payments_received, purchases_amount, cash_advances_amount,
			refunds_amount, fees_amount, interest_amount, adjustments_amount,
			cashback_earned, cashback_redeemed, new_balance, minimum_payment,
			average_daily_balance, days_in_cycle, apr_applied,
			payments_made, minimum_payment_met, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30
		)
	`

	_, err := s.db.ExecContext(ctx, query,
		cycle.ID, cycle.CreditCardID, cycle.TenantID, cycle.CycleNumber, cycle.CycleType,
		cycle.CycleStartDate, cycle.CycleEndDate, cycle.StatementDate, cycle.DueDate, cycle.GracePeriodEnd,
		cycle.PreviousBalance, cycle.PaymentsReceived, cycle.PurchasesAmount, cycle.CashAdvancesAmount,
		cycle.RefundsAmount, cycle.FeesAmount, cycle.InterestAmount, cycle.AdjustmentsAmount,
		cycle.CashbackEarned, cycle.CashbackRedeemed, cycle.NewBalance, cycle.MinimumPayment,
		cycle.AverageDailyBalance, cycle.DaysInCycle, cycle.APRApplied,
		cycle.PaymentsMade, cycle.MinimumPaymentMet, cycle.Status, cycle.CreatedAt, cycle.UpdatedAt,
	)

	return err
}

// getBillingCycle retrieves a billing cycle by ID
func (s *BillingService) getBillingCycle(ctx context.Context, cycleID uuid.UUID) (*models.BillingCycle, error) {
	query := `
		SELECT id, credit_card_id, tenant_id, cycle_number, cycle_type,
		       cycle_start_date, cycle_end_date, statement_date, due_date, grace_period_end,
		       previous_balance, payments_received, purchases_amount, cash_advances_amount,
		       refunds_amount, fees_amount, interest_amount, adjustments_amount,
		       cashback_earned, cashback_redeemed, new_balance, minimum_payment,
		       average_daily_balance, days_in_cycle, apr_applied,
		       payments_made, last_payment_date, last_payment_amount, minimum_payment_met,
		       status, created_at, updated_at, closed_at
		FROM billing_cycles
		WHERE id = $1
	`

	cycle := &models.BillingCycle{}
	err := s.db.QueryRowContext(ctx, query, cycleID).Scan(
		&cycle.ID, &cycle.CreditCardID, &cycle.TenantID, &cycle.CycleNumber, &cycle.CycleType,
		&cycle.CycleStartDate, &cycle.CycleEndDate, &cycle.StatementDate, &cycle.DueDate, &cycle.GracePeriodEnd,
		&cycle.PreviousBalance, &cycle.PaymentsReceived, &cycle.PurchasesAmount, &cycle.CashAdvancesAmount,
		&cycle.RefundsAmount, &cycle.FeesAmount, &cycle.InterestAmount, &cycle.AdjustmentsAmount,
		&cycle.CashbackEarned, &cycle.CashbackRedeemed, &cycle.NewBalance, &cycle.MinimumPayment,
		&cycle.AverageDailyBalance, &cycle.DaysInCycle, &cycle.APRApplied,
		&cycle.PaymentsMade, &cycle.LastPaymentDate, &cycle.LastPaymentAmount, &cycle.MinimumPaymentMet,
		&cycle.Status, &cycle.CreatedAt, &cycle.UpdatedAt, &cycle.ClosedAt,
	)

	return cycle, err
}

// getPreviousCycle retrieves the most recent billing cycle for a card
func (s *BillingService) getPreviousCycle(ctx context.Context, creditCardID uuid.UUID) (*models.BillingCycle, error) {
	query := `
		SELECT id FROM billing_cycles
		WHERE credit_card_id = $1
		ORDER BY cycle_number DESC
		LIMIT 1
	`

	var cycleID uuid.UUID
	err := s.db.QueryRowContext(ctx, query, creditCardID).Scan(&cycleID)
	if err != nil {
		return nil, err
	}

	return s.getBillingCycle(ctx, cycleID)
}

// updateBillingCycleStatus updates the status of a billing cycle
func (s *BillingService) updateBillingCycleStatus(
	ctx context.Context,
	cycleID uuid.UUID,
	status models.BillingCycleStatus,
) error {
	query := `UPDATE billing_cycles SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := s.db.ExecContext(ctx, query, status, time.Now(), cycleID)
	return err
}

// updateCardStatementDates updates the statement dates on the credit card
func (s *BillingService) updateCardStatementDates(
	ctx context.Context,
	cardID uuid.UUID,
	lastStatementDate time.Time,
) error {
	// Calculate next statement date
	nextStatement := lastStatementDate.AddDate(0, 1, 0) // Monthly default

	query := `
		UPDATE credit_cards
		SET last_statement_date = $1, next_statement_date = $2, updated_at = $3
		WHERE id = $4
	`

	_, err := s.db.ExecContext(ctx, query, lastStatementDate, nextStatement, time.Now(), cardID)
	return err
}

// incrementLateCounts increments the consecutive late payment count
func (s *BillingService) incrementLateCounts(ctx context.Context, cardID uuid.UUID) error {
	query := `
		UPDATE credit_cards
		SET consecutive_late_count = consecutive_late_count + 1, updated_at = $1
		WHERE id = $2
	`
	_, err := s.db.ExecContext(ctx, query, time.Now(), cardID)
	return err
}

// GetUpcomingStatementDates returns cards that have statements due soon
func (s *BillingService) GetUpcomingStatementDates(
	ctx context.Context,
	withinDays int,
) ([]UpcomingStatement, error) {
	query := `
		SELECT cc.id, cc.tenant_id, cc.cardholder_name, cc.next_statement_date
		FROM credit_cards cc
		WHERE cc.status = 'active'
		  AND cc.next_statement_date <= $1
		  AND cc.next_statement_date >= CURRENT_DATE
		ORDER BY cc.next_statement_date
	`

	futureDate := time.Now().AddDate(0, 0, withinDays)
	rows, err := s.db.QueryContext(ctx, query, futureDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var upcoming []UpcomingStatement
	for rows.Next() {
		var stmt UpcomingStatement
		if err := rows.Scan(&stmt.CreditCardID, &stmt.TenantID, &stmt.CardholderName, &stmt.StatementDate); err != nil {
			return nil, err
		}
		upcoming = append(upcoming, stmt)
	}

	return upcoming, rows.Err()
}

// UpcomingStatement represents an upcoming billing statement
type UpcomingStatement struct {
	CreditCardID   uuid.UUID `json:"credit_card_id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	CardholderName string    `json:"cardholder_name"`
	StatementDate  time.Time `json:"statement_date"`
}
