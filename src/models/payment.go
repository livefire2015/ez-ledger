package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PaymentStatus represents the status of a payment
type PaymentStatus string

const (
	PaymentStatusPending    PaymentStatus = "pending"     // Payment initiated, not yet processed
	PaymentStatusProcessing PaymentStatus = "processing"  // Payment being processed by payment processor
	PaymentStatusCleared    PaymentStatus = "cleared"     // Payment successfully completed
	PaymentStatusFailed     PaymentStatus = "failed"      // Payment failed (NSF, declined, etc.)
	PaymentStatusReturned   PaymentStatus = "returned"    // Payment returned (ACH return, chargeback)
	PaymentStatusCancelled  PaymentStatus = "cancelled"   // Payment cancelled before processing
	PaymentStatusReversed   PaymentStatus = "reversed"    // Payment reversed after clearing
)

// PaymentMethod represents the method used for payment
type PaymentMethod string

const (
	PaymentMethodACH           PaymentMethod = "ach"            // Bank transfer (ACH)
	PaymentMethodDebitCard     PaymentMethod = "debit_card"     // Debit card payment
	PaymentMethodCheck         PaymentMethod = "check"          // Paper check
	PaymentMethodWire          PaymentMethod = "wire"           // Wire transfer
	PaymentMethodInternalXfer  PaymentMethod = "internal_xfer"  // Internal account transfer
	PaymentMethodExternalXfer  PaymentMethod = "external_xfer"  // External transfer
	PaymentMethodCash          PaymentMethod = "cash"           // Cash payment (in-person)
	PaymentMethodMoneyOrder    PaymentMethod = "money_order"    // Money order
)

// PaymentType represents the type of payment
type PaymentType string

const (
	PaymentTypeRegular    PaymentType = "regular"     // Standard payment
	PaymentTypeMinimum    PaymentType = "minimum"     // Minimum payment
	PaymentTypeStatement  PaymentType = "statement"   // Statement balance payment
	PaymentTypeFull       PaymentType = "full"        // Full balance payment
	PaymentTypeAutoPay    PaymentType = "auto_pay"    // Automatic payment
	PaymentTypeScheduled  PaymentType = "scheduled"   // Scheduled payment
	PaymentTypeOneTime    PaymentType = "one_time"    // One-time payment
)

