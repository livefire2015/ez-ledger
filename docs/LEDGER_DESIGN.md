# Dual Ledger System Design

## Table of Contents
1. [Overview](#overview)
2. [Statement Ledger System](#statement-ledger-system)
3. [Points Ledger System](#points-ledger-system)
4. [Ledger Reconciliation](#ledger-reconciliation)
5. [FAQ](#faq)
6. [Examples](#examples)

## Overview

This system implements **two independent but coordinated ledgers**:

1. **Statement Ledger**: Tracks financial balance (money owed/paid)
2. **Points Ledger**: Tracks reward points (earned/redeemed)

Both ledgers use **event sourcing** principles where entries are immutable and balances are calculated from the complete transaction history.

## Statement Ledger System

### Purpose
Track all financial activities for tenant accounts including charges, payments, refunds, fees, and credits.

### Database Design

#### Core Tables

**`tenants`** - Customer accounts
- Stores tenant information and minimum payment percentage
- Primary identifier: `tenant_code` and `id`

**`statements`** - Billing periods
- Represents a billing cycle with calculated balances
- Contains: previous balance, cleared payments, opening balance, statement balance
- Status: draft → finalized → closed

**`statement_ledger_entries`** - Immutable transaction log
- **Event sourcing**: All entries are append-only (no updates/deletes)
- Each entry has a type, amount, and status
- Balances are calculated from entries, not stored

#### Entry Types

| Entry Type | Description | Impact on Balance |
|------------|-------------|-------------------|
| `transaction` | Purchase/charge | Increases (debit) |
| `payment` | Payment received | Decreases (credit) |
| `refund` | Refund issued | Decreases (credit) |
| `reward` | Points redeemed as credit | Decreases (credit) |
| `returned_reward` | Reward reversed | Increases (debit) |
| `fee_late` | Late payment fee | Increases (debit) |
| `fee_failed` | Failed payment fee | Increases (debit) |
| `fee_international` | International fee | Increases (debit) |
| `adjustment` | Manual adjustment | +/- based on amount |
| `credit` | Account credit | Decreases (credit) |

### Balance Calculation

**Statement Balance Formula**:
```
Statement Balance = Opening Balance
                  + Transactions
                  - Refunds
                  - Cleared Payments
                  + Rewards (when points redeemed)
                  - Returned Rewards
                  + Fees
                  + Adjustments
                  - Credits
```

**Opening Balance Formula**:
```
Opening Balance = Previous Statement Balance - Cleared Payments
```

### Key Features

1. **Immutability**: Entries cannot be updated, only reversed with new entries
2. **Real-time Balances**: Calculated via database views from all cleared entries
3. **Audit Trail**: Complete history of all financial activities
4. **Double-entry Principles**: Every amount is either a debit or credit

## Points Ledger System

### Purpose
Track reward points earned from transactions and redeemed for benefits.

### Database Design

#### Core Table

**`points_ledger_entries`** - Immutable points transaction log
- **Event sourcing**: All entries are append-only
- Links to `statement_ledger_entries` when applicable
- Tracks external platform redemptions (e.g., Bridge2 Keystone)

#### Entry Types

| Entry Type | Description | Points Impact |
|------------|-------------|---------------|
| `earned_transaction` | Points from purchase | Positive (earned) |
| `earned_refund` | Points adjustment from refund | Negative (deducted) |
| `redeemed_spent` | Points spent (from Keystone) | Negative (spent) |
| `redeemed_cancelled` | Redemption cancelled | Positive (returned) |
| `redeemed_refunded` | Redemption refunded | Positive (returned) |
| `adjustment` | Manual adjustment | +/- based on points |

### Balance Calculation

**Earned Points**:
```
Earned Points = SUM(points from earned_transaction + earned_refund entries)
```

**Redeemed Points**:
```
Redeemed Points = points_spent - points_cancelled - points_refunded
```

**Available Points**:
```
Available Points = Earned Points - Redeemed Points
```

### Points Earning Rules

Points are earned based on transaction amounts:

```go
type PointsEarningRule struct {
    PointsPerDollar decimal.Decimal  // e.g., 0.01 = 1 point per dollar
    MinAmount       decimal.Decimal  // Minimum transaction to earn points
    MaxPoints       *int             // Optional cap per transaction
}

// Example: 1 point per dollar spent
Points Earned = Transaction Amount × Points Per Dollar
```

## Ledger Reconciliation

### How the Two Ledgers Interact

The reconciliation service (`LedgerReconciliationService`) coordinates between the ledgers to ensure data consistency.

### Key Reconciliation Points

#### 1. Transaction (Spending) → Earns Points

```
┌─────────────────┐         ┌──────────────────┐
│ Statement       │         │ Points           │
│ Ledger          │         │ Ledger           │
├─────────────────┤         ├──────────────────┤
│ Entry Type:     │         │ Entry Type:      │
│   transaction   │ ──────► │   earned_transaction │
│ Amount: $100    │         │ Points: +100     │
│ Status: pending │         │ Linked via ID    │
└─────────────────┘         └──────────────────┘
```

**What happens:**
- A $100 transaction is recorded in the statement ledger
- Points are calculated (e.g., $100 × 0.01 = 100 points)
- Points entry is linked to the statement entry via `statement_entry_id`
- Both entries are created in a single database transaction (atomic)

#### 2. Payment → NO EFFECT on Points

```
┌─────────────────┐         ┌──────────────────┐
│ Statement       │         │ Points           │
│ Ledger          │         │ Ledger           │
├─────────────────┤         ├──────────────────┤
│ Entry Type:     │         │                  │
│   payment       │    ✗    │   NO ENTRY       │
│ Amount: $100    │         │                  │
│ Status: pending │         │                  │
└─────────────────┘         └──────────────────┘
```

**IMPORTANT**: Payments DO NOT affect the points balance!
- Points are earned when you SPEND (transactions)
- Paying your bill does not earn or lose points
- This is the key difference between the two ledgers

#### 3. Refund → Adjusts Both Ledgers

```
┌─────────────────┐         ┌──────────────────┐
│ Statement       │         │ Points           │
│ Ledger          │         │ Ledger           │
├─────────────────┤         ├──────────────────┤
│ Entry Type:     │         │ Entry Type:      │
│   refund        │ ──────► │   earned_refund  │
│ Amount: $50     │         │ Points: -50      │
│ (credit)        │         │ (deducted)       │
└─────────────────┘         └──────────────────┘
```

**What happens:**
- Refund credits the statement balance
- Points earned from original transaction are deducted proportionally
- Both ledgers are updated atomically

#### 4. Reward Redemption → Points to Statement Credit

```
┌──────────────────┐         ┌─────────────────┐
│ Points           │         │ Statement       │
│ Ledger           │         │ Ledger          │
├──────────────────┤         ├─────────────────┤
│ Entry Type:      │         │ Entry Type:     │
│   redeemed_spent │ ──────► │   reward        │
│ Points: -1000    │         │ Amount: $10     │
│ External: Y      │         │ (credit)        │
└──────────────────┘         └─────────────────┘
```

**What happens:**
- Tenant redeems points (e.g., 1000 points = $10 credit)
- Points are deducted from points ledger
- Credit is applied to statement ledger
- Both entries are linked and created atomically

### Reconciliation Algorithm

```go
// Pseudo-code for key reconciliation flows

// 1. Record Transaction with Points
func RecordTransaction(amount, description) {
    BEGIN TRANSACTION

    // Step 1: Create statement entry
    statementEntry = CreateStatementEntry(
        type: "transaction",
        amount: amount,
    )

    // Step 2: Calculate and create points entry
    points = CalculatePoints(amount) // e.g., amount * 0.01
    if points > 0 {
        pointsEntry = CreatePointsEntry(
            type: "earned_transaction",
            points: points,
            linkedTo: statementEntry.ID,
        )
    }

    COMMIT TRANSACTION
}

// 2. Record Payment (NO points impact)
func RecordPayment(amount, description) {
    // Only affects statement ledger
    CreateStatementEntry(
        type: "payment",
        amount: amount,
    )
    // NO points ledger entry created!
}

// 3. Redeem Points for Statement Credit
func RedeemPoints(points, creditAmount) {
    BEGIN TRANSACTION

    // Step 1: Validate sufficient points
    if AvailablePoints < points {
        ROLLBACK "Insufficient points"
    }

    // Step 2: Deduct points
    pointsEntry = CreatePointsEntry(
        type: "redeemed_spent",
        points: -points, // negative
    )

    // Step 3: Apply credit to statement
    statementEntry = CreateStatementEntry(
        type: "reward",
        amount: creditAmount, // credit
        linkedTo: pointsEntry.ID,
    )

    COMMIT TRANSACTION
}
```

## FAQ

### Q: How should I design my points ledger system of records?

**Answer**: Use an **event sourcing approach** with immutable entries:

1. **Single source of truth**: `points_ledger_entries` table
2. **Append-only**: Never update/delete entries, only append new ones
3. **Linked to statement**: Use `statement_entry_id` to link earning events
4. **External tracking**: Use `external_reference_id` for Keystone redemptions
5. **Calculate balances**: Use database views to sum entries in real-time
6. **Atomic operations**: Use database transactions for consistency

**Key design principles**:
- Store events (what happened), not state (current balance)
- Balance = `SUM(all points entries)` for a tenant
- Separate earned vs redeemed tracking
- Maintain audit trail with timestamps and creators

### Q: How should I reconcile these two ledgers?

**Answer**: Use a **coordination service** that ensures atomic updates:

1. **Shared transactions**: Use database transactions to update both ledgers atomically
2. **Event linking**: Link related entries via foreign keys (`statement_entry_id`)
3. **Validation**: Check business rules before creating entries (e.g., sufficient points)
4. **One-way flows**:
   - Transactions → Earn points (statement → points)
   - Redemptions → Apply credits (points → statement)
5. **Independent balances**: Each ledger maintains its own balance calculation

**Reconciliation rules**:
```
Transaction  → Update both ledgers (if points enabled)
Payment      → Update statement only (NO points impact)
Refund       → Update both ledgers (reverse original)
Redemption   → Update both ledgers (points → credit)
Fee          → Update statement only
Adjustment   → Can update either or both (manual)
```

### Q: If a tenant paid their statement balance, will that affect their reward points balance?

**Answer**: **NO**, payments do NOT affect points balance.

**Why?**
- Points are earned when you **spend money** (transactions)
- Paying your bill is just **settling the debt**, not spending
- Your points balance remains the same after payment

**Example**:
```
Starting state:
  Statement Balance: $500
  Points Balance: 1000 points

Tenant pays $500:
  Statement Balance: $0      (decreased by payment)
  Points Balance: 1000 points (UNCHANGED)
```

**What DOES affect points?**
- **Earning**: Making purchases (transactions)
- **Spending**: Redeeming points for rewards/credits
- **Adjustments**: Refunds on purchases that earned points

## Examples

### Example 1: Complete Transaction Flow

```sql
-- 1. Tenant makes a $100 purchase
-- Statement Ledger Entry
INSERT INTO statement_ledger_entries (
    tenant_id, entry_type, amount, description, status
) VALUES (
    'tenant-123', 'transaction', 100.00, 'Purchase at Store', 'pending'
);

-- Points Ledger Entry (linked)
INSERT INTO points_ledger_entries (
    tenant_id, statement_entry_id, entry_type, points,
    transaction_amount, points_rate
) VALUES (
    'tenant-123', '[statement-entry-id]', 'earned_transaction', 100,
    100.00, 0.01
);

-- Result:
--   Statement Balance: +$100
--   Points Balance: +100 points
```

### Example 2: Payment (No Points Impact)

```sql
-- 2. Tenant pays $100
-- Statement Ledger Entry ONLY
INSERT INTO statement_ledger_entries (
    tenant_id, entry_type, amount, description, status
) VALUES (
    'tenant-123', 'payment', 100.00, 'Payment received', 'cleared'
);

-- NO points ledger entry!

-- Result:
--   Statement Balance: -$100
--   Points Balance: UNCHANGED
```

### Example 3: Points Redemption

```sql
-- 3. Tenant redeems 1000 points for $10 credit
-- Points Ledger Entry (deduction)
INSERT INTO points_ledger_entries (
    tenant_id, entry_type, points, external_platform, external_reference_id
) VALUES (
    'tenant-123', 'redeemed_spent', -1000, 'keystone', 'redeem-789'
);

-- Statement Ledger Entry (credit)
INSERT INTO statement_ledger_entries (
    tenant_id, entry_type, amount, description, reference_id, status
) VALUES (
    'tenant-123', 'reward', 10.00, 'Reward redemption', 'redeem-789', 'pending'
);

-- Result:
--   Statement Balance: -$10 (credit applied)
--   Points Balance: -1000 points
```

### Example 4: Refund with Points Adjustment

```sql
-- 4. Refund $50 purchase (that earned 50 points)
-- Statement Ledger Entry
INSERT INTO statement_ledger_entries (
    tenant_id, entry_type, amount, description, status
) VALUES (
    'tenant-123', 'refund', 50.00, 'Refund for item return', 'pending'
);

-- Points Ledger Entry (adjustment)
INSERT INTO points_ledger_entries (
    tenant_id, statement_entry_id, entry_type, points,
    transaction_amount, points_rate
) VALUES (
    'tenant-123', '[refund-entry-id]', 'earned_refund', -50,
    50.00, 0.01
);

-- Result:
--   Statement Balance: -$50 (credited)
--   Points Balance: -50 points (deducted)
```

### Example 5: Monthly Statement Calculation

```go
// Calculate statement for billing period
func CalculateStatement(tenantID, startDate, endDate) {
    // Get previous statement balance
    previousBalance := GetPreviousStatementBalance(tenantID)

    // Get cleared payments in this period
    clearedPayments := SumClearedPayments(tenantID, startDate, endDate)

    // Calculate opening balance
    openingBalance := previousBalance - clearedPayments

    // Sum all activities in billing period
    transactions := SumByType(tenantID, startDate, endDate, "transaction")
    refunds := SumByType(tenantID, startDate, endDate, "refund")
    rewards := SumByType(tenantID, startDate, endDate, "reward")
    fees := SumByType(tenantID, startDate, endDate, "fee_%")

    // Calculate statement balance
    statementBalance := openingBalance +
                       transactions -
                       refunds -
                       rewards +
                       fees

    // Calculate minimum payment
    minimumPayment := statementBalance * 0.05 // 5%

    return Statement{
        PreviousBalance: previousBalance,
        ClearedPayments: clearedPayments,
        OpeningBalance: openingBalance,
        StatementBalance: statementBalance,
        MinimumPayment: minimumPayment,
    }
}
```

## Data Integrity Guarantees

### Database Constraints

1. **Immutability**: Triggers prevent updates to ledger entries
2. **Referential Integrity**: Foreign keys ensure valid relationships
3. **Check Constraints**: Validate entry amounts are logical
4. **Unique Constraints**: Prevent duplicate entries

### Application-Level Validation

1. **Atomic Transactions**: All related ledger updates in single DB transaction
2. **Balance Validation**: Check sufficient points before redemption
3. **Status Tracking**: Entries move through pending → cleared → reversed
4. **Audit Trail**: All entries record creator and timestamps

## Performance Considerations

### Indexing Strategy

```sql
-- Fast tenant balance lookups
CREATE INDEX idx_statement_entries_tenant ON statement_ledger_entries(tenant_id);
CREATE INDEX idx_points_entries_tenant ON points_ledger_entries(tenant_id);

-- Fast statement period queries
CREATE INDEX idx_statement_entries_posting_date ON statement_ledger_entries(posting_date);

-- Fast external reference lookups
CREATE INDEX idx_points_entries_external_ref ON points_ledger_entries(external_reference_id);
```

### Views for Real-Time Balances

```sql
-- Materialized view option for high-volume tenants
CREATE MATERIALIZED VIEW tenant_balances_snapshot AS
SELECT
    tenant_id,
    statement_balance,
    points_balance,
    last_updated
FROM (
    -- Join both balance views
    SELECT * FROM statement_balances
    JOIN points_balances USING (tenant_id)
);

-- Refresh periodically or on-demand
REFRESH MATERIALIZED VIEW tenant_balances_snapshot;
```

## Next Steps

1. **Implement Statement Generation**: Build service to create monthly statements
2. **Add Webhooks**: Notify external systems (Keystone) of balance changes
3. **Build Reconciliation Reports**: Compare ledgers for audit purposes
4. **Add Business Rules**: Late fees, minimum payment enforcement
5. **Create API Endpoints**: REST API for ledger operations
6. **Build Admin Dashboard**: View and manage both ledgers
