package service

import (
	"context"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrGroupNotFound = infraerrors.NotFound("GROUP_NOT_FOUND", "group not found")
	ErrGroupExists   = infraerrors.Conflict("GROUP_EXISTS", "group name already exists")
)

type GroupRepository interface {
	Create(ctx context.Context, group *Group) error
	GetByID(ctx context.Context, id int64) (*Group, error)
	GetByIDLite(ctx context.Context, id int64) (*Group, error)
	Update(ctx context.Context, group *Group) error
	Delete(ctx context.Context, id int64) error
	DeleteCascade(ctx context.Context, id int64) ([]int64, error)

	List(ctx context.Context, params pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]Group, *pagination.PaginationResult, error)
	ListActive(ctx context.Context) ([]Group, error)
	ListActiveByPlatform(ctx context.Context, platform string) ([]Group, error)

	ExistsByName(ctx context.Context, name string) (bool, error)
	GetAccountCount(ctx context.Context, groupID int64) (total int64, active int64, err error)
	DeleteAccountGroupsByGroupID(ctx context.Context, groupID int64) (int64, error)
	// GetAccountIDsByGroupIDs
	GetAccountIDsByGroupIDs(ctx context.Context, groupIDs []int64) ([]int64, error)
	// BindAccountsToGroup
	BindAccountsToGroup(ctx context.Context, groupID int64, accountIDs []int64) error
	// UpdateSortOrders
	UpdateSortOrders(ctx context.Context, updates []GroupSortOrderUpdate) error
}

// GroupSortOrderUpdate
type GroupSortOrderUpdate struct {
	ID        int64 `json:"id"`
	SortOrder int   `json:"sort_order"`
}

// CreateGroupRequest
type CreateGroupRequest struct {
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	RateMultiplier       float64  `json:"rate_multiplier"`
	IsExclusive          bool     `json:"is_exclusive"`
	AllowImageGeneration bool     `json:"allow_image_generation"`
	ImageRateIndependent bool     `json:"image_rate_independent"`
	ImageRateMultiplier  *float64 `json:"image_rate_multiplier"`
}

// UpdateGroupRequest
type UpdateGroupRequest struct {
	Name                 *string  `json:"name"`
	Description          *string  `json:"description"`
	RateMultiplier       *float64 `json:"rate_multiplier"`
	IsExclusive          *bool    `json:"is_exclusive"`
	Status               *string  `json:"status"`
	AllowImageGeneration *bool    `json:"allow_image_generation"`
	ImageRateIndependent *bool    `json:"image_rate_independent"`
	ImageRateMultiplier  *float64 `json:"image_rate_multiplier"`
}

// GroupService
type GroupService struct {
	groupRepo            GroupRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
}

// NewGroupService
func NewGroupService(groupRepo GroupRepository, authCacheInvalidator APIKeyAuthCacheInvalidator) *GroupService {
	return &GroupService{
		groupRepo:            groupRepo,
		authCacheInvalidator: authCacheInvalidator,
	}
}

// Create
func (s *GroupService) Create(ctx context.Context, req CreateGroupRequest) (*Group, error) {
	imageRateMultiplier := 1.0
	if req.ImageRateMultiplier != nil {
		if *req.ImageRateMultiplier < 0 {
			return nil, fmt.Errorf("image_rate_multiplier must be >= 0")
		}
		imageRateMultiplier = *req.ImageRateMultiplier
	}
	exists, err := s.groupRepo.ExistsByName(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("check group exists: %w", err)
	}
	if exists {
		return nil, ErrGroupExists
	}

	group := &Group{
		Name:                 req.Name,
		Description:          req.Description,
		Platform:             PlatformAnthropic,
		RateMultiplier:       req.RateMultiplier,
		IsExclusive:          req.IsExclusive,
		Status:               StatusActive,
		SubscriptionType:     SubscriptionTypeStandard,
		AllowImageGeneration: req.AllowImageGeneration,
		ImageRateIndependent: req.ImageRateIndependent,
		ImageRateMultiplier:  imageRateMultiplier,
	}

	if err := s.groupRepo.Create(ctx, group); err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}

	return group, nil
}

// GetByID
func (s *GroupService) GetByID(ctx context.Context, id int64) (*Group, error) {
	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}
	return group, nil
}

// List
func (s *GroupService) List(ctx context.Context, params pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	groups, pagination, err := s.groupRepo.List(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list groups: %w", err)
	}
	return groups, pagination, nil
}

// ListActive
func (s *GroupService) ListActive(ctx context.Context) ([]Group, error) {
	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active groups: %w", err)
	}
	return groups, nil
}

// Update
func (s *GroupService) Update(ctx context.Context, id int64, req UpdateGroupRequest) (*Group, error) {
	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}

	if req.Name != nil && *req.Name != group.Name {
		exists, err := s.groupRepo.ExistsByName(ctx, *req.Name)
		if err != nil {
			return nil, fmt.Errorf("check group exists: %w", err)
		}
		if exists {
			return nil, ErrGroupExists
		}
		group.Name = *req.Name
	}

	if req.Description != nil {
		group.Description = *req.Description
	}

	if req.RateMultiplier != nil {
		group.RateMultiplier = *req.RateMultiplier
	}

	if req.IsExclusive != nil {
		group.IsExclusive = *req.IsExclusive
	}

	if req.Status != nil {
		group.Status = *req.Status
	}
	if req.AllowImageGeneration != nil {
		group.AllowImageGeneration = *req.AllowImageGeneration
	}
	if req.ImageRateIndependent != nil {
		group.ImageRateIndependent = *req.ImageRateIndependent
	}
	if req.ImageRateMultiplier != nil {
		if *req.ImageRateMultiplier < 0 {
			return nil, fmt.Errorf("image_rate_multiplier must be >= 0")
		}
		group.ImageRateMultiplier = *req.ImageRateMultiplier
	}

	if err := s.groupRepo.Update(ctx, group); err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, id)
	}

	return group, nil
}

// Delete
func (s *GroupService) Delete(ctx context.Context, id int64) error {
	_, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get group: %w", err)
	}

	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByGroupID(ctx, id)
	}
	if err := s.groupRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}

	return nil
}

// GetStats
func (s *GroupService) GetStats(ctx context.Context, id int64) (map[string]any, error) {
	group, err := s.groupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}

	accountCount, _, err := s.groupRepo.GetAccountCount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get account count: %w", err)
	}

	stats := map[string]any{
		"id":              group.ID,
		"name":            group.Name,
		"rate_multiplier": group.RateMultiplier,
		"is_exclusive":    group.IsExclusive,
		"status":          group.Status,
		"account_count":   accountCount,
	}

	return stats, nil
}
