package unit

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/livefire2015/ez-ledger/src/models"
	"github.com/shopspring/decimal"
)

func TestBillingCycleCalculateNewBalance(t *testing.T) {
	tests := []struct {
		name            string
		previousBalance decimal.Decimal
		payments        decimal.Decimal
		purchases       decimal.Decimal
		cashAdvances    decimal.Decimal
		refunds         decimal.Decimal
		fees            decimal.Decimal
		interest        decimal.Decimal
		adjustments     decimal.Decimal
		cashback        decimal.Decimal
		expectedBalance decimal.Decimal
	}{
		{
			name:            "simple purchase only",
			previousBalance: decimal.NewFromInt(0),
			purchases:       decimal.NewFromInt(500),
			expectedBalance: decimal.NewFromInt(500),
		},
		{
			name:            "previous balance with payment",
			previousBalance: decimal.NewFromInt(1000),
			payments:        decimal.NewFromInt(300),
			purchases:       decimal.NewFromInt(200),
			expectedBalance: decimal.NewFromInt(900), // 1000 - 300 + 200
		},
		{
			name:            "full payment scenario",
			previousBalance: decimal.NewFromInt(500),
			payments:        decimal.NewFromInt(500),
			expectedBalance: decimal.NewFromInt(0),
		},
		{
			name:            "complex scenario",
			previousBalance: decimal.NewFromInt(1000),
			payments:        decimal.NewFromInt(200),
			purchases:       decimal.NewFromInt(500),
			cashAdvances:    decimal.NewFromInt(100),
			refunds:         decimal.NewFromInt(50),
			fees:            decimal.NewFromInt(35),
			interest:        decimal.NewFromInt(15),
			adjustments:     decimal.NewFromInt(10),
			cashback:        decimal.NewFromInt(25),
			expectedBalance: decimal.NewFromInt(1385), // 1000 - 200 + 500 + 100 - 50 + 35 + 15 + 10 - 25
		},
		{
			name:            "overpayment results in credit",
			previousBalance: decimal.NewFromInt(100),
			payments:        decimal.NewFromInt(200),
			expectedBalance: decimal.NewFromInt(-100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycle := &models.BillingCycle{
				PreviousBalance:    tt.previousBalance,
				PaymentsReceived:   tt.payments,
				PurchasesAmount:    tt.purchases,
				CashAdvancesAmount: tt.cashAdvances,
				RefundsAmount:      tt.refunds,
				FeesAmount:         tt.fees,
				InterestAmount:     tt.interest,
				AdjustmentsAmount:  tt.adjustments,
				CashbackRedeemed:   tt.cashback,
			}

			result := cycle.CalculateNewBalance()
			if !result.Equal(tt.expectedBalance) {
				t.Errorf("Expected balance %s, got %s", tt.expectedBalance, result)
			}
		})
	}
}

func TestBillingCycleGetRemainingBalance(t *testing.T) {
	cycle := &models.BillingCycle{
		NewBalance:   decimal.NewFromInt(1000),
		PaymentsMade: decimal.NewFromInt(300),
	}

	remaining := cycle.GetRemainingBalance()
	expected := decimal.NewFromInt(700)

	if !remaining.Equal(expected) {
		t.Errorf("Expected remaining %s, got %s", expected, remaining)
	}
}

func TestBillingCycleGetRemainingMinimum(t *testing.T) {
	tests := []struct {
		name         string
		minimum      decimal.Decimal
		payments     decimal.Decimal
		expectedRem  decimal.Decimal
	}{
		{"no payments", decimal.NewFromInt(50), decimal.Zero, decimal.NewFromInt(50)},
		{"partial payment", decimal.NewFromInt(50), decimal.NewFromInt(30), decimal.NewFromInt(20)},
		{"full payment", decimal.NewFromInt(50), decimal.NewFromInt(50), decimal.Zero},
		{"overpayment", decimal.NewFromInt(50), decimal.NewFromInt(100), decimal.Zero},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycle := &models.BillingCycle{
				MinimumPayment: tt.minimum,
				PaymentsMade:   tt.payments,
			}

			remaining := cycle.GetRemainingMinimum()
			if !remaining.Equal(tt.expectedRem) {
				t.Errorf("Expected remaining minimum %s, got %s", tt.expectedRem, remaining)
			}
		})
	}
}

