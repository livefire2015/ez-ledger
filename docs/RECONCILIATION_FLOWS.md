# Credit Card Ledger Reconciliation Flows

This document visualizes how the Statement and Cashback ledgers interact for different types of credit card activities.

## Visual Notation

```
[S] = Statement Ledger
[C] = Cashback Ledger
→   = Creates entry
✗   = No entry created
⚡  = Atomic transaction (both or neither)
```

---

## Flow 1: Purchase Transaction (Earns Cashback)

**Scenario**: Cardholder makes a $100 purchase at Amazon

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Purchase Transaction $100                                │
│ Merchant: Amazon.com | MCC: 5999                                │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Credit Card Service           │
                │  RecordTransaction()           │
                │  ⚡ Atomic Transaction          │
                └────────┬──────────────┬────────┘
                         │              │
         ┌───────────────▼──┐      ┌───▼──────────────────┐
         │ Statement Ledger │      │ Cashback Ledger      │
         └──────────────────┘      └──────────────────────┘

STATEMENT LEDGER [S]                 CASHBACK LEDGER [C]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry Type:         │              │ Entry Type:          │
│   transaction       │              │   earned_transaction │
│ Amount: +$100.00    │              │ Points: +100         │
│ Status: pending     │              │ Linked: statement_id │
│ Description:        │              │ Transaction: $100    │
│   "Amazon.com"      │              │ Rate: 0.01 (1%)      │
│ Merchant: Amazon    │              │ Category: general    │
│ MCC: 5999           │              │                      │
└─────────────────────┘              └──────────────────────┘

CREDIT CARD UPDATE:
  Available Credit: $1000 → $900 (decreased by $100)

RESULT:
  Statement Balance: +$100.00
  Cashback Balance: +100 points
  Available Credit: $900
  Both entries linked via foreign key
```

---

## Flow 2: Payment (No Cashback Impact)

**Scenario**: Cardholder pays $100 towards their balance

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Payment $100                                              │
│ Method: ACH | Reference: PMT-12345                              │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Payment Service               │
                │  ProcessPayment()              │
                │  States: pending → processing  │
                │         → cleared              │
                └────────┬───────────────────────┘
                         │
         ┌───────────────▼──┐
         │ Statement Ledger │
         └──────────────────┘

STATEMENT LEDGER [S]                 CASHBACK LEDGER [C]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry Type:         │              │                      │
│   payment           │              │   NO ENTRY ✗         │
│ Amount: $100.00     │              │                      │
│ Status: cleared     │              │                      │
│ Description:        │              │                      │
│   "Payment PMT..."  │              │                      │
│ Reference: PMT-123  │              │                      │
└─────────────────────┘              └──────────────────────┘

CREDIT CARD UPDATE:
  Available Credit: $900 → $1000 (increased by $100)

RESULT:
  Statement Balance: -$100.00 (credit applied)
  Cashback Balance: UNCHANGED
  Available Credit: $1000

⚠️  KEY INSIGHT: Payments only affect statement ledger, NOT cashback!
    You earn points when SPENDING, not when PAYING your bill.
```

---

## Flow 3: Refund (Adjusts Both Ledgers)

**Scenario**: Cardholder returns a $50 item (originally earned 50 points)

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Refund $50                                                │
│ Original transaction earned 50 points                           │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Credit Card Service           │
                │  RecordRefund()                │
                │  ⚡ Atomic Transaction          │
                └────────┬──────────────┬────────┘
                         │              │
         ┌───────────────▼──┐      ┌───▼──────────────────┐
         │ Statement Ledger │      │ Cashback Ledger      │
         └──────────────────┘      └──────────────────────┘

STATEMENT LEDGER [S]                 CASHBACK LEDGER [C]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry Type:         │              │ Entry Type:          │
│   refund            │              │   adjusted_refund    │
│ Amount: $50.00      │              │ Points: -50          │
│ Status: pending     │              │ Linked: statement_id │
│ Description:        │              │ Transaction: $50     │
│   "Refund for..."   │              │ Rate: 0.01           │
│ Original Txn ID     │              │ Reason: refund       │
└─────────────────────┘              └──────────────────────┘

CREDIT CARD UPDATE:
  Available Credit: $900 → $950 (increased by $50)

