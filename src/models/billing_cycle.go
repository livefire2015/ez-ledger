package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// BillingCycleStatus represents the status of a billing cycle
type BillingCycleStatus string

const (
	BillingCycleStatusOpen      BillingCycleStatus = "open"       // Currently active, accepting transactions
	BillingCycleStatusClosed    BillingCycleStatus = "closed"     // Statement generated, awaiting payment
	BillingCycleStatusPaid      BillingCycleStatus = "paid"       // Minimum payment received
	BillingCycleStatusPaidFull  BillingCycleStatus = "paid_full"  // Full balance paid
	BillingCycleStatusPastDue   BillingCycleStatus = "past_due"   // Payment not received by due date
	BillingCycleStatusDelinquent BillingCycleStatus = "delinquent" // Severely past due
)

// BillingCycle represents a billing period for a credit card account
// Follows GAAP principles for accrual-based accounting
type BillingCycle struct {
	ID           uuid.UUID `json:"id" db:"id"`
	CreditCardID uuid.UUID `json:"credit_card_id" db:"credit_card_id"`
	TenantID     uuid.UUID `json:"tenant_id" db:"tenant_id"`

	// Cycle identification
	CycleNumber int              `json:"cycle_number" db:"cycle_number"` // Sequential cycle number
	CycleType   BillingCycleType `json:"cycle_type" db:"cycle_type"`     // monthly or quarterly

	// Date boundaries
	CycleStartDate time.Time `json:"cycle_start_date" db:"cycle_start_date"`
	CycleEndDate   time.Time `json:"cycle_end_date" db:"cycle_end_date"`
	StatementDate  time.Time `json:"statement_date" db:"statement_date"` // When statement was generated
	DueDate        time.Time `json:"due_date" db:"due_date"`
	GracePeriodEnd time.Time `json:"grace_period_end" db:"grace_period_end"`

	// Balance components (GAAP compliant breakdown)
	PreviousBalance      decimal.Decimal `json:"previous_balance" db:"previous_balance"`           // Carried from previous cycle
	PaymentsReceived     decimal.Decimal `json:"payments_received" db:"payments_received"`         // Total payments this cycle
	PurchasesAmount      decimal.Decimal `json:"purchases_amount" db:"purchases_amount"`           // New purchases
	CashAdvancesAmount   decimal.Decimal `json:"cash_advances_amount" db:"cash_advances_amount"`   // Cash advances
	RefundsAmount        decimal.Decimal `json:"refunds_amount" db:"refunds_amount"`               // Credits/refunds
	FeesAmount           decimal.Decimal `json:"fees_amount" db:"fees_amount"`                     // All fees charged
	InterestAmount       decimal.Decimal `json:"interest_amount" db:"interest_amount"`             // Interest charges
	AdjustmentsAmount    decimal.Decimal `json:"adjustments_amount" db:"adjustments_amount"`       // Manual adjustments
	CashbackEarned       decimal.Decimal `json:"cashback_earned" db:"cashback_earned"`             // Cashback this cycle
	CashbackRedeemed     decimal.Decimal `json:"cashback_redeemed" db:"cashback_redeemed"`         // Cashback applied to balance

	// Calculated totals
	NewBalance     decimal.Decimal `json:"new_balance" db:"new_balance"`         // Statement balance
	MinimumPayment decimal.Decimal `json:"minimum_payment" db:"minimum_payment"` // Minimum due

	// Interest calculation details
	AverageDailyBalance decimal.Decimal `json:"average_daily_balance" db:"average_daily_balance"` // For interest calc
	DaysInCycle         int             `json:"days_in_cycle" db:"days_in_cycle"`
	APRApplied          decimal.Decimal `json:"apr_applied" db:"apr_applied"` // The APR used for this cycle

	// Payment tracking
	PaymentsMade       decimal.Decimal `json:"payments_made" db:"payments_made"`             // Payments toward this statement
	LastPaymentDate    *time.Time      `json:"last_payment_date" db:"last_payment_date"`
	LastPaymentAmount  decimal.Decimal `json:"last_payment_amount" db:"last_payment_amount"`
	MinimumPaymentMet  bool            `json:"minimum_payment_met" db:"minimum_payment_met"`

	// Status
	Status BillingCycleStatus `json:"status" db:"status"`

	// Audit
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" db:"updated_at"`
	ClosedAt   *time.Time `json:"closed_at,omitempty" db:"closed_at"`
}

// CalculateNewBalance computes the statement balance per GAAP
// Formula: Previous Balance - Payments + Purchases + Cash Advances - Refunds + Fees + Interest + Adjustments - Cashback Redeemed
func (bc *BillingCycle) CalculateNewBalance() decimal.Decimal {
	balance := bc.PreviousBalance.
		Sub(bc.PaymentsReceived).
		Add(bc.PurchasesAmount).
		Add(bc.CashAdvancesAmount).
		Sub(bc.RefundsAmount).
		Add(bc.FeesAmount).
		Add(bc.InterestAmount).
		Add(bc.AdjustmentsAmount).
		Sub(bc.CashbackRedeemed)

	// Balance cannot be negative (credit balance)
	// In GAAP, negative balance represents overpayment (liability to customer)
	return balance
}

// GetRemainingBalance returns the balance still owed after payments
func (bc *BillingCycle) GetRemainingBalance() decimal.Decimal {
	return bc.NewBalance.Sub(bc.PaymentsMade)
}

