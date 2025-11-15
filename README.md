# EZ Ledger - Dual Ledger System

A robust, event-sourced dual ledger system for managing financial statements and reward points.

## Overview

This system implements two coordinated ledgers:

1. **Statement Ledger**: Tracks financial balance (charges, payments, refunds, fees)
2. **Points Ledger**: Tracks reward points (earned, redeemed)

Key features:
- Event sourcing with immutable ledger entries
- Atomic reconciliation between ledgers
- Real-time balance calculation
- Full audit trail
- PostgreSQL with strict data integrity

## Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL 14+

### Database Setup

```bash
# Create database
createdb ezledger

# Run migrations
psql ezledger < migrations/001_create_ledger_tables.sql
```

### Install Dependencies

```bash
go mod download
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                  Application Layer                      │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌───────────────────────────────────────────────────┐  │
│  │  LedgerReconciliationService                      │  │
│  │  (Coordinates both ledgers)                       │  │
│  └─────────────┬──────────────────────┬───────────────┘  │
│                │                      │                  │
│    ┌───────────▼─────────┐  ┌────────▼──────────────┐   │
│    │ StatementLedger     │  │ PointsLedger          │   │
│    │ Service             │  │ Service               │   │
│    └───────────┬─────────┘  └────────┬──────────────┘   │
│                │                      │                  │
├────────────────┼──────────────────────┼──────────────────┤
│                │   Database Layer     │                  │
├────────────────┼──────────────────────┼──────────────────┤
│                │                      │                  │
│    ┌───────────▼─────────┐  ┌────────▼──────────────┐   │
│    │ statement_ledger    │  │ points_ledger         │   │
│    │ _entries            │  │ _entries              │   │
│    │ (immutable)         │  │ (immutable)           │   │
│    └─────────────────────┘  └───────────────────────┘   │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

## Usage Examples

### Example 1: Record a Transaction with Points

```go
package main

import (
    "context"
    "github.com/livefire2015/ez-ledger/src/services"
    "github.com/livefire2015/ez-ledger/src/models"
    "github.com/shopspring/decimal"
    "time"
)

func main() {
    // Initialize service
    reconciliationService := services.NewLedgerReconciliationService(
        db,
        models.PointsEarningRule{
            PointsPerDollar: decimal.NewFromFloat(0.01), // 1 point per dollar
            MinAmount:       decimal.NewFromFloat(1.00),
        },
    )

    // Record a $100 purchase
    req := services.TransactionRequest{
        TenantID:        tenantID,
        Amount:          decimal.NewFromFloat(100.00),
        Description:     "Purchase at Amazon",
        ReferenceID:     "txn-12345",
        TransactionDate: time.Now(),
        PostingDate:     time.Now(),
        EarnPoints:      true, // Enable points earning
    }

    statementEntry, pointsEntry, err := reconciliationService.RecordTransaction(
        context.Background(),
        req,
    )

    // Result:
    //   Statement entry created: +$100 charge
    //   Points entry created: +100 points earned
}
```

### Example 2: Record a Payment (No Points Impact)

```go
// Record a $100 payment
req := services.PaymentRequest{
    TenantID:    tenantID,
    Amount:      decimal.NewFromFloat(100.00),
    Description: "Payment received",
    ReferenceID: "pay-67890",
    PaymentDate: time.Now(),
    PostingDate: time.Now(),
}

statementEntry, err := reconciliationService.RecordPayment(
    context.Background(),
    req,
)

// Result:
//   Statement entry created: -$100 credit
//   Points ledger: UNCHANGED
```

### Example 3: Redeem Points for Statement Credit

```go
// Redeem 1000 points for $10 credit
req := services.RewardRedemptionRequest{
    TenantID:            tenantID,
    PointsToRedeem:      1000,
    CreditAmount:        decimal.NewFromFloat(10.00),
    Description:         "Reward redemption",
    ExternalPlatform:    "keystone",
    ExternalReferenceID: "redeem-999",
    RedemptionDate:      time.Now(),
    PostingDate:         time.Now(),
}

statementEntry, pointsEntry, err := reconciliationService.RecordRewardRedemption(
    context.Background(),
    req,
)

// Result:
//   Points entry: -1000 points
//   Statement entry: -$10 credit
```

### Example 4: Check Balances

```go
// Get reconciliation report
report, err := reconciliationService.GenerateReconciliationReport(
    context.Background(),
    tenantID,
)

