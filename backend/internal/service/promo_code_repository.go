package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// PromoCodeRepository
type PromoCodeRepository interface {
	//
	Create(ctx context.Context, code *PromoCode) error
	GetByID(ctx context.Context, id int64) (*PromoCode, error)
	GetByCode(ctx context.Context, code string) (*PromoCode, error)
	GetByCodeForUpdate(ctx context.Context, code string) (*PromoCode, error) // 带行锁的query，用于concurrency control
	Update(ctx context.Context, code *PromoCode) error
	Delete(ctx context.Context, id int64) error

	List(ctx context.Context, params pagination.PaginationParams) ([]PromoCode, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, status, search string) ([]PromoCode, *pagination.PaginationResult, error)

	CreateUsage(ctx context.Context, usage *PromoCodeUsage) error
	GetUsageByPromoCodeAndUser(ctx context.Context, promoCodeID, userID int64) (*PromoCodeUsage, error)
	ListUsagesByPromoCode(ctx context.Context, promoCodeID int64, params pagination.PaginationParams) ([]PromoCodeUsage, *pagination.PaginationResult, error)

	IncrementUsedCount(ctx context.Context, id int64) error
}
