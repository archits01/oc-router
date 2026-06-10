package service

import (
	"context"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrProxyNotFound = infraerrors.NotFound("PROXY_NOT_FOUND", "proxy not found")
	ErrProxyInUse    = infraerrors.Conflict("PROXY_IN_USE", "proxy is in use by accounts")
)

type ProxyRepository interface {
	Create(ctx context.Context, proxy *Proxy) error
	GetByID(ctx context.Context, id int64) (*Proxy, error)
	ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error)
	Update(ctx context.Context, proxy *Proxy) error
	Delete(ctx context.Context, id int64) error

	List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error)
	ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error)
	ListActive(ctx context.Context) ([]Proxy, error)
	ListActiveWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error)

	ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error)
	CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error)
	ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error)

	SweepExpiredProxies(ctx context.Context, now time.Time) (changed int64, err error)
	ListAllForFallback(ctx context.Context) ([]Proxy, error)
	CountExpired(ctx context.Context) (int64, error)
	CountExpiringSoon(ctx context.Context, now time.Time) (int64, error)
}

// CreateProxyRequest
type CreateProxyRequest struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// UpdateProxyRequest
type UpdateProxyRequest struct {
	Name     *string `json:"name"`
	Protocol *string `json:"protocol"`
	Host     *string `json:"host"`
	Port     *int    `json:"port"`
	Username *string `json:"username"`
	Password *string `json:"password"`
	Status   *string `json:"status"`
}

// ProxyService
type ProxyService struct {
	proxyRepo ProxyRepository
}

// NewProxyService
func NewProxyService(proxyRepo ProxyRepository) *ProxyService {
	return &ProxyService{
		proxyRepo: proxyRepo,
	}
}

// Create
func (s *ProxyService) Create(ctx context.Context, req CreateProxyRequest) (*Proxy, error) {
	proxy := &Proxy{
		Name:     req.Name,
		Protocol: req.Protocol,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
		Status:   StatusActive,
	}

	if err := s.proxyRepo.Create(ctx, proxy); err != nil {
		return nil, fmt.Errorf("create proxy: %w", err)
	}

	return proxy, nil
}

// GetByID
func (s *ProxyService) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get proxy: %w", err)
	}
	return proxy, nil
}

// List
func (s *ProxyService) List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	proxies, pagination, err := s.proxyRepo.List(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list proxies: %w", err)
	}
	return proxies, pagination, nil
}

// ListActive
func (s *ProxyService) ListActive(ctx context.Context) ([]Proxy, error) {
	proxies, err := s.proxyRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active proxies: %w", err)
	}
	return proxies, nil
}

// Update
func (s *ProxyService) Update(ctx context.Context, id int64, req UpdateProxyRequest) (*Proxy, error) {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get proxy: %w", err)
	}

	if req.Name != nil {
		proxy.Name = *req.Name
	}

	if req.Protocol != nil {
		proxy.Protocol = *req.Protocol
	}

	if req.Host != nil {
		proxy.Host = *req.Host
	}

	if req.Port != nil {
		proxy.Port = *req.Port
	}

	if req.Username != nil {
		proxy.Username = *req.Username
	}

	if req.Password != nil {
		proxy.Password = *req.Password
	}

	if req.Status != nil {
		proxy.Status = *req.Status
	}

	if err := s.proxyRepo.Update(ctx, proxy); err != nil {
		return nil, fmt.Errorf("update proxy: %w", err)
	}

	return proxy, nil
}

// Delete
func (s *ProxyService) Delete(ctx context.Context, id int64) error {
	_, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get proxy: %w", err)
	}

	if err := s.proxyRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete proxy: %w", err)
	}

	return nil
}

// TestConnection
func (s *ProxyService) TestConnection(ctx context.Context, id int64) error {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get proxy: %w", err)
	}

	// TODO:
	_ = proxy

	return nil
}

// GetURL
func (s *ProxyService) GetURL(ctx context.Context, id int64) (string, error) {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("get proxy: %w", err)
	}

	return proxy.URL(), nil
}
