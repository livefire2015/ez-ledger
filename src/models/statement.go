package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Statement represents a billing period statement
type Statement struct {
	ID               uuid.UUID       `json:"id" db:"id"`
	TenantID         uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	StatementNumber  string          `json:"statement_number" db:"statement_number"`

	// Billing period
	BillingStartDate time.Time       `json:"billing_start_date" db:"billing_start_date"`
	BillingEndDate   time.Time       `json:"billing_end_date" db:"billing_end_date"`
	DueDate          time.Time       `json:"due_date" db:"due_date"`

	// Balances
	PreviousBalance   decimal.Decimal `json:"previous_balance" db:"previous_balance"`
	ClearedPayments   decimal.Decimal `json:"cleared_payments" db:"cleared_payments"`
	OpeningBalance    decimal.Decimal `json:"opening_balance" db:"opening_balance"`
	StatementBalance  decimal.Decimal `json:"statement_balance" db:"statement_balance"`
	MinimumPayment    decimal.Decimal `json:"minimum_payment" db:"minimum_payment"`

	// Status
	Status       StatementStatus `json:"status" db:"status"`
	FinalizedAt  *time.Time      `json:"finalized_at,omitempty" db:"finalized_at"`

	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`
}

// StatementStatus represents valid statement statuses
type StatementStatus string

const (
	StatementStatusDraft     StatementStatus = "draft"
	StatementStatusFinalized StatementStatus = "finalized"
	StatementStatusClosed    StatementStatus = "closed"
)

// CalculateOpeningBalance calculates the opening balance
// Formula: Previous Balance - Cleared Payments
func (s *Statement) CalculateOpeningBalance() decimal.Decimal {
	return s.PreviousBalance.Sub(s.ClearedPayments)
}

// CalculateMinimumPayment calculates the minimum payment required
// Formula: Statement Balance * Minimum Payment Percentage
func (s *Statement) CalculateMinimumPayment(minPercentage decimal.Decimal) decimal.Decimal {
	return s.StatementBalance.Mul(minPercentage)
}
