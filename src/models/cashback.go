package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CashbackEntryType represents the type of cashback entry
type CashbackEntryType string

const (
	CashbackEarned             CashbackEntryType = "earned"              // Cashback earned from transaction
	CashbackEarnedRefund       CashbackEntryType = "earned_refund"       // Cashback adjustment from refund
	CashbackRedeemed           CashbackEntryType = "redeemed"            // Cashback applied to statement
	CashbackRedeemedCancelled  CashbackEntryType = "redeemed_cancelled"  // Redemption cancelled
	CashbackExpired            CashbackEntryType = "expired"             // Expired cashback (if applicable)
	CashbackAdjustment         CashbackEntryType = "adjustment"          // Manual adjustment
)

// CashbackLedgerEntry represents a single entry in the cashback ledger
// Uses event sourcing - entries are immutable
type CashbackLedgerEntry struct {
	ID               uuid.UUID          `json:"id" db:"id"`
	TenantID         uuid.UUID          `json:"tenant_id" db:"tenant_id"`
	CreditCardID     uuid.UUID          `json:"credit_card_id" db:"credit_card_id"`
	StatementEntryID *uuid.UUID         `json:"statement_entry_id,omitempty" db:"statement_entry_id"`

	// Entry details
	EntryType CashbackEntryType `json:"entry_type" db:"entry_type"`
	EntryDate time.Time         `json:"entry_date" db:"entry_date"`

	// Amount (positive = earned, negative = redeemed/expired)
	Amount decimal.Decimal `json:"amount" db:"amount"`

	// Description and reference
	Description string  `json:"description" db:"description"`
	ReferenceID *string `json:"reference_id,omitempty" db:"reference_id"`

	// Calculation details (for earned entries)
	TransactionAmount *decimal.Decimal `json:"transaction_amount,omitempty" db:"transaction_amount"`
	CashbackRate      *decimal.Decimal `json:"cashback_rate,omitempty" db:"cashback_rate"`
	CategoryBonus     *decimal.Decimal `json:"category_bonus,omitempty" db:"category_bonus"`

	// Metadata for additional context
	Metadata map[string]interface{} `json:"metadata,omitempty" db:"metadata"`

	// Audit
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	CreatedBy *string   `json:"created_by,omitempty" db:"created_by"`
}

// IsEarning returns true if the entry represents earning cashback
func (c *CashbackLedgerEntry) IsEarning() bool {
	return c.EntryType == CashbackEarned || c.EntryType == CashbackRedeemedCancelled
}

// IsDeduction returns true if the entry represents a cashback deduction
func (c *CashbackLedgerEntry) IsDeduction() bool {
	return c.EntryType == CashbackRedeemed ||
		c.EntryType == CashbackEarnedRefund ||
		c.EntryType == CashbackExpired
}

// GetSignedAmount returns the amount with proper sign for balance calculation
func (c *CashbackLedgerEntry) GetSignedAmount() decimal.Decimal {
	return c.Amount // Amount is already signed appropriately
}

// CashbackBalance represents the current cashback balance for a card
type CashbackBalance struct {
	TenantID          uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	CreditCardID      uuid.UUID       `json:"credit_card_id" db:"credit_card_id"`
	EarnedTotal       decimal.Decimal `json:"earned_total" db:"earned_total"`
	RedeemedTotal     decimal.Decimal `json:"redeemed_total" db:"redeemed_total"`
	ExpiredTotal      decimal.Decimal `json:"expired_total" db:"expired_total"`
	AvailableBalance  decimal.Decimal `json:"available_balance" db:"available_balance"`
	PendingBalance    decimal.Decimal `json:"pending_balance" db:"pending_balance"` // Not yet cleared
	TotalEntries      int             `json:"total_entries" db:"total_entries"`
	LastActivityDate  time.Time       `json:"last_activity_date" db:"last_activity_date"`
}

// CashbackCategory represents a category with bonus cashback rate
type CashbackCategory struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	CreditCardID uuid.UUID       `json:"credit_card_id" db:"credit_card_id"`
	CategoryCode string          `json:"category_code" db:"category_code"` // MCC code or category name
	CategoryName string          `json:"category_name" db:"category_name"`
	BonusRate    decimal.Decimal `json:"bonus_rate" db:"bonus_rate"`       // e.g., 3% for restaurants
	MaxBonus     *decimal.Decimal `json:"max_bonus,omitempty" db:"max_bonus"` // Cap per period
	IsActive     bool            `json:"is_active" db:"is_active"`
	StartDate    *time.Time      `json:"start_date,omitempty" db:"start_date"`
	EndDate      *time.Time      `json:"end_date,omitempty" db:"end_date"`
}

