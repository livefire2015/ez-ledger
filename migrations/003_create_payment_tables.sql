-- Migration: 003_create_payment_tables.sql
-- Description: Create tables for payment tracking with full status history
-- Supports: Multiple payment methods, status transitions, ACH returns, retry tracking

-- ============================================
-- PAYMENT STATUS TYPE
-- ============================================
CREATE TYPE payment_status AS ENUM (
    'pending',      -- Payment initiated, not yet processed
    'processing',   -- Payment being processed by payment processor
    'cleared',      -- Payment successfully completed
    'failed',       -- Payment failed (NSF, declined, etc.)
    'returned',     -- Payment returned (ACH return, chargeback)
    'cancelled',    -- Payment cancelled before processing
    'reversed'      -- Payment reversed after clearing
);

-- ============================================
-- PAYMENT METHOD TYPE
-- ============================================
CREATE TYPE payment_method AS ENUM (
    'ach',              -- Bank transfer (ACH)
    'debit_card',       -- Debit card payment
    'check',            -- Paper check
    'wire',             -- Wire transfer
    'internal_xfer',    -- Internal account transfer
    'external_xfer',    -- External transfer
    'cash',             -- Cash payment (in-person)
    'money_order'       -- Money order
);

-- ============================================
-- PAYMENT TYPE
-- ============================================
CREATE TYPE payment_type AS ENUM (
    'regular',      -- Standard payment
    'minimum',      -- Minimum payment
    'statement',    -- Statement balance payment
    'full',         -- Full balance payment
    'auto_pay',     -- Automatic payment
    'scheduled',    -- Scheduled payment
    'one_time'      -- One-time payment
);

-- ============================================
-- PAYMENTS TABLE
-- ============================================
CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    credit_card_id UUID NOT NULL REFERENCES credit_cards(id),

    -- Payment identification
    payment_number VARCHAR(50) NOT NULL UNIQUE,
    confirmation_num VARCHAR(100),

    -- Amount details
    amount DECIMAL(15,2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    applied_amount DECIMAL(15,2) NOT NULL DEFAULT 0.00,
    processing_fee DECIMAL(10,2) NOT NULL DEFAULT 0.00,

    -- Payment type and method
    payment_type payment_type NOT NULL DEFAULT 'regular',
    payment_method payment_method NOT NULL,

    -- Source information (masked)
    source_account_last4 VARCHAR(4),
    source_routing_last4 VARCHAR(4),
    source_bank_name VARCHAR(255),

    -- Billing cycle linkage
    billing_cycle_id UUID REFERENCES billing_cycles(id),
    statement_entry_id UUID REFERENCES statement_ledger_entries(id),

    -- Status tracking
    status payment_status NOT NULL DEFAULT 'pending',
    previous_status payment_status,
    status_reason TEXT,

    -- Key timestamps
    scheduled_date DATE,
    initiated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    processing_at TIMESTAMP WITH TIME ZONE,
    cleared_at TIMESTAMP WITH TIME ZONE,
    failed_at TIMESTAMP WITH TIME ZONE,
    returned_at TIMESTAMP WITH TIME ZONE,
    cancelled_at TIMESTAMP WITH TIME ZONE,
    reversed_at TIMESTAMP WITH TIME ZONE,
    effective_date DATE NOT NULL DEFAULT CURRENT_DATE,

    -- Processing details
    processor_ref VARCHAR(255),
    processor_response TEXT,
    return_reason_code VARCHAR(10),
    return_reason_desc TEXT,

    -- Retry tracking
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMP WITH TIME ZONE,
    next_retry_at TIMESTAMP WITH TIME ZONE,
    max_retries INTEGER NOT NULL DEFAULT 3,

    -- Metadata and audit
    metadata JSONB,
    notes TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by VARCHAR(100),
    updated_by VARCHAR(100),

    -- Constraints
    CONSTRAINT positive_amount CHECK (amount > 0),
    CONSTRAINT valid_applied_amount CHECK (applied_amount >= 0 AND applied_amount <= amount),
    CONSTRAINT valid_attempt_count CHECK (attempt_count >= 0),
    CONSTRAINT valid_max_retries CHECK (max_retries >= 0)
);

