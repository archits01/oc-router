// Package schema
package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// TLSFingerprintProfile
//
// TLS
//
//
type TLSFingerprintProfile struct {
	ent.Schema
}

// Annotations
func (TLSFingerprintProfile) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "tls_fingerprint_profiles"},
	}
}

// Mixin
func (TLSFingerprintProfile) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

// Fields
func (TLSFingerprintProfile) Fields() []ent.Field {
	return []ent.Field{
		// name:
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Unique(),

		// description:
		field.Text("description").
			Optional().
			Nillable(),

		// enable_grease:
		field.Bool("enable_grease").
			Default(false),

		// cipher_suites: TLS
		field.JSON("cipher_suites", []uint16{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// curves:
		field.JSON("curves", []uint16{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// point_formats: EC
		field.JSON("point_formats", []uint16{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// signature_algorithms:
		field.JSON("signature_algorithms", []uint16{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// alpn_protocols: ALPN ["http/1.1"]）
		field.JSON("alpn_protocols", []string{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// supported_versions: [0x0304, 0x0303]）
		field.JSON("supported_versions", []uint16{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// key_share_groups: Key Share [29]
		field.JSON("key_share_groups", []uint16{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// psk_modes: PSK [1]
		field.JSON("psk_modes", []uint16{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),

		// extensions: TLS
		field.JSON("extensions", []uint16{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}
