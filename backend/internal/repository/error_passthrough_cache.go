package repository

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	errorPassthroughCacheKey  = "error_passthrough_rules"
	errorPassthroughPubSubKey = "error_passthrough_rules_updated"
	errorPassthroughCacheTTL  = 24 * time.Hour
)

type errorPassthroughCache struct {
	rdb        *redis.Client
	localCache []*model.ErrorPassthroughRule
	localMu    sync.RWMutex
}

// NewErrorPassthroughCache
func NewErrorPassthroughCache(rdb *redis.Client) service.ErrorPassthroughCache {
	return &errorPassthroughCache{
		rdb: rdb,
	}
}

// Get
func (c *errorPassthroughCache) Get(ctx context.Context) ([]*model.ErrorPassthroughRule, bool) {
	c.localMu.RLock()
	if c.localCache != nil {
		rules := c.localCache
		c.localMu.RUnlock()
		return rules, true
	}
	c.localMu.RUnlock()

	//
	data, err := c.rdb.Get(ctx, errorPassthroughCacheKey).Bytes()
	if err != nil {
		if err != redis.Nil {
			log.Printf("[ErrorPassthroughCache] Failed to get from Redis: %v", err)
		}
		return nil, false
	}

	var rules []*model.ErrorPassthroughRule
	if err := json.Unmarshal(data, &rules); err != nil {
		log.Printf("[ErrorPassthroughCache] Failed to unmarshal rules: %v", err)
		return nil, false
	}

	c.localMu.Lock()
	c.localCache = rules
	c.localMu.Unlock()

	return rules, true
}

// Set
func (c *errorPassthroughCache) Set(ctx context.Context, rules []*model.ErrorPassthroughRule) error {
	data, err := json.Marshal(rules)
	if err != nil {
		return err
	}

	if err := c.rdb.Set(ctx, errorPassthroughCacheKey, data, errorPassthroughCacheTTL).Err(); err != nil {
		return err
	}

	c.localMu.Lock()
	c.localCache = rules
	c.localMu.Unlock()

	return nil
}

// Invalidate
func (c *errorPassthroughCache) Invalidate(ctx context.Context) error {
	c.localMu.Lock()
	c.localCache = nil
	c.localMu.Unlock()

	//
	return c.rdb.Del(ctx, errorPassthroughCacheKey).Err()
}

// NotifyUpdate
func (c *errorPassthroughCache) NotifyUpdate(ctx context.Context) error {
	return c.rdb.Publish(ctx, errorPassthroughPubSubKey, "refresh").Err()
}

// SubscribeUpdates
func (c *errorPassthroughCache) SubscribeUpdates(ctx context.Context, handler func()) {
	go func() {
		sub := c.rdb.Subscribe(ctx, errorPassthroughPubSubKey)
		defer func() { _ = sub.Close() }()

		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-ch:
				if msg == nil {
					return
				}
				//
				c.localMu.Lock()
				c.localCache = nil
				c.localMu.Unlock()

				handler()
			}
		}
	}()
}
