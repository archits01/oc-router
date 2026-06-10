package service

import "context"

// RPMCache RPM
//
type RPMCache interface {
	// IncrementRPM
	//
	IncrementRPM(ctx context.Context, accountID int64) (count int, err error)

	// GetRPM
	GetRPM(ctx context.Context, accountID int64) (count int, err error)

	// GetRPMBatch
	GetRPMBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error)
}
