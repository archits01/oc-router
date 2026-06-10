package openai_ws_v2

import (
	"sync/atomic"
)

// MetricsSnapshot
type MetricsSnapshot struct {
	SemanticMutationTotal  int64 `json:"semantic_mutation_total"`
	UsageParseFailureTotal int64 `json:"usage_parse_failure_total"`
}

var (
	// passthrough
	passthroughSemanticMutationTotal  atomic.Int64
	passthroughUsageParseFailureTotal atomic.Int64
)

func recordUsageParseFailure() {
	passthroughUsageParseFailureTotal.Add(1)
}

// SnapshotMetrics
func SnapshotMetrics() MetricsSnapshot {
	return MetricsSnapshot{
		SemanticMutationTotal:  passthroughSemanticMutationTotal.Load(),
		UsageParseFailureTotal: passthroughUsageParseFailureTotal.Load(),
	}
}
