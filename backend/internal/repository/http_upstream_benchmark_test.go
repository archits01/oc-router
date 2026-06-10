package repository

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// httpClientSink
var httpClientSink *http.Client

// BenchmarkHTTPUpstreamProxyClient
//
//
// - "" ""
// - ""
func BenchmarkHTTPUpstreamProxyClient(b *testing.B) {
	cfg := &config.Config{
		Gateway: config.GatewayConfig{ResponseHeaderTimeout: 300},
	}
	upstream := NewHTTPUpstream(cfg)
	svc, ok := upstream.(*httpUpstreamService)
	if !ok {
		b.Fatalf("类型断言failed，无法获取 httpUpstreamService")
	}

	proxyURL := "http://127.0.0.1:8080"
	b.ReportAllocs() // 报告内存分配统计

	//
	b.Run("新建", func(b *testing.B) {
		parsedProxy, err := url.Parse(proxyURL)
		if err != nil {
			b.Fatalf("parse代理地址failed: %v", err)
		}
		settings := defaultPoolSettings(cfg)
		for i := 0; i < b.N; i++ {
			//
			transport, err := buildUpstreamTransport(settings, parsedProxy, upstreamProtocolModeDefault)
			if err != nil {
				b.Fatalf("create Transport failed: %v", err)
			}
			httpClientSink = &http.Client{
				Transport: transport,
			}
		}
	})

	b.Run("复用", func(b *testing.B) {
		entry, err := svc.getOrCreateClient(proxyURL, 1, 1)
		if err != nil {
			b.Fatalf("getOrCreateClient: %v", err)
		}
		client := entry.client
		b.ResetTimer() // 重置计时器，排除预热时间
		for i := 0; i < b.N; i++ {
			httpClientSink = client
		}
	})
}
