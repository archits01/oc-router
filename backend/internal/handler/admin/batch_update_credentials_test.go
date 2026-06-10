//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// failingAdminService
type failingAdminService struct {
	*stubAdminService
	failOnAccountID int64
	updateCallCount atomic.Int64
}

func (f *failingAdminService) UpdateAccount(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	f.updateCallCount.Add(1)
	if id == f.failOnAccountID {
		return nil, errors.New("database error")
	}
	return f.stubAdminService.UpdateAccount(ctx, id, input)
}

func setupAccountHandlerWithService(adminSvc service.AdminService) (*gin.Engine, *AccountHandler) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/batch-update-credentials", handler.BatchUpdateCredentials)
	return router, handler
}

func TestBatchUpdateCredentials_AllSuccess(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	body, _ := json.Marshal(BatchUpdateCredentialsRequest{
		AccountIDs: []int64{1, 2, 3},
		Field:      "account_uuid",
		Value:      "test-uuid",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "全部success时应returned 200")
	require.Equal(t, int64(3), svc.updateCallCount.Load(), "应调用 3 次 UpdateAccount")
}

func TestBatchUpdateCredentials_PartialFailure(t *testing.T) {
	// =2）
	svc := &failingAdminService{
		stubAdminService: newStubAdminService(),
		failOnAccountID:  2,
	}
	router, _ := setupAccountHandlerWithService(svc)

	body, _ := json.Marshal(BatchUpdateCredentialsRequest{
		AccountIDs: []int64{1, 2, 3},
		Field:      "org_uuid",
		Value:      "test-org",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	// ""+
	require.Equal(t, http.StatusOK, w.Code, "批量updatereturned 200 + success/failed明细")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	require.Equal(t, float64(2), data["success"], "应有 2 个success")
	require.Equal(t, float64(1), data["failed"], "应有 1 个failed")

	//
	require.Equal(t, int64(3), svc.updateCallCount.Load(),
		"应调用 3 次 UpdateAccount（逐个尝试，failed后继续）")
}

func TestBatchUpdateCredentials_FirstAccountNotFound(t *testing.T) {
	// GetAccount
	svc := &getAccountFailingService{
		stubAdminService: newStubAdminService(),
		failOnAccountID:  1,
	}
	router, _ := setupAccountHandlerWithService(svc)

	body, _ := json.Marshal(BatchUpdateCredentialsRequest{
		AccountIDs: []int64{1, 2, 3},
		Field:      "account_uuid",
		Value:      "test",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "第一阶段validationfailed应returned 404")
}

// getAccountFailingService
type getAccountFailingService struct {
	*stubAdminService
	failOnAccountID int64
}

func (f *getAccountFailingService) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	if id == f.failOnAccountID {
		return nil, errors.New("not found")
	}
	return f.stubAdminService.GetAccount(ctx, id)
}

func TestBatchUpdateCredentials_InterceptWarmupRequests_NonBool(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	// intercept_warmup_requests
	body, _ := json.Marshal(map[string]any{
		"account_ids": []int64{1},
		"field":       "intercept_warmup_requests",
		"value":       "not-a-bool",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code,
		"intercept_warmup_requests 传入非 bool 值应returned 400")
}

func TestBatchUpdateCredentials_InterceptWarmupRequests_ValidBool(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	body, _ := json.Marshal(map[string]any{
		"account_ids": []int64{1},
		"field":       "intercept_warmup_requests",
		"value":       true,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"intercept_warmup_requests 传入合法 bool 值应returned 200")
}

func TestBatchUpdateCredentials_AccountUUID_NonString(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	// account_uuid
	body, _ := json.Marshal(map[string]any{
		"account_ids": []int64{1},
		"field":       "account_uuid",
		"value":       12345,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code,
		"account_uuid 传入非 string 值应returned 400")
}

func TestBatchUpdateCredentials_AccountUUID_NullValue(t *testing.T) {
	svc := &failingAdminService{stubAdminService: newStubAdminService()}
	router, _ := setupAccountHandlerWithService(svc)

	// account_uuid
	body, _ := json.Marshal(map[string]any{
		"account_ids": []int64{1},
		"field":       "account_uuid",
		"value":       nil,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/admin/accounts/batch-update-credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"account_uuid 传入 null 应returned 200")
}