-- Indexes for common queries
CREATE INDEX idx_payments_tenant ON payments(tenant_id);
CREATE INDEX idx_payments_credit_card ON payments(credit_card_id);
CREATE INDEX idx_payments_billing_cycle ON payments(billing_cycle_id);
CREATE INDEX idx_payments_status ON payments(status);
CREATE INDEX idx_payments_payment_number ON payments(payment_number);
CREATE INDEX idx_payments_initiated_at ON payments(initiated_at);
CREATE INDEX idx_payments_effective_date ON payments(effective_date);
CREATE INDEX idx_payments_scheduled_date ON payments(scheduled_date) WHERE scheduled_date IS NOT NULL;
CREATE INDEX idx_payments_next_retry ON payments(next_retry_at) WHERE next_retry_at IS NOT NULL AND status = 'failed';

-- ============================================
-- PAYMENT STATUS TRANSITIONS TABLE (Audit Log)
-- ============================================
CREATE TABLE payment_status_transitions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    payment_id UUID NOT NULL REFERENCES payments(id),

    -- Transition details
    from_status payment_status,
    to_status payment_status NOT NULL,
    reason TEXT,

    -- Timestamp and attribution
    transition_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    triggered_by VARCHAR(100),  -- 'system', 'user', 'processor', etc.

    -- Additional context
    metadata JSONB
);

-- Indexes for transition queries
CREATE INDEX idx_payment_transitions_payment ON payment_status_transitions(payment_id);
CREATE INDEX idx_payment_transitions_at ON payment_status_transitions(transition_at);
CREATE INDEX idx_payment_transitions_to_status ON payment_status_transitions(to_status);

-- ============================================
-- ACH RETURN CODES REFERENCE TABLE
-- ============================================
CREATE TABLE ach_return_codes (
    code VARCHAR(4) PRIMARY KEY,
    description TEXT NOT NULL,
    is_hard_failure BOOLEAN NOT NULL DEFAULT false,
    is_administrative BOOLEAN NOT NULL DEFAULT false,
    typical_action TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Insert common ACH return codes
INSERT INTO ach_return_codes (code, description, is_hard_failure, typical_action) VALUES
    ('R01', 'Insufficient Funds', false, 'May retry after funds available'),
    ('R02', 'Account Closed', true, 'Obtain new account information'),
    ('R03', 'No Account/Unable to Locate Account', true, 'Verify account number'),
    ('R04', 'Invalid Account Number', true, 'Verify account number format'),
    ('R05', 'Unauthorized Debit to Consumer Account', true, 'Obtain new authorization'),
    ('R06', 'Returned per ODFI Request', false, 'Contact originating bank'),
    ('R07', 'Authorization Revoked by Customer', true, 'Obtain new authorization'),
    ('R08', 'Payment Stopped', false, 'Contact customer'),
    ('R09', 'Uncollected Funds', false, 'May retry when funds collected'),
    ('R10', 'Customer Advises Not Authorized', true, 'Obtain proper authorization'),
    ('R16', 'Account Frozen', true, 'Customer must resolve with bank'),
    ('R20', 'Non-Transaction Account', true, 'Obtain different account'),
    ('R29', 'Corporate Customer Advises Not Authorized', true, 'Obtain corporate authorization');

-- ============================================
-- SCHEDULED PAYMENTS TABLE
-- ============================================
CREATE TABLE scheduled_payments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    credit_card_id UUID NOT NULL REFERENCES credit_cards(id),

    -- Schedule configuration
    schedule_type VARCHAR(20) NOT NULL, -- 'one_time', 'recurring'
    frequency VARCHAR(20),              -- 'weekly', 'biweekly', 'monthly', 'quarterly'
    day_of_month INTEGER CHECK (day_of_month IS NULL OR (day_of_month BETWEEN 1 AND 28)),

    -- Payment details
    payment_type payment_type NOT NULL,
    payment_method payment_method NOT NULL,
    amount DECIMAL(15,2),               -- NULL means dynamic (e.g., minimum, statement balance)
    amount_type VARCHAR(20),            -- 'fixed', 'minimum', 'statement', 'full'

    -- Source account
    source_account_last4 VARCHAR(4),
    source_routing_last4 VARCHAR(4),
    source_bank_name VARCHAR(255),

    -- Schedule dates
    start_date DATE NOT NULL,
    end_date DATE,
    next_payment_date DATE,
    last_payment_date DATE,

    -- Status
    is_active BOOLEAN NOT NULL DEFAULT true,
    failed_count INTEGER NOT NULL DEFAULT 0,
    max_failures INTEGER NOT NULL DEFAULT 3,  -- Deactivate after N failures

    -- Audit
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by VARCHAR(100),

    CONSTRAINT valid_schedule CHECK (
        (schedule_type = 'one_time' AND frequency IS NULL) OR
        (schedule_type = 'recurring' AND frequency IS NOT NULL)
    )
);

