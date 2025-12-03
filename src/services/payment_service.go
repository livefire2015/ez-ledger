package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/livefire2015/ez-ledger/src/models"
	"github.com/shopspring/decimal"
)

// PaymentService handles payment processing, tracking, and status management
type PaymentService struct {
	ledgerService *StatementLedgerService
	feeService    *FeeService
}

// NewPaymentService creates a new payment service
func NewPaymentService(ledgerService *StatementLedgerService, feeService *FeeService) *PaymentService {
	return &PaymentService{
		ledgerService: ledgerService,
		feeService:    feeService,
	}
}

// InitiatePaymentRequest contains parameters for initiating a payment
type InitiatePaymentRequest struct {
	TenantID       uuid.UUID
	CreditCardID   uuid.UUID
	Amount         decimal.Decimal
	PaymentType    models.PaymentType
	PaymentMethod  models.PaymentMethod
	ScheduledDate  *time.Time
	BillingCycleID *uuid.UUID
	SourceAccount  *PaymentSourceAccount
	CreatedBy      string
}

// PaymentSourceAccount holds source account information
type PaymentSourceAccount struct {
	Last4        string
	RoutingLast4 string
	BankName     string
}

// PaymentResult contains the result of a payment operation
type PaymentResult struct {
	Payment      *models.Payment
	Transitions  []models.PaymentStatusTransition
	LedgerEntry  *models.StatementLedgerEntry
	Error        error
	ErrorCode    string
	ErrorMessage string
}

// InitiatePayment creates a new payment in pending status
func (s *PaymentService) InitiatePayment(req InitiatePaymentRequest) (*PaymentResult, error) {
	if req.Amount.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("payment amount must be positive")
	}

	builder := models.NewPaymentBuilder().
		WithTenant(req.TenantID, req.CreditCardID).
		WithAmount(req.Amount).
		WithType(req.PaymentType).
		WithMethod(req.PaymentMethod).
		WithPaymentNumber(generatePaymentNumber()).
		WithCreatedBy(req.CreatedBy)

	if req.ScheduledDate != nil {
		builder.WithScheduledDate(*req.ScheduledDate)
	}

	if req.BillingCycleID != nil {
		builder.WithBillingCycle(*req.BillingCycleID)
	}

	if req.SourceAccount != nil {
		builder.WithSourceAccount(
			req.SourceAccount.Last4,
			req.SourceAccount.RoutingLast4,
			req.SourceAccount.BankName,
		)
	}

	payment := builder.Build()

	// Create initial transition record
	transition := models.PaymentStatusTransition{
		ID:           uuid.New(),
		PaymentID:    payment.ID,
		FromStatus:   "",
		ToStatus:     models.PaymentStatusPending,
		TransitionAt: time.Now(),
		TriggeredBy:  &req.CreatedBy,
	}

	return &PaymentResult{
		Payment:     payment,
		Transitions: []models.PaymentStatusTransition{transition},
	}, nil
}

// ProcessPayment moves a payment from pending to processing status
func (s *PaymentService) ProcessPayment(payment *models.Payment, processorRef string) (*PaymentResult, error) {
	if !payment.CanTransitionTo(models.PaymentStatusProcessing) {
		return nil, fmt.Errorf("cannot process payment in status %s", payment.Status)
	}

	now := time.Now()
	previousStatus := payment.Status

	payment.PreviousStatus = &previousStatus
	payment.Status = models.PaymentStatusProcessing
	payment.ProcessingAt = &now
	payment.ProcessorRef = &processorRef
	payment.AttemptCount++
	payment.LastAttemptAt = &now
	payment.UpdatedAt = now

	transition := models.PaymentStatusTransition{
		ID:           uuid.New(),
		PaymentID:    payment.ID,
		FromStatus:   previousStatus,
		ToStatus:     models.PaymentStatusProcessing,
		TransitionAt: now,
		TriggeredBy:  strPtr("system"),
	}

	return &PaymentResult{
		Payment:     payment,
		Transitions: []models.PaymentStatusTransition{transition},
	}, nil
}

