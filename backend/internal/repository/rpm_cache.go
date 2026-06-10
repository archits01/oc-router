package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// RPM
//
//
// - Key: rpm:{accountID}:{minuteTimestamp}
// - Value:
// - TTL: 120 +
//
// + EXPIRE，
// ()
//
//   - TxPipeline vs Pipeline：Pipeline
//   - rdb.Time()
//     Lua
const (
	// RPM
	// {accountID}:{minuteTimestamp}
	rpmKeyPrefix = "rpm:"

	// RPM +
	rpmKeyTTL = 120 * time.Second
)

// RPMCacheImpl RPM
type RPMCacheImpl struct {
	rdb *redis.Client
}

// NewRPMCache
func NewRPMCache(rdb *redis.Client) service.RPMCache {
	return &RPMCacheImpl{rdb: rdb}
}

// currentMinuteKey
// ()
func (c *RPMCacheImpl) currentMinuteKey(ctx context.Context, accountID int64) (string, error) {
	serverTime, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return "", fmt.Errorf("redis TIME: %w", err)
	}
	minuteTS := serverTime.Unix() / 60
	return fmt.Sprintf("%s%d:%d", rpmKeyPrefix, accountID, minuteTS), nil
}

// currentMinuteSuffix
// ()
func (c *RPMCacheImpl) currentMinuteSuffix(ctx context.Context) (string, error) {
	serverTime, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return "", fmt.Errorf("redis TIME: %w", err)
	}
	minuteTS := serverTime.Unix() / 60
	return strconv.FormatInt(minuteTS, 10), nil
}

// IncrementRPM
// (MULTI/EXEC) + EXPIRE，
func (c *RPMCacheImpl) IncrementRPM(ctx context.Context, accountID int64) (int, error) {
	key, err := c.currentMinuteKey(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("rpm increment: %w", err)
	}

	// (MULTI/EXEC) + EXPIRE
	// EXPIRE
	pipe := c.rdb.TxPipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, rpmKeyTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("rpm increment: %w", err)
	}

	return int(incrCmd.Val()), nil
}

// GetRPM
func (c *RPMCacheImpl) GetRPM(ctx context.Context, accountID int64) (int, error) {
	key, err := c.currentMinuteKey(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("rpm get: %w", err)
	}

	val, err := c.rdb.Get(ctx, key).Int()
	if errors.Is(err, redis.Nil) {
		return 0, nil // no records for current minute
	}
	if err != nil {
		return 0, fmt.Errorf("rpm get: %w", err)
	}
	return val, nil
}

// GetRPMBatch
func (c *RPMCacheImpl) GetRPMBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error) {
	if len(accountIDs) == 0 {
		return map[int64]int{}, nil
	}

	minuteSuffix, err := c.currentMinuteSuffix(ctx)
	if err != nil {
		return nil, fmt.Errorf("rpm batch get: %w", err)
	}

	//
	pipe := c.rdb.Pipeline()
	cmds := make(map[int64]*redis.StringCmd, len(accountIDs))
	for _, id := range accountIDs {
		key := fmt.Sprintf("%s%d:%s", rpmKeyPrefix, id, minuteSuffix)
		cmds[id] = pipe.Get(ctx, key)
	}

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("rpm batch get: %w", err)
	}

	result := make(map[int64]int, len(accountIDs))
	for id, cmd := range cmds {
		if val, err := cmd.Int(); err == nil {
			result[id] = val
		} else {
			result[id] = 0
		}
	}
	return result, nil
}