// Payment represents a payment record with full status tracking
type Payment struct {
	ID           uuid.UUID     `json:"id" db:"id"`
	TenantID     uuid.UUID     `json:"tenant_id" db:"tenant_id"`
	CreditCardID uuid.UUID     `json:"credit_card_id" db:"credit_card_id"`

	// Payment identification
	PaymentNumber   string        `json:"payment_number" db:"payment_number"`     // Unique payment reference
	ConfirmationNum *string       `json:"confirmation_num,omitempty" db:"confirmation_num"`

	// Amount details
	Amount          decimal.Decimal `json:"amount" db:"amount"`
	Currency        string          `json:"currency" db:"currency"`
	AppliedAmount   decimal.Decimal `json:"applied_amount" db:"applied_amount"`     // Amount applied to balance
	ProcessingFee   decimal.Decimal `json:"processing_fee" db:"processing_fee"`     // Any processing fees

	// Payment type and method
	PaymentType   PaymentType   `json:"payment_type" db:"payment_type"`
	PaymentMethod PaymentMethod `json:"payment_method" db:"payment_method"`

	// Source information
	SourceAccountLast4 *string `json:"source_account_last4,omitempty" db:"source_account_last4"`
	SourceRoutingLast4 *string `json:"source_routing_last4,omitempty" db:"source_routing_last4"`
	SourceBankName     *string `json:"source_bank_name,omitempty" db:"source_bank_name"`

	// Billing cycle linkage
	BillingCycleID  *uuid.UUID `json:"billing_cycle_id,omitempty" db:"billing_cycle_id"`
	StatementEntryID *uuid.UUID `json:"statement_entry_id,omitempty" db:"statement_entry_id"`

	// Status and status history
	Status         PaymentStatus `json:"status" db:"status"`
	PreviousStatus *PaymentStatus `json:"previous_status,omitempty" db:"previous_status"`
	StatusReason   *string        `json:"status_reason,omitempty" db:"status_reason"`

	// Key timestamps
	ScheduledDate    *time.Time `json:"scheduled_date,omitempty" db:"scheduled_date"`
	InitiatedAt      time.Time  `json:"initiated_at" db:"initiated_at"`
	ProcessingAt     *time.Time `json:"processing_at,omitempty" db:"processing_at"`
	ClearedAt        *time.Time `json:"cleared_at,omitempty" db:"cleared_at"`
	FailedAt         *time.Time `json:"failed_at,omitempty" db:"failed_at"`
	ReturnedAt       *time.Time `json:"returned_at,omitempty" db:"returned_at"`
	CancelledAt      *time.Time `json:"cancelled_at,omitempty" db:"cancelled_at"`
	ReversedAt       *time.Time `json:"reversed_at,omitempty" db:"reversed_at"`
	EffectiveDate    time.Time  `json:"effective_date" db:"effective_date"`  // Date payment takes effect

	// Processing details
	ProcessorRef       *string `json:"processor_ref,omitempty" db:"processor_ref"`
	ProcessorResponse  *string `json:"processor_response,omitempty" db:"processor_response"`
	ReturnReasonCode   *string `json:"return_reason_code,omitempty" db:"return_reason_code"`
	ReturnReasonDesc   *string `json:"return_reason_desc,omitempty" db:"return_reason_desc"`

	// Retry tracking
	AttemptCount     int        `json:"attempt_count" db:"attempt_count"`
	LastAttemptAt    *time.Time `json:"last_attempt_at,omitempty" db:"last_attempt_at"`
	NextRetryAt      *time.Time `json:"next_retry_at,omitempty" db:"next_retry_at"`
	MaxRetries       int        `json:"max_retries" db:"max_retries"`

	// Metadata and audit
	Metadata  map[string]interface{} `json:"metadata,omitempty" db:"metadata"`
	Notes     *string                `json:"notes,omitempty" db:"notes"`
	CreatedAt time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt time.Time              `json:"updated_at" db:"updated_at"`
	CreatedBy *string                `json:"created_by,omitempty" db:"created_by"`
	UpdatedBy *string                `json:"updated_by,omitempty" db:"updated_by"`
}

// PaymentStatusTransition represents a status change in payment history
type PaymentStatusTransition struct {
	ID           uuid.UUID     `json:"id" db:"id"`
	PaymentID    uuid.UUID     `json:"payment_id" db:"payment_id"`
	FromStatus   PaymentStatus `json:"from_status" db:"from_status"`
	ToStatus     PaymentStatus `json:"to_status" db:"to_status"`
	Reason       *string       `json:"reason,omitempty" db:"reason"`
	TransitionAt time.Time     `json:"transition_at" db:"transition_at"`
	TriggeredBy  *string       `json:"triggered_by,omitempty" db:"triggered_by"` // System, user, processor
	Metadata     map[string]interface{} `json:"metadata,omitempty" db:"metadata"`
}

// CanTransitionTo checks if the payment can transition to a new status
func (p *Payment) CanTransitionTo(newStatus PaymentStatus) bool {
	validTransitions := map[PaymentStatus][]PaymentStatus{
		PaymentStatusPending: {
			PaymentStatusProcessing,
			PaymentStatusCancelled,
		},
		PaymentStatusProcessing: {
			PaymentStatusCleared,
			PaymentStatusFailed,
			PaymentStatusCancelled,
		},
		PaymentStatusCleared: {
			PaymentStatusReturned,
			PaymentStatusReversed,
		},
		PaymentStatusFailed: {
			PaymentStatusPending, // Retry
		},
		PaymentStatusReturned: {}, // Terminal state
		PaymentStatusCancelled: {}, // Terminal state
		PaymentStatusReversed: {},  // Terminal state
	}

	allowed, exists := validTransitions[p.Status]
	if !exists {
		return false
	}

	for _, s := range allowed {
		if s == newStatus {
			return true
		}
	}
	return false
}

