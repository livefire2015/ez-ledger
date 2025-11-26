package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// BillingCycleType represents the billing period frequency
type BillingCycleType string

const (
	BillingCycleMonthly   BillingCycleType = "monthly"
	BillingCycleQuarterly BillingCycleType = "quarterly"
)

// CreditCardStatus represents the status of a credit card account
type CreditCardStatus string

const (
	CreditCardStatusActive    CreditCardStatus = "active"
	CreditCardStatusFrozen    CreditCardStatus = "frozen"
	CreditCardStatusClosed    CreditCardStatus = "closed"
	CreditCardStatusDelinquent CreditCardStatus = "delinquent"
)

// CreditCard represents a revolving credit card account
// Follows GAAP principles for liability accounting
type CreditCard struct {
	ID       uuid.UUID `json:"id" db:"id"`
	TenantID uuid.UUID `json:"tenant_id" db:"tenant_id"`

	// Card identification
	CardNumber     string `json:"card_number" db:"card_number"`           // Masked/last 4 digits
	CardholderName string `json:"cardholder_name" db:"cardholder_name"`

	// Credit limits
	CreditLimit     decimal.Decimal `json:"credit_limit" db:"credit_limit"`
	AvailableCredit decimal.Decimal `json:"available_credit" db:"available_credit"`

	// Interest rates (Annual Percentage Rate)
	PurchaseAPR       decimal.Decimal `json:"purchase_apr" db:"purchase_apr"`               // Standard purchase APR
	CashAdvanceAPR    decimal.Decimal `json:"cash_advance_apr" db:"cash_advance_apr"`       // Cash advance APR (typically higher)
	PenaltyAPR        decimal.Decimal `json:"penalty_apr" db:"penalty_apr"`                 // Penalty APR for late payments
	IntroductoryAPR   decimal.Decimal `json:"introductory_apr" db:"introductory_apr"`       // Promotional APR
	IntroductoryEndDate *time.Time    `json:"introductory_end_date" db:"introductory_end_date"`

	// Fee configuration
	AnnualFee              decimal.Decimal `json:"annual_fee" db:"annual_fee"`
	LatePaymentFee         decimal.Decimal `json:"late_payment_fee" db:"late_payment_fee"`
	FailedPaymentFee       decimal.Decimal `json:"failed_payment_fee" db:"failed_payment_fee"`
	InternationalFeeRate   decimal.Decimal `json:"international_fee_rate" db:"international_fee_rate"` // Percentage of transaction
	CashAdvanceFee         decimal.Decimal `json:"cash_advance_fee" db:"cash_advance_fee"`             // Flat fee or percentage
	CashAdvanceFeeRate     decimal.Decimal `json:"cash_advance_fee_rate" db:"cash_advance_fee_rate"`   // Percentage rate
	OverLimitFee           decimal.Decimal `json:"over_limit_fee" db:"over_limit_fee"`

	// Billing configuration
	BillingCycleType       BillingCycleType `json:"billing_cycle_type" db:"billing_cycle_type"`
	BillingCycleDay        int              `json:"billing_cycle_day" db:"billing_cycle_day"`       // Day of month (1-28)
	PaymentDueDays         int              `json:"payment_due_days" db:"payment_due_days"`         // Days after statement
	GracePeriodDays        int              `json:"grace_period_days" db:"grace_period_days"`       // Days before interest accrues
	MinimumPaymentPercent  decimal.Decimal  `json:"minimum_payment_percent" db:"minimum_payment_percent"`
	MinimumPaymentAmount   decimal.Decimal  `json:"minimum_payment_amount" db:"minimum_payment_amount"` // Minimum fixed amount

	// Cashback configuration
	CashbackEnabled       bool            `json:"cashback_enabled" db:"cashback_enabled"`
	CashbackRate          decimal.Decimal `json:"cashback_rate" db:"cashback_rate"`                     // Default cashback rate
	CashbackRedemptionMin decimal.Decimal `json:"cashback_redemption_min" db:"cashback_redemption_min"` // Min amount to redeem

	// Status and tracking
	Status             CreditCardStatus `json:"status" db:"status"`
	LastStatementDate  *time.Time       `json:"last_statement_date" db:"last_statement_date"`
	NextStatementDate  *time.Time       `json:"next_statement_date" db:"next_statement_date"`
	LastPaymentDate    *time.Time       `json:"last_payment_date" db:"last_payment_date"`
	LastPaymentAmount  decimal.Decimal  `json:"last_payment_amount" db:"last_payment_amount"`
	ConsecutiveLateCount int            `json:"consecutive_late_count" db:"consecutive_late_count"`

	// Audit fields
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty" db:"closed_at"`
}

