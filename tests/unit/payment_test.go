package unit

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/livefire2015/ez-ledger/src/models"
	"github.com/shopspring/decimal"
)

func TestPaymentStatusTransitions(t *testing.T) {
	tests := []struct {
		name        string
		fromStatus  models.PaymentStatus
		toStatus    models.PaymentStatus
		shouldAllow bool
	}{
		// From Pending
		{"pending to processing", models.PaymentStatusPending, models.PaymentStatusProcessing, true},
		{"pending to cancelled", models.PaymentStatusPending, models.PaymentStatusCancelled, true},
		{"pending to cleared", models.PaymentStatusPending, models.PaymentStatusCleared, false},
		{"pending to failed", models.PaymentStatusPending, models.PaymentStatusFailed, false},

		// From Processing
		{"processing to cleared", models.PaymentStatusProcessing, models.PaymentStatusCleared, true},
		{"processing to failed", models.PaymentStatusProcessing, models.PaymentStatusFailed, true},
		{"processing to cancelled", models.PaymentStatusProcessing, models.PaymentStatusCancelled, true},
		{"processing to pending", models.PaymentStatusProcessing, models.PaymentStatusPending, false},

		// From Cleared
		{"cleared to returned", models.PaymentStatusCleared, models.PaymentStatusReturned, true},
		{"cleared to reversed", models.PaymentStatusCleared, models.PaymentStatusReversed, true},
		{"cleared to failed", models.PaymentStatusCleared, models.PaymentStatusFailed, false},
		{"cleared to pending", models.PaymentStatusCleared, models.PaymentStatusPending, false},

		// From Failed
		{"failed to pending (retry)", models.PaymentStatusFailed, models.PaymentStatusPending, true},
		{"failed to cleared", models.PaymentStatusFailed, models.PaymentStatusCleared, false},
		{"failed to processing", models.PaymentStatusFailed, models.PaymentStatusProcessing, false},

		// Terminal states
		{"returned to any", models.PaymentStatusReturned, models.PaymentStatusPending, false},
		{"cancelled to any", models.PaymentStatusCancelled, models.PaymentStatusPending, false},
		{"reversed to any", models.PaymentStatusReversed, models.PaymentStatusPending, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payment := &models.Payment{Status: tt.fromStatus}
			result := payment.CanTransitionTo(tt.toStatus)
			if result != tt.shouldAllow {
				t.Errorf("Expected CanTransitionTo(%s) = %v, got %v", tt.toStatus, tt.shouldAllow, result)
			}
		})
	}
}

func TestPaymentIsTerminal(t *testing.T) {
	tests := []struct {
		status     models.PaymentStatus
		isTerminal bool
	}{
		{models.PaymentStatusPending, false},
		{models.PaymentStatusProcessing, false},
		{models.PaymentStatusCleared, false},
		{models.PaymentStatusFailed, false},
		{models.PaymentStatusReturned, true},
		{models.PaymentStatusCancelled, true},
		{models.PaymentStatusReversed, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			payment := &models.Payment{Status: tt.status}
			if payment.IsTerminal() != tt.isTerminal {
				t.Errorf("Expected IsTerminal() = %v for status %s", tt.isTerminal, tt.status)
			}
		})
	}
}

func TestPaymentIsSuccessful(t *testing.T) {
	tests := []struct {
		status       models.PaymentStatus
		isSuccessful bool
	}{
		{models.PaymentStatusPending, false},
		{models.PaymentStatusProcessing, false},
		{models.PaymentStatusCleared, true},
		{models.PaymentStatusFailed, false},
		{models.PaymentStatusReturned, false},
		{models.PaymentStatusCancelled, false},
		{models.PaymentStatusReversed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			payment := &models.Payment{Status: tt.status}
			if payment.IsSuccessful() != tt.isSuccessful {
				t.Errorf("Expected IsSuccessful() = %v for status %s", tt.isSuccessful, tt.status)
			}
		})
	}
}

func TestPaymentIsPendingProcessing(t *testing.T) {
	tests := []struct {
		status            models.PaymentStatus
		isPendingOrProcessing bool
	}{
		{models.PaymentStatusPending, true},
		{models.PaymentStatusProcessing, true},
		{models.PaymentStatusCleared, false},
		{models.PaymentStatusFailed, false},
		{models.PaymentStatusReturned, false},
		{models.PaymentStatusCancelled, false},
		{models.PaymentStatusReversed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			payment := &models.Payment{Status: tt.status}
			if payment.IsPendingProcessing() != tt.isPendingOrProcessing {
				t.Errorf("Expected IsPendingProcessing() = %v for status %s", tt.isPendingOrProcessing, tt.status)
			}
		})
	}
}