// IsActiveOn checks if the category bonus is active on a given date
func (c *CashbackCategory) IsActiveOn(date time.Time) bool {
	if !c.IsActive {
		return false
	}
	if c.StartDate != nil && date.Before(*c.StartDate) {
		return false
	}
	if c.EndDate != nil && date.After(*c.EndDate) {
		return false
	}
	return true
}

// CashbackEarningRule defines how cashback is calculated
type CashbackEarningRule struct {
	BaseRate         decimal.Decimal             // Default cashback rate (e.g., 1.5%)
	MinTransaction   decimal.Decimal             // Minimum transaction to earn cashback
	MaxCashbackPerTx *decimal.Decimal            // Cap per transaction (optional)
	CategoryRules    map[string]CashbackCategory // Category-specific rules
}

// CalculateCashback calculates cashback for a transaction
func (r *CashbackEarningRule) CalculateCashback(
	amount decimal.Decimal,
	categoryCode string,
	txDate time.Time,
) (cashback decimal.Decimal, rate decimal.Decimal) {
	// Check minimum transaction amount
	if amount.LessThan(r.MinTransaction) {
		return decimal.Zero, decimal.Zero
	}

	// Determine effective rate (base or category bonus)
	rate = r.BaseRate
	if cat, ok := r.CategoryRules[categoryCode]; ok && cat.IsActiveOn(txDate) {
		if cat.BonusRate.GreaterThan(rate) {
			rate = cat.BonusRate
		}
	}

	// Calculate cashback
	cashback = amount.Mul(rate).Div(decimal.NewFromInt(100)).Round(2)

	// Apply per-transaction cap if configured
	if r.MaxCashbackPerTx != nil && cashback.GreaterThan(*r.MaxCashbackPerTx) {
		cashback = *r.MaxCashbackPerTx
	}

	return cashback, rate
}

// CashbackRedemptionRequest represents a request to redeem cashback
type CashbackRedemptionRequest struct {
	CreditCardID uuid.UUID       `json:"credit_card_id"`
	Amount       decimal.Decimal `json:"amount"`
	RedeemAsType string          `json:"redeem_as_type"` // "statement_credit", "check", "direct_deposit"
}

// CashbackStatement represents cashback earned during a billing cycle
type CashbackStatement struct {
	BillingCycleID    uuid.UUID       `json:"billing_cycle_id"`
	CreditCardID      uuid.UUID       `json:"credit_card_id"`
	CycleStartDate    time.Time       `json:"cycle_start_date"`
	CycleEndDate      time.Time       `json:"cycle_end_date"`
	TotalPurchases    decimal.Decimal `json:"total_purchases"`
	CashbackEarned    decimal.Decimal `json:"cashback_earned"`
	CashbackRedeemed  decimal.Decimal `json:"cashback_redeemed"`
	EndingBalance     decimal.Decimal `json:"ending_balance"`
	EffectiveRate     decimal.Decimal `json:"effective_rate"` // Actual rate earned this cycle
	CategoryBreakdown []CategoryCashbackSummary `json:"category_breakdown"`
}

// CategoryCashbackSummary summarizes cashback by category
type CategoryCashbackSummary struct {
	CategoryCode    string          `json:"category_code"`
	CategoryName    string          `json:"category_name"`
	TransactionCount int            `json:"transaction_count"`
	TotalSpent      decimal.Decimal `json:"total_spent"`
	CashbackEarned  decimal.Decimal `json:"cashback_earned"`
	EffectiveRate   decimal.Decimal `json:"effective_rate"`
}

// DefaultCashbackRule returns a standard cashback earning rule
func DefaultCashbackRule(baseRate decimal.Decimal) CashbackEarningRule {
	return CashbackEarningRule{
		BaseRate:       baseRate,
		MinTransaction: decimal.NewFromFloat(0.01),
		CategoryRules:  make(map[string]CashbackCategory),
	}
}
