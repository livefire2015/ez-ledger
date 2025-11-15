package services

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/livefire2015/ez-ledger/src/models"
	"github.com/shopspring/decimal"
)

// StatementLedgerService handles statement ledger operations
type StatementLedgerService struct {
	db *sql.DB
}

// NewStatementLedgerService creates a new statement ledger service
func NewStatementLedgerService(db *sql.DB) *StatementLedgerService {
	return &StatementLedgerService{db: db}
}

// CreateEntry creates a new statement ledger entry
// This is the core function for recording all financial activities
func (s *StatementLedgerService) CreateEntry(ctx context.Context, entry *models.StatementLedgerEntry) error {
	query := `
		INSERT INTO statement_ledger_entries (
			id, tenant_id, statement_id, entry_type, entry_date, posting_date,
			amount, description, reference_id, metadata, status, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}

	_, err := s.db.ExecContext(ctx, query,
		entry.ID,
		entry.TenantID,
		entry.StatementID,
		entry.EntryType,
		entry.EntryDate,
		entry.PostingDate,
		entry.Amount,
		entry.Description,
		entry.ReferenceID,
		entry.Metadata,
		entry.Status,
		entry.CreatedBy,
	)

	return err
}

// ClearEntry marks an entry as cleared (processed)
func (s *StatementLedgerService) ClearEntry(ctx context.Context, entryID uuid.UUID) error {
	query := `
		UPDATE statement_ledger_entries
		SET status = $1, cleared_at = $2
		WHERE id = $3 AND status = $4
	`

	now := time.Now()
	result, err := s.db.ExecContext(ctx, query,
		models.EntryStatusCleared,
		now,
		entryID,
		models.EntryStatusPending,
	)

	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return fmt.Errorf("entry not found or already cleared: %s", entryID)
	}

	return nil
}

// GetBalance calculates the current balance for a tenant
func (s *StatementLedgerService) GetBalance(ctx context.Context, tenantID uuid.UUID) (*models.StatementBalance, error) {
	query := `SELECT * FROM statement_balances WHERE tenant_id = $1`

	var balance models.StatementBalance
	err := s.db.QueryRowContext(ctx, query, tenantID).Scan(
		&balance.TenantID,
		&balance.CurrentBalance,
		&balance.TotalEntries,
		&balance.LastActivityDate,
	)

	if err == sql.ErrNoRows {
		// No entries yet, return zero balance
		return &models.StatementBalance{
			TenantID:       tenantID,
			CurrentBalance: decimal.Zero,
			TotalEntries:   0,
		}, nil
	}

	if err != nil {
		return nil, err
	}

	return &balance, nil
}

// GetEntriesByStatement retrieves all entries for a statement
func (s *StatementLedgerService) GetEntriesByStatement(ctx context.Context, statementID uuid.UUID) ([]*models.StatementLedgerEntry, error) {
	query := `
		SELECT id, tenant_id, statement_id, entry_type, entry_date, posting_date,
		       amount, description, reference_id, metadata, status, cleared_at,
		       created_at, created_by
		FROM statement_ledger_entries
		WHERE statement_id = $1
		ORDER BY posting_date, entry_date
	`

	rows, err := s.db.QueryContext(ctx, query, statementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*models.StatementLedgerEntry
	for rows.Next() {
		entry := &models.StatementLedgerEntry{}
		err := rows.Scan(
			&entry.ID,
			&entry.TenantID,
			&entry.StatementID,
			&entry.EntryType,
			&entry.EntryDate,
			&entry.PostingDate,
			&entry.Amount,
			&entry.Description,
			&entry.ReferenceID,
			&entry.Metadata,
			&entry.Status,
			&entry.ClearedAt,
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

// CalculateStatementBalance calculates the statement balance for a billing period
func (s *StatementLedgerService) CalculateStatementBalance(
	ctx context.Context,
	tenantID uuid.UUID,
	startDate, endDate time.Time,
	openingBalance decimal.Decimal,
) (decimal.Decimal, error) {
	query := `
		SELECT COALESCE(SUM(
			CASE
				WHEN entry_type IN ('transaction', 'fee_late', 'fee_failed', 'fee_international', 'returned_reward')
					THEN amount
				WHEN entry_type IN ('payment', 'refund', 'reward', 'credit')
					THEN -amount
				WHEN entry_type = 'adjustment'
					THEN amount
				ELSE 0
			END
		), 0) as period_total
		FROM statement_ledger_entries
		WHERE tenant_id = $1
		  AND posting_date >= $2
		  AND posting_date <= $3
		  AND status = 'cleared'
	`

	var periodTotal decimal.Decimal
	err := s.db.QueryRowContext(ctx, query, tenantID, startDate, endDate).Scan(&periodTotal)
	if err != nil {
		return decimal.Zero, err
	}

	return openingBalance.Add(periodTotal), nil
}
