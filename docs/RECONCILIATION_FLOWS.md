# Ledger Reconciliation Flows

This document visualizes how the Statement and Points ledgers interact for different types of activities.

## Visual Notation

```
[S] = Statement Ledger
[P] = Points Ledger
→   = Creates entry
✗   = No entry created
```

## Flow 1: Transaction (Purchase)

**Scenario**: Tenant makes a $100 purchase

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Transaction $100                                          │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Reconciliation Service        │
                │  RecordTransaction()           │
                └────────┬──────────────┬────────┘
                         │              │
         ┌───────────────▼──┐      ┌───▼──────────────────┐
         │ Statement Ledger │      │ Points Ledger        │
         └──────────────────┘      └──────────────────────┘

STATEMENT LEDGER [S]                 POINTS LEDGER [P]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry Type:         │              │ Entry Type:          │
│   transaction       │              │   earned_transaction │
│ Amount: +$100.00    │              │ Points: +100         │
│ Status: pending     │              │ Linked: statement_id │
│ Description:        │              │ Transaction: $100    │
│   "Purchase at..."  │              │ Rate: 0.01           │
└─────────────────────┘              └──────────────────────┘

RESULT:
  Statement Balance: +$100.00
  Points Balance: +100 points
  Both entries linked via foreign key
```

---

## Flow 2: Payment

**Scenario**: Tenant pays $100 towards their balance

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Payment $100                                              │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Reconciliation Service        │
                │  RecordPayment()               │
                └────────┬───────────────────────┘
                         │
         ┌───────────────▼──┐
         │ Statement Ledger │
         └──────────────────┘

STATEMENT LEDGER [S]                 POINTS LEDGER [P]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry Type:         │              │                      │
│   payment           │              │   NO ENTRY ✗         │
│ Amount: $100.00     │              │                      │
│ Status: pending     │              │                      │
│ Description:        │              │                      │
│   "Payment..."      │              │                      │
└─────────────────────┘              └──────────────────────┘

RESULT:
  Statement Balance: -$100.00 (credit applied)
  Points Balance: UNCHANGED

KEY INSIGHT: Payments only affect statement ledger, NOT points!
```

---

## Flow 3: Refund

**Scenario**: Tenant returns a $50 item (originally earned 50 points)

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Refund $50 (original transaction earned 50 points)       │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Reconciliation Service        │
                │  RecordRefund()                │
                └────────┬──────────────┬────────┘
                         │              │
         ┌───────────────▼──┐      ┌───▼──────────────────┐
         │ Statement Ledger │      │ Points Ledger        │
         └──────────────────┘      └──────────────────────┘

STATEMENT LEDGER [S]                 POINTS LEDGER [P]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry Type:         │              │ Entry Type:          │
│   refund            │              │   earned_refund      │
│ Amount: $50.00      │              │ Points: -50          │
│ Status: pending     │              │ Linked: statement_id │
│ Description:        │              │ Transaction: $50     │
│   "Refund for..."   │              │ Rate: 0.01           │
└─────────────────────┘              └──────────────────────┘

RESULT:
  Statement Balance: -$50.00 (credit applied)
  Points Balance: -50 points (deducted)
  Both ledgers adjusted proportionally
```

---

## Flow 4: Points Redemption for Statement Credit

**Scenario**: Tenant redeems 1000 points for $10 statement credit

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Redeem 1000 points → $10 credit                          │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Reconciliation Service        │
                │  RecordRewardRedemption()      │
                │  1. Validate points balance    │
                │  2. Create both entries        │
                └────────┬──────────────┬────────┘
                         │              │
         ┌───────────────▼──┐      ┌───▼──────────────────┐
         │ Statement Ledger │      │ Points Ledger        │
         └──────────────────┘      └──────────────────────┘

POINTS LEDGER [P]                    STATEMENT LEDGER [S]
┌──────────────────────┐              ┌─────────────────────┐
│ Entry Type:          │              │ Entry Type:         │
│   redeemed_spent     │◄─────link────┤   reward            │
│ Points: -1000        │              │ Amount: $10.00      │
│ Platform: keystone   │              │ Status: pending     │
│ External Ref: XXX    │              │ Description:        │
│                      │              │   "Reward: 1000pts" │
└──────────────────────┘              └─────────────────────┘

RESULT:
  Points Balance: -1000 points (deducted)
  Statement Balance: -$10.00 (credit applied)
  Cross-ledger transaction (points → money)
```

---

## Flow 5: Fee Assessment

**Scenario**: Late payment fee of $25 assessed

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Late Fee $25                                              │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Statement Ledger Service      │
                │  CreateEntry()                 │
                └────────┬───────────────────────┘
                         │
         ┌───────────────▼──┐
         │ Statement Ledger │
         └──────────────────┘

STATEMENT LEDGER [S]                 POINTS LEDGER [P]
┌─────────────────────┐              ┌──────────────────────┐
│ Entry Type:         │              │                      │
│   fee_late          │              │   NO ENTRY ✗         │
│ Amount: +$25.00     │              │                      │
│ Status: pending     │              │                      │
│ Description:        │              │                      │
│   "Late fee..."     │              │                      │
└─────────────────────┘              └──────────────────────┘

RESULT:
  Statement Balance: +$25.00 (charge added)
  Points Balance: UNCHANGED
  Fees don't earn points