RESULT:
  Statement Balance: -$50.00 (credit applied)
  Cashback Balance: -50 points (deducted)
  Available Credit: $950
  Both ledgers adjusted proportionally
```

---

## Flow 4: Cashback Redemption for Statement Credit

**Scenario**: Cardholder redeems 1000 points for $10 statement credit

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Redeem 1000 points → $10 credit                          │
│ Redemption Type: Statement Credit                               │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Cashback Service              │
                │  RedeemCashback()              │
                │  1. Validate points balance    │
                │  2. Create both entries        │
                │  ⚡ Atomic Transaction          │
                └────────┬──────────────┬────────┘
                         │              │
         ┌───────────────▼──┐      ┌───▼──────────────────┐
         │ Cashback Ledger  │      │ Statement Ledger     │
         └──────────────────┘      └──────────────────────┘

CASHBACK LEDGER [C]                  STATEMENT LEDGER [S]
┌──────────────────────┐              ┌─────────────────────┐
│ Entry Type:          │              │ Entry Type:         │
│   redeemed_spent     │◄─────link────┤   credit            │
│ Points: -1000        │              │ Amount: $10.00      │
│ Description:         │              │ Status: pending     │
│   "Redeemed for..."  │              │ Description:        │
│                      │              │   "Cashback: 1000"  │
└──────────────────────┘              └─────────────────────┘

CREDIT CARD UPDATE:
  Available Credit: $950 → $960 (increased by $10)

RESULT:
  Cashback Balance: -1000 points (deducted)
  Statement Balance: -$10.00 (credit applied)
  Available Credit: $960
  Cross-ledger transaction (points → money)
```

---

## Flow 5: Interest Charge (Statement Only)

**Scenario**: Monthly interest of $15.50 charged based on Average Daily Balance

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Calculate Interest for Billing Cycle                     │
│ ADB: $1100 | APR: 18.25% | Days: 30                            │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Interest Service              │
                │  CalculateInterest()           │
                │  Method: Average Daily Balance │
                │  Interest = ADB × DPR × Days   │
                └────────┬───────────────────────┘
                         │
         ┌───────────────▼──┐
         │ Statement Ledger │
         └──────────────────┘

STATEMENT LEDGER [S]                 CASHBACK LEDGER [C]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry Type:         │              │                      │
│   fee_interest      │              │   NO ENTRY ✗         │
│ Amount: +$15.50     │              │                      │
│ Status: pending     │              │                      │
│ Description:        │              │                      │
│   "Interest charge" │              │                      │
│ Metadata:           │              │                      │
│   APR: 18.25%       │              │                      │
│   ADB: $1100        │              │                      │
│   DPR: 0.05%        │              │                      │
│   Days: 30          │              │                      │
└─────────────────────┘              └──────────────────────┘

CREDIT CARD UPDATE:
  Available Credit: $960 → $944.50 (decreased by $15.50)

RESULT:
  Statement Balance: +$15.50 (charge added)
  Cashback Balance: UNCHANGED
  Available Credit: $944.50
  Interest charges don't earn cashback
```

---

## Flow 6: Late Payment Fee (Statement Only)

**Scenario**: Late payment fee of $35 assessed (payment overdue by 5 days)

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Late Fee $35                                              │
│ Reason: Minimum payment not received by due date                │
│ Days Overdue: 5                                                  │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Fee Service                   │
                │  AssessLatePaymentFee()        │
                └────────┬───────────────────────┘
                         │
         ┌───────────────▼──┐
         │ Statement Ledger │
         └──────────────────┘

STATEMENT LEDGER [S]                 CASHBACK LEDGER [C]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry Type:         │              │                      │
│   fee_late          │              │   NO ENTRY ✗         │
│ Amount: +$35.00     │              │                      │
│ Status: pending     │              │                      │
│ Description:        │              │                      │
│   "Late fee - 5..."  │              │                      │
│ Metadata:           │              │                      │
│   days_overdue: 5   │              │                      │
│   billing_cycle_id  │              │                      │
└─────────────────────┘              └──────────────────────┘

CREDIT CARD UPDATE:
  Available Credit: $944.50 → $909.50 (decreased by $35)

RESULT:
  Statement Balance: +$35.00 (charge added)
  Cashback Balance: UNCHANGED
  Available Credit: $909.50
  Fees don't earn cashback
```

