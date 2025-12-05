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

// This example demonstrates a complete flow through both ledgers:
// 1. Create a tenant
// 2. Record transactions (earning points)
// 3. Record payment (no points impact)
// 4. Redeem points for statement credit
// 5. Generate reconciliation report

func main() {
	// Connect to database
	db, err := sql.Open("postgres", "postgres://localhost/ezledger?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	// Initialize services
	pointsRule := models.PointsEarningRule{
		PointsPerDollar: decimal.NewFromFloat(0.01), // 1 point per dollar
		MinAmount:       decimal.NewFromFloat(1.00),  // Min $1 to earn points
		MaxPoints:       nil,                         // No cap
	}

	reconciliationService := services.NewLedgerReconciliationService(db, pointsRule)

	// Create a test tenant
	tenantID := uuid.New()
	if err := createTenant(ctx, db, tenantID); err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== EZ Ledger - Complete Flow Example ===\n")

	// Step 1: Record multiple transactions
	fmt.Println("Step 1: Recording Transactions")
	fmt.Println("--------------------------------")

	transactions := []struct {
		amount      float64
		description string
	}{
		{150.00, "Purchase at Amazon"},
		{75.50, "Purchase at Walmart"},
		{220.00, "Purchase at Best Buy"},
	}

	for _, txn := range transactions {
		stmtEntry, ptsEntry, err := reconciliationService.RecordTransaction(ctx, services.TransactionRequest{
			TenantID:        tenantID,
			Amount:          decimal.NewFromFloat(txn.amount),
			Description:     txn.description,
			ReferenceID:     fmt.Sprintf("txn-%s", uuid.New().String()[:8]),
			TransactionDate: time.Now(),
			PostingDate:     time.Now(),
			EarnPoints:      true,
		})

		if err != nil {
			log.Fatal(err)
		}

		// Clear the entries (mark as processed)
		if err := clearEntry(ctx, db, stmtEntry.ID); err != nil {
			log.Fatal(err)
		}

		pointsEarned := 0
		if ptsEntry != nil {
			pointsEarned = ptsEntry.Points
		}

		fmt.Printf("  ✓ Transaction: $%.2f → Statement: +$%.2f, Points: +%d\n",
			txn.amount, txn.amount, pointsEarned)
	}

	// Show balances after transactions
	report, _ := reconciliationService.GenerateReconciliationReport(ctx, tenantID)
	fmt.Printf("\n  Balances after transactions:\n")
	fmt.Printf("    Statement Balance: $%.2f\n", report.StatementBalance)
	fmt.Printf("    Points Balance: %d points\n\n", report.PointsBalance)

	// Step 2: Record a payment
	fmt.Println("Step 2: Recording Payment")
	fmt.Println("-------------------------")

	paymentAmount := 200.00
	stmtEntry, err := reconciliationService.RecordPayment(ctx, services.PaymentRequest{
		TenantID:    tenantID,
		Amount:      decimal.NewFromFloat(paymentAmount),
		Description: "Payment received - Credit Card",
		ReferenceID: fmt.Sprintf("pay-%s", uuid.New().String()[:8]),
		PaymentDate: time.Now(),
		PostingDate: time.Now(),
	})

	if err != nil {
		log.Fatal(err)
	}

	// Clear the payment
	if err := clearEntry(ctx, db, stmtEntry.ID); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("  ✓ Payment: $%.2f → Statement: -$%.2f, Points: NO CHANGE\n\n", paymentAmount, paymentAmount)

	// Show balances after payment
	report, _ = reconciliationService.GenerateReconciliationReport(ctx, tenantID)
	fmt.Printf("  Balances after payment:\n")
	fmt.Printf("    Statement Balance: $%.2f (reduced by payment)\n", report.StatementBalance)
	fmt.Printf("    Points Balance: %d points (UNCHANGED - payments don't affect points!)\n\n", report.PointsBalance)

	// Step 3: Record a refund
	fmt.Println("Step 3: Recording Refund")
	fmt.Println("------------------------")

	refundAmount := 75.50
	stmtEntry, ptsEntry, err := reconciliationService.RecordRefund(ctx, services.RefundRequest{
		TenantID:              tenantID,
		Amount:                decimal.NewFromFloat(refundAmount),
		Description:           "Refund - Returned item",
		ReferenceID:           fmt.Sprintf("ref-%s", uuid.New().String()[:8]),
		OriginalTransactionID: uuid.New(), // In real app, use actual transaction ID
		RefundDate:            time.Now(),
		PostingDate:           time.Now(),
		AdjustPoints:          true,
	})

	if err != nil {
		log.Fatal(err)
	}

	// Clear the refund
	if err := clearEntry(ctx, db, stmtEntry.ID); err != nil {
		log.Fatal(err)
	}

	pointsAdjusted := 0
	if ptsEntry != nil {
		pointsAdjusted = ptsEntry.Points
	}

	fmt.Printf("  ✓ Refund: $%.2f → Statement: -$%.2f, Points: %d (deducted)\n\n", refundAmount, refundAmount, pointsAdjusted)

	// Show balances after refund
	report, _ = reconciliationService.GenerateReconciliationReport(ctx, tenantID)
	fmt.Printf("  Balances after refund:\n")
	fmt.Printf("    Statement Balance: $%.2f\n", report.StatementBalance)
	fmt.Printf("    Points Balance: %d points\n\n", report.PointsBalance)

	// Step 4: Redeem points for statement credit
	fmt.Println("Step 4: Redeeming Points")
	fmt.Println("------------------------")

	pointsToRedeem := 300
	creditAmount := 3.00 // 100 points = $1

	stmtEntry, ptsEntry, err = reconciliationService.RecordRewardRedemption(ctx, services.RewardRedemptionRequest{
		TenantID:            tenantID,
		PointsToRedeem:      pointsToRedeem,
		CreditAmount:        decimal.NewFromFloat(creditAmount),
		Description:         "Reward redemption",
		ExternalPlatform:    "keystone",
		ExternalReferenceID: fmt.Sprintf("redeem-%s", uuid.New().String()[:8]),
		RedemptionDate:      time.Now(),
		PostingDate:         time.Now(),
	})

	if err != nil {
		log.Fatal(err)
	}

	// Clear the reward entry
	if err := clearEntry(ctx, db, stmtEntry.ID); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("  ✓ Redemption: %d points → Statement: -$%.2f credit, Points: -%d\n\n",
		pointsToRedeem, creditAmount, pointsToRedeem)

	// Step 5: Final reconciliation report
	fmt.Println("Step 5: Final Reconciliation Report")
	fmt.Println("====================================")

	report, err = reconciliationService.GenerateReconciliationReport(ctx, tenantID)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nTenant: %s\n", tenantID)
	fmt.Printf("Report Generated: %s\n\n", report.ReportGeneratedAt.Format(time.RFC1123))

	fmt.Printf("STATEMENT LEDGER:\n")
	fmt.Printf("  Current Balance: $%.2f\n", report.StatementBalance)
	fmt.Printf("  Last Activity: %s\n\n", report.LastStatementActivity.Format(time.RFC1123))

	fmt.Printf("POINTS LEDGER:\n")
	fmt.Printf("  Available Points: %d points\n", report.PointsBalance)
	fmt.Printf("  Last Activity: %s\n\n", report.LastPointsActivity.Format(time.RFC1123))

	fmt.Println("Summary of Activity:")
	fmt.Println("  • Transactions: $445.50 → Earned 445 points")
	fmt.Println("  • Payments: $200.00 → No points impact")
	fmt.Println("  • Refunds: $75.50 → Deducted ~75 points")
	fmt.Println("  • Redemptions: 300 points → $3.00 credit")
	fmt.Printf("\n  Final Statement Balance: $%.2f\n", report.StatementBalance)
	fmt.Printf("  Final Points Balance: %d points\n\n", report.PointsBalance)

	fmt.Println("=== Example Complete ===")
}

// Helper function to create a tenant
func createTenant(ctx context.Context, db *sql.DB, tenantID uuid.UUID) error {
	query := `
		INSERT INTO tenants (id, tenant_code, name, email, status, minimum_payment_percentage)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := db.ExecContext(ctx, query,
		tenantID,
		fmt.Sprintf("TENANT-%s", tenantID.String()[:8]),
		"Example Tenant",
		"example@example.com",
		"active",
		0.05, // 5% minimum payment
	)

	return err
}

// Helper function to clear (mark as processed) a statement entry
func clearEntry(ctx context.Context, db *sql.DB, entryID uuid.UUID) error {
	query := `
		UPDATE statement_ledger_entries
		SET status = 'cleared', cleared_at = NOW()
		WHERE id = $1
	`

	// Note: This bypasses the immutability trigger for demonstration purposes
	// In production, you'd handle clearing through proper service methods
	_, err := db.ExecContext(ctx, query, entryID)
	return err
}