CREATE INDEX idx_scheduled_payments_tenant ON scheduled_payments(tenant_id);
CREATE INDEX idx_scheduled_payments_card ON scheduled_payments(credit_card_id);
CREATE INDEX idx_scheduled_payments_next ON scheduled_payments(next_payment_date) WHERE is_active = true;
CREATE INDEX idx_scheduled_payments_active ON scheduled_payments(is_active);

-- ============================================
-- PAYMENT SUMMARY VIEW
-- ============================================
CREATE VIEW payment_summaries AS
SELECT
    p.tenant_id,
    p.credit_card_id,
    DATE_TRUNC('month', p.initiated_at) as period,
    COUNT(*) as total_payments,
    SUM(p.amount) as total_amount,
    COUNT(*) FILTER (WHERE p.status = 'cleared') as cleared_payments,
    SUM(p.amount) FILTER (WHERE p.status = 'cleared') as cleared_amount,
    COUNT(*) FILTER (WHERE p.status IN ('pending', 'processing')) as pending_payments,
    SUM(p.amount) FILTER (WHERE p.status IN ('pending', 'processing')) as pending_amount,
    COUNT(*) FILTER (WHERE p.status = 'failed') as failed_payments,
    SUM(p.amount) FILTER (WHERE p.status = 'failed') as failed_amount,
    COUNT(*) FILTER (WHERE p.status = 'returned') as returned_payments,
    SUM(p.amount) FILTER (WHERE p.status = 'returned') as returned_amount,
    AVG(p.amount) FILTER (WHERE p.status = 'cleared') as avg_payment,
    MAX(p.amount) FILTER (WHERE p.status = 'cleared') as largest_payment,
    MAX(p.cleared_at) as last_payment_date
FROM payments p
GROUP BY p.tenant_id, p.credit_card_id, DATE_TRUNC('month', p.initiated_at);

-- ============================================
-- TRIGGERS
-- ============================================

-- Update timestamps on payments
CREATE TRIGGER update_payments_updated_at
    BEFORE UPDATE ON payments
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Update timestamps on scheduled payments
CREATE TRIGGER update_scheduled_payments_updated_at
    BEFORE UPDATE ON scheduled_payments
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Log status transitions automatically
CREATE OR REPLACE FUNCTION log_payment_status_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status IS DISTINCT FROM NEW.status THEN
        INSERT INTO payment_status_transitions (
            payment_id,
            from_status,
            to_status,
            transition_at,
            triggered_by
        ) VALUES (
            NEW.id,
            OLD.status,
            NEW.status,
            NOW(),
            COALESCE(NEW.updated_by, 'system')
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER log_payment_status_change
    AFTER UPDATE ON payments
    FOR EACH ROW
    EXECUTE FUNCTION log_payment_status_transition();

-- ============================================
-- COMMENTS
-- ============================================
COMMENT ON TABLE payments IS 'Payment records with full status tracking and retry support';
COMMENT ON TABLE payment_status_transitions IS 'Immutable audit log of payment status changes';
COMMENT ON TABLE ach_return_codes IS 'Reference table for ACH return reason codes';
COMMENT ON TABLE scheduled_payments IS 'Scheduled and recurring payment configurations';
COMMENT ON VIEW payment_summaries IS 'Monthly payment statistics by credit card';
