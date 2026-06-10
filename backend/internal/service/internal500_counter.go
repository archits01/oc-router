package service

import "context"

// Internal500CounterCache
type Internal500CounterCache interface {
	// IncrementInternal500Count
	IncrementInternal500Count(ctx context.Context, accountID int64) (int64, error)
	// ResetInternal500Count
	ResetInternal500Count(ctx context.Context, accountID int64) error
}
