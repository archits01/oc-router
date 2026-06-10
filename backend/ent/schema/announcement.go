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

// Announcement holds the schema definition for the Announcement entity.
//
type Announcement struct {
	ent.Schema
}

func (Announcement) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "announcements"},
	}
}

func (Announcement) Fields() []ent.Field {
	return []ent.Field{
		field.String("title").
			MaxLen(200).
			NotEmpty().
			Comment("announcement title"),
		field.String("content").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			NotEmpty().
			Comment("announcement content (supports Markdown)"),
		field.String("status").
			MaxLen(20).
			Default(domain.AnnouncementStatusDraft).
			Comment("status: draft, active, archived"),
		field.String("notify_mode").
			MaxLen(20).
			Default(domain.AnnouncementNotifyModeSilent).
			Comment("notification mode: silent (bell icon only), popup (popup alert)"),
		field.JSON("targeting", domain.AnnouncementTargeting{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("display conditions (JSON rules)"),
		field.Time("starts_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("display start time (empty means effective immediately)"),
		field.Time("ends_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("display end time (empty means permanently effective)"),
		field.Int64("created_by").
			Optional().
			Nillable().
			Comment("creator user ID (admin)"),
		field.Int64("updated_by").
			Optional().
			Nillable().
			Comment("updater user ID (admin)"),
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

func (Announcement) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("reads", AnnouncementRead.Type),
	}
}

func (Announcement) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("created_at"),
		index.Fields("starts_at"),
		index.Fields("ends_at"),
	}
}
