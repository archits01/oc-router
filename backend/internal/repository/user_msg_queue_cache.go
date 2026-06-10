package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// Redis Key
// {accountID}:lock / umq:{accountID}:last
const (
	umqKeyPrefix  = "umq:"
	umqLockSuffix = ":lock" // STRING (requestID), PX lockTtlMs
	umqLastSuffix = ":last" // STRING (millisecond timestamp), EX 60s
)

// Lua +
var acquireLockScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if cur == ARGV[1] then
    redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[2]))
    return 1
end
if cur ~= false then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'PX', tonumber(ARGV[2]))
return 1
`)

// Lua +
var releaseLockScript = redis.NewScript(`
-- Redis 3.2-4.x compat: opt into effects replication so redis.call('TIME')
-- replicates correctly. No-op on Redis 5.0+ (effects replication is default).
redis.replicate_commands()
local cur = redis.call('GET', KEYS[1])
if cur == ARGV[1] then
    redis.call('DEL', KEYS[1])
    local t = redis.call('TIME')
    local ms = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
    redis.call('SET', KEYS[2], ms, 'EX', 60)
    return 1
end
return 0
`)

// Lua == -1
var forceReleaseLockScript = redis.NewScript(`
local pttl = redis.call('PTTL', KEYS[1])
if pttl == -1 then
    redis.call('DEL', KEYS[1])
    return 1
end
return 0
`)

type userMsgQueueCache struct {
	rdb *redis.Client
}

// NewUserMsgQueueCache
func NewUserMsgQueueCache(rdb *redis.Client) service.UserMsgQueueCache {
	return &userMsgQueueCache{rdb: rdb}
}

func umqLockKey(accountID int64) string {
	// {123}:lock —
	return umqKeyPrefix + "{" + strconv.FormatInt(accountID, 10) + "}" + umqLockSuffix
}

func umqLastKey(accountID int64) string {
	// {123}:last —
	return umqKeyPrefix + "{" + strconv.FormatInt(accountID, 10) + "}" + umqLastSuffix
}

// umqScanPattern
func umqScanPattern() string {
	return umqKeyPrefix + "{*}" + umqLockSuffix
}

// AcquireLock
func (c *userMsgQueueCache) AcquireLock(ctx context.Context, accountID int64, requestID string, lockTtlMs int) (bool, error) {
	key := umqLockKey(accountID)
	result, err := acquireLockScript.Run(ctx, c.rdb, []string{key}, requestID, lockTtlMs).Int()
	if err != nil {
		return false, fmt.Errorf("umq acquire lock: %w", err)
	}
	return result == 1, nil
}

// ReleaseLock
func (c *userMsgQueueCache) ReleaseLock(ctx context.Context, accountID int64, requestID string) (bool, error) {
	lockKey := umqLockKey(accountID)
	lastKey := umqLastKey(accountID)
	result, err := releaseLockScript.Run(ctx, c.rdb, []string{lockKey, lastKey}, requestID).Int()
	if err != nil {
		return false, fmt.Errorf("umq release lock: %w", err)
	}
	return result == 1, nil
}

// GetLastCompletedMs
func (c *userMsgQueueCache) GetLastCompletedMs(ctx context.Context, accountID int64) (int64, error) {
	key := umqLastKey(accountID)
	val, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("umq get last completed: %w", err)
	}
	ms, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("umq parse last completed: %w", err)
	}
	return ms, nil
}

// ForceReleaseLock == -1
func (c *userMsgQueueCache) ForceReleaseLock(ctx context.Context, accountID int64) error {
	key := umqLockKey(accountID)
	_, err := forceReleaseLockScript.Run(ctx, c.rdb, []string{key}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("umq force release lock: %w", err)
	}
	return nil
}

// ScanLockKeys == -1（
// == -1
func (c *userMsgQueueCache) ScanLockKeys(ctx context.Context, maxCount int) ([]int64, error) {
	var accountIDs []int64
	var cursor uint64
	pattern := umqScanPattern()

	for {
		keys, nextCursor, err := c.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("umq scan lock keys: %w", err)
		}
		for _, key := range keys {
			// == -1（
			pttl, err := c.rdb.PTTL(ctx, key).Result()
			if err != nil {
				continue
			}
			// PTTL = key = >0 =
			// go-redis (-1)/-2
			if pttl != time.Duration(-1) {
				continue
			}

			// {123}:lock → {}
			openBrace := strings.IndexByte(key, '{')
			closeBrace := strings.IndexByte(key, '}')
			if openBrace < 0 || closeBrace <= openBrace+1 {
				continue
			}
			idStr := key[openBrace+1 : closeBrace]
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				continue
			}
			accountIDs = append(accountIDs, id)
			if len(accountIDs) >= maxCount {
				return accountIDs, nil
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return accountIDs, nil
}

// GetCurrentTimeMs
func (c *userMsgQueueCache) GetCurrentTimeMs(ctx context.Context) (int64, error) {
	t, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return 0, fmt.Errorf("umq get redis time: %w", err)
	}
	return t.UnixMilli(), nil
}
