package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Tenant represents a customer account
type Tenant struct {
	ID                        uuid.UUID       `json:"id" db:"id"`
	TenantCode                string          `json:"tenant_code" db:"tenant_code"`
	Name                      string          `json:"name" db:"name"`
	Email                     string          `json:"email" db:"email"`
	Status                    string          `json:"status" db:"status"` // active, suspended, closed
	MinimumPaymentPercentage  decimal.Decimal `json:"minimum_payment_percentage" db:"minimum_payment_percentage"`
	CreatedAt                 time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt                 time.Time       `json:"updated_at" db:"updated_at"`
}

// TenantStatus represents valid tenant statuses
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusClosed    TenantStatus = "closed"
)
