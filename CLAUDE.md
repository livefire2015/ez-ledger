# CLAUDE.md - AI Assistant Development Guide for ez-ledger

## Project Overview

**ez-ledger** is a comprehensive credit card ledger system implementing GAAP-compliant accounting for revolving credit card accounts with integrated cashback rewards tracking. This document provides essential information for AI assistants working on this codebase.

**Status**: Active development - Core features implemented

## Table of Contents

1. [Project Structure](#project-structure)
2. [Core Features](#core-features)
3. [Development Workflows](#development-workflows)
4. [Coding Conventions](#coding-conventions)
5. [Git Workflow](#git-workflow)
6. [Testing Strategy](#testing-strategy)
7. [Key Concepts](#key-concepts)
8. [Common Tasks](#common-tasks)
9. [AI Assistant Guidelines](#ai-assistant-guidelines)

---

## Project Structure

```
ez-ledger/
├── src/                    # Source code
│   ├── models/            # Data models
│   │   ├── billing_cycle.go       # Billing cycle management
│   │   ├── cashback.go            # Cashback rewards
│   │   ├── credit_card.go         # Credit card accounts
│   │   ├── payment.go             # Payment processing
│   │   ├── points_ledger.go       # Points tracking
│   │   ├── statement.go           # Statement generation
│   │   ├── statement_ledger.go    # Transaction ledger
│   │   └── tenant.go              # Multi-tenancy
│   └── services/          # Business logic services
│       ├── billing_service.go              # Billing cycle management
│       ├── cashback_service.go             # Cashback calculations
│       ├── credit_card_service.go          # Card operations
│       ├── fee_service.go                  # Fee assessment
│       ├── interest_service.go             # Interest calculations
│       ├── ledger_reconciliation_service.go # Ledger coordination
│       ├── payment_service.go              # Payment processing
│       ├── points_ledger_service.go        # Points tracking
│       └── statement_ledger_service.go     # Transaction ledger
├── tests/                 # Test files
│   └── unit/             # Unit tests
├── docs/                  # Documentation
│   ├── LEDGER_DESIGN.md
│   └── RECONCILIATION_FLOWS.md
├── migrations/            # Database migrations
├── go.mod                 # Go dependencies
└── README.md             # User documentation
```

### Technology Stack
- **Language**: Go 1.21+
- **Database**: PostgreSQL 14+ (ACID compliance required)
- **Key Libraries**:
  - `github.com/shopspring/decimal` - Precise decimal arithmetic
  - `github.com/google/uuid` - UUID generation
  - `github.com/lib/pq` - PostgreSQL driver

---

## Core Features

### 1. Revolving Credit Card Ledger (Main Feature)

The system implements a complete revolving credit card ledger with GAAP-compliant accounting:

#### Statement Ledger
- **Immutable event sourcing**: All transactions are append-only
- **Entry types**:
  - `transaction` - Purchase transactions
  - `payment` - Customer payments
  - `refund` - Merchant refunds
  - `cash_advance` - ATM withdrawals
  - `fee_*` - Various fees (late, failed payment, international, etc.)
  - `fee_interest` - Interest charges
  - `adjustment` - Manual adjustments
  - `credit` - Credits/waivers

#### Billing Cycle Management
- Monthly billing cycles with configurable cycle days
- Automatic statement generation
- Minimum payment calculation (greater of % or fixed amount)
- Due date tracking with grace periods
- Overdue detection and late fee assessment

#### Interest Calculation
- **GAAP-compliant Average Daily Balance (ADB) method**
- Daily Periodic Rate (DPR) = APR / 365
- Interest = ADB × DPR × Days in cycle
- Grace period support (waived if previous balance paid in full)
- Multiple APR types:
  - Purchase APR
  - Cash advance APR
  - Penalty APR
  - Introductory APR (time-limited)

#### Fee Management
- Late payment fees
- Failed/returned payment fees
- International transaction fees (percentage-based)
- Cash advance fees (greater of flat fee or percentage)
- Over-limit fees
- Annual membership fees
- Fee waiver capability with audit trail

#### Payment Processing
- Multiple payment states (pending, processing, cleared, failed, returned, cancelled, reversed)
- ACH return code handling
- Payment retry logic
- Failed payment fee assessment
- Payment application to balance

### 2. Reward Points Ledger (Bonus Feature)

Integrated cashback/points tracking system:

#### Points Earning
- Configurable earning rate (e.g., 1% = 1 point per dollar)
- Category-based multipliers (e.g., 3x on dining)
- Minimum transaction thresholds
- Automatic earning on qualifying transactions
- No points on cash advances or fees

#### Points Redemption
- Statement credit redemption
- Minimum redemption thresholds
- External platform integration (e.g., Keystone)
- Redemption tracking with reference IDs

#### Points Adjustments
- Refund adjustments (deduct points when transaction refunded)
- Manual adjustments with approval tracking
- Expiration handling (if configured)

#### Points Ledger Entries
- `earned_transaction` - Points earned from purchases
- `redeemed_spent` - Points redeemed for statement credit
- `redeemed_external` - Points redeemed via external platform
- `adjusted_refund` - Points deducted for refunds
- `adjusted_manual` - Manual adjustments
- `expired` - Expired points

---

## Development Workflows

### Setting Up Development Environment

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd ez-ledger
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Set up PostgreSQL database**
   ```bash
   createdb ezledger
   psql ezledger < migrations/001_create_ledger_tables.sql
   ```

4. **Run tests**
   ```bash
   go test ./...
   ```

### Development Cycle

1. Create feature branch from main
2. Implement changes with tests
3. Run linter and formatter (`gofmt`, `golint`)
4. Run test suite (`go test -v ./...`)
5. Commit with descriptive message
6. Push and create pull request

---

## Coding Conventions

### General Principles

- **DRY (Don't Repeat Yourself)**: Extract common logic into reusable functions
- **SOLID Principles**: Follow object-oriented design principles
- **Error Handling**: Always return errors, never panic in production code
- **Documentation**: Document exported functions and complex logic
- **Security**: Never commit secrets, validate all inputs

### Go-Specific Conventions

- **Files**: `snake_case.go`
- **Types**: `PascalCase`
- **Functions/Methods**: `PascalCase` (exported), `camelCase` (unexported)
- **Constants**: `PascalCase` or `UPPER_SNAKE_CASE`
- **Variables**: `camelCase`

### Code Organization

- **Single Responsibility**: Each file/struct should have one clear purpose
- **Separation of Concerns**: Keep business logic in services, data in models
- **Dependency Injection**: Pass dependencies explicitly (e.g., `*sql.DB`)
- **Builder Pattern**: Use for complex object construction (see `BillingCycleBuilder`)

---

## Git Workflow

### Branch Naming

- **Feature branches**: `feature/<description>`
- **Bug fixes**: `fix/<description>`
- **Hotfixes**: `hotfix/<description>`

### Commit Messages

Follow conventional commits format:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style/formatting
- `refactor`: Code refactoring
- `test`: Adding/updating tests
- `chore`: Maintenance tasks

**Examples**:
```
feat(interest): implement GAAP-compliant ADB interest calculation
fix(billing): correct minimum payment calculation for zero balance
docs(readme): add detailed credit card ledger explanation
test(interest): add unit tests for interest service
```

---

## Testing Strategy

### Test Coverage Goals

- **Unit Tests**: 80%+ coverage for business logic
- **Integration Tests**: Cover critical user flows
- **Edge Cases**: Test boundary conditions and error scenarios

### Test Organization

```
tests/
└── unit/
    ├── billing_cycle_test.go
    ├── cashback_test.go
    ├── credit_card_test.go
    ├── payment_test.go
    └── interest_service_test.go (in src/services/)
```

### Testing Best Practices

- **Table-driven tests**: Use for multiple test cases
- **Isolation**: Tests should not depend on each other
- **Clarity**: Test names should describe what they test
- **No database**: Use in-memory data structures for unit tests
- **Decimal precision**: Test currency calculations with exact decimal values

---

## Key Concepts

### Credit Card Accounting Fundamentals

1. **Revolving Credit**: 
   - Credit limit defines maximum borrowing
   - Available credit = Credit limit - Current balance
   - Balance carries forward month to month if not paid in full

2. **Billing Cycle**:
   - Monthly statement period (e.g., 1st to 30th)
   - Statement generated at cycle end
   - Payment due date (typically 21-25 days after cycle end)
   - Grace period for interest (if previous balance paid in full)

3. **Interest Calculation (GAAP-compliant)**:
   ```
   Average Daily Balance (ADB) = Sum of daily balances / Days in cycle
   Daily Periodic Rate (DPR) = APR / 365
   Interest Charge = ADB × DPR × Days in cycle
   ```

4. **Minimum Payment**:
   ```
   Minimum = MAX(
     Balance × MinimumPaymentPercent,
     MinimumPaymentAmount
   )
   ```

5. **Transaction Flow**:
   ```
   Purchase → Pending → Posted → Included in statement → Due → Paid/Overdue
   ```

### Cashback/Rewards Fundamentals

1. **Earning**:
   - Points earned on purchases (not payments)
   - Rate: typically 1% (1 point per dollar)
   - Category multipliers possible
   - No points on cash advances, fees, or interest

2. **Redemption**:
   - Statement credit (reduces balance)
   - External platform redemption
   - Minimum redemption threshold

3. **Adjustments**:
   - Refunds deduct previously earned points
   - Manual adjustments require approval

### Data Integrity

- **Immutability**: Once posted, entries are immutable (use reversals)
- **Audit Trail**: Complete history with timestamps and creators
- **Validation**: Ensure all amounts use decimal.Decimal for precision
- **Atomicity**: Related ledger updates in single transaction

---

## Common Tasks

### Adding a New Fee Type

1. Add constant to `models/statement_ledger.go`
2. Create request struct in `services/fee_service.go`
3. Implement assessment function
4. Add to fee summary calculation
5. Write tests
6. Update documentation

### Calculating Interest for a Billing Cycle

1. Get billing cycle details
2. Retrieve daily balances for cycle period
3. Calculate Average Daily Balance
4. Check grace period eligibility
5. Apply DPR × Days calculation
6. Apply minimum interest charge if configured
7. Create interest fee entry

### Processing a Payment

1. Validate payment amount
2. Create payment record with status
3. Create statement ledger entry
4. Update available credit
5. Track payment status transitions
6. Handle failures/returns with appropriate fees

---

## AI Assistant Guidelines

### When Working on This Codebase

1. **Understand Financial Context**
   - This is a credit card system, not a simple ledger
   - Interest calculations must be GAAP-compliant
   - Decimal precision is critical (never use float64 for money)
   - Understand the difference between transactions and payments

2. **Prioritize Data Integrity**
   - All financial entries are immutable
   - Use database transactions for atomic operations
   - Validate all amounts and dates
   - Maintain complete audit trails

3. **Security Considerations**
   - Never expose sensitive financial data in logs
   - Validate and sanitize all inputs
   - Use UUIDs for all IDs (prevent enumeration)
   - Implement proper tenant isolation

4. **Testing Requirements**
   - Test with realistic credit card scenarios
   - Test edge cases (zero amounts, negative balances, etc.)
   - Test interest calculations with known values
   - Test payment state transitions
   - Test fee assessments and waivers

5. **Documentation Standards**
   - Document complex calculations with formulas
   - Explain GAAP compliance requirements
   - Keep API documentation current
   - Comment non-obvious business logic

### Common Pitfalls to Avoid

- **Floating Point Arithmetic**: ALWAYS use `decimal.Decimal` for currency
- **Timezone Issues**: Store dates in UTC, use `time.Time` consistently
- **Payment vs Transaction Confusion**: Payments reduce balance, transactions increase it
- **Points on Payments**: Points are earned on spending, NOT on paying bills
- **Mutable Ledger Entries**: Never update entries, always create new ones
- **Missing Grace Period Logic**: Interest may be waived if previous balance paid in full
- **Incorrect APR Calculation**: DPR = APR / 365 (not 360, not 12)

### Credit Card Specific Rules

1. **Interest Grace Period**:
   - If previous statement paid in full by due date → No interest on new purchases
   - If carrying balance → Interest accrues from transaction date

2. **Cash Advances**:
   - No grace period (interest starts immediately)
   - Higher APR than purchases
   - Separate fee assessed

3. **Payments Application**:
   - Typically applied to highest APR balance first
   - Minimum payment goes to oldest balance

4. **Fee Assessment**:
   - Late fee: Only if minimum payment not received by due date
   - Failed payment fee: When payment returns/fails
   - One fee per occurrence (don't double-charge)

### Before Committing

- [ ] All tests pass (`go test -v ./...`)
- [ ] Code formatted (`gofmt -w .`)
- [ ] No debugging code left in
- [ ] Decimal.Decimal used for all currency
- [ ] Documentation updated
- [ ] CLAUDE.md updated if patterns changed

---

## Useful Resources

### Credit Card Industry References
- GAAP accounting standards for revolving credit
- Truth in Lending Act (TILA) requirements
- Average Daily Balance calculation methodology
- Credit card statement requirements

### Accounting References
- Plain Text Accounting: https://plaintextaccounting.org/
- Double-entry bookkeeping principles
- Event sourcing patterns

### Go Best Practices
- Effective Go: https://golang.org/doc/effective_go
- Go Code Review Comments
- Decimal arithmetic in Go

---

## Change Log

### 2025-12-03
- Updated CLAUDE.md with comprehensive credit card and rewards documentation
- Added detailed explanation of revolving credit ledger
- Added cashback/rewards points ledger documentation
- Added credit card specific guidelines and pitfalls

### 2025-11-15
- Initial CLAUDE.md creation
- Established base structure and conventions

---

## Contact & Support

For questions about this codebase or to report issues, please refer to the project's issue tracker or documentation.

---

**Note to AI Assistants**: This document should be updated as the codebase evolves. If you establish new patterns, make architectural decisions, or implement significant features, please update this guide to reflect those changes.