// IsTerminal returns true if the payment is in a terminal state
func (p *Payment) IsTerminal() bool {
	return p.Status == PaymentStatusReturned ||
		p.Status == PaymentStatusCancelled ||
		p.Status == PaymentStatusReversed
}

// IsSuccessful returns true if the payment completed successfully
func (p *Payment) IsSuccessful() bool {
	return p.Status == PaymentStatusCleared
}

// IsPendingProcessing returns true if the payment is awaiting processing
func (p *Payment) IsPendingProcessing() bool {
	return p.Status == PaymentStatusPending || p.Status == PaymentStatusProcessing
}

// CanRetry returns true if a failed payment can be retried
func (p *Payment) CanRetry() bool {
	return p.Status == PaymentStatusFailed && p.AttemptCount < p.MaxRetries
}

// GetDaysUntilEffective returns days until the payment takes effect
func (p *Payment) GetDaysUntilEffective(now time.Time) int {
	if now.After(p.EffectiveDate) || now.Equal(p.EffectiveDate) {
		return 0
	}
	return int(p.EffectiveDate.Sub(now).Hours() / 24)
}

// PaymentSummary provides a summary view of payment activity
type PaymentSummary struct {
	TenantID          uuid.UUID       `json:"tenant_id"`
	CreditCardID      uuid.UUID       `json:"credit_card_id"`
	Period            string          `json:"period"`
	TotalPayments     int             `json:"total_payments"`
	TotalAmount       decimal.Decimal `json:"total_amount"`
	ClearedPayments   int             `json:"cleared_payments"`
	ClearedAmount     decimal.Decimal `json:"cleared_amount"`
	PendingPayments   int             `json:"pending_payments"`
	PendingAmount     decimal.Decimal `json:"pending_amount"`
	FailedPayments    int             `json:"failed_payments"`
	FailedAmount      decimal.Decimal `json:"failed_amount"`
	ReturnedPayments  int             `json:"returned_payments"`
	ReturnedAmount    decimal.Decimal `json:"returned_amount"`
	AveragePayment    decimal.Decimal `json:"average_payment"`
	LargestPayment    decimal.Decimal `json:"largest_payment"`
	LastPaymentDate   *time.Time      `json:"last_payment_date,omitempty"`
	LastPaymentAmount decimal.Decimal `json:"last_payment_amount"`
}

// ACHReturnCode represents common ACH return reason codes
type ACHReturnCode string

const (
	ACHReturnR01 ACHReturnCode = "R01" // Insufficient Funds
	ACHReturnR02 ACHReturnCode = "R02" // Account Closed
	ACHReturnR03 ACHReturnCode = "R03" // No Account/Unable to Locate Account
	ACHReturnR04 ACHReturnCode = "R04" // Invalid Account Number
	ACHReturnR05 ACHReturnCode = "R05" // Unauthorized Debit to Consumer Account
	ACHReturnR06 ACHReturnCode = "R06" // Returned per ODFI's Request
	ACHReturnR07 ACHReturnCode = "R07" // Authorization Revoked by Customer
	ACHReturnR08 ACHReturnCode = "R08" // Payment Stopped
	ACHReturnR09 ACHReturnCode = "R09" // Uncollected Funds
	ACHReturnR10 ACHReturnCode = "R10" // Customer Advises Not Authorized
	ACHReturnR16 ACHReturnCode = "R16" // Account Frozen
	ACHReturnR20 ACHReturnCode = "R20" // Non-Transaction Account
	ACHReturnR29 ACHReturnCode = "R29" // Corporate Customer Advises Not Authorized
)