---

## Flow 7: Cash Advance (No Cashback)

**Scenario**: Cardholder withdraws $200 cash from ATM

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Cash Advance $200                                         │
│ ATM Location: "Chase Bank ATM #1234"                            │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Credit Card Service           │
                │  RecordCashAdvance()           │
                │  1. Create cash advance entry  │
                │  2. Assess cash advance fee    │
                └────────┬───────────────────────┘
                         │
         ┌───────────────▼──┐
         │ Statement Ledger │
         └──────────────────┘

STATEMENT LEDGER [S]                 CASHBACK LEDGER [C]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry 1:            │              │                      │
│   cash_advance      │              │   NO ENTRY ✗         │
│ Amount: +$200.00    │              │                      │
│ Status: pending     │              │                      │
│ Description:        │              │                      │
│   "Cash advance..." │              │                      │
├─────────────────────┤              │                      │
│ Entry 2:            │              │                      │
│   fee_cash_advance  │              │                      │
│ Amount: +$10.00     │              │                      │
│ Status: pending     │              │                      │
│ Description:        │              │                      │
│   "Cash adv fee..." │              │                      │
└─────────────────────┘              └──────────────────────┘

CREDIT CARD UPDATE:
  Available Credit: $909.50 → $699.50 (decreased by $210)

RESULT:
  Statement Balance: +$210.00 ($200 + $10 fee)
  Cashback Balance: UNCHANGED
  Available Credit: $699.50
  Cash advances don't earn cashback
  Higher APR applies (no grace period)
```

---

## Flow 8: Failed Payment (Fee Assessment)

**Scenario**: Payment of $100 fails (NSF), failed payment fee assessed

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Payment $100 FAILED                                       │
│ Reason: Insufficient Funds (NSF)                                │
│ ACH Return Code: R01                                            │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Payment Service               │
                │  1. Update payment status      │
                │  2. Assess failed payment fee  │
                └────────┬───────────────────────┘
                         │
         ┌───────────────▼──┐
         │ Statement Ledger │
         └──────────────────┘

STATEMENT LEDGER [S]                 CASHBACK LEDGER [C]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry 1:            │              │                      │
│   payment           │              │   NO ENTRY ✗         │
│ Amount: $100.00     │              │                      │
│ Status: returned    │              │                      │
│ (original payment)  │              │                      │
├─────────────────────┤              │                      │
│ Entry 2:            │              │                      │
│   adjustment        │              │                      │
│ Amount: +$100.00    │              │                      │
│ Status: cleared     │              │                      │
│ (reversal)          │              │                      │
├─────────────────────┤              │                      │
│ Entry 3:            │              │                      │
│   fee_failed        │              │                      │
│ Amount: +$25.00     │              │                      │
│ Status: pending     │              │                      │
│ Description:        │              │                      │
│   "Failed payment"  │              │                      │
└─────────────────────┘              └──────────────────────┘

CREDIT CARD UPDATE:
  Available Credit: $699.50 → $574.50 (decreased by $125)

RESULT:
  Statement Balance: +$125.00 ($100 reversed + $25 fee)
  Cashback Balance: UNCHANGED
  Available Credit: $574.50
  Payment state: pending → processing → cleared → returned
```

---

## Reconciliation Matrix

| Activity Type | Statement Impact | Cashback Impact | Atomic? | Notes |
|---------------|------------------|-----------------|---------|-------|
| **Purchase** | Charge (+) | Earn (+) | ✓ Yes | Standard transaction |
| **Payment** | Credit (-) | None | ✗ No | Paying bill doesn't earn points |
| **Refund** | Credit (-) | Deduct (-) | ✓ Yes | Reverses original earning |
| **Cash Advance** | Charge (+) | None | ✗ No | No grace period, higher APR |
| **Cashback Redemption** | Credit (-) | Deduct (-) | ✓ Yes | Points → Statement credit |
| **Interest** | Charge (+) | None | ✗ No | Based on ADB calculation |
| **Late Fee** | Charge (+) | None | ✗ No | Assessed if payment overdue |
| **Failed Payment Fee** | Charge (+) | None | ✗ No | Payment returned/failed |
| **International Fee** | Charge (+) | None | ✗ No | % of foreign transaction |
| **Cash Advance Fee** | Charge (+) | None | ✗ No | Flat fee or % of advance |
| **Annual Fee** | Charge (+) | None | ✗ No | Yearly membership fee |
| **Manual Adjustment** | +/- | Optional | Maybe | Requires approval |
| **Fee Waiver** | Credit (-) | None | ✗ No | Reverses previously assessed fee |

