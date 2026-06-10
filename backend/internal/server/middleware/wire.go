package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/wire"
)

// JWTAuthMiddleware JWT
type JWTAuthMiddleware gin.HandlerFunc

// AdminAuthMiddleware
type AdminAuthMiddleware gin.HandlerFunc

// APIKeyAuthMiddleware API Key
type APIKeyAuthMiddleware gin.HandlerFunc

// ProviderSet
var ProviderSet = wire.NewSet(
	NewJWTAuthMiddleware,
	NewAdminAuthMiddleware,
	NewAPIKeyAuthMiddleware,
)
