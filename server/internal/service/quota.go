// Package service: quota.go — workspace_quota enforcement helpers per PRD §12.
//
// QuotaService gates cloud-mode task execution against a workspace's monthly
// USD ceiling and concurrent-execution cap. It also lazily resets the monthly
// counter at the start of a new calendar month (cron-based reset is post-MVP
// per PRD §12.5; lazy-on-claim is the only mechanism here).
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	// ErrQuotaExceeded indicates the workspace's monthly USD spend has met
	// or exceeded max_monthly_usd. Maps to errcode.QuotaExceeded.
	ErrQuotaExceeded = errors.New("workspace quota: monthly USD limit reached")
	// ErrQuotaConcurrentLimit indicates the workspace already has
	// max_concurrent_cloud_exec dispatched/running cloud tasks.
	// Maps to errcode.QuotaConcurrentLimit.
	ErrQuotaConcurrentLimit = errors.New("workspace quota: concurrent cloud execution limit reached")
)

// QuotaService enforces workspace_quota limits for cloud execution.
type QuotaService struct {
	Q *db.Queries
}

// NewQuotaService constructs a QuotaService bound to the given Queries.
func NewQuotaService(q *db.Queries) *QuotaService {
	return &QuotaService{Q: q}
}

// CheckBeforeClaim returns nil when the workspace can claim another cloud
// execution. Returns ErrQuotaExceeded when the monthly USD ceiling is met
// or exceeded, ErrQuotaConcurrentLimit when the concurrent cap is reached,
// or a wrapped DB error on infrastructure failure.
//
// concurrentInflight is the caller-computed count of agent_task_queue rows
// in ('dispatched','running') for cloud-mode runtimes belonging to this
// workspace. CountInflightCloudExecutions covers the standard case.
func (s *QuotaService) CheckBeforeClaim(ctx context.Context, workspaceID uuid.UUID, concurrentInflight int) error {
	if s == nil || s.Q == nil {
		return nil
	}

	// Lazy monthly reset: zeroes current_monthly_usd when current_month is in
	// the past. Best-effort; a transient DB error here should not block
	// claims, since the next claim will retry.
	if err := s.Q.ResetMonthlyQuota(ctx, toPgUUID(workspaceID)); err != nil {
		slog.Warn("quota: monthly reset failed", "ws", workspaceID.String(), "err", err)
	}

	quota, err := s.Q.GetOrInitWorkspaceQuota(ctx, toPgUUID(workspaceID))
	if err != nil {
		return fmt.Errorf("get/init workspace quota: %w", err)
	}

	currentUSD, err := numericToFloat64(quota.CurrentMonthlyUsd)
	if err != nil {
		return fmt.Errorf("decode current_monthly_usd: %w", err)
	}
	maxUSD, err := numericToFloat64(quota.MaxMonthlyUsd)
	if err != nil {
		return fmt.Errorf("decode max_monthly_usd: %w", err)
	}

	if currentUSD >= maxUSD {
		return ErrQuotaExceeded
	}
	if int32(concurrentInflight) >= quota.MaxConcurrentCloudExec {
		return ErrQuotaConcurrentLimit
	}
	return nil
}

// RecordCost adds the spend (in USD) to the workspace's monthly total.
// Best-effort: errors are logged but never returned, since cost recording
// must not block completion of a successful task.
func (s *QuotaService) RecordCost(ctx context.Context, workspaceID uuid.UUID, usd float64) {
	if s == nil || s.Q == nil {
		return
	}
	if usd <= 0 {
		return
	}
	if err := s.Q.AddWorkspaceCostUSD(ctx, db.AddWorkspaceCostUSDParams{
		WorkspaceID: toPgUUID(workspaceID),
		Amount:      float64ToNumeric(usd),
	}); err != nil {
		slog.Warn("quota: cost record failed", "ws", workspaceID.String(), "usd", usd, "err", err)
	}
}

// float64ToNumeric converts a float64 to a pgtype.Numeric using a 4-decimal
// text representation. This matches the precision of cost columns
// (NUMERIC(10,4)) and avoids the lossy big.Rat round-trip path.
func float64ToNumeric(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	// Scan accepts a textual decimal representation per pgx v5 numeric.go.
	if err := n.Scan(fmt.Sprintf("%.4f", f)); err != nil {
		// Fallback: NULL — AddWorkspaceCostUSD will then do `+ NULL` → no-op.
		// This is best-effort cost recording; we log via the caller path.
		return pgtype.Numeric{}
	}
	return n
}

// numericToFloat64 extracts a float64 from a pgtype.Numeric. Returns 0 for
// NULL/invalid values without raising an error so quota comparisons remain
// well-defined for fresh rows.
func numericToFloat64(n pgtype.Numeric) (float64, error) {
	if !n.Valid {
		return 0, nil
	}
	f8, err := n.Float64Value()
	if err != nil {
		return 0, err
	}
	if !f8.Valid {
		return 0, nil
	}
	return f8.Float64, nil
}
