# EZ Ledger - Credit Card Ledger System

A comprehensive, GAAP-compliant credit card ledger system with integrated cashback rewards tracking. Built with Go and PostgreSQL for precision, reliability, and auditability.

## Overview

**EZ Ledger** implements a complete revolving credit card accounting system with two coordinated ledgers:

1. **Statement Ledger** (Main Feature): Tracks all financial activities - purchases, payments, fees, interest, and refunds
2. **Cashback Points Ledger** (Bonus Feature): Tracks reward points earned and redeemed

### Key Features

- ✅ GAAP-compliant interest calculation (Average Daily Balance method)
- ✅ Complete billing cycle management
- ✅ Automated fee assessment (late fees, international fees, etc.)
- ✅ Payment processing with state machine
- ✅ Cashback rewards earning and redemption
- ✅ Event-sourced immutable ledger entries
- ✅ Real-time balance calculation
- ✅ Full audit trail
- ✅ Multi-tenant support
- ✅ PostgreSQL with strict data integrity

---

## Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL 14+

### Installation

```bash
# Clone repository
git clone <repository-url>
cd ez-ledger

# Install dependencies
go mod download

# Create database
createdb ezledger

# Run migrations
psql ezledger < migrations/001_create_ledger_tables.sql

# Run tests
go test ./...
```

---

## Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                     Application Layer                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Credit Card Service (Orchestrates all operations)         │ │
│  └──────┬──────────────┬──────────────┬──────────────┬────────┘ │
│         │              │              │              │          │
│    ┌────▼────┐   ┌────▼────┐   ┌────▼────┐   ┌────▼────┐     │
│    │Billing  │   │Interest │   │  Fee    │   │Payment  │     │
│    │Service  │   │Service  │   │Service  │   │Service  │     │
│    └────┬────┘   └────┬────┘   └────┬────┘   └────┬────┘     │
│         │              │              │              │          │
│         └──────────────┴──────────────┴──────────────┘          │
│                            │                                     │
│         ┌──────────────────┴──────────────────┐                 │
│         │                                      │                 │
│    ┌────▼──────────────┐          ┌──────────▼─────────┐       │
│    │ Statement Ledger  │          │ Cashback Service   │       │
│    │ Service           │          │ (Points Ledger)    │       │
│    └────┬──────────────┘          └──────────┬─────────┘       │
│         │                                      │                 │
├─────────┼──────────────────────────────────────┼─────────────────┤
│         │         Database Layer               │                 │
├─────────┼──────────────────────────────────────┼─────────────────┤
│         │                                      │                 │
│    ┌────▼──────────────┐          ┌──────────▼─────────┐       │
│    │ statement_ledger  │          │ cashback_ledger    │       │
│    │ _entries          │          │ _entries           │       │
│    │ (immutable)       │          │ (immutable)        │       │
│    └───────────────────┘          └────────────────────┘       │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

### Data Flow

```
Purchase Transaction
    ↓
1. Create statement ledger entry (+$100 charge)
2. Assess international fee if applicable (+$3)
3. Calculate cashback (100 points earned)
4. Update available credit ($900 remaining)
    ↓
Statement Balance: $103 | Points: 100

Payment Received
    ↓
1. Create payment record (state: pending)
2. Create statement ledger entry (-$100 credit)
3. Update available credit ($1000 available)
4. Update payment state (pending → processing → cleared)
    ↓
Statement Balance: $3 | Points: 100 (unchanged)
```

---

## Core Feature: Revolving Credit Card Ledger

### What is a Revolving Credit Card Ledger?

