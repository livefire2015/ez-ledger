package services

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/livefire2015/ez-ledger/src/models"
)

// PointsLedgerService handles points ledger operations
type PointsLedgerService struct {
	db *sql.DB
}

// NewPointsLedgerService creates a new points ledger service
func NewPointsLedgerService(db *sql.DB) *PointsLedgerService {
	return &PointsLedgerService{db: db}
}

// CreateEntry creates a new points ledger entry
func (s *PointsLedgerService) CreateEntry(ctx context.Context, entry *models.PointsLedgerEntry) error {
	query := `
		INSERT INTO points_ledger_entries (
			id, tenant_id, statement_entry_id, entry_type, entry_date,
			points, description, external_platform, external_reference_id,
			transaction_amount, points_rate, metadata, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}

	_, err := s.db.ExecContext(ctx, query,
		entry.ID,
		entry.TenantID,
		entry.StatementEntryID,
		entry.EntryType,
		entry.EntryDate,
		entry.Points,
		entry.Description,
		entry.ExternalPlatform,
		entry.ExternalReferenceID,
		entry.TransactionAmount,
		entry.PointsRate,
		entry.Metadata,
		entry.CreatedBy,
	)

	return err
}

// GetBalance calculates the current points balance for a tenant
func (s *PointsLedgerService) GetBalance(ctx context.Context, tenantID uuid.UUID) (*models.PointsBalance, error) {
	query := `SELECT * FROM points_balances WHERE tenant_id = $1`

	var balance models.PointsBalance
	err := s.db.QueryRowContext(ctx, query, tenantID).Scan(
		&balance.TenantID,
		&balance.EarnedPoints,
		&balance.RedeemedPoints,
		&balance.AvailablePoints,
		&balance.TotalEntries,
		&balance.LastActivityDate,
	)

	if err == sql.ErrNoRows {
		// No entries yet, return zero balance
		return &models.PointsBalance{
			TenantID:        tenantID,
			EarnedPoints:    0,
			RedeemedPoints:  0,
			AvailablePoints: 0,
			TotalEntries:    0,
		}, nil
	}

	if err != nil {
		return nil, err
	}

	return &balance, nil
}

// GetEntriesByTenant retrieves all points entries for a tenant
func (s *PointsLedgerService) GetEntriesByTenant(ctx context.Context, tenantID uuid.UUID, limit int) ([]*models.PointsLedgerEntry, error) {
	query := `
		SELECT id, tenant_id, statement_entry_id, entry_type, entry_date,
		       points, description, external_platform, external_reference_id,
		       transaction_amount, points_rate, metadata, created_at, created_by
		FROM points_ledger_entries
		WHERE tenant_id = $1
		ORDER BY entry_date DESC, created_at DESC
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*models.PointsLedgerEntry
	for rows.Next() {
		entry := &models.PointsLedgerEntry{}
		err := rows.Scan(
			&entry.ID,
			&entry.TenantID,
			&entry.StatementEntryID,
			&entry.EntryType,
			&entry.EntryDate,
			&entry.Points,
			&entry.Description,
			&entry.ExternalPlatform,
			&entry.ExternalReferenceID,
			&entry.TransactionAmount,
			&entry.PointsRate,
			&entry.Metadata,
			&entry.CreatedAt,
			&entry.CreatedBy,
		)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// ValidateRedemption checks if tenant has enough points for redemption
func (s *PointsLedgerService) ValidateRedemption(ctx context.Context, tenantID uuid.UUID, pointsToRedeem int) error {
	balance, err := s.GetBalance(ctx, tenantID)
	if err != nil {
		return err
	}

	if balance.AvailablePoints < pointsToRedeem {
		return fmt.Errorf("insufficient points: available=%d, requested=%d",
			balance.AvailablePoints, pointsToRedeem)
	}

	return nil
}

// RecordRedemption records a points redemption from external platform (e.g., Keystone)
func (s *PointsLedgerService) RecordRedemption(
	ctx context.Context,
	tenantID uuid.UUID,
	pointsSpent int,
	platform string,
	externalRefID string,
	description string,
) (*models.PointsLedgerEntry, error) {
	// Validate redemption
	if err := s.ValidateRedemption(ctx, tenantID, pointsSpent); err != nil {
		return nil, err
	}

	// Create redemption entry (negative points)
	entry := &models.PointsLedgerEntry{
		TenantID:            tenantID,
		EntryType:           models.PointsRedeemedSpent,
		EntryDate:           time.Now(),
		Points:              -pointsSpent, // Negative for redemption
		Description:         description,
		ExternalPlatform:    &platform,
		ExternalReferenceID: &externalRefID,
	}

	if err := s.CreateEntry(ctx, entry); err != nil {
		return nil, err
	}

	return entry, nil
}