---

## Atomic Transaction Guarantee

All reconciliation operations that affect both ledgers use database transactions:

```go
BEGIN TRANSACTION

  // Step 1: Validate business rules
  if err := ValidateBusinessRules(); err != nil {
      ROLLBACK
      return err
  }

  // Step 2: Create statement ledger entry
  statementEntry, err := CreateStatementEntry(...)
  if err != nil {
      ROLLBACK
      return err
  }

  // Step 3: Create cashback ledger entry (if applicable)
  if shouldEarnCashback {
      cashbackEntry, err := CreateCashbackEntry(...)
      if err != nil {
          ROLLBACK  // Rolls back statement entry too
          return err
      }
  }

  // Step 4: Update credit card (available credit)
  err = UpdateAvailableCredit(...)
  if err != nil {
      ROLLBACK  // Rolls back all entries
      return err
  }

  COMMIT  // All or nothing

END TRANSACTION
```

This ensures:
- **Consistency**: Both ledgers always stay in sync
- **Atomicity**: Either all entries are created or none are
- **Integrity**: No orphaned entries or inconsistent states
- **Isolation**: Concurrent transactions don't interfere

---

## Balance Calculation Flow

### Statement Balance

```
current_balance = 0

FOR each cleared entry WHERE tenant_id = X:
  SWITCH entry_type:
    CASE transaction, cash_advance:
      current_balance += amount
    
    CASE fee_late, fee_failed, fee_international, 
         fee_interest, fee_cash_advance, fee_annual, 
         fee_over_limit:
      current_balance += amount
    
    CASE payment, refund, credit:
      current_balance -= amount
    
    CASE adjustment:
      current_balance += amount  // Can be positive or negative

RETURN current_balance
```

### Cashback Balance

```
available_points = 0

FOR each entry WHERE credit_card_id = X:
  SWITCH entry_type:
    CASE earned_transaction:
      available_points += points
    
    CASE adjusted_refund:
      available_points += points  // Note: points is negative for refunds
    
    CASE redeemed_spent, redeemed_external:
      available_points += points  // Note: points is negative for redemptions
    
    CASE adjusted_manual:
      available_points += points  // Can be positive or negative
    
    CASE expired:
      available_points += points  // Note: points is negative for expirations

RETURN available_points
```

### Available Credit

```
available_credit = credit_limit - current_balance

// Can be negative if over limit
```

---

## Error Handling

### Insufficient Points Example

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Redeem 5000 points (but cardholder only has 1000)       │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Cashback Service              │
                │  1. Check points balance       │
                │  2. Available: 1000 < 5000     │
                │  3. REJECT ✗                   │
                └────────────────────────────────┘

RESULT: Error returned
  "Insufficient points: available=1000, requested=5000"

NO entries created in either ledger
```

### Insufficient Credit Example

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Purchase $500 (but available credit is $400)            │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Credit Card Service           │
                │  1. Check available credit     │
                │  2. Available: $400 < $500     │
                │  3. REJECT ✗                   │
                └────────────────────────────────┘

RESULT: Error returned
  "Insufficient credit: available=$400, requested=$500"

NO entries created
Alternative: Assess over-limit fee and allow (if configured)
```

---

## Monthly Statement Generation

