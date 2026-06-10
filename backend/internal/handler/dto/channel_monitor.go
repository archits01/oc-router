package dto

// ChannelMonitorExtraModelStatus
//
type ChannelMonitorExtraModelStatus struct {
	Model     string `json:"model"`
	Status    string `json:"status"`
	LatencyMs *int   `json:"latency_ms"`
}