func TestBillingCycleIsOverdue(t *testing.T) {
	dueDate := time.Date(2024, 6, 25, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		currentDate    time.Time
		minimumPaid    bool
		expectedOverdue bool
	}{
		{"before due date", time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC), false, false},
		{"on due date", time.Date(2024, 6, 25, 0, 0, 0, 0, time.UTC), false, false},
		{"after due date - not paid", time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC), false, true},
		{"after due date - paid", time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycle := &models.BillingCycle{
				DueDate:           dueDate,
				MinimumPaymentMet: tt.minimumPaid,
			}

			result := cycle.IsOverdue(tt.currentDate)
			if result != tt.expectedOverdue {
				t.Errorf("Expected overdue %v, got %v", tt.expectedOverdue, result)
			}
		})
	}
}

func TestBillingCycleDaysOverdue(t *testing.T) {
	dueDate := time.Date(2024, 6, 25, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		currentDate  time.Time
		minimumPaid  bool
		expectedDays int
	}{
		{"before due date", time.Date(2024, 6, 20, 0, 0, 0, 0, time.UTC), false, 0},
		{"on due date", time.Date(2024, 6, 25, 0, 0, 0, 0, time.UTC), false, 0},
		{"5 days after - not paid", time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC), false, 5},
		{"5 days after - paid", time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC), true, 0},
		{"30 days after - not paid", time.Date(2024, 7, 25, 0, 0, 0, 0, time.UTC), false, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycle := &models.BillingCycle{
				DueDate:           dueDate,
				MinimumPaymentMet: tt.minimumPaid,
			}

			result := cycle.DaysOverdue(tt.currentDate)
			if result != tt.expectedDays {
				t.Errorf("Expected %d days overdue, got %d", tt.expectedDays, result)
			}
		})
	}
}