```
┌─────────────────────────────────────────────────────┐
│ Generate Statement for Card X                      │
│ Period: 2025-01-01 to 2025-01-31                   │
└────────────────────────┬────────────────────────────┘
                         │
         ┌───────────────▼────────────────┐
         │ Billing Service                │
         │ 1. Close current cycle         │
         │ 2. Calculate balances          │
         │ 3. Calculate interest          │
         │ 4. Assess fees if needed       │
         │ 5. Calculate minimum payment   │
         │ 6. Set due date                │
         └────────────────────────────────┘

CALCULATION:
  Previous Balance:        $500.00  (from Dec statement)
  Payments Received:      -$200.00  (payments in period)
  ─────────────────────────────────
  Opening Balance:         $300.00

  + Purchases:             $450.00
  + Cash Advances:         $200.00
  - Refunds:               -$75.00
  - Cashback Redeemed:     -$10.00
  + Interest:              +$15.50
  + Late Fees:             +$35.00
  + Cash Advance Fee:      +$10.00
  ─────────────────────────────────
  New Balance:             $925.50

  Minimum Payment (3%):     $27.77
  OR Minimum Amount:        $25.00
  ─────────────────────────────────
  Minimum Payment Due:      $27.77

  Due Date: 2025-02-25 (25 days after cycle end)
  Grace Period End: 2025-02-21
```

---

## Best Practices

### 1. Always Use Service Layer

✓ **DO**:
```go
creditCardService.RecordTransaction(...)
cashbackService.RedeemCashback(...)
```

✗ **DON'T**:
```go
statementLedgerService.CreateEntry(...)  // Bypasses business logic
cashbackLedgerService.CreateEntry(...)   // Entries won't be linked!
```

### 2. Validate Before Creating Entries

```go
// Check available credit before transaction
if err := card.HasAvailableCredit(amount); err != nil {
    return err  // Don't create any entries
}

// Check points balance before redemption
if err := cashbackService.ValidateRedemption(cardID, points); err != nil {
    return err  // Don't create any entries
}

// Then proceed with atomic operation
result, err := creditCardService.RecordTransaction(...)
```

### 3. Handle Errors Properly

```go
stmtEntry, cashbackEntry, err := creditCardService.RecordTransaction(...)
if err != nil {
    // Transaction rolled back - neither ledger affected
    log.Error("Failed to record transaction", err)
    return err
}

// Success - both entries created and linked
log.Info("Transaction recorded", 
    "statement_id", stmtEntry.ID,
    "cashback_id", cashbackEntry.ID)
```

### 4. Use Proper Entry Types

```go
// WRONG: Using generic "adjustment" for everything
CreateEntry(type: "adjustment", amount: 100)

// RIGHT: Using specific types for auditability
RecordTransaction(...)     // For purchases
RecordPayment(...)         // For payments
RecordRefund(...)          // For refunds
RecordCashAdvance(...)     // For ATM withdrawals
AssessLateFee(...)         // For late fees
```

### 5. Always Use Decimal for Currency

```go
// WRONG: Using float64 for money
amount := 100.50  // float64

// RIGHT: Using decimal.Decimal
amount := decimal.NewFromFloat(100.50)
```

### 6. Track Payment States

```go
// Payment state machine
payment.Status = PaymentStatusPending
// ... process payment ...
payment.Status = PaymentStatusProcessing
// ... wait for processor ...
payment.Status = PaymentStatusCleared

// Handle failures
if processorFailed {
    payment.Status = PaymentStatusFailed
    if payment.CanRetry() {
        payment.NextRetryAt = time.Now().Add(24 * time.Hour)
    }
}
```

---

## Summary

### Key Principles

1. **Two Independent Ledgers**: Statement (money) and Cashback (rewards)
2. **Coordinated Updates**: Some activities affect both, some only one
3. **Critical Rule**: Payments DON'T affect cashback (only purchases do)
4. **Atomic Reconciliation**: Both ledgers updated in single transaction
5. **Immutable Entries**: Never update/delete, only append new entries
6. **Real-time Balances**: Calculated from complete entry history
7. **GAAP Compliance**: Interest calculated using Average Daily Balance
8. **Precision**: Always use decimal.Decimal for currency

### What Earns Cashback?

✓ **YES**:
- Purchase transactions
- (Points deducted on refunds)

✗ **NO**:
- Payments
- Cash advances
- Fees (late, failed payment, international, etc.)
- Interest charges
- Manual adjustments

### Reconciliation Service Responsibilities

1. **Validate** business rules before creating entries
2. **Create** entries in both ledgers atomically
3. **Link** related entries via foreign keys
4. **Update** credit card available credit
5. **Rollback** all changes if any step fails
6. **Audit** all operations with timestamps and creators

The reconciliation service ensures these ledgers stay synchronized while maintaining their independence and enforcing credit card business rules.
