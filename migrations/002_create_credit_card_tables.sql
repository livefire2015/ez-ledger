-- Migration: 002_create_credit_card_tables.sql
-- Description: Create tables for credit card ledger system
-- Supports: Revolving credit, configurable APR, fees, cashback, monthly/quarterly billing

-- ============================================
-- CREDIT CARDS TABLE
-- ============================================
CREATE TABLE credit_cards (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),

    -- Card identification
    card_number VARCHAR(20), -- Masked/last 4 digits only
    cardholder_name VARCHAR(255) NOT NULL,

    -- Credit limits
    credit_limit DECIMAL(15,2) NOT NULL,
    available_credit DECIMAL(15,2) NOT NULL,

    -- Interest rates (APR - Annual Percentage Rate)
    purchase_apr DECIMAL(5,2) NOT NULL DEFAULT 19.99,           -- Standard purchase APR
    cash_advance_apr DECIMAL(5,2) NOT NULL DEFAULT 24.99,       -- Cash advance APR (typically higher)
    penalty_apr DECIMAL(5,2) NOT NULL DEFAULT 29.99,            -- Penalty APR for late payments
    introductory_apr DECIMAL(5,2) DEFAULT 0.00,                 -- Promotional APR
    introductory_end_date DATE,                                  -- When intro APR expires

    -- Fee configuration
    annual_fee DECIMAL(10,2) NOT NULL DEFAULT 0.00,
    late_payment_fee DECIMAL(10,2) NOT NULL DEFAULT 35.00,
    failed_payment_fee DECIMAL(10,2) NOT NULL DEFAULT 35.00,
    international_fee_rate DECIMAL(5,2) NOT NULL DEFAULT 3.00,  -- Percentage of transaction
    cash_advance_fee DECIMAL(10,2) NOT NULL DEFAULT 10.00,      -- Flat fee minimum
    cash_advance_fee_rate DECIMAL(5,2) NOT NULL DEFAULT 5.00,   -- Percentage rate
    over_limit_fee DECIMAL(10,2) NOT NULL DEFAULT 35.00,

    -- Billing configuration
    billing_cycle_type VARCHAR(20) NOT NULL DEFAULT 'monthly',  -- 'monthly' or 'quarterly'
    billing_cycle_day INTEGER NOT NULL DEFAULT 1 CHECK (billing_cycle_day BETWEEN 1 AND 28),
    payment_due_days INTEGER NOT NULL DEFAULT 25,               -- Days after statement close
    grace_period_days INTEGER NOT NULL DEFAULT 21,              -- Days before interest accrues
    minimum_payment_percent DECIMAL(5,2) NOT NULL DEFAULT 2.00, -- Percentage of balance
    minimum_payment_amount DECIMAL(10,2) NOT NULL DEFAULT 25.00, -- Minimum fixed amount

    -- Cashback configuration
    cashback_enabled BOOLEAN NOT NULL DEFAULT true,
    cashback_rate DECIMAL(5,2) NOT NULL DEFAULT 1.50,           -- Default cashback rate (%)
    cashback_redemption_min DECIMAL(10,2) NOT NULL DEFAULT 25.00, -- Minimum redemption amount

    -- Status and tracking
    status VARCHAR(20) NOT NULL DEFAULT 'active',               -- active, frozen, closed, delinquent
    last_statement_date DATE,
    next_statement_date DATE,
    last_payment_date DATE,
    last_payment_amount DECIMAL(15,2) DEFAULT 0.00,
    consecutive_late_count INTEGER NOT NULL DEFAULT 0,

    -- Audit fields
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    closed_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT valid_credit_limit CHECK (credit_limit > 0),
    CONSTRAINT valid_apr CHECK (purchase_apr >= 0 AND purchase_apr <= 100),
    CONSTRAINT valid_billing_type CHECK (billing_cycle_type IN ('monthly', 'quarterly')),
    CONSTRAINT valid_status CHECK (status IN ('active', 'frozen', 'closed', 'delinquent'))
);

CREATE INDEX idx_credit_cards_tenant ON credit_cards(tenant_id);
CREATE INDEX idx_credit_cards_status ON credit_cards(status);
CREATE INDEX idx_credit_cards_next_statement ON credit_cards(next_statement_date);

