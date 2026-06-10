package service

import "context"

// OpenAI403CounterCache
type OpenAI403CounterCache interface {
	// IncrementOpenAI403Count
	IncrementOpenAI403Count(ctx context.Context, accountID int64, windowMinutes int) (int64, error)
	// ResetOpenAI403Count
	ResetOpenAI403Count(ctx context.Context, accountID int64) error
}
