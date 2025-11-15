-- Migration: 001_create_ledger_tables.sql
-- Description: Create core tables for Statement and Points ledgers

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================
-- TENANTS TABLE
-- ============================================
CREATE TABLE tenants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_code VARCHAR(50) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    status VARCHAR(20) NOT NULL DEFAULT 'active', -- active, suspended, closed
    minimum_payment_percentage DECIMAL(5,4) NOT NULL DEFAULT 0.0500, -- 5% default
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tenants_code ON tenants(tenant_code);
CREATE INDEX idx_tenants_status ON tenants(status);

-- ============================================
-- STATEMENTS TABLE (Billing Periods)
-- ============================================
CREATE TABLE statements (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    statement_number VARCHAR(50) NOT NULL,

    -- Billing period
    billing_start_date DATE NOT NULL,
    billing_end_date DATE NOT NULL,
    due_date DATE NOT NULL,

    -- Balances (calculated and stored for performance)
    previous_balance DECIMAL(15,2) NOT NULL DEFAULT 0.00, -- From previous statement
    cleared_payments DECIMAL(15,2) NOT NULL DEFAULT 0.00,  -- Payments in this period
    opening_balance DECIMAL(15,2) NOT NULL DEFAULT 0.00,   -- Calculated: previous - cleared_payments
    statement_balance DECIMAL(15,2) NOT NULL DEFAULT 0.00, -- Total amount due
    minimum_payment DECIMAL(15,2) NOT NULL DEFAULT 0.00,   -- Calculated from statement_balance

    -- Status
    status VARCHAR(20) NOT NULL DEFAULT 'draft', -- draft, finalized, closed
    finalized_at TIMESTAMP WITH TIME ZONE,

    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, statement_number)
);

CREATE INDEX idx_statements_tenant ON statements(tenant_id);
CREATE INDEX idx_statements_billing_period ON statements(billing_start_date, billing_end_date);
CREATE INDEX idx_statements_status ON statements(status);

-- ============================================
-- STATEMENT LEDGER ENTRIES (Event Sourcing)
-- ============================================
CREATE TYPE statement_entry_type AS ENUM (
    'transaction',        -- Purchase/charge
    'payment',           -- Payment received
    'refund',            -- Refund issued
    'reward',            -- Points redeemed as credit
    'returned_reward',   -- Reward reversed
    'fee_late',          -- Late payment fee
    'fee_failed',        -- Failed payment fee
    'fee_international', -- International transaction fee
    'adjustment',        -- Manual adjustment
    'credit'             -- Account credit
);

CREATE TABLE statement_ledger_entries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    statement_id UUID REFERENCES statements(id), -- NULL if not yet assigned to statement

    -- Entry details
    entry_type statement_entry_type NOT NULL,
    entry_date TIMESTAMP WITH TIME ZONE NOT NULL,
    posting_date DATE NOT NULL, -- Date for billing period assignment

    -- Amount (positive = debit/charge, negative = credit/payment)
    amount DECIMAL(15,2) NOT NULL,

    -- Description and metadata
    description TEXT NOT NULL,
    reference_id VARCHAR(100), -- External reference (transaction ID, payment ID, etc.)
    metadata JSONB, -- Additional flexible data

    -- Status tracking
    status VARCHAR(20) NOT NULL DEFAULT 'pending', -- pending, cleared, reversed
    cleared_at TIMESTAMP WITH TIME ZONE,

    -- Audit trail
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by VARCHAR(100),

    -- Ensure immutability (no updates allowed)
    CONSTRAINT no_negative_fees CHECK (
        entry_type NOT LIKE 'fee_%' OR amount >= 0
    )
);

CREATE INDEX idx_statement_entries_tenant ON statement_ledger_entries(tenant_id);
CREATE INDEX idx_statement_entries_statement ON statement_ledger_entries(statement_id);
CREATE INDEX idx_statement_entries_type ON statement_ledger_entries(entry_type);
CREATE INDEX idx_statement_entries_posting_date ON statement_ledger_entries(posting_date);
CREATE INDEX idx_statement_entries_status ON statement_ledger_entries(status);
CREATE INDEX idx_statement_entries_reference ON statement_ledger_entries(reference_id);

-- ============================================
-- POINTS LEDGER ENTRIES (Event Sourcing)
-- ============================================
CREATE TYPE points_entry_type AS ENUM (
    'earned_transaction', -- Points earned from transaction
    'earned_refund',      -- Points adjustment from refund
    'redeemed_spent',     -- Points spent (from Keystone)
    'redeemed_cancelled', -- Redemption cancelled (from Keystone)
    'redeemed_refunded',  -- Redemption refunded (from Keystone)
    'adjustment'          -- Manual adjustment
);

