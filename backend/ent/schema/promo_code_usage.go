package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PromoCodeUsage holds the schema definition for the PromoCodeUsage entity.
//
type PromoCodeUsage struct {
	ent.Schema
}

func (PromoCodeUsage) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "promo_code_usages"},
	}
}

func (PromoCodeUsage) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("promo_code_id").
			Comment("promo code ID"),
		field.Int64("user_id").
			Comment("user ID who used the code"),
		field.Float("bonus_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Comment("actual bonus amount"),
		field.Time("used_at").
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("usage time"),
	}
}

func (PromoCodeUsage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("promo_code", PromoCode.Type).
			Ref("usage_records").
			Field("promo_code_id").
			Required().
			Unique(),
		edge.From("user", User.Type).
			Ref("promo_code_usages").
			Field("user_id").
			Required().
			Unique(),
	}
}

func (PromoCodeUsage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("promo_code_id"),
		index.Fields("user_id"),
		index.Fields("promo_code_id", "user_id").Unique(),
	}
}
