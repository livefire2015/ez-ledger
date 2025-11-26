package unit

import (
	"testing"
	"time"

	"github.com/livefire2015/ez-ledger/src/models"
	"github.com/shopspring/decimal"
)

func TestCreditCardDefaults(t *testing.T) {
	card := models.CreditCardDefaults()

	// Verify APR defaults
	if !card.PurchaseAPR.Equal(decimal.NewFromFloat(19.99)) {
		t.Errorf("Expected purchase APR 19.99, got %s", card.PurchaseAPR)
	}
	if !card.CashAdvanceAPR.Equal(decimal.NewFromFloat(24.99)) {
		t.Errorf("Expected cash advance APR 24.99, got %s", card.CashAdvanceAPR)
	}
	if !card.PenaltyAPR.Equal(decimal.NewFromFloat(29.99)) {
		t.Errorf("Expected penalty APR 29.99, got %s", card.PenaltyAPR)
	}

	// Verify fee defaults
	if !card.LatePaymentFee.Equal(decimal.NewFromInt(35)) {
		t.Errorf("Expected late payment fee 35, got %s", card.LatePaymentFee)
	}
	if !card.FailedPaymentFee.Equal(decimal.NewFromInt(35)) {
		t.Errorf("Expected failed payment fee 35, got %s", card.FailedPaymentFee)
	}
	if !card.InternationalFeeRate.Equal(decimal.NewFromFloat(3.0)) {
		t.Errorf("Expected international fee rate 3.0, got %s", card.InternationalFeeRate)
	}

	// Verify billing defaults
	if card.BillingCycleType != models.BillingCycleMonthly {
		t.Errorf("Expected monthly billing cycle, got %s", card.BillingCycleType)
	}
	if card.BillingCycleDay != 1 {
		t.Errorf("Expected billing cycle day 1, got %d", card.BillingCycleDay)
	}
	if card.PaymentDueDays != 25 {
		t.Errorf("Expected payment due days 25, got %d", card.PaymentDueDays)
	}
	if card.GracePeriodDays != 21 {
		t.Errorf("Expected grace period days 21, got %d", card.GracePeriodDays)
	}

	// Verify cashback defaults
	if !card.CashbackEnabled {
		t.Error("Expected cashback to be enabled by default")
	}
	if !card.CashbackRate.Equal(decimal.NewFromFloat(1.5)) {
		t.Errorf("Expected cashback rate 1.5, got %s", card.CashbackRate)
	}
}

func TestCreditCardValidation(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(*models.CreditCard)
		expectError error
	}{
		{
			name: "valid card",
			modify: func(c *models.CreditCard) {
				c.CreditLimit = decimal.NewFromInt(5000)
			},
			expectError: nil,
		},
		{
			name: "invalid credit limit - zero",
			modify: func(c *models.CreditCard) {
				c.CreditLimit = decimal.Zero
			},
			expectError: models.ErrInvalidCreditLimit,
		},
		{
			name: "invalid credit limit - negative",
			modify: func(c *models.CreditCard) {
				c.CreditLimit = decimal.NewFromInt(-1000)
			},
			expectError: models.ErrInvalidCreditLimit,
		},
		{
			name: "invalid APR - over 100",
			modify: func(c *models.CreditCard) {
				c.CreditLimit = decimal.NewFromInt(5000)
				c.PurchaseAPR = decimal.NewFromInt(150)
			},
			expectError: models.ErrInvalidAPR,
		},
		{
			name: "invalid APR - negative",
			modify: func(c *models.CreditCard) {
				c.CreditLimit = decimal.NewFromInt(5000)
				c.PurchaseAPR = decimal.NewFromInt(-5)
			},
			expectError: models.ErrInvalidAPR,
		},
		{
			name: "invalid billing cycle day - zero",
			modify: func(c *models.CreditCard) {
				c.CreditLimit = decimal.NewFromInt(5000)
				c.BillingCycleDay = 0
			},
			expectError: models.ErrInvalidBillingCycleDay,
		},
		{
			name: "invalid billing cycle day - 29",
			modify: func(c *models.CreditCard) {
				c.CreditLimit = decimal.NewFromInt(5000)
				c.BillingCycleDay = 29
			},
			expectError: models.ErrInvalidBillingCycleDay,
		},
		{
			name: "invalid minimum payment percent - over 100",
			modify: func(c *models.CreditCard) {
				c.CreditLimit = decimal.NewFromInt(5000)
				c.MinimumPaymentPercent = decimal.NewFromInt(150)
			},
			expectError: models.ErrInvalidMinimumPayment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := models.CreditCardDefaults()
			tt.modify(&card)

			err := card.Validate()
			if err != tt.expectError {
				t.Errorf("Expected error %v, got %v", tt.expectError, err)
			}
		})
	}
}

