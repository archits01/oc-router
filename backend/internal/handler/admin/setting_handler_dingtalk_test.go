package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// dingtalkSettingsRepoStub

func newDingTalkSettingsHandler() (*SettingHandler, *settingHandlerRepoStub) {
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	handler := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
	return handler, repo
}

// baseValidDingTalkBody
func baseValidDingTalkBody() map[string]any {
	return map[string]any{
		"dingtalk_connect_enabled":                 true,
		"dingtalk_connect_client_id":               "test-client-id",
		"dingtalk_connect_client_secret":           "test-client-secret",
		"dingtalk_connect_redirect_url":            "https://example.com/auth/dingtalk/callback",
		"dingtalk_connect_corp_restriction_policy": "none",
	}
}

// TestSettingsPUT_DingTalk_V3_InternalOnlyAllowsEmptyCorpID
// internal_only + internal_corp_id="" → 200），
func TestSettingsPUT_DingTalk_V3_InternalOnlyAllowsEmptyCorpID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newDingTalkSettingsHandler()

	body := baseValidDingTalkBody()
	body["dingtalk_connect_corp_restriction_policy"] = "internal_only"
	body["dingtalk_connect_internal_corp_id"] = "" // 空值现在合法

	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
}

// TestSettingsPUT_DingTalk_HappyPath_None → 200
func TestSettingsPUT_DingTalk_HappyPath_None(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newDingTalkSettingsHandler()

	body := baseValidDingTalkBody()
	body["dingtalk_connect_corp_restriction_policy"] = "none"

	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp response.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, data["dingtalk_connect_enabled"])
}

// TestSettingsPUT_DingTalk_HappyPath_InternalOnly_WithCorpID + corp_id → 200
func TestSettingsPUT_DingTalk_HappyPath_InternalOnly_WithCorpID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newDingTalkSettingsHandler()

	body := baseValidDingTalkBody()
	body["dingtalk_connect_corp_restriction_policy"] = "internal_only"
	body["dingtalk_connect_internal_corp_id"] = "ding-corp-123"

	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
}

// TestSettingsPUT_DingTalk_BypassRegistration_RoundTrip +load。
// =internal_only：bypass
func TestSettingsPUT_DingTalk_BypassRegistration_RoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newDingTalkSettingsHandler()

	body := baseValidDingTalkBody()
	body["dingtalk_connect_corp_restriction_policy"] = "internal_only"
	body["dingtalk_connect_bypass_registration"] = true

	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp response.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, data["dingtalk_connect_bypass_registration"])
}

// TestSettingsPUT_DingTalk_Disabled_SkipsValidation → 200。
// =true "Client ID is required when enabled"
// ——=false
func TestSettingsPUT_DingTalk_Disabled_SkipsValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newDingTalkSettingsHandler()

	body := map[string]any{
		"dingtalk_connect_enabled":                 false,
		"dingtalk_connect_client_id":               "", // 这种空值在 enabled=true 时会被 400 拒绝
		"dingtalk_connect_corp_restriction_policy": "internal_only",
	}

	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
}

// TestSettingsPUT_DingTalk_SyncFlags_InternalOnly_RoundTrip +load。
func TestSettingsPUT_DingTalk_SyncFlags_InternalOnly_RoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newDingTalkSettingsHandler()

	body := baseValidDingTalkBody()
	body["dingtalk_connect_corp_restriction_policy"] = "internal_only"
	body["dingtalk_connect_sync_corp_email"] = true
	body["dingtalk_connect_sync_display_name"] = true
	body["dingtalk_connect_sync_dept"] = true

	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp response.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, data["dingtalk_connect_sync_corp_email"], "sync_corp_email should be true for internal_only")
	require.Equal(t, true, data["dingtalk_connect_sync_display_name"], "sync_display_name should be true for internal_only")
	require.Equal(t, true, data["dingtalk_connect_sync_dept"], "sync_dept should be true for internal_only")
}

