package service

import (
	"context"
	"time"
)

// SessionLimitCache
//
//
// Key {accountID}
// (member=sessionUUID, score=timestamp)
//
type SessionLimitCache interface {
	// RegisterSession
	// -
	// - < maxSessions，
	// - >= maxSessions，
	//
	//   accountID:
	//   sessionUUID:
	//   maxSessions:
	//   idleTimeout:
	//
	//   allowed: true
	//   error:
	RegisterSession(ctx context.Context, accountID int64, sessionUUID string, maxSessions int, idleTimeout time.Duration) (allowed bool, err error)

	// RefreshSession
	RefreshSession(ctx context.Context, accountID int64, sessionUUID string, idleTimeout time.Duration) error

	// GetActiveSessionCount
	GetActiveSessionCount(ctx context.Context, accountID int64) (int, error)

	// GetActiveSessionCountBatch
	// idleTimeouts:
	// [accountID]count，
	GetActiveSessionCountBatch(ctx context.Context, accountIDs []int64, idleTimeouts map[int64]time.Duration) (map[int64]int, error)

	// IsSessionActive
	IsSessionActive(ctx context.Context, accountID int64, sessionUUID string) (bool, error)

	// ========== 5h==========
	// Key {accountID}

	// GetWindowCost
	// (cost, true, nil)
	// (0, false, nil)
	// (0, false, err)
	GetWindowCost(ctx context.Context, accountID int64) (cost float64, hit bool, err error)

	// SetWindowCost
	SetWindowCost(ctx context.Context, accountID int64, cost float64) error

	// GetWindowCostBatch
	// [accountID]cost，
	GetWindowCostBatch(ctx context.Context, accountIDs []int64) (map[int64]float64, error)
}