// ClearPayment marks a payment as successfully cleared
func (s *PaymentService) ClearPayment(payment *models.Payment, card *models.CreditCard, confirmationNum string) (*PaymentResult, error) {
	if !payment.CanTransitionTo(models.PaymentStatusCleared) {
		return nil, fmt.Errorf("cannot clear payment in status %s", payment.Status)
	}

	now := time.Now()
	previousStatus := payment.Status

	payment.PreviousStatus = &previousStatus
	payment.Status = models.PaymentStatusCleared
	payment.ClearedAt = &now
	payment.ConfirmationNum = &confirmationNum
	payment.UpdatedAt = now

	transition := models.PaymentStatusTransition{
		ID:           uuid.New(),
		PaymentID:    payment.ID,
		FromStatus:   previousStatus,
		ToStatus:     models.PaymentStatusCleared,
		TransitionAt: now,
		TriggeredBy:  strPtr("processor"),
	}

	// Create ledger entry for the payment
	var ledgerEntry *models.StatementLedgerEntry
	if s.ledgerService != nil {
		entry := &models.StatementLedgerEntry{
			ID:          uuid.New(),
			TenantID:    payment.TenantID,
			EntryType:   models.EntryTypePayment,
			EntryDate:   now,
			PostingDate: payment.EffectiveDate,
			Amount:      payment.AppliedAmount,
			Description: fmt.Sprintf("Payment - %s", payment.PaymentNumber),
			ReferenceID: &payment.PaymentNumber,
			CreatedAt:   now,
		}
		ledgerEntry = entry
	}

	return &PaymentResult{
		Payment:     payment,
		Transitions: []models.PaymentStatusTransition{transition},
		LedgerEntry: ledgerEntry,
	}, nil
}

// FailPayment marks a payment as failed
func (s *PaymentService) FailPayment(payment *models.Payment, reason string, processorResponse string) (*PaymentResult, error) {
	if !payment.CanTransitionTo(models.PaymentStatusFailed) {
		return nil, fmt.Errorf("cannot fail payment in status %s", payment.Status)
	}

	now := time.Now()
	previousStatus := payment.Status

	payment.PreviousStatus = &previousStatus
	payment.Status = models.PaymentStatusFailed
	payment.FailedAt = &now
	payment.StatusReason = &reason
	payment.ProcessorResponse = &processorResponse
	payment.UpdatedAt = now

	// Set retry time if retries available
	if payment.CanRetry() {
		retryTime := now.Add(time.Hour * 24) // Retry in 24 hours
		payment.NextRetryAt = &retryTime
	}

	transition := models.PaymentStatusTransition{
		ID:           uuid.New(),
		PaymentID:    payment.ID,
		FromStatus:   previousStatus,
		ToStatus:     models.PaymentStatusFailed,
		Reason:       &reason,
		TransitionAt: now,
		TriggeredBy:  strPtr("processor"),
	}

	return &PaymentResult{
		Payment:      payment,
		Transitions:  []models.PaymentStatusTransition{transition},
		ErrorCode:    "PAYMENT_FAILED",
		ErrorMessage: reason,
	}, nil
}

// ReturnPayment marks a cleared payment as returned (ACH return, chargeback)
func (s *PaymentService) ReturnPayment(payment *models.Payment, card *models.CreditCard, returnCode models.ACHReturnCode) (*PaymentResult, error) {
	if !payment.CanTransitionTo(models.PaymentStatusReturned) {
		return nil, fmt.Errorf("cannot return payment in status %s", payment.Status)
	}

	now := time.Now()
	previousStatus := payment.Status
	codeStr := string(returnCode)
	desc := models.ACHReturnCodeDescriptions[returnCode]

	payment.PreviousStatus = &previousStatus
	payment.Status = models.PaymentStatusReturned
	payment.ReturnedAt = &now
	payment.ReturnReasonCode = &codeStr
	payment.ReturnReasonDesc = &desc
	payment.UpdatedAt = now

	transition := models.PaymentStatusTransition{
		ID:           uuid.New(),
		PaymentID:    payment.ID,
		FromStatus:   previousStatus,
		ToStatus:     models.PaymentStatusReturned,
		Reason:       &desc,
		TransitionAt: now,
		TriggeredBy:  strPtr("processor"),
		Metadata: map[string]interface{}{
			"return_code":     codeStr,
			"description":     desc,
			"is_hard_failure": returnCode.IsHardFailure(),
		},
	}

	// Create reversal ledger entry
	var ledgerEntry *models.StatementLedgerEntry
	if s.ledgerService != nil {
		entry := &models.StatementLedgerEntry{
			ID:          uuid.New(),
			TenantID:    payment.TenantID,
			EntryType:   models.EntryTypeAdjustment,
			EntryDate:   now,
			PostingDate: now,
			Amount:      payment.AppliedAmount.Neg(), // Negative to reverse
			Description: fmt.Sprintf("Payment Returned - %s: %s", codeStr, desc),
			ReferenceID: &payment.PaymentNumber,
			CreatedAt:   now,
		}
		ledgerEntry = entry
	}

	return &PaymentResult{
		Payment:      payment,
		Transitions:  []models.PaymentStatusTransition{transition},
		LedgerEntry:  ledgerEntry,
		ErrorCode:    codeStr,
		ErrorMessage: desc,
	}, nil
}