-- ============================================
-- BILLING CYCLES TABLE
-- ============================================
CREATE TABLE billing_cycles (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    credit_card_id UUID NOT NULL REFERENCES credit_cards(id),
    tenant_id UUID NOT NULL REFERENCES tenants(id),

    -- Cycle identification
    cycle_number INTEGER NOT NULL,                              -- Sequential cycle number
    cycle_type VARCHAR(20) NOT NULL,                            -- monthly or quarterly

    -- Date boundaries
    cycle_start_date DATE NOT NULL,
    cycle_end_date DATE NOT NULL,
    statement_date TIMESTAMP WITH TIME ZONE,                    -- When statement was generated
    due_date DATE NOT NULL,
    grace_period_end DATE NOT NULL,

    -- Balance components (GAAP compliant breakdown)
    previous_balance DECIMAL(15,2) NOT NULL DEFAULT 0.00,       -- Carried from previous cycle
    payments_received DECIMAL(15,2) NOT NULL DEFAULT 0.00,      -- Total payments this cycle
    purchases_amount DECIMAL(15,2) NOT NULL DEFAULT 0.00,       -- New purchases
    cash_advances_amount DECIMAL(15,2) NOT NULL DEFAULT 0.00,   -- Cash advances
    refunds_amount DECIMAL(15,2) NOT NULL DEFAULT 0.00,         -- Credits/refunds
    fees_amount DECIMAL(15,2) NOT NULL DEFAULT 0.00,            -- All fees charged
    interest_amount DECIMAL(15,2) NOT NULL DEFAULT 0.00,        -- Interest charges
    adjustments_amount DECIMAL(15,2) NOT NULL DEFAULT 0.00,     -- Manual adjustments
    cashback_earned DECIMAL(15,2) NOT NULL DEFAULT 0.00,        -- Cashback this cycle
    cashback_redeemed DECIMAL(15,2) NOT NULL DEFAULT 0.00,      -- Cashback applied

    -- Calculated totals
    new_balance DECIMAL(15,2) NOT NULL DEFAULT 0.00,            -- Statement balance
    minimum_payment DECIMAL(15,2) NOT NULL DEFAULT 0.00,        -- Minimum due

    -- Interest calculation details
    average_daily_balance DECIMAL(15,2) NOT NULL DEFAULT 0.00,  -- For interest calc
    days_in_cycle INTEGER NOT NULL DEFAULT 0,
    apr_applied DECIMAL(5,2) NOT NULL DEFAULT 0.00,             -- APR used this cycle

    -- Payment tracking
    payments_made DECIMAL(15,2) NOT NULL DEFAULT 0.00,          -- Payments toward this statement
    last_payment_date DATE,
    last_payment_amount DECIMAL(15,2) DEFAULT 0.00,
    minimum_payment_met BOOLEAN NOT NULL DEFAULT false,

    -- Status
    status VARCHAR(20) NOT NULL DEFAULT 'open',                 -- open, closed, paid, paid_full, past_due, delinquent

    -- Audit
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    closed_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT valid_cycle_dates CHECK (cycle_end_date >= cycle_start_date),
    CONSTRAINT valid_cycle_status CHECK (status IN ('open', 'closed', 'paid', 'paid_full', 'past_due', 'delinquent')),
    UNIQUE(credit_card_id, cycle_number)
);

CREATE INDEX idx_billing_cycles_card ON billing_cycles(credit_card_id);
CREATE INDEX idx_billing_cycles_tenant ON billing_cycles(tenant_id);
CREATE INDEX idx_billing_cycles_status ON billing_cycles(status);
CREATE INDEX idx_billing_cycles_dates ON billing_cycles(cycle_start_date, cycle_end_date);
CREATE INDEX idx_billing_cycles_due_date ON billing_cycles(due_date);

-- ============================================
-- CASHBACK LEDGER ENTRIES TABLE (Event Sourcing)
-- ============================================
CREATE TYPE cashback_entry_type AS ENUM (
    'earned',              -- Cashback earned from transaction
    'earned_refund',       -- Cashback adjustment from refund
    'redeemed',            -- Cashback applied to statement
    'redeemed_cancelled',  -- Redemption cancelled
    'expired',             -- Expired cashback (if applicable)
    'adjustment'           -- Manual adjustment
);

CREATE TABLE cashback_ledger_entries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    credit_card_id UUID NOT NULL REFERENCES credit_cards(id),
    statement_entry_id UUID REFERENCES statement_ledger_entries(id),

    -- Entry details
    entry_type cashback_entry_type NOT NULL,
    entry_date TIMESTAMP WITH TIME ZONE NOT NULL,

    -- Amount (positive = earned, negative = redeemed/expired)
    amount DECIMAL(10,2) NOT NULL,

    -- Description and reference
    description TEXT NOT NULL,
    reference_id VARCHAR(100),

    -- Calculation details (for earned entries)
    transaction_amount DECIMAL(15,2),
    cashback_rate DECIMAL(5,2),
    category_bonus DECIMAL(5,2),

    -- Metadata
    metadata JSONB,

    -- Audit trail
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by VARCHAR(100),

    -- Constraints
    CONSTRAINT earned_positive CHECK (
        entry_type != 'earned' OR amount > 0
    ),
    CONSTRAINT redeemed_negative CHECK (
        entry_type != 'redeemed' OR amount < 0
    )
);

CREATE INDEX idx_cashback_entries_tenant ON cashback_ledger_entries(tenant_id);
CREATE INDEX idx_cashback_entries_card ON cashback_ledger_entries(credit_card_id);
CREATE INDEX idx_cashback_entries_statement ON cashback_ledger_entries(statement_entry_id);
CREATE INDEX idx_cashback_entries_type ON cashback_ledger_entries(entry_type);
CREATE INDEX idx_cashback_entries_date ON cashback_ledger_entries(entry_date);