// TestSettingsPUT_DingTalk_SyncFlags_PolicyNone_CoercedToFalse =none
func TestSettingsPUT_DingTalk_SyncFlags_PolicyNone_CoercedToFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newDingTalkSettingsHandler()

	body := baseValidDingTalkBody()
	body["dingtalk_connect_corp_restriction_policy"] = "none"
	body["dingtalk_connect_sync_corp_email"] = true
	body["dingtalk_connect_sync_display_name"] = true
	body["dingtalk_connect_sync_dept"] = true

	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp response.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, data["dingtalk_connect_sync_corp_email"], "sync_corp_email must be coerced to false when policy=none")
	require.Equal(t, false, data["dingtalk_connect_sync_display_name"], "sync_display_name must be coerced to false when policy=none")
	require.Equal(t, false, data["dingtalk_connect_sync_dept"], "sync_dept must be coerced to false when policy=none")
}

// TestSettingsPUT_DingTalk_StaleWhitelist_CoercedToNone
// admin =whitelist
//
func TestSettingsPUT_DingTalk_StaleWhitelist_CoercedToNone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, repo := newDingTalkSettingsHandler()

	body := baseValidDingTalkBody()
	body["dingtalk_connect_corp_restriction_policy"] = "whitelist"

	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "none", repo.values[service.SettingKeyDingTalkConnectCorpRestrictionPolicy],
		"stale whitelist 应在写入路径被 coerce 为 none")
}

// TestSettingsPUT_DingTalk_SyncAttrKey_RoundTrip +load +
func TestSettingsPUT_DingTalk_SyncAttrKey_RoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("custom_attr_keys_saved", func(t *testing.T) {
		handler, repo := newDingTalkSettingsHandler()

		body := baseValidDingTalkBody()
		body["dingtalk_connect_corp_restriction_policy"] = "internal_only"
		body["dingtalk_connect_sync_corp_email"] = true
		body["dingtalk_connect_sync_display_name"] = true
		body["dingtalk_connect_sync_dept"] = true
		body["dingtalk_connect_sync_corp_email_attr_key"] = "my_email_attr"
		body["dingtalk_connect_sync_display_name_attr_key"] = "my_name_attr"
		body["dingtalk_connect_sync_dept_attr_key"] = "my_dept_attr"

		rawBody, err := json.Marshal(body)
		require.NoError(t, err)

		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.UpdateSettings(c)

		require.Equal(t, http.StatusOK, rec.Code)

		//
		require.Equal(t, "my_email_attr", repo.values[service.SettingKeyDingTalkConnectSyncCorpEmailAttrKey])
		require.Equal(t, "my_name_attr", repo.values[service.SettingKeyDingTalkConnectSyncDisplayNameAttrKey])
		require.Equal(t, "my_dept_attr", repo.values[service.SettingKeyDingTalkConnectSyncDeptAttrKey])

		//
		var resp response.Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		data, ok := resp.Data.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "my_email_attr", data["dingtalk_connect_sync_corp_email_attr_key"])
		require.Equal(t, "my_name_attr", data["dingtalk_connect_sync_display_name_attr_key"])
		require.Equal(t, "my_dept_attr", data["dingtalk_connect_sync_dept_attr_key"])
	})

	t.Run("empty_attr_keys_fallback_to_defaults", func(t *testing.T) {
		handler, repo := newDingTalkSettingsHandler()

		body := baseValidDingTalkBody()
		body["dingtalk_connect_corp_restriction_policy"] = "internal_only"
		// →
		body["dingtalk_connect_sync_corp_email_attr_key"] = ""
		body["dingtalk_connect_sync_display_name_attr_key"] = ""
		body["dingtalk_connect_sync_dept_attr_key"] = ""

		rawBody, err := json.Marshal(body)
		require.NoError(t, err)

		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.UpdateSettings(c)

		require.Equal(t, http.StatusOK, rec.Code)

		//
		require.Equal(t, "dingtalk_email", repo.values[service.SettingKeyDingTalkConnectSyncCorpEmailAttrKey])
		require.Equal(t, "dingtalk_name", repo.values[service.SettingKeyDingTalkConnectSyncDisplayNameAttrKey])
		require.Equal(t, "dingtalk_department", repo.values[service.SettingKeyDingTalkConnectSyncDeptAttrKey])
	})
}
