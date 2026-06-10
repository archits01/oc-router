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

// ChannelMonitorDailyRollup (monitor_id, model, bucket_date)
// +
// = sum_latency_ms / count_latency；availability = ok_count / total_checks）。
//
type ChannelMonitorDailyRollup struct {
	ent.Schema
}

func (ChannelMonitorDailyRollup) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "channel_monitor_daily_rollups"},
	}
}

func (ChannelMonitorDailyRollup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("monitor_id"),
		field.String("model").
			NotEmpty().
			MaxLen(200),
		field.Time("bucket_date").
			SchemaType(map[string]string{dialect.Postgres: "date"}),
		field.Int("total_checks").Default(0),
		field.Int("ok_count").Default(0),
		field.Int("operational_count").Default(0),
		field.Int("degraded_count").Default(0),
		field.Int("failed_count").Default(0),
		field.Int("error_count").Default(0),
		field.Int64("sum_latency_ms").Default(0),
		field.Int("count_latency").Default(0),
		field.Int64("sum_ping_latency_ms").Default(0),
		field.Int("count_ping_latency").Default(0),
		field.Time("computed_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (ChannelMonitorDailyRollup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("monitor", ChannelMonitor.Type).
			Ref("daily_rollups").
			Field("monitor_id").
			Unique().
			Required(),
	}
}

func (ChannelMonitorDailyRollup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("monitor_id", "model", "bucket_date").Unique(),
		index.Fields("bucket_date"),
	}
}
