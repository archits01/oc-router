package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const timeoutCounterPrefix = "timeout_count:account:"

// timeoutCounterIncrScript
//
var timeoutCounterIncrScript = redis.NewScript(`
	local key = KEYS[1]
	local ttl = tonumber(ARGV[1])

	local count = redis.call('INCR', key)
	if count == 1 then
		redis.call('EXPIRE', key, ttl)
	end

	return count
`)

type timeoutCounterCache struct {
	rdb *redis.Client
}

// NewTimeoutCounterCache
func NewTimeoutCounterCache(rdb *redis.Client) service.TimeoutCounterCache {
	return &timeoutCounterCache{rdb: rdb}
}

// IncrementTimeoutCount
// windowMinutes
func (c *timeoutCounterCache) IncrementTimeoutCount(ctx context.Context, accountID int64, windowMinutes int) (int64, error) {
	key := fmt.Sprintf("%s%d", timeoutCounterPrefix, accountID)

	ttlSeconds := windowMinutes * 60
	if ttlSeconds < 60 {
		ttlSeconds = 60 // minimum 1 minute
	}

	result, err := timeoutCounterIncrScript.Run(ctx, c.rdb, []string{key}, ttlSeconds).Int64()
	if err != nil {
		return 0, fmt.Errorf("increment timeout count: %w", err)
	}

	return result, nil
}

// GetTimeoutCount
func (c *timeoutCounterCache) GetTimeoutCount(ctx context.Context, accountID int64) (int64, error) {
	key := fmt.Sprintf("%s%d", timeoutCounterPrefix, accountID)

	val, err := c.rdb.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get timeout count: %w", err)
	}

	return val, nil
}

// ResetTimeoutCount
func (c *timeoutCounterCache) ResetTimeoutCount(ctx context.Context, accountID int64) error {
	key := fmt.Sprintf("%s%d", timeoutCounterPrefix, accountID)
	return c.rdb.Del(ctx, key).Err()
}

// GetTimeoutCountTTL
func (c *timeoutCounterCache) GetTimeoutCountTTL(ctx context.Context, accountID int64) (time.Duration, error) {
	key := fmt.Sprintf("%s%d", timeoutCounterPrefix, accountID)
	return c.rdb.TTL(ctx, key).Result()
}
