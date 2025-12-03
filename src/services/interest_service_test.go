package services

import (
	"testing"

	"github.com/livefire2015/ez-ledger/src/models"
	"github.com/shopspring/decimal"
)

func TestCalculateADBInterest(t *testing.T) {
	service := &InterestService{}

	tests := []struct {
		name        string
		adb         decimal.Decimal
		dpr         decimal.Decimal
		daysInCycle int
		expected    decimal.Decimal
	}{
		{
			name:        "Standard calculation",
			adb:         decimal.NewFromFloat(1000.00),
			dpr:         decimal.NewFromFloat(0.0005), // 0.05% daily
			daysInCycle: 30,
			expected:    decimal.NewFromFloat(15.00), // 1000 * 0.0005 * 30 = 15
		},
		{
			name:        "Zero ADB",
			adb:         decimal.Zero,
			dpr:         decimal.NewFromFloat(0.0005),
			daysInCycle: 30,
			expected:    decimal.Zero,
		},
		{
			name:        "Negative ADB",
			adb:         decimal.NewFromFloat(-100.00),
			dpr:         decimal.NewFromFloat(0.0005),
			daysInCycle: 30,
			expected:    decimal.Zero,
		},
		{
			name:        "Zero DPR",
			adb:         decimal.NewFromFloat(1000.00),
			dpr:         decimal.Zero,
			daysInCycle: 30,
			expected:    decimal.Zero,
		},
		{
			name:        "Rounding check",
			adb:         decimal.NewFromFloat(1234.56),
			dpr:         decimal.NewFromFloat(0.000345),
			daysInCycle: 31,
			// 1234.56 * 0.000345 * 31 = 13.2048528 -> 13.20
			expected: decimal.NewFromFloat(13.20),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.calculateADBInterest(tt.adb, tt.dpr, tt.daysInCycle)
			if !result.Equal(tt.expected) {
				t.Errorf("calculateADBInterest() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateDailyBalanceInterest(t *testing.T) {
	service := &InterestService{}

	tests := []struct {
		name     string
		balances []models.DailyBalanceRecord
		dpr      decimal.Decimal
		expected decimal.Decimal
	}{
		{
			name: "Constant balance",
			balances: []models.DailyBalanceRecord{
				{Balance: decimal.NewFromFloat(1000.00)},
				{Balance: decimal.NewFromFloat(1000.00)},
				{Balance: decimal.NewFromFloat(1000.00)},
			},
			dpr:      decimal.NewFromFloat(0.0005),
			expected: decimal.NewFromFloat(1.50), // (1000*0.0005)*3 = 1.50
		},
		{
			name: "Varying balance",
			balances: []models.DailyBalanceRecord{
				{Balance: decimal.NewFromFloat(1000.00)}, // Int: 0.5
				{Balance: decimal.NewFromFloat(2000.00)}, // Int: 1.0
				{Balance: decimal.NewFromFloat(500.00)},  // Int: 0.25
			},
			dpr:      decimal.NewFromFloat(0.0005),
			expected: decimal.NewFromFloat(1.75), // 0.5 + 1.0 + 0.25 = 1.75
		},
		{
			name: "Includes negative balance",
			balances: []models.DailyBalanceRecord{
				{Balance: decimal.NewFromFloat(1000.00)}, // Int: 0.5
				{Balance: decimal.NewFromFloat(-500.00)}, // Int: 0 (ignored)
				{Balance: decimal.NewFromFloat(1000.00)}, // Int: 0.5
			},
			dpr:      decimal.NewFromFloat(0.0005),
			expected: decimal.NewFromFloat(1.00),
		},
		{
			name:     "Empty balances",
			balances: []models.DailyBalanceRecord{},
			dpr:      decimal.NewFromFloat(0.0005),
			expected: decimal.Zero,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.calculateDailyBalanceInterest(tt.balances, tt.dpr)
			if !result.Equal(tt.expected) {
				t.Errorf("calculateDailyBalanceInterest() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateAdjustedBalanceInterest(t *testing.T) {
	service := &InterestService{}

	tests := []struct {
		name            string
		previousBalance decimal.Decimal
		payments        decimal.Decimal
		apr             decimal.Decimal
		expected        decimal.Decimal
	}{
		{
			name:            "Standard calculation",
			previousBalance: decimal.NewFromFloat(1000.00),
			payments:        decimal.NewFromFloat(200.00),
			apr:             decimal.NewFromFloat(12.00), // 12% APR -> 1% monthly
			// Adjusted Balance: 800
			// Monthly Rate: 12 / 12 / 100 = 0.01
			// Interest: 800 * 0.01 = 8.00
			expected: decimal.NewFromFloat(8.00),
		},
		{
			name:            "Full payment",
			previousBalance: decimal.NewFromFloat(1000.00),
			payments:        decimal.NewFromFloat(1000.00),
			apr:             decimal.NewFromFloat(12.00),
			expected:        decimal.Zero,
		},
		{
			name:            "Overpayment",
			previousBalance: decimal.NewFromFloat(1000.00),
			payments:        decimal.NewFromFloat(1200.00),
			apr:             decimal.NewFromFloat(12.00),
			expected:        decimal.Zero,
		},
		{
			name:            "Zero APR",
			previousBalance: decimal.NewFromFloat(1000.00),
			payments:        decimal.NewFromFloat(0.00),
			apr:             decimal.Zero,
			expected:        decimal.Zero,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.calculateAdjustedBalanceInterest(tt.previousBalance, tt.payments, tt.apr)
			if !result.Equal(tt.expected) {
				t.Errorf("calculateAdjustedBalanceInterest() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateProjectedInterest(t *testing.T) {
	service := &InterestService{}

	tests := []struct {
		name           string
		currentBalance decimal.Decimal
		apr            decimal.Decimal
		days           int
		expectedInt    decimal.Decimal
		expectedTotal  decimal.Decimal
	}{
		{
			name:           "Standard projection",
			currentBalance: decimal.NewFromFloat(1000.00),
			apr:            decimal.NewFromFloat(18.25), // Easy division: 18.25 / 365 = 0.05
			days:           30,
			// DPR = 18.25 / 365 / 100 = 0.0005
			// Interest = 1000 * 0.0005 * 30 = 15.00
			expectedInt:   decimal.NewFromFloat(15.00),
			expectedTotal: decimal.NewFromFloat(1015.00),
		},
		{
			name:           "Zero balance",
			currentBalance: decimal.Zero,
			apr:            decimal.NewFromFloat(18.25),
			days:           30,
			expectedInt:    decimal.Zero,
			expectedTotal:  decimal.Zero,
		},
		{
			name:           "Negative balance",
			currentBalance: decimal.NewFromFloat(-100.00),
			apr:            decimal.NewFromFloat(18.25),
			days:           30,
			expectedInt:    decimal.Zero,
			expectedTotal:  decimal.NewFromFloat(-100.00),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.CalculateProjectedInterest(tt.currentBalance, tt.apr, tt.days)
			if !result.ProjectedInterest.Equal(tt.expectedInt) {
				t.Errorf("ProjectedInterest = %v, want %v", result.ProjectedInterest, tt.expectedInt)
			}
			if !result.TotalIfUnpaid.Equal(tt.expectedTotal) {
				t.Errorf("TotalIfUnpaid = %v, want %v", result.TotalIfUnpaid, tt.expectedTotal)
			}
		})
	}
}
