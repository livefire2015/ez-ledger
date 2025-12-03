# Credit Card Ledger System Design

## Table of Contents
1. [Overview](#overview)
2. [Statement Ledger System](#statement-ledger-system)
3. [Cashback Points Ledger System](#cashback-points-ledger-system)
4. [Credit Card Features](#credit-card-features)
5. [Ledger Reconciliation](#ledger-reconciliation)
6. [FAQ](#faq)
7. [Examples](#examples)

## Overview

This system implements a **comprehensive revolving credit card ledger** with integrated cashback rewards tracking:

1. **Statement Ledger**: Tracks all financial activities - purchases, payments, fees, interest, and refunds
2. **Cashback Points Ledger**: Tracks reward points earned and redeemed

Both ledgers use **event sourcing** principles where entries are immutable and balances are calculated from the complete transaction history.

### Key Characteristics

- **Revolving Credit**: Unlike traditional loans, credit cards allow repeated borrowing up to a credit limit
- **GAAP-Compliant**: Interest calculations follow Generally Accepted Accounting Principles
- **Multi-tenant**: Supports multiple cardholders with complete isolation
- **Immutable Ledger**: All entries are append-only for complete audit trail
- **Real-time Balances**: Calculated from ledger entries, not stored

---

## Statement Ledger System

### Purpose
Track all financial activities for credit card accounts including purchases, payments, refunds, fees, interest, and credits.

### Database Design

#### Core Tables

**`credit_cards`** - Credit card accounts
- Stores credit limit, available credit, APRs, and fees
- Tracks billing cycle configuration
- Manages cashback settings
- Primary identifier: `id` (UUID)

**`billing_cycles`** - Monthly billing periods
- Represents a billing cycle with calculated balances
- Contains: previous balance, payments, purchases, fees, interest
- Tracks minimum payment and due date
- Status: open → closed → paid_full/paid_minimum/overdue

**`statement_ledger_entries`** - Immutable transaction log
- **Event sourcing**: All entries are append-only (no updates/deletes)
- Each entry has a type, amount, and status
- Balances are calculated from entries, not stored
- Links to billing cycles via `statement_id`

#### Entry Types

| Entry Type | Description | Impact on Balance | Earns Cashback? |
|------------|-------------|-------------------|-----------------|
| `transaction` | Purchase/charge | Increases (debit) | Yes |
| `payment` | Payment received | Decreases (credit) | No |
| `refund` | Merchant refund | Decreases (credit) | Reverses earned |
| `cash_advance` | ATM withdrawal | Increases (debit) | No |
| `fee_late` | Late payment fee | Increases (debit) | No |
| `fee_failed` | Failed payment fee | Increases (debit) | No |
| `fee_international` | Foreign transaction fee | Increases (debit) | No |
| `fee_interest` | Interest charge | Increases (debit) | No |
| `fee_cash_advance` | Cash advance fee | Increases (debit) | No |
| `fee_annual` | Annual membership fee | Increases (debit) | No |
| `fee_over_limit` | Over credit limit fee | Increases (debit) | No |
| `adjustment` | Manual adjustment | +/- based on amount | No |
| `credit` | Account credit/waiver | Decreases (credit) | No |

### Balance Calculation

**Current Balance Formula**:
```
Current Balance = Credit Limit - Available Credit

OR (from ledger entries):

Current Balance = SUM of:
  + Transactions
  + Cash Advances
  + All Fees (late, failed, international, interest, etc.)
  - Payments
  - Refunds
  - Credits
  +/- Adjustments
```

**Available Credit Formula**:
```
Available Credit = Credit Limit - Current Balance
```

### Key Features

1. **Immutability**: Entries cannot be updated, only reversed with new entries
2. **Real-time Balances**: Calculated via database views from all cleared entries
3. **Audit Trail**: Complete history of all financial activities
4. **Precision**: Uses `decimal.Decimal` for all currency calculations (no floating point)

---

## Cashback Points Ledger System

### Purpose
Track reward points earned from qualifying purchases and redeemed for statement credits or external rewards.

### Database Design

#### Core Table

**`cashback_ledger_entries`** - Immutable points transaction log
- **Event sourcing**: All entries are append-only
- Links to `statement_ledger_entries` when applicable
- Tracks external platform redemptions (e.g., Keystone)
- Stores earning rate and transaction amount for auditability

#### Entry Types

| Entry Type | Description | Points Impact |
|------------|-------------|---------------|
| `earned_transaction` | Points from purchase | Positive (earned) |
| `adjusted_refund` | Points deducted for refund | Negative (deducted) |
| `redeemed_spent` | Points redeemed for statement credit | Negative (spent) |
| `redeemed_external` | Points redeemed via external platform | Negative (spent) |
| `adjusted_manual` | Manual adjustment | +/- based on points |
| `expired` | Points expired | Negative (expired) |

### Balance Calculation

**Available Points**:
```
Available Points = SUM of all points entries

Where:
  earned_transaction: +points
  adjusted_refund: -points
  redeemed_spent: -points
  redeemed_external: -points
  adjusted_manual: +/- points
  expired: -points
```

### Points Earning Rules

Points are earned based on transaction amounts:

```go
type PointsEarningRule struct {
    PointsPerDollar decimal.Decimal  // e.g., 0.01 = 1 point per dollar
    MinAmount       decimal.Decimal  // Minimum transaction to earn points
}

// Example: 1% cashback = 1 point per dollar spent
Points Earned = Transaction Amount × Points Per Dollar
```

**Category Multipliers** (optional):
- Dining: 3x points
- Gas: 2x points  
- Everything else: 1x points

---

## Credit Card Features

### 1. Billing Cycle Management

**Monthly Billing Cycle**:
```
Day 1-30: Billing Cycle (Open)
├─ Purchases and payments recorded
├─ Daily balances tracked for interest calculation
└─ Day 30: Cycle closes

Day 31: Statement Generated (Closed)
├─ Previous balance
├─ Purchases this cycle
├─ Payments received
├─ Fees assessed
├─ Interest charged
├─ New balance
├─ Minimum payment (greater of % or fixed amount)
└─ Due date (typically 21-25 days later)

Day 55: Payment Due
├─ If paid in full → No interest on next cycle (grace period)
├─ If paid minimum → Interest accrues on remaining balance
└─ If not paid → Late fee assessed, status becomes overdue
```

### 2. Interest Calculation (GAAP-Compliant)

**Method**: Average Daily Balance (ADB)

```
Step 1: Calculate daily balances for the billing cycle
  Track balance at end of each day

Step 2: Calculate Average Daily Balance
  ADB = SUM(daily balances) / Days in cycle

Step 3: Calculate Daily Periodic Rate
  DPR = APR / 365

Step 4: Calculate Interest Charge
  Interest = ADB × DPR × Days in cycle

Step 5: Apply minimum interest charge (if configured)
  If Interest > 0 AND Interest < Minimum:
    Interest = Minimum Interest Charge
```

**Example**:
```
Billing Cycle: 30 days
Daily Balances:
  Days 1-10:  $1000
  Days 11-20: $1500
  Days 21-30: $800

ADB = (1000×10 + 1500×10 + 800×10) / 30
    = (10000 + 15000 + 8000) / 30
    = $1100

APR = 18.25%
DPR = 18.25% / 365 = 0.05%

Interest = $1100 × 0.0005 × 30 = $16.50
```

**Grace Period**: If the previous statement balance was paid in full by the due date, no interest is charged on new purchases during the current cycle.

**APR Types**:
- **Purchase APR**: Standard rate for purchases
- **Cash Advance APR**: Higher rate for cash advances (no grace period)
- **Penalty APR**: Applied after missed payments
- **Introductory APR**: Promotional rate (time-limited)

### 3. Fee Assessment

#### Late Payment Fee
- **Trigger**: Minimum payment not received by due date
- **Amount**: Configured per card (e.g., $35)
- **Frequency**: Once per billing cycle
- **Prevention**: Assessed only if `MinimumPaymentMet = false` after due date

#### Failed Payment Fee
- **Trigger**: Payment returned/failed (NSF, closed account, etc.)
- **Amount**: Configured per card (e.g., $25)
- **Includes**: ACH return code tracking
- **Effect**: Original charge re-applied to balance

#### International Transaction Fee
- **Trigger**: Transaction in foreign currency
- **Amount**: Percentage of transaction (e.g., 3%)
- **Calculation**: `Fee = Transaction Amount × Fee Rate`

#### Cash Advance Fee
- **Trigger**: ATM withdrawal or cash equivalent
- **Amount**: Greater of flat fee or percentage
- **Calculation**: `Fee = MAX(Flat Fee, Amount × Fee Rate)`
- **Example**: `MAX($10, $200 × 0.05) = MAX($10, $10) = $10`

#### Annual Fee
- **Trigger**: Account anniversary
- **Amount**: Configured per card (e.g., $95)
- **Frequency**: Once per year
- **Prevention**: Check for recent annual fee (within 11 months)

### 4. Payment Processing

Payments follow a state machine:

```
pending → processing → cleared
   ↓           ↓          ↓
cancelled   failed    returned
               ↓
            retrying → pending (retry)
                ↓
             failed (max retries)
```

**States**:
- `pending`: Payment initiated, awaiting processing
- `processing`: Being processed by payment processor
- `cleared`: Successfully processed and applied to balance
- `failed`: Processing failed (insufficient funds, invalid account, etc.)
- `returned`: Cleared payment was returned (ACH return)
- `cancelled`: Cancelled before processing
- `reversed`: Manual reversal of cleared payment

**Payment Types**:
- `full`: Pays entire statement balance
- `minimum`: Pays minimum payment amount
- `statement`: Pays statement balance
- `regular`: Custom amount

### 5. Minimum Payment Calculation

```
Minimum Payment = MAX(
  Balance × Minimum Payment Percent,
  Minimum Payment Amount
)

Example:
  Balance: $1000
  Minimum Percent: 3%
  Minimum Amount: $25

  Minimum = MAX($1000 × 0.03, $25)
          = MAX($30, $25)
          = $30
```

---

## Ledger Reconciliation

### How the Two Ledgers Interact

The system coordinates between ledgers to ensure data consistency while maintaining their independence.

### Key Reconciliation Points

#### 1. Transaction (Spending) → Earns Cashback

```
┌─────────────────┐         ┌──────────────────┐
│ Statement       │         │ Cashback         │
│ Ledger          │         │ Ledger           │
├─────────────────┤         ├──────────────────┤
│ Entry Type:     │         │ Entry Type:      │
│   transaction   │ ──────► │   earned_transaction │
│ Amount: $100    │         │ Points: +100     │
│ Status: pending │         │ Linked via ID    │
└─────────────────┘         └──────────────────┘
```

**What happens:**
- A $100 purchase is recorded in the statement ledger
- Cashback is calculated (e.g., $100 × 0.01 = 100 points)
- Points entry is linked to the statement entry via `statement_entry_id`
- Both entries are created in a single database transaction (atomic)

#### 2. Payment → NO EFFECT on Cashback

```
┌─────────────────┐         ┌──────────────────┐
│ Statement       │         │ Cashback         │
│ Ledger          │         │ Ledger           │
├─────────────────┤         ├──────────────────┤
│ Entry Type:     │         │                  │
│   payment       │    ✗    │   NO ENTRY       │
│ Amount: $100    │         │                  │
│ Status: pending │         │                  │
└─────────────────┘         └──────────────────┘
```

**CRITICAL**: Payments DO NOT affect the cashback balance!
- Cashback is earned when you SPEND (transactions)
- Paying your bill does not earn or lose points
- This is the fundamental difference between the two ledgers

#### 3. Refund → Adjusts Both Ledgers

```
┌─────────────────┐         ┌──────────────────┐
│ Statement       │         │ Cashback         │
│ Ledger          │         │ Ledger           │
├─────────────────┤         ├──────────────────┤
│ Entry Type:     │         │ Entry Type:      │
│   refund        │ ──────► │   adjusted_refund│
│ Amount: $50     │         │ Points: -50      │
│ (credit)        │         │ (deducted)       │
└─────────────────┘         └──────────────────┘
```

**What happens:**
- Refund credits the statement balance
- Points earned from original transaction are deducted proportionally
- Both ledgers are updated atomically

#### 4. Cashback Redemption → Points to Statement Credit

```
┌──────────────────┐         ┌─────────────────┐
│ Cashback         │         │ Statement       │
│ Ledger           │         │ Ledger          │
├──────────────────┤         ├─────────────────┤
│ Entry Type:      │         │ Entry Type:     │
│   redeemed_spent │ ──────► │   credit        │
│ Points: -1000    │         │ Amount: $10     │
│                  │         │ (credit)        │
└──────────────────┘         └─────────────────┘
```

**What happens:**
- Cardholder redeems points (e.g., 1000 points = $10 credit)
- Points are deducted from cashback ledger
- Credit is applied to statement ledger
- Both entries are linked and created atomically

#### 5. Interest Charge → Statement Only

```
┌─────────────────┐         ┌──────────────────┐
│ Statement       │         │ Cashback         │
│ Ledger          │         │ Ledger           │
├─────────────────┤         ├──────────────────┤
│ Entry Type:     │         │                  │
│   fee_interest  │    ✗    │   NO ENTRY       │
│ Amount: $15.50  │         │                  │
│ Status: pending │         │                  │
└─────────────────┘         └──────────────────┘
```

**What happens:**
- Interest is calculated using ADB method
- Interest charge is added to statement ledger
- Cashback ledger is NOT affected (fees don't earn points)

### Reconciliation Rules Summary

| Activity | Statement Impact | Cashback Impact | Atomic? |
|----------|------------------|-----------------|---------|
| **Purchase** | Charge (+) | Earn (+) | ✓ Yes |
| **Payment** | Credit (-) | None | ✗ No |
| **Refund** | Credit (-) | Deduct (-) | ✓ Yes |
| **Cash Advance** | Charge (+) | None | ✗ No |
| **Cashback Redemption** | Credit (-) | Deduct (-) | ✓ Yes |
| **Interest** | Charge (+) | None | ✗ No |
| **Late Fee** | Charge (+) | None | ✗ No |
| **Failed Payment Fee** | Charge (+) | None | ✗ No |
| **International Fee** | Charge (+) | None | ✗ No |
| **Manual Adjustment** | +/- | Optional | Maybe |

---

## FAQ

### Q: How is this different from a regular ledger?

**A:** This is specifically a **revolving credit card** ledger:
- Has a credit limit (not unlimited)
- Calculates GAAP-compliant interest on unpaid balances
- Generates monthly billing statements
- Tracks minimum payments and due dates
- Assesses fees (late, international, failed payment, etc.)
- Integrates cashback rewards earning and redemption
- Supports multiple APR types (purchase, cash advance, penalty)

### Q: How should I design my cashback ledger system of records?

**A:** Use an **event sourcing approach** with immutable entries:

1. **Single source of truth**: `cashback_ledger_entries` table
2. **Append-only**: Never update/delete entries, only append new ones
3. **Linked to statement**: Use `statement_entry_id` to link earning events
4. **External tracking**: Use `external_reference_id` for platform redemptions
5. **Calculate balances**: Use database views to sum entries in real-time
6. **Atomic operations**: Use database transactions for consistency

**Key design principles**:
- Store events (what happened), not state (current balance)
- Balance = `SUM(all points entries)` for a credit card
- Separate earned vs redeemed tracking
- Maintain audit trail with timestamps and creators

### Q: How should I reconcile these two ledgers?

**A:** Use a **coordination service** that ensures atomic updates:

1. **Shared transactions**: Use database transactions to update both ledgers atomically
2. **Event linking**: Link related entries via foreign keys (`statement_entry_id`)
3. **Validation**: Check business rules before creating entries (e.g., sufficient points)
4. **One-way flows**:
   - Purchases → Earn cashback (statement → cashback)
   - Redemptions → Apply credits (cashback → statement)
5. **Independent balances**: Each ledger maintains its own balance calculation

### Q: If a cardholder paid their statement balance, will that affect their cashback balance?

**A:** **NO**, payments do NOT affect cashback balance.

**Why?**
- Cashback is earned when you **spend money** (purchases)
- Paying your bill is just **settling the debt**, not spending
- Your cashback balance remains the same after payment

**Example**:
```
Starting state:
  Statement Balance: $500
  Cashback Balance: 1000 points

Cardholder pays $500:
  Statement Balance: $0      (decreased by payment)
  Cashback Balance: 1000 points (UNCHANGED)
```

**What DOES affect cashback?**
- **Earning**: Making purchases (transactions)
- **Spending**: Redeeming points for statement credits or rewards
- **Adjustments**: Refunds on purchases that earned points

### Q: How is interest calculated?

**A:** Using the GAAP-compliant **Average Daily Balance (ADB)** method:

1. Track the balance at the end of each day in the billing cycle
2. Calculate the average of all daily balances
3. Multiply by the Daily Periodic Rate (APR / 365)
4. Multiply by the number of days in the cycle

**Formula**: `Interest = ADB × (APR / 365) × Days in Cycle`

### Q: What is a grace period?

**A:** If you paid your previous statement balance in full by the due date, you won't be charged interest on new purchases during the current billing cycle. This is the "grace period."

**Important**: Grace period does NOT apply to:
- Cash advances (interest starts immediately)
- Balances carried over from previous cycles

### Q: How do I reverse a transaction?

**A:** Create a new entry with the opposite effect. Never delete or update existing entries.

**Examples**:
- To reverse a $100 purchase → Create a $100 refund entry
- To reverse a fee → Create a credit entry for the fee amount
- To reverse a payment → Create an adjustment entry

### Q: Can available credit be negative?

**A:** Yes, if the balance exceeds the credit limit (over-limit situation). This typically triggers an over-limit fee (if configured).

**Example**:
```
Credit Limit: $1000
Current Balance: $1050
Available Credit: -$50 (over limit by $50)
```

### Q: What happens when a payment fails?

**A:** 
1. Payment state changes from `processing` to `failed`
2. Failed payment fee is assessed (e.g., $25)
3. Original charge remains on the balance
4. Payment may be retried if retries are available
5. ACH return code is tracked for audit purposes

---

## Examples

### Example 1: Complete Purchase Flow with Cashback

```sql
-- 1. Cardholder makes a $100 purchase
-- Statement Ledger Entry
INSERT INTO statement_ledger_entries (
    id, tenant_id, entry_type, amount, description, 
    entry_date, posting_date, status
) VALUES (
    gen_random_uuid(), 'tenant-123', 'transaction', 100.00, 
    'Purchase at Amazon',
    NOW(), CURRENT_DATE, 'pending'
);

-- Cashback Ledger Entry (linked)
INSERT INTO cashback_ledger_entries (
    id, tenant_id, credit_card_id, statement_entry_id, 
    entry_type, points, entry_date
) VALUES (
    gen_random_uuid(), 'tenant-123', 'card-456', '[statement-entry-id]',
    'earned_transaction', 100, NOW()
);

-- Update Available Credit
UPDATE credit_cards
SET available_credit = available_credit - 100.00
WHERE id = 'card-456';

-- Result:
--   Statement Balance: +$100
--   Cashback Balance: +100 points
--   Available Credit: $900 (was $1000)
```

### Example 2: Payment (No Cashback Impact)

```sql
-- 2. Cardholder pays $100
-- Statement Ledger Entry ONLY
INSERT INTO statement_ledger_entries (
    id, tenant_id, entry_type, amount, description, 
    entry_date, posting_date, status
) VALUES (
    gen_random_uuid(), 'tenant-123', 'payment', 100.00,
    'Payment received',
    NOW(), CURRENT_DATE, 'cleared'
);

-- Update Available Credit
UPDATE credit_cards
SET available_credit = available_credit + 100.00
WHERE id = 'card-456';

-- NO cashback ledger entry!

-- Result:
--   Statement Balance: -$100
--   Cashback Balance: UNCHANGED
--   Available Credit: $1000 (was $900)
```

### Example 3: Interest Calculation and Accrual

```go
// Calculate interest for billing cycle
func CalculateAndAccrueInterest(card *CreditCard, cycle *BillingCycle) {
    // Step 1: Get daily balances
    dailyBalances := GetDailyBalances(card.ID, cycle.StartDate, cycle.EndDate)
    
    // Step 2: Calculate ADB
    adb := CalculateAverageDailyBalance(dailyBalances)
    // Example: $1100
    
    // Step 3: Get DPR
    apr := card.PurchaseAPR  // 18.25%
    dpr := apr / 365         // 0.05%
    
    // Step 4: Calculate interest
    daysInCycle := cycle.DaysInCycle  // 30
    interest := adb * dpr * daysInCycle
    // $1100 * 0.0005 * 30 = $16.50
    
    // Step 5: Check grace period
    if QualifiesForGracePeriod(card, cycle) {
        interest = 0  // Waived
    }
    
    // Step 6: Apply minimum interest charge
    if interest > 0 && interest < MinimumInterestCharge {
        interest = MinimumInterestCharge  // e.g., $0.50
    }
    
    // Step 7: Create interest entry
    CreateStatementEntry(
        type: "fee_interest",
        amount: interest,
        description: fmt.Sprintf("Interest charge (APR: %.2f%%, ADB: $%.2f)", apr, adb),
    )
}
```

### Example 4: Cashback Redemption

```sql
-- 3. Cardholder redeems 1000 points for $10 credit
-- Cashback Ledger Entry (deduction)
INSERT INTO cashback_ledger_entries (
    id, tenant_id, credit_card_id, entry_type, points, entry_date
) VALUES (
    gen_random_uuid(), 'tenant-123', 'card-456',
    'redeemed_spent', -1000, NOW()
);

-- Statement Ledger Entry (credit)
INSERT INTO statement_ledger_entries (
    id, tenant_id, entry_type, amount, description, 
    entry_date, posting_date, status
) VALUES (
    gen_random_uuid(), 'tenant-123', 'credit', 10.00,
    'Cashback redemption - 1000 points',
    NOW(), CURRENT_DATE, 'pending'
);

-- Result:
--   Statement Balance: -$10 (credit applied)
--   Cashback Balance: -1000 points
--   Available Credit: $1010 (was $1000)
```

### Example 5: Late Fee Assessment

```go
// Assess late fee if payment overdue
func AssessLateFeeIfNeeded(card *CreditCard, cycle *BillingCycle) {
    currentDate := time.Now()
    
    // Check if overdue
    if !cycle.IsOverdue(currentDate) {
        return  // Not overdue yet
    }
    
    // Check if minimum payment was met
    if cycle.MinimumPaymentMet {
        return  // No late fee if minimum was paid
    }
    
    // Check if we already assessed a late fee for this cycle
    if HasExistingLateFee(cycle.ID) {
        return  // Already assessed
    }
    
    // Assess late fee
    lateFee := card.LatePaymentFee  // e.g., $35
    
    CreateStatementEntry(
        type: "fee_late",
        amount: lateFee,
        description: fmt.Sprintf("Late payment fee - %d days overdue", 
            cycle.DaysOverdue(currentDate)),
        statement_id: cycle.ID,
    )
    
    // Result:
    //   Statement Balance: +$35
    //   Cashback Balance: UNCHANGED (fees don't earn points)
}
```

---

## Data Integrity Guarantees

### Database Constraints

1. **Immutability**: Triggers prevent updates to ledger entries (append-only)
2. **Referential Integrity**: Foreign keys ensure valid relationships
3. **Check Constraints**: Validate amounts are non-negative, points are non-zero
4. **Unique Constraints**: Prevent duplicate entries
5. **Decimal Precision**: All currency stored as DECIMAL(15,2)

### Application-Level Validation

1. **Atomic Transactions**: All related ledger updates in single DB transaction
2. **Balance Validation**: Check sufficient points before redemption
3. **Credit Limit Validation**: Check available credit before transactions
4. **Status Tracking**: Entries move through pending → cleared → reversed
5. **Audit Trail**: All entries record creator and timestamps

---

## Performance Considerations

### Indexing Strategy

```sql
-- Fast tenant/card balance lookups
CREATE INDEX idx_statement_entries_tenant ON statement_ledger_entries(tenant_id);
CREATE INDEX idx_cashback_entries_card ON cashback_ledger_entries(credit_card_id);

-- Fast billing cycle queries
CREATE INDEX idx_statement_entries_posting_date ON statement_ledger_entries(posting_date);
CREATE INDEX idx_statement_entries_statement_id ON statement_ledger_entries(statement_id);

-- Fast status queries
CREATE INDEX idx_statement_entries_status ON statement_ledger_entries(status);
```

### Views for Real-Time Balances

```sql
-- Real-time statement balance
CREATE VIEW statement_balances AS
SELECT 
    tenant_id,
    SUM(
        CASE 
            WHEN entry_type IN ('transaction', 'cash_advance', 'fee_late', 
                                'fee_failed', 'fee_international', 'fee_interest',
                                'fee_cash_advance', 'fee_annual', 'fee_over_limit')
                THEN amount
            WHEN entry_type IN ('payment', 'refund', 'credit')
                THEN -amount
            WHEN entry_type = 'adjustment'
                THEN amount  -- Can be positive or negative
            ELSE 0
        END
    ) as current_balance
FROM statement_ledger_entries
WHERE status = 'cleared'
GROUP BY tenant_id;

-- Real-time cashback balance
CREATE VIEW cashback_balances AS
SELECT 
    credit_card_id,
    SUM(points) as available_points
FROM cashback_ledger_entries
GROUP BY credit_card_id;
```

---

## Next Steps

1. **Implement Automated Billing**: Scheduled job to close cycles and generate statements
2. **Add Payment Processor Integration**: Connect to ACH/card payment processors
3. **Build Reconciliation Reports**: Compare ledgers for audit purposes
4. **Add Dispute Management**: Handle transaction disputes and chargebacks
5. **Create API Endpoints**: REST API for all credit card operations
6. **Build Cardholder Portal**: Web interface for viewing statements and making payments
7. **Add Notifications**: Email/SMS for due dates, payments, and alerts
8. **Implement Fraud Detection**: Monitor for suspicious transaction patterns