func TestPaymentCanRetry(t *testing.T) {
	tests := []struct {
		name         string
		status       models.PaymentStatus
		attemptCount int
		maxRetries   int
		canRetry     bool
	}{
		{"failed with retries left", models.PaymentStatusFailed, 1, 3, true},
		{"failed at max retries", models.PaymentStatusFailed, 3, 3, false},
		{"failed exceeds max", models.PaymentStatusFailed, 5, 3, false},
		{"cleared cannot retry", models.PaymentStatusCleared, 0, 3, false},
		{"pending cannot retry", models.PaymentStatusPending, 0, 3, false},
		{"failed no retries allowed", models.PaymentStatusFailed, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payment := &models.Payment{
				Status:       tt.status,
				AttemptCount: tt.attemptCount,
				MaxRetries:   tt.maxRetries,
			}
			if payment.CanRetry() != tt.canRetry {
				t.Errorf("Expected CanRetry() = %v", tt.canRetry)
			}
		})
	}
}

func TestPaymentGetDaysUntilEffective(t *testing.T) {
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		effectiveDate time.Time
		expectedDays  int
	}{
		{"same day", time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC), 0},
		{"past date", time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC), 0},
		{"5 days future", time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC), 4}, // Truncates partial days
		{"10 days future", time.Date(2024, 6, 25, 0, 0, 0, 0, time.UTC), 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payment := &models.Payment{EffectiveDate: tt.effectiveDate}
			days := payment.GetDaysUntilEffective(now)
			if days != tt.expectedDays {
				t.Errorf("Expected %d days, got %d", tt.expectedDays, days)
			}
		})
	}
}

func TestPaymentBuilder(t *testing.T) {
	tenantID := uuid.New()
	cardID := uuid.New()
	cycleID := uuid.New()
	amount := decimal.NewFromFloat(250.00)
	scheduledDate := time.Now().AddDate(0, 0, 7)

	payment := models.NewPaymentBuilder().
		WithTenant(tenantID, cardID).
		WithAmount(amount).
		WithPaymentNumber("PMT-12345678").
		WithMethod(models.PaymentMethodACH).
		WithType(models.PaymentTypeRegular).
		WithSourceAccount("1234", "5678", "Test Bank").
		WithScheduledDate(scheduledDate).
		WithBillingCycle(cycleID).
		WithCreatedBy("test_user").
		Build()

	if payment.TenantID != tenantID {
		t.Error("TenantID not set correctly")
	}
	if payment.CreditCardID != cardID {
		t.Error("CreditCardID not set correctly")
	}
	if !payment.Amount.Equal(amount) {
		t.Errorf("Expected amount %s, got %s", amount, payment.Amount)
	}
	if !payment.AppliedAmount.Equal(amount) {
		t.Error("AppliedAmount should equal Amount")
	}
	if payment.PaymentNumber != "PMT-12345678" {
		t.Error("PaymentNumber not set correctly")
	}
	if payment.PaymentMethod != models.PaymentMethodACH {
		t.Error("PaymentMethod not set correctly")
	}
	if payment.PaymentType != models.PaymentTypeRegular {
		t.Error("PaymentType not set correctly")
	}
	if *payment.SourceAccountLast4 != "1234" {
		t.Error("SourceAccountLast4 not set correctly")
	}
	if *payment.SourceRoutingLast4 != "5678" {
		t.Error("SourceRoutingLast4 not set correctly")
	}
	if *payment.SourceBankName != "Test Bank" {
		t.Error("SourceBankName not set correctly")
	}
	if payment.ScheduledDate == nil || !payment.ScheduledDate.Equal(scheduledDate) {
		t.Error("ScheduledDate not set correctly")
	}
	if payment.BillingCycleID == nil || *payment.BillingCycleID != cycleID {
		t.Error("BillingCycleID not set correctly")
	}
	if payment.CreatedBy == nil || *payment.CreatedBy != "test_user" {
		t.Error("CreatedBy not set correctly")
	}
	if payment.Status != models.PaymentStatusPending {
		t.Errorf("Expected initial status 'pending', got '%s'", payment.Status)
	}
	if payment.Currency != "USD" {
		t.Error("Expected default currency USD")
	}
	if payment.MaxRetries != 3 {
		t.Errorf("Expected default max retries 3, got %d", payment.MaxRetries)
	}
}

