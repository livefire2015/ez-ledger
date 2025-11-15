# CLAUDE.md - AI Assistant Development Guide for ez-ledger

## Project Overview

**ez-ledger** is a personal finance and accounting ledger application. This document provides essential information for AI assistants working on this codebase.

**Status**: New project - initial setup phase

## Table of Contents

1. [Project Structure](#project-structure)
2. [Development Workflows](#development-workflows)
3. [Coding Conventions](#coding-conventions)
4. [Git Workflow](#git-workflow)
5. [Testing Strategy](#testing-strategy)
6. [Key Concepts](#key-concepts)
7. [Common Tasks](#common-tasks)
8. [AI Assistant Guidelines](#ai-assistant-guidelines)

---

## Project Structure

```
ez-ledger/
├── src/                    # Source code
│   ├── core/              # Core ledger logic
│   ├── models/            # Data models (accounts, transactions, etc.)
│   ├── services/          # Business logic services
│   ├── api/               # API routes/endpoints
│   └── utils/             # Utility functions
├── tests/                 # Test files
│   ├── unit/             # Unit tests
│   └── integration/      # Integration tests
├── docs/                  # Documentation
├── config/                # Configuration files
└── scripts/               # Build/deployment scripts
```

### Current State
- **Repository**: Empty/Initial state
- **Tech Stack**: To be determined based on requirements
- **Database**: To be determined

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
   # Will vary based on chosen tech stack
   # npm install / pip install -r requirements.txt / go mod download
   ```

3. **Set up configuration**
   ```bash
   # Copy example config and customize
   cp .env.example .env
   ```

4. **Run tests**
   ```bash
   # Verify setup is correct
   # npm test / pytest / go test
   ```

### Development Cycle

1. Create feature branch from main
2. Implement changes with tests
3. Run linter and formatter
4. Run test suite
5. Commit with descriptive message
6. Push and create pull request

---

## Coding Conventions

### General Principles

- **DRY (Don't Repeat Yourself)**: Extract common logic into reusable functions
- **SOLID Principles**: Follow object-oriented design principles
- **Error Handling**: Always handle errors gracefully with meaningful messages
- **Documentation**: Document complex logic and public APIs
- **Security**: Never commit secrets, sanitize inputs, validate data

### Naming Conventions

- **Files**: `snake_case.ext` or `kebab-case.ext` (maintain consistency)
- **Classes**: `PascalCase`
- **Functions/Methods**: `camelCase` or `snake_case` (language-dependent)
- **Constants**: `UPPER_SNAKE_CASE`
- **Variables**: `camelCase` or `snake_case` (language-dependent)

### Code Organization

- **Single Responsibility**: Each file/class should have one clear purpose
- **Separation of Concerns**: Keep business logic separate from presentation
- **Dependency Injection**: Favor DI over hard-coded dependencies
- **Configuration**: Externalize configuration, never hardcode values

---

## Git Workflow

### Branch Naming

- **Feature branches**: `feature/<description>` or `claude/<session-id>`
- **Bug fixes**: `fix/<description>`
- **Hotfixes**: `hotfix/<description>`
- **Releases**: `release/<version>`

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
feat(transactions): add support for recurring transactions
fix(balance): correct calculation for multi-currency accounts
docs(api): update API endpoint documentation
```

### Pull Request Guidelines

1. **Title**: Clear, descriptive summary
2. **Description**: Explain what, why, and how
3. **Tests**: Include test coverage
4. **Documentation**: Update relevant docs
5. **Breaking Changes**: Clearly mark any breaking changes

---

## Testing Strategy

### Test Coverage Goals

- **Unit Tests**: 80%+ coverage for business logic
- **Integration Tests**: Cover critical user flows
- **Edge Cases**: Test boundary conditions and error scenarios

### Test Organization

```
tests/
├── unit/
│   ├── test_accounts.{ext}
│   ├── test_transactions.{ext}
│   └── test_balance_calculator.{ext}
└── integration/
    ├── test_api_endpoints.{ext}
    └── test_transaction_flow.{ext}
```

### Testing Best Practices

- **Isolation**: Tests should not depend on each other
- **Clarity**: Test names should describe what they test
- **AAA Pattern**: Arrange, Act, Assert
- **Mock External Dependencies**: Use mocks for databases, APIs, etc.
- **Fast Execution**: Keep tests fast to encourage frequent running

---

## Key Concepts

### Ledger Accounting Fundamentals

1. **Double-Entry Bookkeeping**: Every transaction affects at least two accounts
2. **Account Types**:
   - Assets (debit increases)
   - Liabilities (credit increases)
   - Equity (credit increases)
   - Income/Revenue (credit increases)
   - Expenses (debit increases)

3. **Transaction Structure**:
   ```
   {
     date: "YYYY-MM-DD",
     description: "Transaction description",
     entries: [
       { account: "Account1", debit: amount },
       { account: "Account2", credit: amount }
     ]
   }
   ```

4. **Balance Equation**: Assets = Liabilities + Equity

### Data Integrity

- **Immutability**: Once posted, transactions should be immutable (use reversals)
- **Audit Trail**: Maintain complete history of all changes
- **Validation**: Ensure debits = credits for each transaction
- **Reconciliation**: Regular balance checks and reconciliation

---

## Common Tasks

### Adding a New Account Type

1. Define the account type in models
2. Update account validation logic
3. Add to account hierarchy if needed
4. Write tests for new account type
5. Update documentation

### Creating a Transaction

1. Validate transaction data (debits = credits)
2. Verify all accounts exist
3. Check permissions/authorization
4. Create transaction record
5. Update account balances
6. Log audit trail

### Generating Reports

1. Define report parameters (date range, accounts, etc.)
2. Query transactions within scope
3. Calculate aggregates/balances
4. Format output (JSON, PDF, CSV, etc.)
5. Cache results if appropriate

---

## AI Assistant Guidelines

### When Working on This Codebase

1. **Understand Context First**
   - Read relevant code before making changes
   - Understand the accounting implications
   - Check for existing patterns to follow

2. **Prioritize Data Integrity**
   - Ledger data must be accurate and consistent
   - Always validate transactions
   - Maintain audit trails
   - Never silently fail on validation errors

3. **Security Considerations**
   - Never expose sensitive financial data in logs
   - Validate and sanitize all inputs
   - Implement proper authentication/authorization
   - Follow principle of least privilege

4. **Testing Requirements**
   - Write tests before or alongside code
   - Test edge cases (zero amounts, negative values, etc.)
   - Test currency handling and precision
   - Test date/time handling across timezones

5. **Documentation Standards**
   - Document complex calculations
   - Explain accounting rules implemented
   - Keep API documentation current
   - Comment non-obvious business logic

6. **Code Review Checklist**
   - Does it maintain transaction integrity?
   - Are all error cases handled?
   - Is it properly tested?
   - Is it documented?
   - Does it follow existing patterns?

### Common Pitfalls to Avoid

- **Floating Point Arithmetic**: Use decimal/money types for currency
- **Timezone Issues**: Store dates in UTC, convert for display
- **Race Conditions**: Handle concurrent transaction creation
- **Incomplete Validation**: Always validate both debits and credits
- **Missing Audit Logs**: Log all state changes
- **Hardcoded Values**: Use configuration for currencies, date formats, etc.

### Code Quality Standards

- **No Magic Numbers**: Use named constants
- **DRY Principle**: Extract repeated logic
- **Error Messages**: Provide actionable error messages
- **Performance**: Consider indexing for large datasets
- **Scalability**: Design for growing transaction volumes

### Before Committing

- [ ] All tests pass
- [ ] Linter/formatter run
- [ ] No debugging code left in
- [ ] No commented-out code
- [ ] Documentation updated
- [ ] CLAUDE.md updated if patterns changed

---

## Technology Stack (To Be Determined)

### Backend Options
- **Node.js/TypeScript**: Fast development, good ecosystem
- **Python**: Excellent for financial calculations, pandas for reporting
- **Go**: High performance, good for concurrent operations
- **Java**: Enterprise-ready, robust type system

### Database Options
- **PostgreSQL**: ACID compliance, JSON support, excellent for financial data
- **SQLite**: Lightweight, good for personal use
- **MySQL**: Wide adoption, good performance

### Frontend Options
- **React**: Component-based, large ecosystem
- **Vue.js**: Simpler learning curve, good for smaller apps
- **Svelte**: High performance, minimal boilerplate

---

## Useful Resources

### Accounting References
- Plain Text Accounting: https://plaintextaccounting.org/
- Ledger CLI: https://www.ledger-cli.org/
- hledger: https://hledger.org/
- Accounting basics: Double-entry bookkeeping principles

### Best Practices
- Financial data handling best practices
- Currency and decimal handling
- Date/time handling in financial systems
- Audit logging patterns

---

## Change Log

### 2025-11-15
- Initial CLAUDE.md creation
- Established base structure and conventions
- Defined development workflows

---

## Contact & Support

For questions about this codebase or to report issues, please refer to the project's issue tracker or documentation.

---

**Note to AI Assistants**: This document should be updated as the codebase evolves. If you establish new patterns, make architectural decisions, or implement significant features, please update this guide to reflect those changes.
