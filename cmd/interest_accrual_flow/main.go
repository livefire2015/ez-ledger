package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/livefire2015/ez-ledger/src/models"
	"github.com/livefire2015/ez-ledger/src/services"
	"github.com/shopspring/decimal"
)

func main() {
	// Connect to database
	db, err := sql.Open("postgres", "postgres://localhost/ezledger?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	// Initialize services
	billingService := services.NewBillingService(db)
	reconciliationService := services.NewLedgerReconciliationService(db, models.PointsEarningRule{
		PointsPerDollar: decimal.NewFromFloat(1.0),
		MinAmount:       decimal.Zero,
	})
	// cardService removed as it was unused

	// Create a test tenant
	tenantID := uuid.New()
	if err := createTenant(ctx, db, tenantID); err != nil {
		log.Fatal(err)
	}

	// Create a credit card
	cardID := uuid.New()
	card := &models.CreditCard{
		ID:               cardID,
		TenantID:         tenantID,
		CardholderName:   "Interest Demo User",
		Status:           "active",
		CreditLimit:      decimal.NewFromFloat(5000.00),
		PurchaseAPR:      decimal.NewFromFloat(0.24), // 24% APR
		BillingCycleType: models.BillingCycleMonthly,
		PaymentDueDays:   20,
		GracePeriodDays:  25,
		CreatedAt:        time.Now().AddDate(0, -4, 0), // Created 4 months ago
		UpdatedAt:        time.Now(),
	}

	if err := createCreditCard(ctx, db, card); err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== EZ Ledger - Interest Accrual Flow Example ===\n")

	// --- Cycle 1: Open -> Closed -> Paid Full ---
	fmt.Println("--- Cycle 1: Paid In Full ---")

	// Start Cycle 1 (e.g., 3 months ago)
	cycle1Start := time.Now().AddDate(0, -3, 0)
	// Manually set dates for simulation
	card.LastStatementDate = &cycle1Start

	cycle1, err := billingService.StartNewBillingCycle(ctx, card)
	if err != nil {
		log.Fatal(fmt.Errorf("start cycle 1: %w", err))
	}
	fmt.Printf("Cycle 1 Started: %s to %s\n", cycle1.CycleStartDate.Format("2006-01-02"), cycle1.CycleEndDate.Format("2006-01-02"))

	// Transaction in Cycle 1
	txnAmount1 := 1000.00
	_, _, err = reconciliationService.RecordTransaction(ctx, services.TransactionRequest{
		TenantID:        tenantID,
		Amount:          decimal.NewFromFloat(txnAmount1),
		Description:     "Cycle 1 Purchase",
		ReferenceID:     fmt.Sprintf("txn-c1-%s", uuid.New().String()[:8]),
		TransactionDate: cycle1.CycleStartDate.AddDate(0, 0, 5),
		PostingDate:     cycle1.CycleStartDate.AddDate(0, 0, 6),
		EarnPoints:      false,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Recorded Transaction: $%.2f\n", txnAmount1)

	// Close Cycle 1
	stmt1, err := billingService.GenerateStatement(ctx, services.GenerateStatementRequest{
		CreditCard: card,
		CycleEnd:   cycle1.CycleEndDate,
	})
	if err != nil {
		log.Fatal(fmt.Errorf("generate statement 1: %w", err))
	}
	fmt.Printf("Cycle 1 Closed. New Balance: $%.2f. Due Date: %s\n", stmt1.BillingCycle.NewBalance, stmt1.BillingCycle.DueDate.Format("2006-01-02"))

	// Pay Full in Cycle 1 (before due date)
	paymentDate1 := stmt1.BillingCycle.DueDate.AddDate(0, 0, -5)
	err = billingService.ProcessPaymentTowardsBillingCycle(ctx, stmt1.BillingCycle.ID, stmt1.BillingCycle.NewBalance, paymentDate1)
	if err != nil {
		log.Fatal(err)
	}

	// Check status
	history1, _ := billingService.GetBillingHistory(ctx, cardID, 1)
	fmt.Printf("Cycle 1 Status: %s (Expected: paid_full)\n\n", history1[0].Status)

	// --- Cycle 2: Open -> Closed -> Paid Minimum ---
	fmt.Println("--- Cycle 2: Paid Minimum ---")

	// Start Cycle 2
	cycle2, err := billingService.StartNewBillingCycle(ctx, card)
	if err != nil {
		log.Fatal(fmt.Errorf("start cycle 2: %w", err))
	}
	fmt.Printf("Cycle 2 Started: %s to %s\n", cycle2.CycleStartDate.Format("2006-01-02"), cycle2.CycleEndDate.Format("2006-01-02"))

	// Transaction in Cycle 2
	txnAmount2 := 500.00
	_, _, err = reconciliationService.RecordTransaction(ctx, services.TransactionRequest{
		TenantID:        tenantID,
		Amount:          decimal.NewFromFloat(txnAmount2),
		Description:     "Cycle 2 Purchase",
		ReferenceID:     fmt.Sprintf("txn-c2-%s", uuid.New().String()[:8]),
		TransactionDate: cycle2.CycleStartDate.AddDate(0, 0, 5),
		PostingDate:     cycle2.CycleStartDate.AddDate(0, 0, 6),
		EarnPoints:      false,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Recorded Transaction: $%.2f\n", txnAmount2)

	// Close Cycle 2
	stmt2, err := billingService.GenerateStatement(ctx, services.GenerateStatementRequest{
		CreditCard: card,
		CycleEnd:   cycle2.CycleEndDate,
	})
	if err != nil {
		log.Fatal(fmt.Errorf("generate statement 2: %w", err))
	}

	// Check Interest for Cycle 2 (Should be 0 because Cycle 1 was paid in full)
	fmt.Printf("Cycle 2 Interest: $%.2f (Expected: 0.00)\n", stmt2.BillingCycle.InterestAmount)
	fmt.Printf("Cycle 2 Closed. New Balance: $%.2f. Min Payment: $%.2f\n", stmt2.BillingCycle.NewBalance, stmt2.BillingCycle.MinimumPayment)

	// Pay Minimum in Cycle 2
	paymentDate2 := stmt2.BillingCycle.DueDate.AddDate(0, 0, -2)
	err = billingService.ProcessPaymentTowardsBillingCycle(ctx, stmt2.BillingCycle.ID, stmt2.BillingCycle.MinimumPayment, paymentDate2)
	if err != nil {
		log.Fatal(err)
	}

	history2, _ := billingService.GetBillingHistory(ctx, cardID, 1)
	fmt.Printf("Cycle 2 Status: %s (Expected: paid)\n\n", history2[0].Status)

	// --- Cycle 3: Open -> Closed -> Overdue ---
	fmt.Println("--- Cycle 3: Overdue ---")

	// Start Cycle 3
	cycle3, err := billingService.StartNewBillingCycle(ctx, card)
	if err != nil {
		log.Fatal(fmt.Errorf("start cycle 3: %w", err))
	}
	fmt.Printf("Cycle 3 Started: %s to %s\n", cycle3.CycleStartDate.Format("2006-01-02"), cycle3.CycleEndDate.Format("2006-01-02"))

	// Transaction in Cycle 3
	txnAmount3 := 200.00
	_, _, err = reconciliationService.RecordTransaction(ctx, services.TransactionRequest{
		TenantID:        tenantID,
		Amount:          decimal.NewFromFloat(txnAmount3),
		Description:     "Cycle 3 Purchase",
		ReferenceID:     fmt.Sprintf("txn-c3-%s", uuid.New().String()[:8]),
		TransactionDate: cycle3.CycleStartDate.AddDate(0, 0, 5),
		PostingDate:     cycle3.CycleStartDate.AddDate(0, 0, 6),
		EarnPoints:      false,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Recorded Transaction: $%.2f\n", txnAmount3)

	// Close Cycle 3
	stmt3, err := billingService.GenerateStatement(ctx, services.GenerateStatementRequest{
		CreditCard: card,
		CycleEnd:   cycle3.CycleEndDate,
	})
	if err != nil {
		log.Fatal(fmt.Errorf("generate statement 3: %w", err))
	}

	// Check Interest for Cycle 3 (Should be > 0 because Cycle 2 was NOT paid in full)
	fmt.Printf("Cycle 3 Interest: $%.2f (Expected: > 0.00)\n", stmt3.BillingCycle.InterestAmount)
	fmt.Printf("Cycle 3 Closed. New Balance: $%.2f. Due Date: %s\n", stmt3.BillingCycle.NewBalance, stmt3.BillingCycle.DueDate.Format("2006-01-02"))

	// Simulate passing of due date without payment
	// We need to manually update the due date in DB to be in the past to test "CheckAndAssessLatePaymentFees"

	// First, update the due date of Cycle 3 to be yesterday
	pastDueDate := time.Now().AddDate(0, 0, -1)
	_, err = db.ExecContext(ctx, "UPDATE billing_cycles SET due_date = $1 WHERE id = $2", pastDueDate, stmt3.BillingCycle.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Simulated time passing: Due date is now in the past.")

	// Run Late Fee Assessment
	results, err := billingService.CheckAndAssessLatePaymentFees(ctx)
	if err != nil {
		log.Fatal(err)
	}

	if len(results) > 0 {
		fmt.Printf("Late Fees Assessed: %d\n", len(results))
		fmt.Printf("Fee Amount: $%.2f\n", results[0].FeeAmount)
	} else {
		fmt.Println("No late fees assessed (Unexpected if logic is correct)")
	}

	// Check final status
	history3, _ := billingService.GetBillingHistory(ctx, cardID, 1)
	fmt.Printf("Cycle 3 Status: %s (Expected: past_due)\n", history3[0].Status)

	fmt.Println("\n=== Example Complete ===")
}

// Helper to create tenant
func createTenant(ctx context.Context, db *sql.DB, tenantID uuid.UUID) error {
	query := `
		INSERT INTO tenants (id, tenant_code, name, email, status, minimum_payment_percentage)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := db.ExecContext(ctx, query,
		tenantID,
		fmt.Sprintf("TENANT-%s", tenantID.String()[:8]),
		"Interest Demo Tenant",
		"demo@example.com",
		"active",
		0.05,
	)
	return err
}

// Helper to create credit card
func createCreditCard(ctx context.Context, db *sql.DB, card *models.CreditCard) error {
	query := `
		INSERT INTO credit_cards (
			id, tenant_id, cardholder_name, status, credit_limit, purchase_apr,
			billing_cycle_type, payment_due_days, grace_period_days,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := db.ExecContext(ctx, query,
		card.ID, card.TenantID, card.CardholderName, card.Status, card.CreditLimit, card.PurchaseAPR,
		card.BillingCycleType, card.PaymentDueDays, card.GracePeriodDays,
		card.CreatedAt, card.UpdatedAt,
	)
	return err
}