func TestCreditCardCanTransact(t *testing.T) {
	tests := []struct {
		name        string
		status      models.CreditCardStatus
		expectError error
	}{
		{"active card", models.CreditCardStatusActive, nil},
		{"frozen card", models.CreditCardStatusFrozen, models.ErrCardFrozen},
		{"closed card", models.CreditCardStatusClosed, models.ErrCardClosed},
		{"delinquent card", models.CreditCardStatusDelinquent, nil}, // Can still transact, just at penalty APR
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := models.CreditCardDefaults()
			card.Status = tt.status

			err := card.CanTransact()
			if err != tt.expectError {
				t.Errorf("Expected error %v, got %v", tt.expectError, err)
			}
		})
	}
}

func TestCreditCardHasAvailableCredit(t *testing.T) {
	card := models.CreditCardDefaults()
	card.CreditLimit = decimal.NewFromInt(5000)
	card.AvailableCredit = decimal.NewFromInt(2000)

	tests := []struct {
		name        string
		amount      decimal.Decimal
		expectError error
	}{
		{"within limit", decimal.NewFromInt(1000), nil},
		{"exact limit", decimal.NewFromInt(2000), nil},
		{"over limit", decimal.NewFromInt(3000), models.ErrInsufficientCredit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := card.HasAvailableCredit(tt.amount)
			if err != tt.expectError {
				t.Errorf("Expected error %v, got %v", tt.expectError, err)
			}
		})
	}
}

func TestCreditCardGetEffectiveAPR(t *testing.T) {
	now := time.Now()
	futureDate := now.AddDate(0, 1, 0)
	pastDate := now.AddDate(0, -1, 0)

	tests := []struct {
		name           string
		status         models.CreditCardStatus
		introEndDate   *time.Time
		introAPR       decimal.Decimal
		purchaseAPR    decimal.Decimal
		penaltyAPR     decimal.Decimal
		expectedAPR    decimal.Decimal
	}{
		{
			name:        "normal - purchase APR",
			status:      models.CreditCardStatusActive,
			purchaseAPR: decimal.NewFromFloat(19.99),
			penaltyAPR:  decimal.NewFromFloat(29.99),
			expectedAPR: decimal.NewFromFloat(19.99),
		},
		{
			name:        "delinquent - penalty APR",
			status:      models.CreditCardStatusDelinquent,
			purchaseAPR: decimal.NewFromFloat(19.99),
			penaltyAPR:  decimal.NewFromFloat(29.99),
			expectedAPR: decimal.NewFromFloat(29.99),
		},
		{
			name:         "active intro - intro APR",
			status:       models.CreditCardStatusActive,
			introEndDate: &futureDate,
			introAPR:     decimal.NewFromFloat(0.00),
			purchaseAPR:  decimal.NewFromFloat(19.99),
			expectedAPR:  decimal.NewFromFloat(0.00),
		},
		{
			name:         "expired intro - purchase APR",
			status:       models.CreditCardStatusActive,
			introEndDate: &pastDate,
			introAPR:     decimal.NewFromFloat(0.00),
			purchaseAPR:  decimal.NewFromFloat(19.99),
			expectedAPR:  decimal.NewFromFloat(19.99),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := models.CreditCardDefaults()
			card.Status = tt.status
			card.IntroductoryEndDate = tt.introEndDate
			card.IntroductoryAPR = tt.introAPR
			card.PurchaseAPR = tt.purchaseAPR
			card.PenaltyAPR = tt.penaltyAPR

			apr := card.GetEffectiveAPR(now)
			if !apr.Equal(tt.expectedAPR) {
				t.Errorf("Expected APR %s, got %s", tt.expectedAPR, apr)
			}
		})
	}
}

func TestCreditCardGetDailyPeriodicRate(t *testing.T) {
	card := models.CreditCardDefaults()

	// 19.99% APR should give approximately 0.0547945% daily rate
	apr := decimal.NewFromFloat(19.99)
	dpr := card.GetDailyPeriodicRate(apr)

	// DPR = APR / 365 / 100 = 19.99 / 365 / 100 = 0.0005477...
	expectedDPR := decimal.NewFromFloat(0.0005477)

	// Check with some tolerance for rounding
	diff := dpr.Sub(expectedDPR).Abs()
	tolerance := decimal.NewFromFloat(0.00001)

	if diff.GreaterThan(tolerance) {
		t.Errorf("Expected DPR ~%s, got %s", expectedDPR, dpr)
	}
}