A revolving credit card ledger is an accounting system that tracks:
- **Credit limit**: Maximum borrowing capacity
- **Available credit**: Credit limit minus current balance
- **Purchases**: Transactions that increase the balance
- **Payments**: Reduce the balance (but don't eliminate the account)
- **Interest**: Charges on unpaid balances
- **Fees**: Late fees, international fees, etc.

Unlike a traditional loan (which decreases to zero), a credit card balance "revolves" - you can borrow, pay, and borrow again up to your credit limit.

### Statement Ledger Entry Types

| Entry Type | Effect | Example |
|------------|--------|---------|
| `transaction` | Increases balance | Purchase at Amazon: +$50 |
| `payment` | Decreases balance | Payment received: -$100 |
| `refund` | Decreases balance | Return to store: -$25 |
| `cash_advance` | Increases balance | ATM withdrawal: +$200 |
| `fee_late` | Increases balance | Late payment: +$35 |
| `fee_failed` | Increases balance | Returned payment: +$25 |
| `fee_international` | Increases balance | Foreign transaction: +$3 (3%) |
| `fee_interest` | Increases balance | Interest charge: +$15.50 |
| `fee_cash_advance` | Increases balance | Cash advance fee: +$10 |
| `adjustment` | Increases/decreases | Manual adjustment: ±$X |
| `credit` | Decreases balance | Fee waiver: -$35 |

### Billing Cycle Workflow

```
Day 1-30: Billing Cycle
├─ Day 5:  Purchase $100 → Balance: $100
├─ Day 15: Purchase $50  → Balance: $150
├─ Day 20: Payment $75   → Balance: $75
└─ Day 30: Cycle ends

Day 31: Statement Generated
├─ Previous balance: $0
├─ Purchases: $150
├─ Payments: $75
├─ Fees: $0
├─ Interest: $0
├─ New balance: $75
├─ Minimum payment: $25 (or 3% of balance)
└─ Due date: Day 55 (25 days later)

Day 55: Payment Due
├─ If paid in full ($75) → No interest on next cycle
└─ If paid minimum ($25) → Interest accrues on remaining $50
```

### Interest Calculation (GAAP-Compliant)

**Method**: Average Daily Balance (ADB)

```
Step 1: Calculate daily balances for the billing cycle
  Day 1-5:   $0
  Day 6-15:  $100
  Day 16-20: $150
  Day 21-30: $75

Step 2: Calculate Average Daily Balance
  ADB = (0×5 + 100×10 + 150×5 + 75×10) / 30
      = (0 + 1000 + 750 + 750) / 30
      = $83.33

Step 3: Calculate Daily Periodic Rate
  APR = 18.25%
  DPR = 18.25% / 365 = 0.05%

Step 4: Calculate Interest
  Interest = ADB × DPR × Days in cycle
           = $83.33 × 0.0005 × 30
           = $1.25
```

**Grace Period**: If the previous statement balance was paid in full by the due date, no interest is charged on new purchases during the current cycle.

### Fee Assessment

#### Late Payment Fee
- **Trigger**: Minimum payment not received by due date
- **Amount**: Configured per card (e.g., $35)
- **Frequency**: Once per billing cycle

#### Failed Payment Fee
- **Trigger**: Payment returned/failed (NSF, closed account, etc.)
- **Amount**: Configured per card (e.g., $25)
- **Includes**: ACH return code tracking

#### International Transaction Fee
- **Trigger**: Transaction in foreign currency
- **Amount**: Percentage of transaction (e.g., 3%)
- **Calculation**: `Fee = Transaction Amount × Fee Rate`

#### Cash Advance Fee
- **Trigger**: ATM withdrawal or cash equivalent
- **Amount**: Greater of flat fee or percentage (e.g., $10 or 5%)
- **Calculation**: `Fee = MAX(Flat Fee, Amount × Fee Rate)`

### Payment Processing

Payments follow a state machine:

```
pending → processing → cleared
   ↓           ↓          ↓
cancelled   failed    returned
               ↓
            retrying → pending (retry)
```

**States**:
- `pending`: Payment initiated, awaiting processing
- `processing`: Being processed by payment processor
- `cleared`: Successfully processed and applied
- `failed`: Processing failed (insufficient funds, etc.)
- `returned`: Cleared payment was returned (ACH return)
- `cancelled`: Cancelled before processing
- `reversed`: Manual reversal of cleared payment

---

## Bonus Feature: Cashback Points Ledger

### How Cashback Works

The cashback system tracks reward points separately from the statement balance:

1. **Earning**: Points earned on qualifying purchases
2. **Redemption**: Points redeemed for statement credits
3. **Adjustments**: Points adjusted for refunds

### Points Earning Rules

```go
// Example: 1% cashback
PointsEarningRule{
    PointsPerDollar: 0.01,  // 1 point per dollar
    MinAmount: 1.00,         // Minimum $1 transaction
}

// Purchase $100 → Earn 100 points
// Purchase $0.50 → Earn 0 points (below minimum)
```

**Category Multipliers** (optional):
- Dining: 3x points
- Gas: 2x points
- Everything else: 1x points

### Points Ledger Entry Types

| Entry Type | Effect | Example |
|------------|--------|---------|
| `earned_transaction` | Increases points | Purchase $100 → +100 points |
| `redeemed_spent` | Decreases points | Redeem for $10 credit → -1000 points |
| `redeemed_external` | Decreases points | Redeem via Keystone → -500 points |
| `adjusted_refund` | Decreases points | Refund $50 → -50 points |
| `adjusted_manual` | Increases/decreases | Manual adjustment → ±X points |
| `expired` | Decreases points | Points expired → -100 points |

### Redemption Flow

```
Check Points Balance
    ↓
Validate Sufficient Points (≥ redemption amount)
    ↓
Create Points Ledger Entry (-1000 points)
    ↓
Create Statement Ledger Entry (-$10 credit)
    ↓
Update Balances
    ↓
Points: 1500 → 500
Statement: $50 → $40
```

### Important: Payments Don't Earn Points!

```
❌ INCORRECT: "I paid my bill, I should get points"
✓ CORRECT:   "Points are earned when spending, not when paying"

Transaction (spending):  +$100 charge  → +100 points earned
Payment (paying bill):   -$100 credit  → NO points change
Refund (return item):    -$50 credit   → -50 points deducted
```

**Why?** Points are a reward for spending money with merchants, not for paying your credit card bill.

---

## Usage Examples

### Example 1: Record a Purchase with Cashback

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
    // Initialize services
    db := // ... database connection
    cardService := services.NewCreditCardService(db)
    
    // Get credit card
    card, _ := cardService.GetCreditCard(ctx, cardID)
    
    // Record a $100 purchase
    req := services.CCTransactionRequest{
        CreditCard:      card,
        Amount:          decimal.NewFromFloat(100.00),
        Description:     "Purchase at Amazon",
        MerchantName:    "Amazon.com",
        MerchantCategory: "5999", // MCC code
        TransactionDate: time.Now(),
        PostingDate:     time.Now(),
        ReferenceID:     "txn-12345",
        IsInternational: false,
    }
    
    result, err := cardService.RecordTransaction(ctx, req)
    
    // Result:
    //   Statement entry: +$100 charge
    //   Cashback entry: +100 points earned
    //   Available credit: $900 (was $1000)
}
```

### Example 2: Calculate Interest for Billing Cycle

```go
// Initialize interest service
interestService := services.NewInterestService(db)