func TestPaymentBuilderDefaults(t *testing.T) {
	payment := models.NewPaymentBuilder().Build()

	if payment.ID == uuid.Nil {
		t.Error("Expected UUID to be generated")
	}
	if payment.Status != models.PaymentStatusPending {
		t.Error("Expected default status 'pending'")
	}
	if payment.Currency != "USD" {
		t.Error("Expected default currency 'USD'")
	}
	if payment.AttemptCount != 0 {
		t.Error("Expected default attempt count 0")
	}
	if payment.MaxRetries != 3 {
		t.Error("Expected default max retries 3")
	}
	if payment.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
	if payment.InitiatedAt.IsZero() {
		t.Error("Expected InitiatedAt to be set")
	}
}

func TestACHReturnCodeIsHardFailure(t *testing.T) {
	tests := []struct {
		code        models.ACHReturnCode
		isHard      bool
	}{
		{models.ACHReturnR01, false}, // Insufficient Funds - soft
		{models.ACHReturnR02, true},  // Account Closed - hard
		{models.ACHReturnR03, true},  // No Account - hard
		{models.ACHReturnR04, true},  // Invalid Account Number - hard
		{models.ACHReturnR05, true},  // Unauthorized Debit - hard
		{models.ACHReturnR06, false}, // ODFI Request - soft
		{models.ACHReturnR07, true},  // Authorization Revoked - hard
		{models.ACHReturnR08, false}, // Payment Stopped - soft
		{models.ACHReturnR09, false}, // Uncollected Funds - soft
		{models.ACHReturnR10, true},  // Not Authorized - hard
		{models.ACHReturnR16, true},  // Account Frozen - hard
		{models.ACHReturnR20, true},  // Non-Transaction Account - hard
		{models.ACHReturnR29, true},  // Corporate Not Authorized - hard
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			if tt.code.IsHardFailure() != tt.isHard {
				t.Errorf("Expected IsHardFailure() = %v for code %s", tt.isHard, tt.code)
			}
		})
	}
}

func TestACHReturnCodeDescriptions(t *testing.T) {
	// Verify all codes have descriptions
	codes := []models.ACHReturnCode{
		models.ACHReturnR01, models.ACHReturnR02, models.ACHReturnR03,
		models.ACHReturnR04, models.ACHReturnR05, models.ACHReturnR06,
		models.ACHReturnR07, models.ACHReturnR08, models.ACHReturnR09,
		models.ACHReturnR10, models.ACHReturnR16, models.ACHReturnR20,
		models.ACHReturnR29,
	}

	for _, code := range codes {
		desc, exists := models.ACHReturnCodeDescriptions[code]
		if !exists {
			t.Errorf("Missing description for code %s", code)
		}
		if desc == "" {
			t.Errorf("Empty description for code %s", code)
		}
	}
}

func TestPaymentSummary(t *testing.T) {
	summary := &models.PaymentSummary{
		TenantID:          uuid.New(),
		CreditCardID:      uuid.New(),
		Period:            "2024-06",
		TotalPayments:     10,
		TotalAmount:       decimal.NewFromInt(5000),
		ClearedPayments:   7,
		ClearedAmount:     decimal.NewFromInt(3500),
		PendingPayments:   2,
		PendingAmount:     decimal.NewFromInt(1000),
		FailedPayments:    1,
		FailedAmount:      decimal.NewFromInt(500),
		ReturnedPayments:  0,
		ReturnedAmount:    decimal.Zero,
		AveragePayment:    decimal.NewFromInt(500),
		LargestPayment:    decimal.NewFromInt(1000),
	}

	// Verify totals add up
	clearedPlusPendingPlusFailed := summary.ClearedAmount.Add(summary.PendingAmount).Add(summary.FailedAmount)
	if !clearedPlusPendingPlusFailed.Equal(summary.TotalAmount) {
		t.Error("Cleared + Pending + Failed amounts should equal Total amount")
	}

	paymentCount := summary.ClearedPayments + summary.PendingPayments + summary.FailedPayments + summary.ReturnedPayments
	if paymentCount != summary.TotalPayments {
		t.Error("Payment counts should add up to total")
	}
}