func TestBillingCycleIsPaidInFull(t *testing.T) {
	tests := []struct {
		name       string
		balance    decimal.Decimal
		payments   decimal.Decimal
		expected   bool
	}{
		{"not paid", decimal.NewFromInt(1000), decimal.NewFromInt(100), false},
		{"partially paid", decimal.NewFromInt(1000), decimal.NewFromInt(500), false},
		{"exactly paid", decimal.NewFromInt(1000), decimal.NewFromInt(1000), true},
		{"overpaid", decimal.NewFromInt(1000), decimal.NewFromInt(1200), true},
		{"zero balance", decimal.Zero, decimal.Zero, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycle := &models.BillingCycle{
				NewBalance:   tt.balance,
				PaymentsMade: tt.payments,
			}

			result := cycle.IsPaidInFull()
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCalculateAverageDailyBalance(t *testing.T) {
	tests := []struct {
		name        string
		records     []models.DailyBalanceRecord
		expectedADB decimal.Decimal
	}{
		{
			name:        "empty records",
			records:     []models.DailyBalanceRecord{},
			expectedADB: decimal.Zero,
		},
		{
			name: "constant balance",
			records: []models.DailyBalanceRecord{
				{Balance: decimal.NewFromInt(1000)},
				{Balance: decimal.NewFromInt(1000)},
				{Balance: decimal.NewFromInt(1000)},
			},
			expectedADB: decimal.NewFromInt(1000),
		},
		{
			name: "varying balance",
			records: []models.DailyBalanceRecord{
				{Balance: decimal.NewFromInt(0)},
				{Balance: decimal.NewFromInt(500)},
				{Balance: decimal.NewFromInt(1000)},
			},
			expectedADB: decimal.NewFromInt(500), // (0 + 500 + 1000) / 3 = 500
		},
		{
			name: "declining balance",
			records: []models.DailyBalanceRecord{
				{Balance: decimal.NewFromInt(1000)},
				{Balance: decimal.NewFromInt(800)},
				{Balance: decimal.NewFromInt(600)},
				{Balance: decimal.NewFromInt(400)},
			},
			expectedADB: decimal.NewFromInt(700), // (1000 + 800 + 600 + 400) / 4 = 700
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := models.CalculateAverageDailyBalance(tt.records)
			if !result.Equal(tt.expectedADB) {
				t.Errorf("Expected ADB %s, got %s", tt.expectedADB, result)
			}
		})
	}
}

func TestBillingCycleToSummary(t *testing.T) {
	cycle := &models.BillingCycle{
		ID:             uuid.New(),
		CycleNumber:    5,
		CycleType:      models.BillingCycleMonthly,
		CycleStartDate: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		CycleEndDate:   time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC),
		DueDate:        time.Date(2024, 7, 25, 0, 0, 0, 0, time.UTC),
		NewBalance:     decimal.NewFromInt(1000),
		MinimumPayment: decimal.NewFromInt(50),
		PaymentsMade:   decimal.NewFromInt(200),
		Status:         models.BillingCycleStatusClosed,
	}

	// Test before due date
	currentDate := time.Date(2024, 7, 10, 0, 0, 0, 0, time.UTC)
	summary := cycle.ToSummary(currentDate)

	if summary.CycleNumber != 5 {
		t.Errorf("Expected cycle number 5, got %d", summary.CycleNumber)
	}
	if !summary.StatementBalance.Equal(decimal.NewFromInt(1000)) {
		t.Errorf("Expected statement balance 1000, got %s", summary.StatementBalance)
	}
	if !summary.RemainingBalance.Equal(decimal.NewFromInt(800)) {
		t.Errorf("Expected remaining balance 800, got %s", summary.RemainingBalance)
	}
	if summary.DaysUntilDue != 15 {
		t.Errorf("Expected 15 days until due, got %d", summary.DaysUntilDue)
	}
	if summary.DaysOverdue != 0 {
		t.Errorf("Expected 0 days overdue, got %d", summary.DaysOverdue)
	}
}

func TestBillingCycleBuilder(t *testing.T) {
	card := &models.CreditCard{
		ID:               uuid.New(),
		TenantID:         uuid.New(),
		BillingCycleType: models.BillingCycleMonthly,
	}

	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC)
	due := time.Date(2024, 7, 25, 0, 0, 0, 0, time.UTC)
	graceEnd := time.Date(2024, 7, 21, 0, 0, 0, 0, time.UTC)
	prevBalance := decimal.NewFromInt(500)
	apr := decimal.NewFromFloat(19.99)

	cycle := models.NewBillingCycleBuilder().
		WithCreditCard(card).
		WithCycleNumber(3).
		WithDateRange(start, end, due, graceEnd).
		WithPreviousBalance(prevBalance).
		WithAPR(apr).
		Build()

	if cycle.CreditCardID != card.ID {
		t.Error("Credit card ID not set correctly")
	}
	if cycle.TenantID != card.TenantID {
		t.Error("Tenant ID not set correctly")
	}
	if cycle.CycleNumber != 3 {
		t.Errorf("Expected cycle number 3, got %d", cycle.CycleNumber)
	}
	if !cycle.CycleStartDate.Equal(start) {
		t.Error("Start date not set correctly")
	}
	if !cycle.CycleEndDate.Equal(end) {
		t.Error("End date not set correctly")
	}
	if !cycle.DueDate.Equal(due) {
		t.Error("Due date not set correctly")
	}
	if !cycle.PreviousBalance.Equal(prevBalance) {
		t.Error("Previous balance not set correctly")
	}
	if !cycle.APRApplied.Equal(apr) {
		t.Error("APR not set correctly")
	}
	if cycle.Status != models.BillingCycleStatusOpen {
		t.Errorf("Expected status 'open', got %s", cycle.Status)
	}
	if cycle.DaysInCycle != 30 {
		t.Errorf("Expected 30 days in cycle, got %d", cycle.DaysInCycle)
	}
}
