// Package schema
package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Account
//
//
//
//
//   -
//   -
type Account struct {
	ent.Schema
}

// Annotations
// "accounts"。
func (Account) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "accounts"},
	}
}

// Mixin
// - TimeMixin:
// - SoftDeleteMixin:
func (Account) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

// Fields
func (Account) Fields() []ent.Field {
	return []ent.Field{
		// name:
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		// notes:
		field.String("notes").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),

		// platform: "claude", "gemini", "openai"
		field.String("platform").
			MaxLen(50).
			NotEmpty(),

		// type: "api_key", "oauth", "cookie"
		//
		field.String("type").
			MaxLen(20).
			NotEmpty(),

		// credentials:
		//
		// - api_key: {"api_key": "sk-xxx"}
		// - oauth: {"access_token": "...", "refresh_token": "...", "expires_at": "..."}
		// - cookie: {"session_key": "..."}
		field.JSON("credentials", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// extra:
		//
		field.JSON("extra", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// proxy_id:
		//
		field.Int64("proxy_id").
			Optional().
			Nillable(),
		field.Int64("proxy_fallback_origin_id").
			Optional().Nillable().
			Comment("Original proxy id replaced by expiry-fallback; for manual revert. NULL = not in fallback."),

		// concurrency:
		field.Int("concurrency").
			Default(3),

		field.Int("load_factor").Optional().Nillable(),

		// priority:
		field.Int("priority").
			Default(50),

		// rate_multiplier: >=0，
		//
		field.Float("rate_multiplier").
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}).
			Default(1.0),

		// status: "active", "error", "disabled"
		field.String("status").
			MaxLen(20).
			Default(domain.StatusActive),

		// error_message:
		field.String("error_message").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),

		// last_used_at:
		field.Time("last_used_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		// expires_at:
		field.Time("expires_at").
			Optional().
			Nillable().
			Comment("Account expiration time (NULL means no expiration).").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		// auto_pause_on_expired:
		field.Bool("auto_pause_on_expired").
			Default(true).
			Comment("Auto pause scheduling when account expires."),

		// ========== ==========
		//

		// schedulable:
		// false
		field.Bool("schedulable").
			Default(true),

		// rate_limited_at:
		//
		field.Time("rate_limited_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),

		// rate_limit_reset_at:
		field.Time("rate_limit_reset_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),

		// overload_until:
		//
		field.Time("overload_until").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),

		// temp_unschedulable_until:
		field.Time("temp_unschedulable_until").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),

		// temp_unschedulable_reason:
		field.String("temp_unschedulable_reason").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),

		// session_window_*:
		//
		field.Time("session_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("session_window_end").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("session_window_status").
			Optional().
			Nillable().
			MaxLen(20),
	}
}

// Edges
func (Account) Edges() []ent.Edge {
	return []ent.Edge{
		// groups:
		//
		edge.To("groups", Group.Type).
			Through("account_groups", AccountGroup.Type),
		// proxy:
		//
		edge.To("proxy", Proxy.Type).
			Field("proxy_id").
			Unique(),
		// usage_logs:
		edge.To("usage_logs", UsageLog.Type),
	}
}

// Indexes
func (Account) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("platform"),            // filter by platform
		index.Fields("type"),                // filter by auth type
		index.Fields("status"),              // filter by status
		index.Fields("proxy_id"),            // filter by proxy
		index.Fields("priority"),            // sort by priority
		index.Fields("last_used_at"),        // sort by last used time
		index.Fields("schedulable"),         // filter schedulable accounts
		index.Fields("rate_limited_at"),     // filter rate-limited accounts
		index.Fields("rate_limit_reset_at"), // filter rate limit reset time
		index.Fields("overload_until"),      // filter overloaded accounts
		//
		index.Fields("platform", "priority"),
		index.Fields("priority", "status"),
		index.Fields("deleted_at"), // soft delete query optimization
	}
}