CREATE TABLE points_ledger_entries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    statement_entry_id UUID REFERENCES statement_ledger_entries(id), -- Link to related statement entry if applicable

    -- Entry details
    entry_type points_entry_type NOT NULL,
    entry_date TIMESTAMP WITH TIME ZONE NOT NULL,

    -- Points (positive = earned, negative = redeemed/spent)
    points INTEGER NOT NULL,

    -- Description and metadata
    description TEXT NOT NULL,

    -- External platform tracking (Bridge2 Keystone)
    external_platform VARCHAR(50), -- 'keystone', etc.
    external_reference_id VARCHAR(100), -- External redemption ID

    -- Related to statement entry (for transaction/refund tracking)
    transaction_amount DECIMAL(15,2), -- Amount that generated points
    points_rate DECIMAL(5,4), -- Points per dollar (e.g., 0.01 = 1 point per dollar)

    metadata JSONB,

    -- Audit trail
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by VARCHAR(100),

    -- Constraints
    CONSTRAINT earned_points_positive CHECK (
        entry_type NOT LIKE 'earned_%' OR points > 0
    ),
    CONSTRAINT redeemed_points_impact CHECK (
        entry_type NOT LIKE 'redeemed_%' OR points != 0
    )
);

CREATE INDEX idx_points_entries_tenant ON points_ledger_entries(tenant_id);
CREATE INDEX idx_points_entries_statement_entry ON points_ledger_entries(statement_entry_id);
CREATE INDEX idx_points_entries_type ON points_ledger_entries(entry_type);
CREATE INDEX idx_points_entries_external_ref ON points_ledger_entries(external_reference_id);

-- ============================================
-- LEDGER BALANCES VIEW (Real-time calculation)
-- ============================================

-- Statement balance view (for current balance query)
CREATE VIEW statement_balances AS
SELECT
    tenant_id,
    SUM(CASE
        WHEN entry_type IN ('transaction', 'fee_late', 'fee_failed', 'fee_international') THEN amount
        WHEN entry_type IN ('payment', 'refund', 'reward', 'credit') THEN -amount
        WHEN entry_type = 'returned_reward' THEN amount
        WHEN entry_type = 'adjustment' THEN amount
        ELSE 0
    END) as current_balance,
    COUNT(*) as total_entries,
    MAX(entry_date) as last_activity_date
FROM statement_ledger_entries
WHERE status = 'cleared'
GROUP BY tenant_id;

-- Points balance view (for current points query)
CREATE VIEW points_balances AS
SELECT
    tenant_id,
    -- Earned points (positive)
    SUM(CASE
        WHEN entry_type LIKE 'earned_%' THEN points
        ELSE 0
    END) as earned_points,
    -- Redeemed points (calculated: spent - cancelled - refunded)
    SUM(CASE
        WHEN entry_type = 'redeemed_spent' THEN ABS(points)
        WHEN entry_type = 'redeemed_cancelled' THEN -ABS(points)
        WHEN entry_type = 'redeemed_refunded' THEN -ABS(points)
        ELSE 0
    END) as redeemed_points,
    -- Available balance
    SUM(points) as available_points,
    COUNT(*) as total_entries,
    MAX(entry_date) as last_activity_date
FROM points_ledger_entries
GROUP BY tenant_id;

-- ============================================
-- FUNCTIONS FOR DATA INTEGRITY
-- ============================================

-- Function to prevent updates on ledger entries (immutability)
CREATE OR REPLACE FUNCTION prevent_ledger_entry_update()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'Ledger entries are immutable. Create a reversal entry instead.';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply immutability triggers
CREATE TRIGGER prevent_statement_entry_update
    BEFORE UPDATE ON statement_ledger_entries
    FOR EACH ROW
    EXECUTE FUNCTION prevent_ledger_entry_update();

CREATE TRIGGER prevent_points_entry_update
    BEFORE UPDATE ON points_ledger_entries
    FOR EACH ROW
    EXECUTE FUNCTION prevent_ledger_entry_update();

-- Function to update statement updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_statements_updated_at
    BEFORE UPDATE ON statements
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================
-- SAMPLE DATA COMMENTS
-- ============================================

COMMENT ON TABLE tenants IS 'Stores tenant account information';
COMMENT ON TABLE statements IS 'Billing period statements with calculated balances';
COMMENT ON TABLE statement_ledger_entries IS 'Immutable event log of all financial transactions';
COMMENT ON TABLE points_ledger_entries IS 'Immutable event log of all points activities';
COMMENT ON VIEW statement_balances IS 'Real-time calculation of tenant statement balances';
COMMENT ON VIEW points_balances IS 'Real-time calculation of tenant points balances';
