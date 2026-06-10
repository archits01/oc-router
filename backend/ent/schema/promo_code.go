package schema

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PromoCode holds the schema definition for the PromoCode entity.
//
//
//
type PromoCode struct {
	ent.Schema
}

func (PromoCode) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "promo_codes"},
	}
}

func (PromoCode) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").
			MaxLen(32).
			NotEmpty().
			Unique().
			Comment("promo code"),
		field.Float("bonus_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0).
			Comment("bonus balance amount"),
		field.Int("max_uses").
			Default(0).
			Comment("max usage count, 0 means unlimited"),
		field.Int("used_count").
			Default(0).
			Comment("current usage count"),
		field.String("status").
			MaxLen(20).
			Default(domain.PromoCodeStatusActive).
			Comment("status: active, disabled"),
		field.Time("expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("expiry time, null means never expires"),
		field.String("notes").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Comment("notes"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (PromoCode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("usage_records", PromoCodeUsage.Type),
	}
}

func (PromoCode) Indexes() []ent.Index {
	return []ent.Index{
		// code () ()，
		index.Fields("status"),
		index.Fields("expires_at"),
	}
}