// Validation errors
var (
	ErrInvalidCreditLimit      = errors.New("credit limit must be positive")
	ErrInvalidAPR              = errors.New("APR must be between 0 and 100")
	ErrInvalidBillingCycleDay  = errors.New("billing cycle day must be between 1 and 28")
	ErrInvalidMinimumPayment   = errors.New("minimum payment percent must be between 0 and 100")
	ErrCardFrozen              = errors.New("card is frozen")
	ErrCardClosed              = errors.New("card is closed")
	ErrInsufficientCredit      = errors.New("insufficient available credit")
	ErrExceedsCreditLimit      = errors.New("transaction exceeds credit limit")
)

// Validate validates the credit card configuration
func (c *CreditCard) Validate() error {
	if c.CreditLimit.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidCreditLimit
	}

	hundred := decimal.NewFromInt(100)
	if c.PurchaseAPR.LessThan(decimal.Zero) || c.PurchaseAPR.GreaterThan(hundred) {
		return ErrInvalidAPR
	}

	if c.BillingCycleDay < 1 || c.BillingCycleDay > 28 {
		return ErrInvalidBillingCycleDay
	}

	if c.MinimumPaymentPercent.LessThan(decimal.Zero) || c.MinimumPaymentPercent.GreaterThan(hundred) {
		return ErrInvalidMinimumPayment
	}

	return nil
}

// CanTransact checks if the card can process transactions
func (c *CreditCard) CanTransact() error {
	switch c.Status {
	case CreditCardStatusFrozen:
		return ErrCardFrozen
	case CreditCardStatusClosed:
		return ErrCardClosed
	}
	return nil
}

// HasAvailableCredit checks if there's enough available credit
func (c *CreditCard) HasAvailableCredit(amount decimal.Decimal) error {
	if amount.GreaterThan(c.AvailableCredit) {
		return ErrInsufficientCredit
	}
	return nil
}

// GetEffectiveAPR returns the current effective APR based on account status
func (c *CreditCard) GetEffectiveAPR(now time.Time) decimal.Decimal {
	// Check for penalty APR due to delinquency
	if c.Status == CreditCardStatusDelinquent {
		return c.PenaltyAPR
	}

	// Check for introductory APR
	if c.IntroductoryEndDate != nil && now.Before(*c.IntroductoryEndDate) {
		return c.IntroductoryAPR
	}

	return c.PurchaseAPR
}

// GetDailyPeriodicRate converts APR to daily rate for interest calculations
// DPR = APR / 365 (per GAAP standard calculation)
func (c *CreditCard) GetDailyPeriodicRate(apr decimal.Decimal) decimal.Decimal {
	daysInYear := decimal.NewFromInt(365)
	return apr.Div(daysInYear).Div(decimal.NewFromInt(100))
}

// CalculateMinimumPayment calculates the minimum payment due
// Returns the greater of: percentage of balance OR fixed minimum amount
func (c *CreditCard) CalculateMinimumPayment(statementBalance decimal.Decimal) decimal.Decimal {
	if statementBalance.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	percentMin := statementBalance.Mul(c.MinimumPaymentPercent).Div(decimal.NewFromInt(100))

	if percentMin.LessThan(c.MinimumPaymentAmount) {
		// If balance is less than minimum fixed amount, return full balance
		if statementBalance.LessThan(c.MinimumPaymentAmount) {
			return statementBalance
		}
		return c.MinimumPaymentAmount
	}

	return percentMin
}