// CancelPayment cancels a pending or processing payment
func (s *PaymentService) CancelPayment(payment *models.Payment, reason string, cancelledBy string) (*PaymentResult, error) {
	if !payment.CanTransitionTo(models.PaymentStatusCancelled) {
		return nil, fmt.Errorf("cannot cancel payment in status %s", payment.Status)
	}

	now := time.Now()
	previousStatus := payment.Status

	payment.PreviousStatus = &previousStatus
	payment.Status = models.PaymentStatusCancelled
	payment.CancelledAt = &now
	payment.StatusReason = &reason
	payment.UpdatedAt = now
	payment.UpdatedBy = &cancelledBy

	transition := models.PaymentStatusTransition{
		ID:           uuid.New(),
		PaymentID:    payment.ID,
		FromStatus:   previousStatus,
		ToStatus:     models.PaymentStatusCancelled,
		Reason:       &reason,
		TransitionAt: now,
		TriggeredBy:  &cancelledBy,
	}

	return &PaymentResult{
		Payment:     payment,
		Transitions: []models.PaymentStatusTransition{transition},
	}, nil
}

// ReversePayment reverses a cleared payment (full reversal)
func (s *PaymentService) ReversePayment(payment *models.Payment, reason string, reversedBy string) (*PaymentResult, error) {
	if !payment.CanTransitionTo(models.PaymentStatusReversed) {
		return nil, fmt.Errorf("cannot reverse payment in status %s", payment.Status)
	}

	now := time.Now()
	previousStatus := payment.Status

	payment.PreviousStatus = &previousStatus
	payment.Status = models.PaymentStatusReversed
	payment.ReversedAt = &now
	payment.StatusReason = &reason
	payment.UpdatedAt = now
	payment.UpdatedBy = &reversedBy

	transition := models.PaymentStatusTransition{
		ID:           uuid.New(),
		PaymentID:    payment.ID,
		FromStatus:   previousStatus,
		ToStatus:     models.PaymentStatusReversed,
		Reason:       &reason,
		TransitionAt: now,
		TriggeredBy:  &reversedBy,
	}

	// Create reversal ledger entry
	var ledgerEntry *models.StatementLedgerEntry
	if s.ledgerService != nil {
		entry := &models.StatementLedgerEntry{
			ID:          uuid.New(),
			TenantID:    payment.TenantID,
			EntryType:   models.EntryTypeAdjustment,
			EntryDate:   now,
			PostingDate: now,
			Amount:      payment.AppliedAmount.Neg(), // Negative to reverse
			Description: fmt.Sprintf("Payment Reversal - %s: %s", payment.PaymentNumber, reason),
			ReferenceID: &payment.PaymentNumber,
			CreatedAt:   now,
		}
		ledgerEntry = entry
	}

	return &PaymentResult{
		Payment:     payment,
		Transitions: []models.PaymentStatusTransition{transition},
		LedgerEntry: ledgerEntry,
	}, nil
}