// Get billing cycle
cycle, _ := billingService.GetBillingCycle(ctx, cycleID)

// Calculate interest
config := services.DefaultInterestConfig()
result, err := interestService.CalculateInterest(ctx, card, cycle, config)

fmt.Printf("Interest charge: $%.2f\n", result.InterestCharge)
fmt.Printf("APR used: %.2f%%\n", result.APRUsed)
fmt.Printf("Average daily balance: $%.2f\n", result.AverageDailyBalance)
fmt.Printf("Days in cycle: %d\n", result.DaysInCycle)

// Accrue interest to statement
entry, err := interestService.AccrueInterest(ctx, tenantID, cycle, result)
```

### Example 3: Process a Payment

```go
// Initialize payment service
paymentService := services.NewPaymentService(ledgerService, feeService)

// Initiate payment
req := services.InitiatePaymentRequest{
    TenantID:      tenantID,
    CreditCardID:  cardID,
    Amount:        decimal.NewFromFloat(100.00),
    PaymentType:   models.PaymentTypeFull,
    PaymentMethod: models.PaymentMethodACH,
    CreatedBy:     "user-123",
}

paymentResult, err := paymentService.InitiatePayment(req)

// Process payment
paymentResult, err = paymentService.ProcessPayment(
    paymentResult.Payment,
    "processor-ref-456",
)

// Clear payment
paymentResult, err = paymentService.ClearPayment(
    paymentResult.Payment,
    card,
    "confirmation-789",
)

