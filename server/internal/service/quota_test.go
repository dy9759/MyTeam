package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// quotaTestPool opens a pool against DATABASE_URL for raw SQL — tests need
// to set max/current quota fields and backdate current_month, neither of
// which sqlc exposes (by design: those are admin-only operations).
func quotaTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping DB-backed test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("quota test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// quotaTestSetup creates a fresh workspace and returns the QuotaService
// bound to it.
func quotaTestSetup(t *testing.T) (*db.Queries, pgtype.UUID, uuid.UUID, *QuotaService) {
	t.Helper()
	q := testDB(t)
	wsID := createTestWorkspace(t, q)
	wsUUID, err := uuid.FromBytes(wsID.Bytes[:])
	if err != nil {
		t.Fatalf("uuid.FromBytes: %v", err)
	}
	return q, wsID, wsUUID, NewQuotaService(q)
}

// setQuota writes max/current values for a workspace, lazily creating the
// row first. Use it to put the quota row into the desired test state.
func setQuota(t *testing.T, q *db.Queries, wsID pgtype.UUID, maxUSD, currentUSD float64, maxConcurrent int32) {
	t.Helper()
	ctx := context.Background()

	// Init the row.
	if _, err := q.GetOrInitWorkspaceQuota(ctx, wsID); err != nil {
		t.Fatalf("init quota: %v", err)
	}

	pool := quotaTestPool(t)
	if _, err := pool.Exec(ctx, `
		UPDATE workspace_quota
		SET max_monthly_usd = $2,
		    current_monthly_usd = $3,
		    max_concurrent_cloud_exec = $4,
		    updated_at = now()
		WHERE workspace_id = $1
	`, wsID, fmt.Sprintf("%.4f", maxUSD), fmt.Sprintf("%.4f", currentUSD), maxConcurrent); err != nil {
		t.Fatalf("update quota: %v", err)
	}
}

// setQuotaCurrentMonth backdates the quota's current_month so the lazy
// reset path inside CheckBeforeClaim is exercised.
func setQuotaCurrentMonth(t *testing.T, wsID pgtype.UUID, when time.Time) {
	t.Helper()
	pool := quotaTestPool(t)
	if _, err := pool.Exec(context.Background(), `
		UPDATE workspace_quota
		SET current_month = $2
		WHERE workspace_id = $1
	`, wsID, when); err != nil {
		t.Fatalf("backdate current_month: %v", err)
	}
}

func TestQuotaService_CheckBeforeClaim_AllowsWithinLimits(t *testing.T) {
	_, wsID, wsUUID, svc := quotaTestSetup(t)
	setQuota(t, svc.Q, wsID, 100.0, 25.0, 10)

	if err := svc.CheckBeforeClaim(context.Background(), wsUUID, 3); err != nil {
		t.Fatalf("expected nil (within budget + concurrent limit), got %v", err)
	}
}

func TestQuotaService_CheckBeforeClaim_ReturnsQuotaExceeded(t *testing.T) {
	_, wsID, wsUUID, svc := quotaTestSetup(t)
	setQuota(t, svc.Q, wsID, 50.0, 50.0, 10) // current >= max

	err := svc.CheckBeforeClaim(context.Background(), wsUUID, 0)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
}

func TestQuotaService_CheckBeforeClaim_ReturnsConcurrentLimit(t *testing.T) {
	_, wsID, wsUUID, svc := quotaTestSetup(t)
	setQuota(t, svc.Q, wsID, 100.0, 0.0, 5)

	// inflight exactly equals max → concurrent limit hits.
	err := svc.CheckBeforeClaim(context.Background(), wsUUID, 5)
	if !errors.Is(err, ErrQuotaConcurrentLimit) {
		t.Fatalf("expected ErrQuotaConcurrentLimit, got %v", err)
	}
}

func TestQuotaService_RecordCost_IncrementsMonthlyTotal(t *testing.T) {
	q, wsID, wsUUID, svc := quotaTestSetup(t)
	setQuota(t, svc.Q, wsID, 100.0, 10.0, 10)

	svc.RecordCost(context.Background(), wsUUID, 2.5)

	row, err := q.GetWorkspaceQuota(context.Background(), wsID)
	if err != nil {
		t.Fatalf("get quota: %v", err)
	}
	got, err := numericToFloat64(row.CurrentMonthlyUsd)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	const want = 12.5
	if diff := got - want; diff > 0.0001 || diff < -0.0001 {
		t.Fatalf("current_monthly_usd: got %v, want %v", got, want)
	}
}

func TestQuotaService_CheckBeforeClaim_LazyMonthlyReset(t *testing.T) {
	q, wsID, wsUUID, svc := quotaTestSetup(t)
	// Workspace would normally trip ErrQuotaExceeded on these numbers.
	setQuota(t, svc.Q, wsID, 100.0, 99.99, 10)

	// Backdate current_month so the lazy reset fires inside CheckBeforeClaim.
	setQuotaCurrentMonth(t, wsID, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))

	if err := svc.CheckBeforeClaim(context.Background(), wsUUID, 0); err != nil {
		t.Fatalf("expected nil after lazy reset, got %v", err)
	}

	// Confirm the row was zeroed and current_month bumped to this month.
	row, err := q.GetWorkspaceQuota(context.Background(), wsID)
	if err != nil {
		t.Fatalf("get quota: %v", err)
	}
	if got, _ := numericToFloat64(row.CurrentMonthlyUsd); got != 0 {
		t.Fatalf("expected current_monthly_usd reset to 0, got %v", got)
	}
	thisMonth := time.Now().UTC()
	rowMonth := row.CurrentMonth.Time
	if rowMonth.Year() != thisMonth.Year() || rowMonth.Month() != thisMonth.Month() {
		t.Fatalf("expected current_month bumped to %s, got %s", thisMonth.Format("2006-01"), rowMonth.Format("2006-01"))
	}
}