// RetryPayment attempts to retry a failed payment
func (s *PaymentService) RetryPayment(payment *models.Payment) (*PaymentResult, error) {
	if !payment.CanRetry() {
		return nil, fmt.Errorf("payment cannot be retried: status=%s, attempts=%d, max=%d",
			payment.Status, payment.AttemptCount, payment.MaxRetries)
	}

	if !payment.CanTransitionTo(models.PaymentStatusPending) {
		return nil, fmt.Errorf("cannot retry payment in status %s", payment.Status)
	}

	now := time.Now()
	previousStatus := payment.Status

	payment.PreviousStatus = &previousStatus
	payment.Status = models.PaymentStatusPending
	payment.UpdatedAt = now
	payment.NextRetryAt = nil

	transition := models.PaymentStatusTransition{
		ID:           uuid.New(),
		PaymentID:    payment.ID,
		FromStatus:   previousStatus,
		ToStatus:     models.PaymentStatusPending,
		Reason:       strPtr("Retry attempt"),
		TransitionAt: now,
		TriggeredBy:  strPtr("system"),
		Metadata: map[string]interface{}{
			"attempt_number": payment.AttemptCount + 1,
		},
	}

	return &PaymentResult{
		Payment:     payment,
		Transitions: []models.PaymentStatusTransition{transition},
	}, nil
}

// AssessFailedPaymentFee assesses a failed payment fee when applicable
func (s *PaymentService) AssessFailedPaymentFee(ctx context.Context, payment *models.Payment, card *models.CreditCard) (*FeeAssessmentResult, error) {
	if payment.Status != models.PaymentStatusFailed && payment.Status != models.PaymentStatusReturned {
		return nil, errors.New("can only assess fee for failed or returned payments")
	}

	if s.feeService == nil {
		return nil, errors.New("fee service not configured")
	}

	req := FailedPaymentFeeRequest{
		CreditCard:    card,
		PaymentAmount: payment.Amount,
		PaymentDate:   time.Now(),
		FailureReason: "Payment Failed",
		PaymentMethod: string(payment.PaymentMethod),
		ReferenceID:   payment.PaymentNumber,
	}

	if payment.StatusReason != nil {
		req.FailureReason = *payment.StatusReason
	}

	return s.feeService.AssessFailedPaymentFee(ctx, req)
}

// GetPaymentHistory returns the status transition history for a payment
func (s *PaymentService) GetPaymentHistory(payment *models.Payment) []models.PaymentStatusTransition {
	// In a real implementation, this would query from database
	// For now, we return based on payment timestamps
	var history []models.PaymentStatusTransition

	// Initial pending
	if !payment.InitiatedAt.IsZero() {
		history = append(history, models.PaymentStatusTransition{
			PaymentID:    payment.ID,
			ToStatus:     models.PaymentStatusPending,
			TransitionAt: payment.InitiatedAt,
		})
	}

	// Processing
	if payment.ProcessingAt != nil {
		history = append(history, models.PaymentStatusTransition{
			PaymentID:    payment.ID,
			FromStatus:   models.PaymentStatusPending,
			ToStatus:     models.PaymentStatusProcessing,
			TransitionAt: *payment.ProcessingAt,
		})
	}

	// Cleared
	if payment.ClearedAt != nil {
		history = append(history, models.PaymentStatusTransition{
			PaymentID:    payment.ID,
			FromStatus:   models.PaymentStatusProcessing,
			ToStatus:     models.PaymentStatusCleared,
			TransitionAt: *payment.ClearedAt,
		})
	}

	// Failed
	if payment.FailedAt != nil {
		history = append(history, models.PaymentStatusTransition{
			PaymentID:    payment.ID,
			FromStatus:   models.PaymentStatusProcessing,
			ToStatus:     models.PaymentStatusFailed,
			TransitionAt: *payment.FailedAt,
		})
	}

	// Returned
	if payment.ReturnedAt != nil {
		history = append(history, models.PaymentStatusTransition{
			PaymentID:    payment.ID,
			FromStatus:   models.PaymentStatusCleared,
			ToStatus:     models.PaymentStatusReturned,
			TransitionAt: *payment.ReturnedAt,
		})
	}

	// Cancelled
	if payment.CancelledAt != nil {
		history = append(history, models.PaymentStatusTransition{
			PaymentID:    payment.ID,
			ToStatus:     models.PaymentStatusCancelled,
			TransitionAt: *payment.CancelledAt,
		})
	}

	// Reversed
	if payment.ReversedAt != nil {
		history = append(history, models.PaymentStatusTransition{
			PaymentID:    payment.ID,
			FromStatus:   models.PaymentStatusCleared,
			ToStatus:     models.PaymentStatusReversed,
			TransitionAt: *payment.ReversedAt,
		})
	}

	return history
}

