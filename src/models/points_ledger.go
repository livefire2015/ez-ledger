package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PointsEntryType represents the type of points ledger entry
type PointsEntryType string

const (
	PointsEarnedTransaction PointsEntryType = "earned_transaction"
	PointsEarnedRefund      PointsEntryType = "earned_refund"
	PointsRedeemedSpent     PointsEntryType = "redeemed_spent"
	PointsRedeemedCancelled PointsEntryType = "redeemed_cancelled"
	PointsRedeemedRefunded  PointsEntryType = "redeemed_refunded"
	PointsAdjustment        PointsEntryType = "adjustment"
)

// PointsLedgerEntry represents a single entry in the points ledger
// This is an immutable event log entry
type PointsLedgerEntry struct {
	ID                 uuid.UUID       `json:"id" db:"id"`
	TenantID           uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	StatementEntryID   *uuid.UUID      `json:"statement_entry_id,omitempty" db:"statement_entry_id"`

	// Entry details
	EntryType          PointsEntryType `json:"entry_type" db:"entry_type"`
	EntryDate          time.Time       `json:"entry_date" db:"entry_date"`

	// Points (positive = earned, negative = redeemed)
	Points             int             `json:"points" db:"points"`

	// Description and metadata
	Description        string          `json:"description" db:"description"`

	// External platform tracking (e.g., Keystone)
	ExternalPlatform   *string         `json:"external_platform,omitempty" db:"external_platform"`
	ExternalReferenceID *string        `json:"external_reference_id,omitempty" db:"external_reference_id"`

	// Transaction details (for earning points)
	TransactionAmount  *decimal.Decimal `json:"transaction_amount,omitempty" db:"transaction_amount"`
	PointsRate         *decimal.Decimal `json:"points_rate,omitempty" db:"points_rate"`

	Metadata           map[string]interface{} `json:"metadata,omitempty" db:"metadata"`

	// Audit
	CreatedAt          time.Time       `json:"created_at" db:"created_at"`
	CreatedBy          *string         `json:"created_by,omitempty" db:"created_by"`
}

// IsEarning returns true if the entry represents earning points
func (p *PointsLedgerEntry) IsEarning() bool {
	switch p.EntryType {
	case PointsEarnedTransaction, PointsEarnedRefund:
		return true
	default:
		return false
	}
}

// IsRedemption returns true if the entry represents redeeming points
func (p *PointsLedgerEntry) IsRedemption() bool {
	switch p.EntryType {
	case PointsRedeemedSpent, PointsRedeemedCancelled, PointsRedeemedRefunded:
		return true
	default:
		return false
	}
}

// GetSignedPoints returns points with proper sign for balance calculation
// Positive = earned, Negative = redeemed
func (p *PointsLedgerEntry) GetSignedPoints() int {
	return p.Points
}

// PointsBalance represents the current points balance for a tenant
type PointsBalance struct {
	TenantID         uuid.UUID `json:"tenant_id" db:"tenant_id"`
	EarnedPoints     int       `json:"earned_points" db:"earned_points"`
	RedeemedPoints   int       `json:"redeemed_points" db:"redeemed_points"`
	AvailablePoints  int       `json:"available_points" db:"available_points"`
	TotalEntries     int       `json:"total_entries" db:"total_entries"`
	LastActivityDate time.Time `json:"last_activity_date" db:"last_activity_date"`
}

// PointsEarningRule defines how points are earned from transactions
type PointsEarningRule struct {
	PointsPerDollar decimal.Decimal // e.g., 0.01 = 1 point per dollar
	MinAmount       decimal.Decimal // Minimum transaction amount to earn points
	MaxPoints       *int            // Maximum points per transaction (optional)
}

// CalculatePointsEarned calculates points earned from a transaction amount
func (r *PointsEarningRule) CalculatePointsEarned(amount decimal.Decimal) int {
	if amount.LessThan(r.MinAmount) {
		return 0
	}

	points := amount.Mul(r.PointsPerDollar).IntPart()

	if r.MaxPoints != nil && points > int64(*r.MaxPoints) {
		return *r.MaxPoints
	}

	return int(points)
}
