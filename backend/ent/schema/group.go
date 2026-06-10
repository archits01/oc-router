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

// Group holds the schema definition for the Group entity.
type Group struct {
	ent.Schema
}

func (Group) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "groups"},
	}
}

func (Group) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (Group) Fields() []ent.Field {
	return []ent.Field{
		//
		//
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		field.String("description").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Float("rate_multiplier").
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}).
			Default(1.0),
		field.Bool("is_exclusive").
			Default(false),
		field.String("status").
			MaxLen(20).
			Default(domain.StatusActive),

		// Subscription-related fields (added by migration 003)
		field.String("platform").
			MaxLen(50).
			Default(domain.PlatformAnthropic),
		field.String("subscription_type").
			MaxLen(20).
			Default(domain.SubscriptionTypeStandard),
		field.Float("daily_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("weekly_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("monthly_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Int("default_validity_days").
			Default(30),

		//
		field.Bool("allow_image_generation").
			Default(false).
			Comment("whether this group is allowed to use image generation"),
		field.Bool("image_rate_independent").
			Default(false).
			Comment("whether image generation uses an independent rate multiplier; false means sharing the group rate multiplier"),
		field.Float("image_rate_multiplier").
			SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}).
			Default(1.0).
			Comment("independent image generation rate multiplier, only effective when image_rate_independent=true"),
		field.Float("image_price_1k").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("image_price_2k").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("image_price_4k").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),

		// Claude Code (added by migration 029)
		field.Bool("claude_code_only").
			Default(false).
			Comment("whether to allow only Claude Code clients"),
		field.Int64("fallback_group_id").
			Optional().
			Nillable().
			Comment("fallback group ID for non-Claude Code requests"),
		field.Int64("fallback_group_id_on_invalid_request").
			Optional().
			Nillable().
			Comment("fallback group ID for invalid requests"),

		// (added by migration 040)
		field.JSON("model_routing", map[string][]int64{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("model routing configuration: model pattern -> preferred account ID list"),

		// (added by migration 041)
		field.Bool("model_routing_enabled").
			Default(false).
			Comment("whether model routing configuration is enabled"),

		// MCP XML (added by migration 042)
		field.Bool("mcp_xml_inject").
			Default(true).
			Comment("whether to inject MCP XML invocation protocol prompt (antigravity platform only)"),

		// (added by migration 046)
		field.JSON("supported_model_scopes", []string{}).
			Default([]string{"claude", "gemini_text", "gemini_image"}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("supported model families: claude, gemini_text, gemini_image"),

		// (added by migration 052)
		field.Int("sort_order").
			Default(0).
			Comment("group display sort order, lower value means higher position"),

		// OpenAI Messages (added by migration 069)
		field.Bool("allow_messages_dispatch").
			Default(false).
			Comment("whether to allow /v1/messages dispatching to this OpenAI group"),
		field.Bool("require_oauth_only").
			Default(false).
			Comment("only allow non-apikey type accounts to be associated with this group"),
		field.Bool("require_privacy_set").
			Default(false).
			Comment("only allow accounts with privacy successfully set during scheduling"),
		field.String("default_mapped_model").
			MaxLen(100).
			Default("").
			Comment("default mapped model ID, used when account-level mapping is not found"),
		field.JSON("messages_dispatch_model_config", domain.OpenAIMessagesDispatchModelConfig{}).
			Default(domain.OpenAIMessagesDispatchModelConfig{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("OpenAI Messages dispatch model configuration: maps Claude family/exact model to target GPT model"),
		field.JSON("models_list_config", domain.GroupModelsListConfig{}).
			Default(domain.GroupModelsListConfig{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("custom /v1/models display list configuration; only affects model list response, does not affect scheduling"),

		// =
		field.Int("rpm_limit").
			Default(0).
			Comment("group RPM limit, 0 means no limit; when set, takes over rate limiting for users of this group"),
	}
}

func (Group) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("api_keys", APIKey.Type),
		edge.To("redeem_codes", RedeemCode.Type),
		edge.To("subscriptions", UserSubscription.Type),
		edge.To("usage_logs", UsageLog.Type),
		edge.From("accounts", Account.Type).
			Ref("groups").
			Through("account_groups", AccountGroup.Type),
		edge.From("allowed_users", User.Type).
			Ref("allowed_groups").
			Through("user_allowed_groups", UserAllowedGroup.Type),
		//
		//
	}
}

func (Group) Indexes() []ent.Index {
	return []ent.Index{
		// name () ()，
		index.Fields("status"),
		index.Fields("platform"),
		index.Fields("subscription_type"),
		index.Fields("is_exclusive"),
		index.Fields("deleted_at"),
		index.Fields("sort_order"),
	}
}