```

---

## Reconciliation Matrix

| Activity Type | Statement Impact | Points Impact | Reconciliation Required |
|---------------|------------------|---------------|------------------------|
| **Transaction** | Charge (+) | Earn (+) | ✓ Yes (atomic) |
| **Payment** | Credit (-) | None | ✗ No |
| **Refund** | Credit (-) | Deduct (-) | ✓ Yes (atomic) |
| **Points Redemption** | Credit (-) | Deduct (-) | ✓ Yes (atomic) |
| **Late Fee** | Charge (+) | None | ✗ No |
| **Failed Payment Fee** | Charge (+) | None | ✗ No |
| **Manual Adjustment** | +/- | Optional | Maybe |
| **Account Credit** | Credit (-) | None | ✗ No |

---

## Atomic Transaction Guarantee

All reconciliation operations that affect both ledgers use database transactions:

```go
BEGIN TRANSACTION

  1. Validate business rules (e.g., sufficient points)
  2. Create statement ledger entry
  3. Create points ledger entry (if applicable)
  4. Link entries via foreign keys

  IF any step fails:
    ROLLBACK entire transaction
  ELSE:
    COMMIT both entries atomically

END TRANSACTION
```

This ensures:
- **Consistency**: Both ledgers always stay in sync
- **Atomicity**: Either both entries are created or neither is
- **Integrity**: No orphaned entries or inconsistent states

---

## Balance Calculation Flow

### Statement Balance

```
FOR each cleared entry WHERE tenant_id = X:
  IF entry_type IN (transaction, fee_*, returned_reward, adjustment[+]):
    balance += amount
  ELSE IF entry_type IN (payment, refund, reward, credit, adjustment[-]):
    balance -= amount

RETURN balance
```

### Points Balance

```
earned_points = 0
redeemed_points = 0

FOR each entry WHERE tenant_id = X:
  IF entry_type = earned_transaction:
    earned_points += points
  ELSE IF entry_type = earned_refund:
    earned_points += points  // Note: points is negative for refunds
  ELSE IF entry_type = redeemed_spent:
    redeemed_points += ABS(points)
  ELSE IF entry_type = redeemed_cancelled:
    redeemed_points -= ABS(points)
  ELSE IF entry_type = redeemed_refunded:
    redeemed_points -= ABS(points)

available_points = earned_points - redeemed_points
RETURN available_points
```

---

## Error Handling

### Insufficient Points Example

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Redeem 5000 points (but tenant only has 1000)            │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Reconciliation Service        │
                │  1. Check points balance       │
                │  2. Available: 1000 < 5000     │
                │  3. REJECT ✗                   │
                └────────────────────────────────┘

RESULT: Error returned
  "Insufficient points: available=1000, requested=5000"

NO entries created in either ledger
```

### Payment Processing Failure

```
┌──────────────────────────────────────────────────────────────────┐
│ INPUT: Payment $100                                              │
└───────────────────────────────┬──────────────────────────────────┘
                                │
                ┌───────────────▼────────────────┐
                │  Statement Ledger Service      │
                │  1. Create entry (pending)     │
                │  2. External payment fails     │
                │  3. Create reversal entry      │
                └────────────────────────────────┘

STATEMENT LEDGER:
  Entry 1: payment, $100, status=pending
  Entry 2: adjustment, -$100, status=cleared (reversal)

Net effect: $0 (payment cancelled)
Points: No impact (payments never affect points)
```

---

## Monthly Statement Generation

```
┌─────────────────────────────────────────────────────┐
│ Generate Statement for Tenant X                    │
│ Period: 2025-01-01 to 2025-01-31                   │
└────────────────────────┬────────────────────────────┘
                         │
         ┌───────────────▼────────────────┐
         │ Statement Service              │
         │ 1. Get previous balance        │
         │ 2. Calculate cleared payments  │
         │ 3. Sum all entries in period   │
         │ 4. Generate statement          │
         └────────────────────────────────┘

CALCULATION:
  Previous Balance:        $500.00  (from Dec statement)
  Cleared Payments:       -$200.00  (payments in period)
  ─────────────────────────────────
  Opening Balance:         $300.00

  + Transactions:          $450.00
  - Refunds:               -$75.00
  - Rewards (redeemed):    -$10.00
  + Late Fees:             +$25.00
  ─────────────────────────────────
  Statement Balance:       $690.00

  Minimum Payment (5%):     $34.50
```

---

## Best Practices

### 1. Always Use Reconciliation Service

✓ **DO**:
```go
reconciliationService.RecordTransaction(...)
```

✗ **DON'T**:
```go
statementService.CreateEntry(...)  // Bypasses reconciliation
pointsService.CreateEntry(...)     // Entries won't be linked!
```

### 2. Validate Before Creating Entries

```go
// Check points balance before redemption
if err := pointsService.ValidateRedemption(tenantID, points); err != nil {
    return err  // Don't create any entries
}

// Then proceed with both entries atomically
reconciliationService.RecordRewardRedemption(...)
```

### 3. Handle Errors Properly

```go
stmtEntry, ptsEntry, err := reconciliationService.RecordTransaction(...)
if err != nil {
    // Transaction rolled back - neither ledger affected
    log.Error("Failed to record transaction", err)
    return err
}

// Success - both entries created and linked
```

### 4. Use Proper Entry Types

```go
// WRONG: Using generic "adjustment" for everything
CreateEntry(type: "adjustment", amount: 100)

// RIGHT: Using specific types for auditability
RecordTransaction(...)  // For purchases
RecordPayment(...)      // For payments
RecordRefund(...)       // For refunds
```

---

## Summary

1. **Two Independent Ledgers**: Statement (money) and Points (rewards)
2. **Coordinated Updates**: Some activities affect both, some only one
3. **Key Rule**: Payments DON'T affect points (only transactions do)
4. **Atomic Reconciliation**: Both ledgers updated in single transaction
5. **Immutable Entries**: Never update/delete, only append new entries
6. **Real-time Balances**: Calculated from complete entry history

The reconciliation service ensures these ledgers stay synchronized while maintaining their independence.