// GetRemainingMinimum returns the remaining minimum payment due
func (bc *BillingCycle) GetRemainingMinimum() decimal.Decimal {
	remaining := bc.MinimumPayment.Sub(bc.PaymentsMade)
	if remaining.LessThan(decimal.Zero) {
		return decimal.Zero
	}
	return remaining
}

// IsOverdue checks if the billing cycle is past due
func (bc *BillingCycle) IsOverdue(currentDate time.Time) bool {
	return currentDate.After(bc.DueDate) && !bc.MinimumPaymentMet
}

// DaysOverdue returns the number of days past the due date
func (bc *BillingCycle) DaysOverdue(currentDate time.Time) int {
	if !bc.IsOverdue(currentDate) {
		return 0
	}
	return int(currentDate.Sub(bc.DueDate).Hours() / 24)
}

// IsPaidInFull checks if the full statement balance has been paid
func (bc *BillingCycle) IsPaidInFull() bool {
	return bc.PaymentsMade.GreaterThanOrEqual(bc.NewBalance)
}

// DailyBalanceRecord represents a daily balance snapshot for interest calculation
type DailyBalanceRecord struct {
	Date    time.Time       `json:"date" db:"date"`
	Balance decimal.Decimal `json:"balance" db:"balance"`
}

// CalculateAverageDailyBalance computes ADB from daily balance records
// ADB = Sum of daily balances / Number of days
// This is the standard method for credit card interest calculation per GAAP
func CalculateAverageDailyBalance(records []DailyBalanceRecord) decimal.Decimal {
	if len(records) == 0 {
		return decimal.Zero
	}

	var totalBalance decimal.Decimal
	for _, record := range records {
		totalBalance = totalBalance.Add(record.Balance)
	}

	days := decimal.NewFromInt(int64(len(records)))
	return totalBalance.Div(days).Round(2)
}

// BillingCycleSummary provides a summary view of a billing cycle
type BillingCycleSummary struct {
	CycleID         uuid.UUID          `json:"cycle_id"`
	CycleNumber     int                `json:"cycle_number"`
	CycleType       BillingCycleType   `json:"cycle_type"`
	StartDate       time.Time          `json:"start_date"`
	EndDate         time.Time          `json:"end_date"`
	DueDate         time.Time          `json:"due_date"`
	StatementBalance decimal.Decimal   `json:"statement_balance"`
	MinimumPayment  decimal.Decimal    `json:"minimum_payment"`
	PaymentsMade    decimal.Decimal    `json:"payments_made"`
	RemainingBalance decimal.Decimal   `json:"remaining_balance"`
	Status          BillingCycleStatus `json:"status"`
	DaysUntilDue    int                `json:"days_until_due"`
	DaysOverdue     int                `json:"days_overdue"`
}

// ToSummary converts a BillingCycle to a BillingCycleSummary
func (bc *BillingCycle) ToSummary(currentDate time.Time) BillingCycleSummary {
	summary := BillingCycleSummary{
		CycleID:          bc.ID,
		CycleNumber:      bc.CycleNumber,
		CycleType:        bc.CycleType,
		StartDate:        bc.CycleStartDate,
		EndDate:          bc.CycleEndDate,
		DueDate:          bc.DueDate,
		StatementBalance: bc.NewBalance,
		MinimumPayment:   bc.MinimumPayment,
		PaymentsMade:     bc.PaymentsMade,
		RemainingBalance: bc.GetRemainingBalance(),
		Status:           bc.Status,
	}

	if currentDate.Before(bc.DueDate) {
		summary.DaysUntilDue = int(bc.DueDate.Sub(currentDate).Hours() / 24)
	} else {
		summary.DaysOverdue = bc.DaysOverdue(currentDate)
	}

	return summary
}

// BillingCycleBuilder helps construct billing cycles
type BillingCycleBuilder struct {
	cycle *BillingCycle
}

// NewBillingCycleBuilder creates a new builder
func NewBillingCycleBuilder() *BillingCycleBuilder {
	return &BillingCycleBuilder{
		cycle: &BillingCycle{
			ID:        uuid.New(),
			Status:    BillingCycleStatusOpen,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
}

// WithCreditCard sets the credit card and tenant
func (b *BillingCycleBuilder) WithCreditCard(card *CreditCard) *BillingCycleBuilder {
	b.cycle.CreditCardID = card.ID
	b.cycle.TenantID = card.TenantID
	b.cycle.CycleType = card.BillingCycleType
	return b
}

// WithCycleNumber sets the cycle number
func (b *BillingCycleBuilder) WithCycleNumber(number int) *BillingCycleBuilder {
	b.cycle.CycleNumber = number
	return b
}

// WithDateRange sets the cycle date boundaries
func (b *BillingCycleBuilder) WithDateRange(start, end, due, graceEnd time.Time) *BillingCycleBuilder {
	b.cycle.CycleStartDate = start
	b.cycle.CycleEndDate = end
	b.cycle.DueDate = due
	b.cycle.GracePeriodEnd = graceEnd
	b.cycle.DaysInCycle = int(end.Sub(start).Hours() / 24) + 1
	return b
}

// WithPreviousBalance sets the carried balance from previous cycle
func (b *BillingCycleBuilder) WithPreviousBalance(balance decimal.Decimal) *BillingCycleBuilder {
	b.cycle.PreviousBalance = balance
	return b
}

// WithAPR sets the APR for interest calculation
func (b *BillingCycleBuilder) WithAPR(apr decimal.Decimal) *BillingCycleBuilder {
	b.cycle.APRApplied = apr
	return b
}

// Build creates the billing cycle
func (b *BillingCycleBuilder) Build() *BillingCycle {
	return b.cycle
}
