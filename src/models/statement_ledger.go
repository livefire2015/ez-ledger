package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// StatementEntryType represents the type of statement ledger entry
type StatementEntryType string

const (
	EntryTypeTransaction       StatementEntryType = "transaction"
	EntryTypePayment           StatementEntryType = "payment"
	EntryTypeRefund            StatementEntryType = "refund"
	EntryTypeReward            StatementEntryType = "reward"
	EntryTypeReturnedReward    StatementEntryType = "returned_reward"
	EntryTypeFeeLate           StatementEntryType = "fee_late"
	EntryTypeFeeFailed         StatementEntryType = "fee_failed"
	EntryTypeFeeInternational  StatementEntryType = "fee_international"
	EntryTypeFeeInterest       StatementEntryType = "fee_interest"
	EntryTypeFeeOverLimit      StatementEntryType = "fee_over_limit"
	EntryTypeFeeAnnual         StatementEntryType = "fee_annual"
	EntryTypeFeeCashAdvance    StatementEntryType = "fee_cash_advance"
	EntryTypeCashAdvance       StatementEntryType = "cash_advance"
	EntryTypeCashbackEarned    StatementEntryType = "cashback_earned"
	EntryTypeCashbackRedeemed  StatementEntryType = "cashback_redeemed"
	EntryTypeAdjustment        StatementEntryType = "adjustment"
	EntryTypeCredit            StatementEntryType = "credit"
)

// StatementLedgerEntry represents a single entry in the statement ledger
// This is an immutable event log entry
type StatementLedgerEntry struct {
	ID          uuid.UUID          `json:"id" db:"id"`
	TenantID    uuid.UUID          `json:"tenant_id" db:"tenant_id"`
	StatementID *uuid.UUID         `json:"statement_id,omitempty" db:"statement_id"`

	// Entry details
	EntryType   StatementEntryType `json:"entry_type" db:"entry_type"`
	EntryDate   time.Time          `json:"entry_date" db:"entry_date"`
	PostingDate time.Time          `json:"posting_date" db:"posting_date"`

	// Amount (positive = debit/charge, negative = credit/payment)
	Amount      decimal.Decimal    `json:"amount" db:"amount"`

	// Description and metadata
	Description string             `json:"description" db:"description"`
	ReferenceID *string            `json:"reference_id,omitempty" db:"reference_id"`
	Metadata    map[string]interface{} `json:"metadata,omitempty" db:"metadata"`

	// Status
	Status      EntryStatus        `json:"status" db:"status"`
	ClearedAt   *time.Time         `json:"cleared_at,omitempty" db:"cleared_at"`

	// Audit
	CreatedAt   time.Time          `json:"created_at" db:"created_at"`
	CreatedBy   *string            `json:"created_by,omitempty" db:"created_by"`
}

// EntryStatus represents the status of a ledger entry
type EntryStatus string

const (
	EntryStatusPending  EntryStatus = "pending"
	EntryStatusCleared  EntryStatus = "cleared"
	EntryStatusReversed EntryStatus = "reversed"
)

// IsDebit returns true if the entry increases the statement balance
func (e *StatementLedgerEntry) IsDebit() bool {
	switch e.EntryType {
	case EntryTypeTransaction, EntryTypeCashAdvance,
		EntryTypeFeeLate, EntryTypeFeeFailed, EntryTypeFeeInternational,
		EntryTypeFeeInterest, EntryTypeFeeOverLimit, EntryTypeFeeAnnual,
		EntryTypeFeeCashAdvance, EntryTypeReturnedReward:
		return true
	case EntryTypePayment, EntryTypeRefund, EntryTypeReward, EntryTypeCredit,
		EntryTypeCashbackRedeemed:
		return false
	case EntryTypeAdjustment, EntryTypeCashbackEarned:
		// Adjustments and cashback earned can be either debit or credit based on amount sign
		// Cashback earned is typically positive (credit to account) but tracked as earned
		return e.Amount.IsPositive()
	default:
		return false
	}
}

// GetSignedAmount returns the amount with proper sign for balance calculation
// Positive = increases statement balance (debit)
// Negative = decreases statement balance (credit)
func (e *StatementLedgerEntry) GetSignedAmount() decimal.Decimal {
	if e.IsDebit() {
		return e.Amount.Abs()
	}
	return e.Amount.Abs().Neg()
}

// StatementBalance represents the current balance for a tenant
type StatementBalance struct {
	TenantID          uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	CurrentBalance    decimal.Decimal `json:"current_balance" db:"current_balance"`
	TotalEntries      int             `json:"total_entries" db:"total_entries"`
	LastActivityDate  time.Time       `json:"last_activity_date" db:"last_activity_date"`
}
