//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsImageGenerationModel_GeminiProImage
func TestIsImageGenerationModel_GeminiProImage(t *testing.T) {
	require.True(t, isImageGenerationModel("gemini-3-pro-image"))
	require.True(t, isImageGenerationModel("gemini-3-pro-image-preview"))
	require.True(t, isImageGenerationModel("models/gemini-3-pro-image"))
}

// TestIsImageGenerationModel_GeminiFlashImage
func TestIsImageGenerationModel_GeminiFlashImage(t *testing.T) {
	require.True(t, isImageGenerationModel("gemini-2.5-flash-image"))
	require.True(t, isImageGenerationModel("gemini-2.5-flash-image-preview"))
}

// TestIsImageGenerationModel_RegularModel
func TestIsImageGenerationModel_RegularModel(t *testing.T) {
	require.False(t, isImageGenerationModel("claude-3-opus"))
	require.False(t, isImageGenerationModel("claude-sonnet-4-20250514"))
	require.False(t, isImageGenerationModel("gpt-4o"))
	require.False(t, isImageGenerationModel("gemini-2.5-pro")) // 非图片model
	require.False(t, isImageGenerationModel("gemini-2.5-flash"))
	require.False(t, isImageGenerationModel("my-gemini-3-pro-image-test"))
	require.False(t, isImageGenerationModel("custom-gemini-2.5-flash-image-wrapper"))
}

// TestIsImageGenerationModel_CaseInsensitive
func TestIsImageGenerationModel_CaseInsensitive(t *testing.T) {
	require.True(t, isImageGenerationModel("GEMINI-3-PRO-IMAGE"))
	require.True(t, isImageGenerationModel("Gemini-3-Pro-Image"))
	require.True(t, isImageGenerationModel("GEMINI-2.5-FLASH-IMAGE"))
}

// TestExtractImageSize_ValidSizes
func TestExtractImageSize_ValidSizes(t *testing.T) {
	svc := &AntigravityGatewayService{}

	// 1K
	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"1K"}}}`)
	require.Equal(t, "1K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))

	// 2K
	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"2K"}}}`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))

	// 4K
	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"4K"}}}`)
	require.Equal(t, "4K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))
}

// TestExtractImageSize_CaseInsensitive
func TestExtractImageSize_CaseInsensitive(t *testing.T) {
	svc := &AntigravityGatewayService{}

	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"1k"}}}`)
	require.Equal(t, "1K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))

	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"4k"}}}`)
	require.Equal(t, "4K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))
}

// TestExtractImageSize_Default
func TestExtractImageSize_Default(t *testing.T) {
	svc := &AntigravityGatewayService{}

	//
	body := []byte(`{"contents":[]}`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))

	//
	body = []byte(`{"generationConfig":{"temperature":0.7}}`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))

	//
	body = []byte(`{"generationConfig":{"imageConfig":{}}}`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))
}

// TestExtractImageSize_InvalidJSON
func TestExtractImageSize_InvalidJSON(t *testing.T) {
	svc := &AntigravityGatewayService{}

	body := []byte(`not valid json`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))

	body = []byte(`{"broken":`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))
}

// TestExtractImageSize_EmptySize
func TestExtractImageSize_EmptySize(t *testing.T) {
	svc := &AntigravityGatewayService{}

	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":""}}}`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))

	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"   "}}}`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))
}

// TestExtractImageSize_InvalidSize
func TestExtractImageSize_InvalidSize(t *testing.T) {
	svc := &AntigravityGatewayService{}

	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"3K"}}}`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))

	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"8K"}}}`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))

	body = []byte(`{"generationConfig":{"imageConfig":{"imageSize":"invalid"}}}`)
	require.Equal(t, "2K", NormalizeImageBillingTierOrDefault(svc.extractImageInputSize(body)))
}