// CalculateInternationalFee calculates the foreign transaction fee
func (c *CreditCard) CalculateInternationalFee(transactionAmount decimal.Decimal) decimal.Decimal {
	return transactionAmount.Mul(c.InternationalFeeRate).Div(decimal.NewFromInt(100))
}

// CalculateCashAdvanceFee calculates the cash advance fee
// Returns the greater of: flat fee OR percentage of amount
func (c *CreditCard) CalculateCashAdvanceFee(amount decimal.Decimal) decimal.Decimal {
	percentFee := amount.Mul(c.CashAdvanceFeeRate).Div(decimal.NewFromInt(100))

	if percentFee.LessThan(c.CashAdvanceFee) {
		return c.CashAdvanceFee
	}
	return percentFee
}

// GetNextBillingPeriod calculates the next billing period dates
func (c *CreditCard) GetNextBillingPeriod(fromDate time.Time) (startDate, endDate, dueDate time.Time) {
	year, month, _ := fromDate.Date()

	switch c.BillingCycleType {
	case BillingCycleQuarterly:
		// Find the next quarter boundary
		quarterMonth := ((int(month)-1)/3)*3 + 1
		startDate = time.Date(year, time.Month(quarterMonth), c.BillingCycleDay, 0, 0, 0, 0, fromDate.Location())
		if startDate.Before(fromDate) {
			startDate = startDate.AddDate(0, 3, 0)
		}
		endDate = startDate.AddDate(0, 3, -1)

	default: // Monthly
		startDate = time.Date(year, month, c.BillingCycleDay, 0, 0, 0, 0, fromDate.Location())
		if startDate.Before(fromDate) {
			startDate = startDate.AddDate(0, 1, 0)
		}
		endDate = startDate.AddDate(0, 1, -1)
	}

	dueDate = endDate.AddDate(0, 0, c.PaymentDueDays)
	return startDate, endDate, dueDate
}

// IsInGracePeriod checks if current date is within grace period
func (c *CreditCard) IsInGracePeriod(statementDate, currentDate time.Time) bool {
	gracePeriodEnd := statementDate.AddDate(0, 0, c.GracePeriodDays)
	return currentDate.Before(gracePeriodEnd) || currentDate.Equal(gracePeriodEnd)
}

// CreditCardDefaults provides sensible default values for a new credit card
func CreditCardDefaults() CreditCard {
	return CreditCard{
		PurchaseAPR:           decimal.NewFromFloat(19.99),
		CashAdvanceAPR:        decimal.NewFromFloat(24.99),
		PenaltyAPR:            decimal.NewFromFloat(29.99),
		IntroductoryAPR:       decimal.Zero,
		LatePaymentFee:        decimal.NewFromInt(35),
		FailedPaymentFee:      decimal.NewFromInt(35),
		InternationalFeeRate:  decimal.NewFromFloat(3.0),
		CashAdvanceFee:        decimal.NewFromInt(10),
		CashAdvanceFeeRate:    decimal.NewFromFloat(5.0),
		OverLimitFee:          decimal.NewFromInt(35),
		BillingCycleType:      BillingCycleMonthly,
		BillingCycleDay:       1,
		PaymentDueDays:        25,
		GracePeriodDays:       21,
		MinimumPaymentPercent: decimal.NewFromFloat(2.0),
		MinimumPaymentAmount:  decimal.NewFromInt(25),
		CashbackEnabled:       true,
		CashbackRate:          decimal.NewFromFloat(1.5),
		CashbackRedemptionMin: decimal.NewFromInt(25),
		Status:                CreditCardStatusActive,
	}
}
