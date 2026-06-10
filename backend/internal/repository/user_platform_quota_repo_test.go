//go:build unit

package repository

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestMaybeReset(t *testing.T) {
	start := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	other := start.AddDate(0, 0, -1)
	cases := []struct {
		name      string
		prevUsage float64
		prevStart *time.Time
		currStart time.Time
		cost      float64
		want      float64
	}{
		{"nil prev start resets", 10, nil, start, 1.5, 1.5},
		{"different start resets", 10, &other, start, 1.5, 1.5},
		{"same start accumulates", 10, &start, start, 1.5, 11.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maybeReset(c.prevUsage, c.prevStart, c.currStart, c.cost); got != c.want {
				t.Errorf("maybeReset = %v, want %v", got, c.want)
			}
		})
	}
}

// TestMonthlyMaybeReset_NilStart =nil
func TestMonthlyMaybeReset_NilStart(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	usage, start := monthlyMaybeReset(10.0, nil, 1.5, now)
	if usage != 1.5 {
		t.Errorf("usage = %v, want 1.5", usage)
	}
	if !start.Equal(now) {
		t.Errorf("start = %v, want %v", start, now)
	}
}

// TestMonthlyMaybeReset_Expired
func TestMonthlyMaybeReset_Expired(t *testing.T) {
	windowStart := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	// now = windowStart + 30d（
	now := windowStart.Add(30 * 24 * time.Hour)
	usage, start := monthlyMaybeReset(8.0, &windowStart, 2.0, now)
	if usage != 2.0 {
		t.Errorf("usage = %v, want 2.0 (reset)", usage)
	}
	if !start.Equal(now) {
		t.Errorf("start = %v, want %v (new window)", start, now)
	}
}

// TestMonthlyMaybeReset_CrossMonthBoundary
//
func TestMonthlyMaybeReset_CrossMonthBoundary(t *testing.T) {
	windowStart := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	// 5
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	usage, start := monthlyMaybeReset(5.0, &windowStart, 1.0, now)
	if usage != 6.0 {
		t.Errorf("usage = %v, want 6.0 (accumulate, not reset at month boundary)", usage)
	}
	if !start.Equal(windowStart) {
		t.Errorf("start = %v, want %v (preserved)", start, windowStart)
	}
}

// TestMonthlyMaybeReset_Active
func TestMonthlyMaybeReset_Active(t *testing.T) {
	windowStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	now := windowStart.Add(15 * 24 * time.Hour)
	usage, start := monthlyMaybeReset(3.0, &windowStart, 0.5, now)
	if usage != 3.5 {
		t.Errorf("usage = %v, want 3.5", usage)
	}
	if !start.Equal(windowStart) {
		t.Errorf("start = %v, want %v", start, windowStart)
	}
}

// TestUpdateLimitsRowQuery_HasDeletedAtGuard
//
//
func TestUpdateLimitsRowQuery_HasDeletedAtGuard(t *testing.T) {
	src, err := os.ReadFile("user_platform_quota_repo.go")
	if err != nil {
		t.Fatalf("failed to read source file: %v", err)
	}
	const guard = "AND deleted_at IS NULL"
	if !strings.Contains(string(src), guard) {
		t.Errorf("updateLimitsRow SQL must contain %q to prevent bulk reactivation of soft-deleted rows (I-NEW-1)", guard)
	}
}