// Result:
//   Payment state: pending → processing → cleared
//   Statement entry: -$100 credit
//   Available credit: $1000 (was $900)
```

### Example 4: Redeem Cashback Points

```go
// Initialize cashback service
cashbackService := services.NewCashbackService(db)

// Redeem 1000 points for $10 statement credit
req := services.RedeemCashbackRequest{
    TenantID:       tenantID,
    CreditCard:     card,
    PointsToRedeem: 1000,
    CreditAmount:   decimal.NewFromFloat(10.00),
    RedemptionDate: time.Now(),
    Description:    "Cashback redemption",
}

cashbackEntry, statementEntry, err := cashbackService.RedeemCashback(ctx, req)

// Result:
//   Points entry: -1000 points
//   Statement entry: -$10 credit
//   Points balance: 1500 → 500
//   Statement balance: $50 → $40
```

### Example 5: Assess Late Payment Fee

```go
// Initialize fee service
feeService := services.NewFeeService(db)

// Check if payment is overdue
if cycle.IsOverdue(time.Now()) && !cycle.MinimumPaymentMet {
    req := services.LatePaymentFeeRequest{
        CreditCard:  card,
        BillingCycle: cycle,
        CurrentDate: time.Now(),
        DaysOverdue: cycle.DaysOverdue(time.Now()),
    }
    
    feeResult, err := feeService.AssessLatePaymentFee(ctx, req)
    
    // Result:
    //   Fee entry: +$35 late payment fee
    //   Statement balance increased by $35
}
```

---

## Database Schema

### Statement Ledger Entries

```sql
CREATE TABLE statement_ledger_entries (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    statement_id UUID,  -- Links to billing cycle
    entry_type statement_entry_type NOT NULL,
    entry_date TIMESTAMP NOT NULL,
    posting_date DATE NOT NULL,
    amount DECIMAL(15,2) NOT NULL,
    description TEXT NOT NULL,
    reference_id VARCHAR(100),
    metadata JSONB,
    status VARCHAR(20) NOT NULL,  -- pending, cleared, reversed
    cleared_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    created_by VARCHAR(100),
    
    CONSTRAINT positive_amount CHECK (amount >= 0)
);

CREATE INDEX idx_statement_ledger_tenant ON statement_ledger_entries(tenant_id);
CREATE INDEX idx_statement_ledger_posting ON statement_ledger_entries(posting_date);
```

### Cashback Ledger Entries

```sql
CREATE TABLE cashback_ledger_entries (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    credit_card_id UUID NOT NULL,
    entry_type cashback_entry_type NOT NULL,
    points INTEGER NOT NULL,
    statement_entry_id UUID,  -- Link to related transaction
    external_reference_id VARCHAR(100),
    description TEXT,
    entry_date TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    
    CONSTRAINT points_not_zero CHECK (points != 0)
);

