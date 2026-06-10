//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// stubJWTUserRepo
type stubJWTUserRepo struct {
	service.UserRepository
	users map[int64]*service.User
}

func (r *stubJWTUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	u, ok := r.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}

func (r *stubJWTUserRepo) GetUserAvatar(_ context.Context, _ int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (r *stubJWTUserRepo) UpdateUserLastActiveAt(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

type recordingActivityToucher struct {
	userIDs []int64
}

func (r *recordingActivityToucher) TouchLastActiveForUser(_ context.Context, user *service.User) {
	if user == nil {
		return
	}
	r.userIDs = append(r.userIDs, user.ID)
}

// newJWTTestEnv
//
func newJWTTestEnv(users map[int64]*service.User) (*gin.Engine, *service.AuthService) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.JWT.Secret = "test-jwt-secret-32bytes-long!!!"
	cfg.JWT.AccessTokenExpireMinutes = 60

	userRepo := &stubJWTUserRepo{users: users}
	authSvc := service.NewAuthService(nil, userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userSvc := service.NewUserService(userRepo, nil, nil, nil)
	mw := NewJWTAuthMiddleware(authSvc, userSvc)

	r := gin.New()
	r.Use(gin.HandlerFunc(mw))
	r.GET("/protected", func(c *gin.Context) {
		subject, _ := GetAuthSubjectFromContext(c)
		role, _ := GetUserRoleFromContext(c)
		c.JSON(http.StatusOK, gin.H{
			"user_id": subject.UserID,
			"role":    role,
		})
	})
	return r, authSvc
}

func TestJWTAuth_ValidToken(t *testing.T) {
	user := &service.User{
		ID:           1,
		Email:        "test@example.com",
		Role:         "user",
		Status:       service.StatusActive,
		Concurrency:  5,
		TokenVersion: 1,
	}
	router, authSvc := newJWTTestEnv(map[int64]*service.User{1: user})

	token, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, float64(1), body["user_id"])
	require.Equal(t, "user", body["role"])
}

func TestJWTAuth_ValidToken_LowercaseBearer(t *testing.T) {
	user := &service.User{
		ID:           1,
		Email:        "test@example.com",
		Role:         "user",
		Status:       service.StatusActive,
		Concurrency:  5,
		TokenVersion: 1,
	}
	router, authSvc := newJWTTestEnv(map[int64]*service.User{1: user})

	token, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "bearer "+token)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestJWTAuth_ValidToken_TouchesLastActive(t *testing.T) {
	user := &service.User{
		ID:           1,
		Email:        "test@example.com",
		Role:         "user",
		Status:       service.StatusActive,
		Concurrency:  5,
		TokenVersion: 1,
	}

	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.JWT.Secret = "test-jwt-secret-32bytes-long!!!"
	cfg.JWT.AccessTokenExpireMinutes = 60

	userRepo := &stubJWTUserRepo{users: map[int64]*service.User{1: user}}
	authSvc := service.NewAuthService(nil, userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userSvc := service.NewUserService(userRepo, nil, nil, nil)
	toucher := &recordingActivityToucher{}

	r := gin.New()
	r.Use(jwtAuth(authSvc, userSvc, toucher))
	r.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	token, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []int64{1}, toucher.userIDs)
}

func TestJWTAuth_MissingAuthorizationHeader(t *testing.T) {
	router, _ := newJWTTestEnv(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "UNAUTHORIZED", body.Code)
}

func TestJWTAuth_InvalidHeaderFormat(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"无Bearer前缀", "Token abc123"},
		{"缺少空格分隔", "Bearerabc123"},
		{"仅有单词", "abc123"},
	}
	router, _ := newJWTTestEnv(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", tt.header)
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusUnauthorized, w.Code)
			var body ErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, "INVALID_AUTH_HEADER", body.Code)
		})
	}
}

func TestJWTAuth_EmptyToken(t *testing.T) {
	router, _ := newJWTTestEnv(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer ")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "EMPTY_TOKEN", body.Code)
}

func TestJWTAuth_TamperedToken(t *testing.T) {
	router, _ := newJWTTestEnv(nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyX2lkIjoxfQ.invalid_signature")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "INVALID_TOKEN", body.Code)
}

func TestJWTAuth_UserNotFound(t *testing.T) {
	// =1
	fakeUser := &service.User{
		ID:           999,
		Email:        "ghost@example.com",
		Role:         "user",
		Status:       service.StatusActive,
		TokenVersion: 1,
	}
	//
	router, authSvc := newJWTTestEnv(map[int64]*service.User{})

	token, err := authSvc.GenerateToken(fakeUser)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "USER_NOT_FOUND", body.Code)
}

func TestJWTAuth_UserInactive(t *testing.T) {
	user := &service.User{
		ID:           1,
		Email:        "disabled@example.com",
		Role:         "user",
		Status:       service.StatusDisabled,
		TokenVersion: 1,
	}
	router, authSvc := newJWTTestEnv(map[int64]*service.User{1: user})

	token, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "USER_INACTIVE", body.Code)
}

func TestJWTAuth_TokenVersionMismatch(t *testing.T) {
	// Token =1，=2（
	userForToken := &service.User{
		ID:           1,
		Email:        "test@example.com",
		Role:         "user",
		Status:       service.StatusActive,
		TokenVersion: 1,
	}
	userInDB := &service.User{
		ID:           1,
		Email:        "test@example.com",
		Role:         "user",
		Status:       service.StatusActive,
		TokenVersion: 2, // 密码修改后版本递增
	}
	router, authSvc := newJWTTestEnv(map[int64]*service.User{1: userInDB})

	token, err := authSvc.GenerateToken(userForToken)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "TOKEN_REVOKED", body.Code)
}
