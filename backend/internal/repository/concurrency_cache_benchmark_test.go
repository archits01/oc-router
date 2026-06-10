package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

//
const benchSlotTTLMinutes = 15

var benchSlotTTL = time.Duration(benchSlotTTLMinutes) * time.Minute

// BenchmarkAccountConcurrency
func BenchmarkAccountConcurrency(b *testing.B) {
	rdb := newBenchmarkRedisClient(b)
	defer func() {
		_ = rdb.Close()
	}()

	cache, _ := NewConcurrencyCache(rdb, benchSlotTTLMinutes, int(benchSlotTTL.Seconds())).(*concurrencyCache)
	ctx := context.Background()

	for _, size := range []int{10, 100, 1000} {
		size := size
		b.Run(fmt.Sprintf("zset/slots=%d", size), func(b *testing.B) {
			accountID := time.Now().UnixNano()
			key := accountSlotKey(accountID)

			b.StopTimer()
			members := make([]redis.Z, 0, size)
			now := float64(time.Now().Unix())
			for i := 0; i < size; i++ {
				members = append(members, redis.Z{
					Score:  now,
					Member: fmt.Sprintf("req_%d", i),
				})
			}
			if err := rdb.ZAdd(ctx, key, members...).Err(); err != nil {
				b.Fatalf("failed to initialize sorted set: %v", err)
			}
			if err := rdb.Expire(ctx, key, benchSlotTTL).Err(); err != nil {
				b.Fatalf("failed to set sorted set TTL: %v", err)
			}
			b.StartTimer()

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := cache.GetAccountConcurrency(ctx, accountID); err != nil {
					b.Fatalf("failed to get concurrency count: %v", err)
				}
			}

			b.StopTimer()
			if err := rdb.Del(ctx, key).Err(); err != nil {
				b.Fatalf("failed to clean up sorted set: %v", err)
			}
		})

		b.Run(fmt.Sprintf("scan/slots=%d", size), func(b *testing.B) {
			accountID := time.Now().UnixNano()
			pattern := fmt.Sprintf("%s%d:*", accountSlotKeyPrefix, accountID)
			keys := make([]string, 0, size)

			b.StopTimer()
			pipe := rdb.Pipeline()
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("%s%d:req_%d", accountSlotKeyPrefix, accountID, i)
				keys = append(keys, key)
				pipe.Set(ctx, key, "1", benchSlotTTL)
			}
			if _, err := pipe.Exec(ctx); err != nil {
				b.Fatalf("failed to initialize scan keys: %v", err)
			}
			b.StartTimer()

			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := scanSlotCount(ctx, rdb, pattern); err != nil {
					b.Fatalf("SCAN count failed: %v", err)
				}
			}

			b.StopTimer()
			if err := rdb.Del(ctx, keys...).Err(); err != nil {
				b.Fatalf("failed to clean up scan keys: %v", err)
			}
		})
	}
}

func scanSlotCount(ctx context.Context, rdb *redis.Client, pattern string) (int, error) {
	var cursor uint64
	count := 0
	for {
		keys, nextCursor, err := rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return 0, err
		}
		count += len(keys)
		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}
	return count, nil
}

func newBenchmarkRedisClient(b *testing.B) *redis.Client {
	b.Helper()

	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		b.Skip("TEST_REDIS_URL not set, skipping Redis benchmark test")
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		b.Fatalf("parse TEST_REDIS_URL failed: %v", err)
	}

	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		b.Fatalf("Redis connection failed: %v", err)
	}

	return client
}
