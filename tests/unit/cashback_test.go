package unit

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/livefire2015/ez-ledger/src/models"
	"github.com/shopspring/decimal"
)

func TestCashbackLedgerEntryIsEarning(t *testing.T) {
	tests := []struct {
		name      string
		entryType models.CashbackEntryType
		expected  bool
	}{
		{"earned", models.CashbackEarned, true},
		{"redeemed cancelled", models.CashbackRedeemedCancelled, true},
		{"redeemed", models.CashbackRedeemed, false},
		{"earned refund", models.CashbackEarnedRefund, false},
		{"expired", models.CashbackExpired, false},
		{"adjustment", models.CashbackAdjustment, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &models.CashbackLedgerEntry{EntryType: tt.entryType}
			result := entry.IsEarning()
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCashbackLedgerEntryIsDeduction(t *testing.T) {
	tests := []struct {
		name      string
		entryType models.CashbackEntryType
		expected  bool
	}{
		{"redeemed", models.CashbackRedeemed, true},
		{"earned refund", models.CashbackEarnedRefund, true},
		{"expired", models.CashbackExpired, true},
		{"earned", models.CashbackEarned, false},
		{"adjustment", models.CashbackAdjustment, false},
		{"redeemed cancelled", models.CashbackRedeemedCancelled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &models.CashbackLedgerEntry{EntryType: tt.entryType}
			result := entry.IsDeduction()
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCashbackCategoryIsActiveOn(t *testing.T) {
	now := time.Now()
	pastDate := now.AddDate(0, -1, 0)
	futureDate := now.AddDate(0, 1, 0)

	tests := []struct {
		name      string
		isActive  bool
		startDate *time.Time
		endDate   *time.Time
		checkDate time.Time
		expected  bool
	}{
		{
			name:     "active with no date restrictions",
			isActive: true,
			checkDate: now,
			expected:  true,
		},
		{
			name:     "inactive",
			isActive: false,
			checkDate: now,
			expected:  false,
		},
		{
			name:      "active within date range",
			isActive:  true,
			startDate: &pastDate,
			endDate:   &futureDate,
			checkDate: now,
			expected:  true,
		},
		{
			name:      "before start date",
			isActive:  true,
			startDate: &futureDate,
			checkDate: now,
			expected:  false,
		},
		{
			name:     "after end date",
			isActive: true,
			endDate:  &pastDate,
			checkDate: now,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := &models.CashbackCategory{
				IsActive:  tt.isActive,
				StartDate: tt.startDate,
				EndDate:   tt.endDate,
			}

			result := category.IsActiveOn(tt.checkDate)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCashbackEarningRuleCalculateCashback(t *testing.T) {
	baseRate := decimal.NewFromFloat(1.5) // 1.5% base rate

	restaurantCategory := models.CashbackCategory{
		ID:           uuid.New(),
		CategoryCode: "restaurants",
		CategoryName: "Restaurants",
		BonusRate:    decimal.NewFromFloat(3.0), // 3% for restaurants
		IsActive:     true,
	}

	rule := models.CashbackEarningRule{
		BaseRate:       baseRate,
		MinTransaction: decimal.NewFromFloat(0.01),
		CategoryRules: map[string]models.CashbackCategory{
			"restaurants": restaurantCategory,
		},
	}

	now := time.Now()

	tests := []struct {
		name             string
		amount           decimal.Decimal
		category         string
		expectedCashback decimal.Decimal
		expectedRate     decimal.Decimal
	}{
		{
			name:             "standard purchase",
			amount:           decimal.NewFromInt(100),
			category:         "groceries",
			expectedCashback: decimal.NewFromFloat(1.50), // 100 * 0.015 = 1.50
			expectedRate:     decimal.NewFromFloat(1.5),
		},
		{
			name:             "restaurant purchase - bonus rate",
			amount:           decimal.NewFromInt(100),
			category:         "restaurants",
			expectedCashback: decimal.NewFromFloat(3.00), // 100 * 0.03 = 3.00
			expectedRate:     decimal.NewFromFloat(3.0),
		},
		{
			name:             "large purchase",
			amount:           decimal.NewFromInt(1000),
			category:         "groceries",
			expectedCashback: decimal.NewFromFloat(15.00), // 1000 * 0.015 = 15.00
			expectedRate:     decimal.NewFromFloat(1.5),
		},
		{
			name:             "small purchase",
			amount:           decimal.NewFromFloat(5.00),
			category:         "groceries",
			expectedCashback: decimal.NewFromFloat(0.08), // 5 * 0.015 = 0.075, rounded to 0.08
			expectedRate:     decimal.NewFromFloat(1.5),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cashback, rate := rule.CalculateCashback(tt.amount, tt.category, now)

			if !cashback.Equal(tt.expectedCashback) {
				t.Errorf("Expected cashback %s, got %s", tt.expectedCashback, cashback)
			}
			if !rate.Equal(tt.expectedRate) {
				t.Errorf("Expected rate %s, got %s", tt.expectedRate, rate)
			}
		})
	}
}

func TestCashbackEarningRuleMinTransaction(t *testing.T) {
	rule := models.CashbackEarningRule{
		BaseRate:       decimal.NewFromFloat(1.5),
		MinTransaction: decimal.NewFromFloat(10.00), // Minimum $10 to earn cashback
		CategoryRules:  make(map[string]models.CashbackCategory),
	}

	now := time.Now()

	tests := []struct {
		name             string
		amount           decimal.Decimal
		expectedCashback decimal.Decimal
	}{
		{
			name:             "below minimum",
			amount:           decimal.NewFromFloat(5.00),
			expectedCashback: decimal.Zero,
		},
		{
			name:             "at minimum",
			amount:           decimal.NewFromFloat(10.00),
			expectedCashback: decimal.NewFromFloat(0.15),
		},
		{
			name:             "above minimum",
			amount:           decimal.NewFromFloat(100.00),
			expectedCashback: decimal.NewFromFloat(1.50),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cashback, _ := rule.CalculateCashback(tt.amount, "general", now)

			if !cashback.Equal(tt.expectedCashback) {
				t.Errorf("Expected cashback %s, got %s", tt.expectedCashback, cashback)
			}
		})
	}
}

func TestCashbackEarningRuleMaxCashback(t *testing.T) {
	maxCashback := decimal.NewFromInt(50)
	rule := models.CashbackEarningRule{
		BaseRate:         decimal.NewFromFloat(5.0), // 5% high rate
		MinTransaction:   decimal.NewFromFloat(0.01),
		MaxCashbackPerTx: &maxCashback,
		CategoryRules:    make(map[string]models.CashbackCategory),
	}

	now := time.Now()

	tests := []struct {
		name             string
		amount           decimal.Decimal
		expectedCashback decimal.Decimal
	}{
		{
			name:             "below cap",
			amount:           decimal.NewFromInt(100),
			expectedCashback: decimal.NewFromFloat(5.00), // 100 * 0.05 = 5
		},
		{
			name:             "at cap",
			amount:           decimal.NewFromInt(1000),
			expectedCashback: decimal.NewFromFloat(50.00), // 1000 * 0.05 = 50 (at cap)
		},
		{
			name:             "above cap",
			amount:           decimal.NewFromInt(5000),
			expectedCashback: decimal.NewFromFloat(50.00), // 5000 * 0.05 = 250, capped at 50
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cashback, _ := rule.CalculateCashback(tt.amount, "general", now)

			if !cashback.Equal(tt.expectedCashback) {
				t.Errorf("Expected cashback %s, got %s", tt.expectedCashback, cashback)
			}
		})
	}
}

func TestDefaultCashbackRule(t *testing.T) {
	rate := decimal.NewFromFloat(2.0)
	rule := models.DefaultCashbackRule(rate)

	if !rule.BaseRate.Equal(rate) {
		t.Errorf("Expected base rate %s, got %s", rate, rule.BaseRate)
	}
	if !rule.MinTransaction.Equal(decimal.NewFromFloat(0.01)) {
		t.Errorf("Expected min transaction 0.01, got %s", rule.MinTransaction)
	}
	if rule.MaxCashbackPerTx != nil {
		t.Error("Expected no max cashback by default")
	}
	if rule.CategoryRules == nil {
		t.Error("Expected initialized category rules map")
	}
	if len(rule.CategoryRules) != 0 {
		t.Error("Expected empty category rules")
	}
}

func TestCashbackStatementEffectiveRate(t *testing.T) {
	statement := &models.CashbackStatement{
		TotalPurchases: decimal.NewFromInt(1000),
		CashbackEarned: decimal.NewFromInt(20),
	}

	// Effective rate = (20 / 1000) * 100 = 2%
	if statement.TotalPurchases.GreaterThan(decimal.Zero) {
		effectiveRate := statement.CashbackEarned.
			Div(statement.TotalPurchases).
			Mul(decimal.NewFromInt(100)).
			Round(2)

		expected := decimal.NewFromFloat(2.0)
		if !effectiveRate.Equal(expected) {
			t.Errorf("Expected effective rate %s, got %s", expected, effectiveRate)
		}
	}
}

func TestCategoryCashbackSummary(t *testing.T) {
	summary := models.CategoryCashbackSummary{
		CategoryCode:     "restaurants",
		CategoryName:     "Restaurants",
		TransactionCount: 10,
		TotalSpent:       decimal.NewFromInt(500),
		CashbackEarned:   decimal.NewFromInt(15),
	}

	// Verify effective rate calculation
	// 15 / 500 * 100 = 3%
	expectedRate := decimal.NewFromFloat(3.0)
	if summary.TotalSpent.GreaterThan(decimal.Zero) {
		effectiveRate := summary.CashbackEarned.
			Div(summary.TotalSpent).
			Mul(decimal.NewFromInt(100)).
			Round(2)

		if !effectiveRate.Equal(expectedRate) {
			t.Errorf("Expected effective rate %s, got %s", expectedRate, effectiveRate)
		}
	}
}
