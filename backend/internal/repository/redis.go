package repository

import (
	"crypto/tls"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"

	"github.com/redis/go-redis/v9"
)

// InitRedis
//
//
//
// 1. PoolSize:
// 2. MinIdleConns:
// 3. DialTimeout/ReadTimeout/WriteTimeout:
func InitRedis(cfg *config.Config) *redis.Client {
	return redis.NewClient(buildRedisOptions(cfg))
}

// buildRedisOptions
func buildRedisOptions(cfg *config.Config) *redis.Options {
	opts := &redis.Options{
		Addr:         cfg.Redis.Address(),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  time.Duration(cfg.Redis.DialTimeoutSeconds) * time.Second,  // connection timeout
		ReadTimeout:  time.Duration(cfg.Redis.ReadTimeoutSeconds) * time.Second,  // read timeout
		WriteTimeout: time.Duration(cfg.Redis.WriteTimeoutSeconds) * time.Second, // write timeout
		PoolSize:     cfg.Redis.PoolSize,                                         // connection pool size
		MinIdleConns: cfg.Redis.MinIdleConns,                                     // minimum idle connections
	}

	if cfg.Redis.EnableTLS {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: cfg.Redis.Host,
		}
	}

	return opts
}