-- ============================================
-- CASHBACK CATEGORIES TABLE (Bonus Rates)
-- ============================================
CREATE TABLE cashback_categories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    credit_card_id UUID NOT NULL REFERENCES credit_cards(id),
    category_code VARCHAR(50) NOT NULL,                         -- MCC code or category name
    category_name VARCHAR(255) NOT NULL,
    bonus_rate DECIMAL(5,2) NOT NULL,                           -- e.g., 3% for restaurants
    max_bonus DECIMAL(10,2),                                    -- Cap per period (optional)
    is_active BOOLEAN NOT NULL DEFAULT true,
    start_date DATE,
    end_date DATE,

    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    UNIQUE(credit_card_id, category_code)
);

CREATE INDEX idx_cashback_categories_card ON cashback_categories(credit_card_id);
CREATE INDEX idx_cashback_categories_active ON cashback_categories(is_active);

-- ============================================
-- UPDATE STATEMENT ENTRY TYPES
-- ============================================
-- Add new entry types for credit card operations
ALTER TYPE statement_entry_type ADD VALUE IF NOT EXISTS 'fee_interest';
ALTER TYPE statement_entry_type ADD VALUE IF NOT EXISTS 'fee_over_limit';
ALTER TYPE statement_entry_type ADD VALUE IF NOT EXISTS 'fee_annual';
ALTER TYPE statement_entry_type ADD VALUE IF NOT EXISTS 'fee_cash_advance';
ALTER TYPE statement_entry_type ADD VALUE IF NOT EXISTS 'cash_advance';
ALTER TYPE statement_entry_type ADD VALUE IF NOT EXISTS 'cashback_earned';
ALTER TYPE statement_entry_type ADD VALUE IF NOT EXISTS 'cashback_redeemed';

-- ============================================
-- VIEWS FOR REAL-TIME BALANCES
-- ============================================

-- Cashback balance view
CREATE VIEW cashback_balances AS
SELECT
    tenant_id,
    credit_card_id,
    SUM(CASE WHEN entry_type = 'earned' THEN amount ELSE 0 END) as earned_total,
    SUM(CASE WHEN entry_type = 'redeemed' THEN ABS(amount) ELSE 0 END) as redeemed_total,
    SUM(CASE WHEN entry_type = 'expired' THEN ABS(amount) ELSE 0 END) as expired_total,
    SUM(amount) as available_balance,
    COUNT(*) as total_entries,
    MAX(entry_date) as last_activity_date
FROM cashback_ledger_entries
GROUP BY tenant_id, credit_card_id;

-- Credit card summary view
CREATE VIEW credit_card_summaries AS
SELECT
    cc.id as credit_card_id,
    cc.tenant_id,
    cc.cardholder_name,
    cc.credit_limit,
    cc.available_credit,
    cc.credit_limit - cc.available_credit as current_balance,
    cc.purchase_apr,
    cc.status,
    cc.next_statement_date,
    bc.cycle_number as current_cycle,
    bc.new_balance as statement_balance,
    bc.minimum_payment,
    bc.due_date,
    COALESCE(cb.available_balance, 0) as cashback_balance
FROM credit_cards cc
LEFT JOIN billing_cycles bc ON bc.credit_card_id = cc.id AND bc.status = 'closed'
    AND bc.cycle_number = (
        SELECT MAX(cycle_number) FROM billing_cycles WHERE credit_card_id = cc.id
    )
LEFT JOIN cashback_balances cb ON cb.credit_card_id = cc.id;

-- ============================================
-- FUNCTIONS FOR IMMUTABILITY
-- ============================================

-- Prevent updates on cashback ledger entries
CREATE TRIGGER prevent_cashback_entry_update
    BEFORE UPDATE ON cashback_ledger_entries
    FOR EACH ROW
    EXECUTE FUNCTION prevent_ledger_entry_update();

-- Update timestamps for credit cards
CREATE TRIGGER update_credit_cards_updated_at
    BEFORE UPDATE ON credit_cards
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Update timestamps for billing cycles
CREATE TRIGGER update_billing_cycles_updated_at
    BEFORE UPDATE ON billing_cycles
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Update timestamps for cashback categories
CREATE TRIGGER update_cashback_categories_updated_at
    BEFORE UPDATE ON cashback_categories
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================
-- COMMENTS
-- ============================================

COMMENT ON TABLE credit_cards IS 'Revolving credit card accounts with configurable APR, fees, and billing cycles';
COMMENT ON TABLE billing_cycles IS 'Billing periods for credit cards with GAAP-compliant balance breakdown';
COMMENT ON TABLE cashback_ledger_entries IS 'Immutable event log of cashback earnings and redemptions';
COMMENT ON TABLE cashback_categories IS 'Bonus cashback rates for merchant categories';
COMMENT ON VIEW cashback_balances IS 'Real-time calculation of cashback balances per credit card';
COMMENT ON VIEW credit_card_summaries IS 'Summary view of credit card status and balances';