func TestCreditCardCalculateMinimumPayment(t *testing.T) {
	card := models.CreditCardDefaults()
	card.MinimumPaymentPercent = decimal.NewFromFloat(2.0) // 2%
	card.MinimumPaymentAmount = decimal.NewFromInt(25)

	tests := []struct {
		name            string
		balance         decimal.Decimal
		expectedMinimum decimal.Decimal
	}{
		{
			name:            "high balance - percentage applies",
			balance:         decimal.NewFromInt(5000),
			expectedMinimum: decimal.NewFromInt(100), // 5000 * 0.02 = 100
		},
		{
			name:            "low balance - minimum fixed applies",
			balance:         decimal.NewFromInt(500),
			expectedMinimum: decimal.NewFromInt(25), // 500 * 0.02 = 10, but min is 25
		},
		{
			name:            "very low balance - full balance",
			balance:         decimal.NewFromInt(15),
			expectedMinimum: decimal.NewFromInt(15), // Balance less than min fixed
		},
		{
			name:            "zero balance",
			balance:         decimal.Zero,
			expectedMinimum: decimal.Zero,
		},
		{
			name:            "negative balance (overpayment)",
			balance:         decimal.NewFromInt(-100),
			expectedMinimum: decimal.Zero,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minimum := card.CalculateMinimumPayment(tt.balance)
			if !minimum.Equal(tt.expectedMinimum) {
				t.Errorf("Expected minimum %s, got %s", tt.expectedMinimum, minimum)
			}
		})
	}
}

func TestCreditCardCalculateInternationalFee(t *testing.T) {
	card := models.CreditCardDefaults()
	card.InternationalFeeRate = decimal.NewFromFloat(3.0) // 3%

	tests := []struct {
		name       string
		amount     decimal.Decimal
		expectedFee decimal.Decimal
	}{
		{"$100 transaction", decimal.NewFromInt(100), decimal.NewFromInt(3)},
		{"$500 transaction", decimal.NewFromInt(500), decimal.NewFromInt(15)},
		{"$1234.56 transaction", decimal.NewFromFloat(1234.56), decimal.NewFromFloat(37.0368)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fee := card.CalculateInternationalFee(tt.amount)
			// Round for comparison
			fee = fee.Round(2)
			expected := tt.expectedFee.Round(2)
			if !fee.Equal(expected) {
				t.Errorf("Expected fee %s, got %s", expected, fee)
			}
		})
	}
}

func TestCreditCardCalculateCashAdvanceFee(t *testing.T) {
	card := models.CreditCardDefaults()
	card.CashAdvanceFee = decimal.NewFromInt(10)        // $10 flat minimum
	card.CashAdvanceFeeRate = decimal.NewFromFloat(5.0) // 5%

	tests := []struct {
		name        string
		amount      decimal.Decimal
		expectedFee decimal.Decimal
	}{
		{"$100 - flat fee wins", decimal.NewFromInt(100), decimal.NewFromInt(10)},   // 5% = $5, flat = $10
		{"$500 - percent wins", decimal.NewFromInt(500), decimal.NewFromInt(25)},    // 5% = $25, flat = $10
		{"$200 - exactly equal", decimal.NewFromInt(200), decimal.NewFromInt(10)},   // 5% = $10, flat = $10
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fee := card.CalculateCashAdvanceFee(tt.amount)
			if !fee.Equal(tt.expectedFee) {
				t.Errorf("Expected fee %s, got %s", tt.expectedFee, fee)
			}
		})
	}
}

func TestCreditCardGetNextBillingPeriod(t *testing.T) {
	tests := []struct {
		name          string
		cycleType     models.BillingCycleType
		cycleDay      int
		fromDate      time.Time
		expectedStart time.Time
		expectedEnd   time.Time
	}{
		{
			name:          "monthly - mid month",
			cycleType:     models.BillingCycleMonthly,
			cycleDay:      15,
			fromDate:      time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 7, 14, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "monthly - past cycle day",
			cycleType:     models.BillingCycleMonthly,
			cycleDay:      1,
			fromDate:      time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 7, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "quarterly - Q2",
			cycleType:     models.BillingCycleQuarterly,
			cycleDay:      1,
			fromDate:      time.Date(2024, 4, 15, 0, 0, 0, 0, time.UTC),
			expectedStart: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:   time.Date(2024, 9, 30, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := models.CreditCardDefaults()
			card.BillingCycleType = tt.cycleType
			card.BillingCycleDay = tt.cycleDay
			card.PaymentDueDays = 25

			start, end, _ := card.GetNextBillingPeriod(tt.fromDate)

			if !start.Equal(tt.expectedStart) {
				t.Errorf("Expected start %v, got %v", tt.expectedStart, start)
			}
			if !end.Equal(tt.expectedEnd) {
				t.Errorf("Expected end %v, got %v", tt.expectedEnd, end)
			}
		})
	}
}

func TestCreditCardIsInGracePeriod(t *testing.T) {
	card := models.CreditCardDefaults()
	card.GracePeriodDays = 21

	statementDate := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		currentDate time.Time
		expected   bool
	}{
		{"within grace period - day 1", time.Date(2024, 6, 5, 0, 0, 0, 0, time.UTC), true},
		{"within grace period - day 21", time.Date(2024, 6, 22, 0, 0, 0, 0, time.UTC), true},
		{"after grace period", time.Date(2024, 6, 25, 0, 0, 0, 0, time.UTC), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := card.IsInGracePeriod(statementDate, tt.currentDate)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