// CalculatePaymentSummary calculates payment summary for a card and period
func (s *PaymentService) CalculatePaymentSummary(
	tenantID, creditCardID uuid.UUID,
	payments []*models.Payment,
	period string,
) *models.PaymentSummary {
	summary := &models.PaymentSummary{
		TenantID:     tenantID,
		CreditCardID: creditCardID,
		Period:       period,
	}

	if len(payments) == 0 {
		return summary
	}

	for _, p := range payments {
		summary.TotalPayments++
		summary.TotalAmount = summary.TotalAmount.Add(p.Amount)

		switch p.Status {
		case models.PaymentStatusCleared:
			summary.ClearedPayments++
			summary.ClearedAmount = summary.ClearedAmount.Add(p.Amount)
		case models.PaymentStatusPending, models.PaymentStatusProcessing:
			summary.PendingPayments++
			summary.PendingAmount = summary.PendingAmount.Add(p.Amount)
		case models.PaymentStatusFailed:
			summary.FailedPayments++
			summary.FailedAmount = summary.FailedAmount.Add(p.Amount)
		case models.PaymentStatusReturned:
			summary.ReturnedPayments++
			summary.ReturnedAmount = summary.ReturnedAmount.Add(p.Amount)
		}

		// Track largest payment
		if p.Amount.GreaterThan(summary.LargestPayment) {
			summary.LargestPayment = p.Amount
		}

		// Track last payment
		if p.ClearedAt != nil {
			if summary.LastPaymentDate == nil || p.ClearedAt.After(*summary.LastPaymentDate) {
				summary.LastPaymentDate = p.ClearedAt
				summary.LastPaymentAmount = p.Amount
			}
		}
	}

	// Calculate average
	if summary.TotalPayments > 0 {
		summary.AveragePayment = summary.TotalAmount.Div(decimal.NewFromInt(int64(summary.TotalPayments))).Round(2)
	}

	return summary
}

// generatePaymentNumber generates a unique payment reference number
func generatePaymentNumber() string {
	return fmt.Sprintf("PMT-%s", uuid.New().String()[:8])
}

// strPtr helper to create string pointers
func strPtr(s string) *string {
	return &s
}

// GetPendingPaymentsForRetry returns payments that are ready for retry
func (s *PaymentService) GetPendingPaymentsForRetry(payments []*models.Payment, now time.Time) []*models.Payment {
	var readyForRetry []*models.Payment

	for _, p := range payments {
		if p.CanRetry() && p.NextRetryAt != nil && !p.NextRetryAt.After(now) {
			readyForRetry = append(readyForRetry, p)
		}
	}

	return readyForRetry
}

// ValidatePaymentAmount validates payment amount against card balance
func (s *PaymentService) ValidatePaymentAmount(amount decimal.Decimal, card *models.CreditCard) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return errors.New("payment amount must be positive")
	}

	currentBalance := card.CreditLimit.Sub(card.AvailableCredit)

	// Warn if payment exceeds balance (creates credit balance)
	if amount.GreaterThan(currentBalance) && currentBalance.GreaterThan(decimal.Zero) {
		// This is allowed but might be worth noting
		// In a real system, we might return a warning
	}

	return nil
}

// DeterminePaymentType determines the appropriate payment type based on amount
func (s *PaymentService) DeterminePaymentType(
	amount decimal.Decimal,
	cycle *models.BillingCycle,
) models.PaymentType {
	if cycle == nil {
		return models.PaymentTypeRegular
	}

	remainingBalance := cycle.GetRemainingBalance()
	remainingMinimum := cycle.GetRemainingMinimum()

	// Check if paying full balance
	if amount.GreaterThanOrEqual(remainingBalance) {
		return models.PaymentTypeFull
	}

	// Check if paying statement balance
	if amount.Equal(cycle.NewBalance) {
		return models.PaymentTypeStatement
	}

	// Check if paying minimum
	if amount.Equal(cycle.MinimumPayment) || amount.Equal(remainingMinimum) {
		return models.PaymentTypeMinimum
	}

	return models.PaymentTypeRegular
}