// ACHReturnCodeDescriptions maps return codes to descriptions
var ACHReturnCodeDescriptions = map[ACHReturnCode]string{
	ACHReturnR01: "Insufficient Funds",
	ACHReturnR02: "Account Closed",
	ACHReturnR03: "No Account/Unable to Locate Account",
	ACHReturnR04: "Invalid Account Number",
	ACHReturnR05: "Unauthorized Debit to Consumer Account",
	ACHReturnR06: "Returned per ODFI's Request",
	ACHReturnR07: "Authorization Revoked by Customer",
	ACHReturnR08: "Payment Stopped",
	ACHReturnR09: "Uncollected Funds",
	ACHReturnR10: "Customer Advises Not Authorized",
	ACHReturnR16: "Account Frozen",
	ACHReturnR20: "Non-Transaction Account",
	ACHReturnR29: "Corporate Customer Advises Not Authorized",
}

// IsHardFailure returns true if the return code indicates a permanent failure
func (code ACHReturnCode) IsHardFailure() bool {
	hardFailures := map[ACHReturnCode]bool{
		ACHReturnR02: true, // Account Closed
		ACHReturnR03: true, // No Account
		ACHReturnR04: true, // Invalid Account
		ACHReturnR05: true, // Unauthorized
		ACHReturnR07: true, // Authorization Revoked
		ACHReturnR10: true, // Not Authorized
		ACHReturnR16: true, // Account Frozen
		ACHReturnR20: true, // Non-Transaction Account
		ACHReturnR29: true, // Corporate Not Authorized
	}
	return hardFailures[code]
}

// PaymentBuilder helps construct payment records
type PaymentBuilder struct {
	payment *Payment
}

// NewPaymentBuilder creates a new payment builder
func NewPaymentBuilder() *PaymentBuilder {
	return &PaymentBuilder{
		payment: &Payment{
			ID:            uuid.New(),
			Status:        PaymentStatusPending,
			Currency:      "USD",
			AttemptCount:  0,
			MaxRetries:    3,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
			InitiatedAt:   time.Now(),
			EffectiveDate: time.Now(),
		},
	}
}

// WithTenant sets the tenant and credit card
func (b *PaymentBuilder) WithTenant(tenantID, creditCardID uuid.UUID) *PaymentBuilder {
	b.payment.TenantID = tenantID
	b.payment.CreditCardID = creditCardID
	return b
}

// WithAmount sets the payment amount
func (b *PaymentBuilder) WithAmount(amount decimal.Decimal) *PaymentBuilder {
	b.payment.Amount = amount
	b.payment.AppliedAmount = amount
	return b
}

// WithPaymentNumber sets the payment number
func (b *PaymentBuilder) WithPaymentNumber(number string) *PaymentBuilder {
	b.payment.PaymentNumber = number
	return b
}

// WithMethod sets the payment method
func (b *PaymentBuilder) WithMethod(method PaymentMethod) *PaymentBuilder {
	b.payment.PaymentMethod = method
	return b
}

// WithType sets the payment type
func (b *PaymentBuilder) WithType(pType PaymentType) *PaymentBuilder {
	b.payment.PaymentType = pType
	return b
}

// WithSourceAccount sets the source account details
func (b *PaymentBuilder) WithSourceAccount(last4, routingLast4, bankName string) *PaymentBuilder {
	b.payment.SourceAccountLast4 = &last4
	b.payment.SourceRoutingLast4 = &routingLast4
	b.payment.SourceBankName = &bankName
	return b
}

// WithScheduledDate sets a future scheduled date
func (b *PaymentBuilder) WithScheduledDate(date time.Time) *PaymentBuilder {
	b.payment.ScheduledDate = &date
	b.payment.EffectiveDate = date
	return b
}

// WithBillingCycle links to a billing cycle
func (b *PaymentBuilder) WithBillingCycle(cycleID uuid.UUID) *PaymentBuilder {
	b.payment.BillingCycleID = &cycleID
	return b
}

// WithCreatedBy sets who created the payment
func (b *PaymentBuilder) WithCreatedBy(user string) *PaymentBuilder {
	b.payment.CreatedBy = &user
	return b
}

// Build creates the payment
func (b *PaymentBuilder) Build() *Payment {
	return b.payment
}