func TestPaymentStatusTransitionStruct(t *testing.T) {
	paymentID := uuid.New()
	now := time.Now()
	reason := "Test reason"
	triggeredBy := "test_user"

	transition := models.PaymentStatusTransition{
		ID:           uuid.New(),
		PaymentID:    paymentID,
		FromStatus:   models.PaymentStatusPending,
		ToStatus:     models.PaymentStatusProcessing,
		Reason:       &reason,
		TransitionAt: now,
		TriggeredBy:  &triggeredBy,
		Metadata: map[string]interface{}{
			"test_key": "test_value",
		},
	}

	if transition.PaymentID != paymentID {
		t.Error("PaymentID not set correctly")
	}
	if transition.FromStatus != models.PaymentStatusPending {
		t.Error("FromStatus not set correctly")
	}
	if transition.ToStatus != models.PaymentStatusProcessing {
		t.Error("ToStatus not set correctly")
	}
	if *transition.Reason != reason {
		t.Error("Reason not set correctly")
	}
	if *transition.TriggeredBy != triggeredBy {
		t.Error("TriggeredBy not set correctly")
	}
	if transition.Metadata["test_key"] != "test_value" {
		t.Error("Metadata not set correctly")
	}
}

func TestPaymentMethodConstants(t *testing.T) {
	methods := []models.PaymentMethod{
		models.PaymentMethodACH,
		models.PaymentMethodDebitCard,
		models.PaymentMethodCheck,
		models.PaymentMethodWire,
		models.PaymentMethodInternalXfer,
		models.PaymentMethodExternalXfer,
		models.PaymentMethodCash,
		models.PaymentMethodMoneyOrder,
	}

	// Ensure all methods are unique
	seen := make(map[models.PaymentMethod]bool)
	for _, m := range methods {
		if seen[m] {
			t.Errorf("Duplicate payment method: %s", m)
		}
		seen[m] = true
	}

	// Ensure all methods have non-empty values
	for _, m := range methods {
		if string(m) == "" {
			t.Error("Payment method has empty string value")
		}
	}
}

func TestPaymentTypeConstants(t *testing.T) {
	types := []models.PaymentType{
		models.PaymentTypeRegular,
		models.PaymentTypeMinimum,
		models.PaymentTypeStatement,
		models.PaymentTypeFull,
		models.PaymentTypeAutoPay,
		models.PaymentTypeScheduled,
		models.PaymentTypeOneTime,
	}

	// Ensure all types are unique
	seen := make(map[models.PaymentType]bool)
	for _, pt := range types {
		if seen[pt] {
			t.Errorf("Duplicate payment type: %s", pt)
		}
		seen[pt] = true
	}

	// Ensure all types have non-empty values
	for _, pt := range types {
		if string(pt) == "" {
			t.Error("Payment type has empty string value")
		}
	}
}

func TestPaymentStatusConstants(t *testing.T) {
	statuses := []models.PaymentStatus{
		models.PaymentStatusPending,
		models.PaymentStatusProcessing,
		models.PaymentStatusCleared,
		models.PaymentStatusFailed,
		models.PaymentStatusReturned,
		models.PaymentStatusCancelled,
		models.PaymentStatusReversed,
	}

	// Ensure all statuses are unique
	seen := make(map[models.PaymentStatus]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("Duplicate payment status: %s", s)
		}
		seen[s] = true
	}

	// Ensure all statuses have non-empty values
	for _, s := range statuses {
		if string(s) == "" {
			t.Error("Payment status has empty string value")
		}
	}
}

func TestPaymentWithScheduledDateSetsEffectiveDate(t *testing.T) {
	scheduledDate := time.Date(2024, 7, 15, 0, 0, 0, 0, time.UTC)

	payment := models.NewPaymentBuilder().
		WithScheduledDate(scheduledDate).
		Build()

	if payment.ScheduledDate == nil {
		t.Fatal("ScheduledDate should be set")
	}
	if !payment.ScheduledDate.Equal(scheduledDate) {
		t.Error("ScheduledDate not set correctly")
	}
	if !payment.EffectiveDate.Equal(scheduledDate) {
		t.Error("EffectiveDate should be set to ScheduledDate")
	}
}
