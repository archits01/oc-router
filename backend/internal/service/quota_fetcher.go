package service

import (
	"context"
)

// QuotaFetcher
type QuotaFetcher interface {
	// CanFetch
	CanFetch(account *Account) bool
	// FetchQuota
	FetchQuota(ctx context.Context, account *Account, proxyURL string) (*QuotaResult, error)
}

// QuotaResult
type QuotaResult struct {
	UsageInfo *UsageInfo     // 转换后的使用info
	Raw       map[string]any // 原始响应，可存入 account.Extra
}