fmt.Printf("Statement Balance: $%.2f\n", report.StatementBalance)
fmt.Printf("Points Balance: %d points\n", report.PointsBalance)

// Output:
//   Statement Balance: $245.50
//   Points Balance: 1500 points
```

## Key Concepts

### Event Sourcing

Both ledgers use event sourcing:
- **Immutable entries**: Never update/delete, only append
- **Calculate balances**: Sum all entries to get current state
- **Complete history**: Full audit trail of all activities

### Reconciliation Rules

| Activity | Statement Ledger | Points Ledger |
|----------|------------------|---------------|
| Transaction | ✓ Charge added | ✓ Points earned |
| Payment | ✓ Credit applied | ✗ No change |
| Refund | ✓ Credit applied | ✓ Points deducted |
| Points Redemption | ✓ Credit applied | ✓ Points deducted |
| Fee | ✓ Charge added | ✗ No change |

### Important: Payments Don't Affect Points!

```
❌ INCORRECT: "I paid my bill, I should get points"
✓ CORRECT:   "Points are earned when spending, not when paying"

Transaction (spending):  +$100 charge  → +100 points
Payment (paying bill):   -$100 credit  → NO points change
```

## Database Schema

### Statement Ledger Entries

```sql
CREATE TABLE statement_ledger_entries (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    entry_type statement_entry_type NOT NULL, -- transaction, payment, etc.
    amount DECIMAL(15,2) NOT NULL,
    status VARCHAR(20) NOT NULL, -- pending, cleared, reversed
    entry_date TIMESTAMP NOT NULL,
    posting_date DATE NOT NULL,
    -- ... additional fields
);
```

### Points Ledger Entries

```sql
CREATE TABLE points_ledger_entries (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    entry_type points_entry_type NOT NULL, -- earned_transaction, redeemed_spent, etc.
    points INTEGER NOT NULL,
    statement_entry_id UUID, -- Link to related statement entry
    external_reference_id VARCHAR(100), -- Keystone redemption ID
    -- ... additional fields
);
```

### Balance Views

```sql
-- Real-time statement balance
CREATE VIEW statement_balances AS
SELECT tenant_id, SUM(signed_amount) as current_balance
FROM statement_ledger_entries
WHERE status = 'cleared'
GROUP BY tenant_id;

-- Real-time points balance
CREATE VIEW points_balances AS
SELECT tenant_id, SUM(points) as available_points
FROM points_ledger_entries
GROUP BY tenant_id;
```

## Project Structure

```
ez-ledger/
├── migrations/
│   └── 001_create_ledger_tables.sql   # Database schema
├── src/
│   ├── models/                         # Data models
│   │   ├── tenant.go
│   │   ├── statement.go
│   │   ├── statement_ledger.go
│   │   └── points_ledger.go
│   └── services/                       # Business logic
│       ├── statement_ledger_service.go
│       ├── points_ledger_service.go
│       └── ledger_reconciliation_service.go
├── docs/
│   └── LEDGER_DESIGN.md               # Detailed design documentation
├── go.mod
└── README.md
```

## Documentation

- [Detailed Design Documentation](docs/LEDGER_DESIGN.md) - Complete system design, reconciliation logic, and FAQs
- [Database Schema](migrations/001_create_ledger_tables.sql) - Full SQL schema with constraints

## Design Principles

1. **Immutability**: Ledger entries are never modified, only appended
2. **Atomicity**: Related ledger updates happen in single database transaction
3. **Auditability**: Complete history with timestamps and creators
4. **Separation of Concerns**: Statement and points are independent ledgers
5. **Data Integrity**: Database constraints enforce business rules

## FAQ

### Q: Do payments affect points balance?

**A:** No! Payments only affect the statement balance. Points are earned when you spend (transactions), not when you pay your bill.

### Q: How do I reverse a transaction?

**A:** Create a new entry with the opposite effect. Never delete or update existing entries.

### Q: Can I have negative points balance?

**A:** No. The system validates that you have sufficient points before allowing redemption.

### Q: How are statement balances calculated?

**A:** By summing all cleared entries. The `statement_balances` view does this in real-time.

### Q: What's the relationship between the two ledgers?

**A:** They're independent but coordinated:
- Transactions → affect both ledgers
- Payments → affect statement only
- Redemptions → affect both ledgers

## License

MIT

## Contributing

See [CLAUDE.md](CLAUDE.md) for development guidelines and coding standards.