CREATE INDEX idx_cashback_tenant ON cashback_ledger_entries(tenant_id);
CREATE INDEX idx_cashback_card ON cashback_ledger_entries(credit_card_id);
```

### Credit Cards

```sql
CREATE TABLE credit_cards (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    cardholder_name VARCHAR(100) NOT NULL,
    credit_limit DECIMAL(15,2) NOT NULL,
    available_credit DECIMAL(15,2) NOT NULL,
    
    -- APRs
    purchase_apr DECIMAL(5,2) NOT NULL,
    cash_advance_apr DECIMAL(5,2) NOT NULL,
    penalty_apr DECIMAL(5,2),
    
    -- Fees
    annual_fee DECIMAL(10,2) DEFAULT 0,
    late_payment_fee DECIMAL(10,2) DEFAULT 0,
    failed_payment_fee DECIMAL(10,2) DEFAULT 0,
    international_fee_rate DECIMAL(5,4) DEFAULT 0,
    
    -- Cashback
    cashback_enabled BOOLEAN DEFAULT false,
    cashback_rate DECIMAL(5,4) DEFAULT 0,
    
    -- Status
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

### Balance Views

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

-- Real-time points balance
CREATE VIEW cashback_balances AS
SELECT 
    tenant_id,
    credit_card_id,
    SUM(points) as available_points
FROM cashback_ledger_entries
GROUP BY tenant_id, credit_card_id;
```

---

## Design Principles

1. **Immutability**: Ledger entries are never modified, only appended
2. **Atomicity**: Related ledger updates happen in single database transaction
3. **Auditability**: Complete history with timestamps and creators
4. **Precision**: Use `decimal.Decimal` for all currency calculations
5. **Separation of Concerns**: Statement and cashback are independent ledgers
6. **GAAP Compliance**: Interest calculations follow accounting standards
7. **Data Integrity**: Database constraints enforce business rules

---

## FAQ

### Q: How is this different from a regular ledger?

**A:** This is specifically a **revolving credit card** ledger:
- Has a credit limit (not unlimited)
- Calculates interest on unpaid balances
- Generates monthly statements
- Tracks minimum payments
- Assesses fees (late, international, etc.)
- Integrates cashback rewards

### Q: Do payments affect points balance?

**A:** No! Payments only affect the statement balance. Points are earned when you **spend** (transactions), not when you **pay** your bill.

### Q: How is interest calculated?

**A:** Using the GAAP-compliant Average Daily Balance (ADB) method:
1. Calculate the average of daily balances for the billing cycle
2. Multiply by the Daily Periodic Rate (APR / 365)
3. Multiply by the number of days in the cycle

### Q: What is a grace period?

**A:** If you paid your previous statement balance in full by the due date, you won't be charged interest on new purchases during the current billing cycle. This is the "grace period."

### Q: How do I reverse a transaction?

**A:** Create a new entry with the opposite effect. Never delete or update existing entries. For example, to reverse a $100 purchase, create a $100 refund entry.

### Q: Can available credit be negative?

**A:** Yes, if the balance exceeds the credit limit (over-limit situation). This typically triggers an over-limit fee.

### Q: What happens when a payment fails?

**A:** 
1. Payment state changes to `failed`
2. Failed payment fee is assessed
3. Original charge is re-applied to balance
4. Payment may be retried if retries are available

### Q: How are minimum payments calculated?

**A:** `Minimum = MAX(Balance × Percent, Fixed Amount)`

Example: For a $1000 balance with 3% minimum or $25 minimum:
- `MAX($1000 × 0.03, $25) = MAX($30, $25) = $30`

---

## Project Structure

```
ez-ledger/
├── src/
│   ├── models/                         # Data models
│   │   ├── billing_cycle.go           # Billing cycle management
│   │   ├── cashback.go                # Cashback rewards
│   │   ├── credit_card.go             # Credit card accounts
│   │   ├── payment.go                 # Payment processing
│   │   ├── points_ledger.go           # Points tracking
│   │   ├── statement.go               # Statement generation
│   │   ├── statement_ledger.go        # Transaction ledger
│   │   └── tenant.go                  # Multi-tenancy
│   └── services/                       # Business logic
│       ├── billing_service.go         # Billing cycle operations
│       ├── cashback_service.go        # Cashback calculations
│       ├── credit_card_service.go     # Card operations
│       ├── fee_service.go             # Fee assessment
│       ├── interest_service.go        # Interest calculations
│       ├── payment_service.go         # Payment processing
│       ├── points_ledger_service.go   # Points tracking
│       └── statement_ledger_service.go # Transaction ledger
├── tests/
│   └── unit/                          # Unit tests
│       ├── billing_cycle_test.go
│       ├── cashback_test.go
│       ├── credit_card_test.go
│       └── payment_test.go
├── docs/
│   ├── LEDGER_DESIGN.md              # Detailed design
│   └── RECONCILIATION_FLOWS.md       # Flow documentation
├── migrations/
│   └── 001_create_ledger_tables.sql  # Database schema
├── go.mod
├── CLAUDE.md                          # AI assistant guide
└── README.md                          # This file
```

---

## Documentation

- [CLAUDE.md](CLAUDE.md) - AI assistant development guide
- [docs/LEDGER_DESIGN.md](docs/LEDGER_DESIGN.md) - Detailed system design
- [docs/RECONCILIATION_FLOWS.md](docs/RECONCILIATION_FLOWS.md) - Reconciliation flows

---

## Contributing

See [CLAUDE.md](CLAUDE.md) for development guidelines, coding standards, and AI assistant instructions.

---

## License

MIT

---

## Acknowledgments

Built with precision decimal arithmetic, GAAP-compliant accounting standards, and event sourcing best practices.
