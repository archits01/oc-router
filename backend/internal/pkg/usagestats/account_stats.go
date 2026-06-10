package usagestats

// AccountStats
//
// cost: * account_rate_multiplier）
// standard_cost:
// user_cost:
type AccountStats struct {
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	Cost         float64 `json:"cost"`
	StandardCost float64 `json:"standard_cost"`
	UserCost     float64 `json:"user_cost"`
}
