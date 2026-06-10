// Package schema
package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ErrorPassthroughRule
//
//   - +
//   -
type ErrorPassthroughRule struct {
	ent.Schema
}

// Annotations
func (ErrorPassthroughRule) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "error_passthrough_rules"},
	}
}

// Mixin
func (ErrorPassthroughRule) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

// Fields
func (ErrorPassthroughRule) Fields() []ent.Field {
	return []ent.Field{
		// name:
		field.String("name").
			MaxLen(100).
			NotEmpty(),

		// enabled:
		field.Bool("enabled").
			Default(true),

		// priority:
		field.Int("priority").
			Default(0),

		// error_codes:
		// [422, 400]
		field.JSON("error_codes", []int{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// keywords:
		// ["context limit", "model not supported"]
		field.JSON("keywords", []string{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// match_mode:
		// - "any":
		// - "all":
		field.String("match_mode").
			MaxLen(10).
			Default("any"),

		// platforms:
		// ["anthropic", "openai", "gemini", "antigravity"]
		field.JSON("platforms", []string{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// passthrough_code:
		// true:
		// false:
		field.Bool("passthrough_code").
			Default(true),

		// response_code:
		// =false
		field.Int("response_code").
			Optional().
			Nillable(),

		// passthrough_body:
		// true:
		// false:
		field.Bool("passthrough_body").
			Default(true),

		// custom_message:
		// =false
		field.Text("custom_message").
			Optional().
			Nillable(),

		// skip_monitoring:
		// true:
		// false:
		field.Bool("skip_monitoring").
			Default(false),

		// description:
		field.Text("description").
			Optional().
			Nillable(),
	}
}

// Indexes
func (ErrorPassthroughRule) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("enabled"),  // filter enabled rules
		index.Fields("priority"), // sort by priority
	}
}
