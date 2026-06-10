// Package repository
//
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/migrations"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq" // PostgreSQL 驱动，通过副作用导入注册驱动
)

// InitEnt *sql.DB。
//
//  2.
//  3.
//  4.
//
//
//
//   - cfg:
//
//   - *ent.Client: Ent ORM
//   - *sql.DB:
//   - error:
func InitEnt(cfg *config.Config) (*ent.Client, *sql.DB, error) {
	if err := timezone.Init(cfg.Timezone); err != nil {
		return nil, nil, err
	}

	// (DSN)。
	//
	dsn := cfg.Database.DSNWithTimezone(cfg.Timezone)

	//
	// dialect.Postgres
	drv, err := entsql.Open(dialect.Postgres, dsn)
	if err != nil {
		return nil, nil, err
	}
	applyDBPoolSettings(drv.DB(), cfg)

	//
	// SQL
	//
	migrationCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := applyMigrationsFS(migrationCtx, drv.DB(), migrations.FS); err != nil {
		_ = drv.Close() // 迁移failed时shutting down驱动，避免资源泄露
		return nil, nil, err
	}

	//
	client := ent.NewClient(ent.Driver(drv))

	if err := ensureBootstrapSecrets(migrationCtx, client, cfg); err != nil {
		_ = client.Close()
		return nil, nil, err
	}

	//
	if err := cfg.Validate(); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("validate config after secret bootstrap: %w", err)
	}

	// SIMPLE
	// - anthropic/openai/gemini: <platform>-default
	// - antigravity: >=2
	if cfg.RunMode == config.RunModeSimple {
		seedCtx, seedCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer seedCancel()
		if err := ensureSimpleModeDefaultGroups(seedCtx, client); err != nil {
			_ = client.Close()
			return nil, nil, err
		}
		if err := ensureSimpleModeAdminConcurrency(seedCtx, client); err != nil {
			_ = client.Close()
			return nil, nil, err
		}
	}

	return client, drv.DB(), nil
}
