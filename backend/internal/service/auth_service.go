package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials      = infraerrors.Unauthorized("INVALID_CREDENTIALS", "invalid email or password")
	ErrUserNotActive           = infraerrors.Forbidden("USER_NOT_ACTIVE", "user is not active")
	ErrEmailExists             = infraerrors.Conflict("EMAIL_EXISTS", "email already exists")
	ErrEmailReserved           = infraerrors.BadRequest("EMAIL_RESERVED", "email is reserved")
	ErrInvalidToken            = infraerrors.Unauthorized("INVALID_TOKEN", "invalid token")
	ErrTokenExpired            = infraerrors.Unauthorized("TOKEN_EXPIRED", "token has expired")
	ErrAccessTokenExpired      = infraerrors.Unauthorized("ACCESS_TOKEN_EXPIRED", "access token has expired")
	ErrTokenTooLarge           = infraerrors.BadRequest("TOKEN_TOO_LARGE", "token too large")
	ErrTokenRevoked            = infraerrors.Unauthorized("TOKEN_REVOKED", "token has been revoked")
	ErrRefreshTokenInvalid     = infraerrors.Unauthorized("REFRESH_TOKEN_INVALID", "invalid refresh token")
	ErrRefreshTokenExpired     = infraerrors.Unauthorized("REFRESH_TOKEN_EXPIRED", "refresh token has expired")
	ErrRefreshTokenReused      = infraerrors.Unauthorized("REFRESH_TOKEN_REUSED", "refresh token has been reused")
	ErrEmailVerifyRequired     = infraerrors.BadRequest("EMAIL_VERIFY_REQUIRED", "email verification is required")
	ErrEmailSuffixNotAllowed   = infraerrors.BadRequest("EMAIL_SUFFIX_NOT_ALLOWED", "email suffix is not allowed")
	ErrRegDisabled             = infraerrors.Forbidden("REGISTRATION_DISABLED", "registration is currently disabled")
	ErrServiceUnavailable      = infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "service temporarily unavailable")
	ErrInvitationCodeRequired  = infraerrors.BadRequest("INVITATION_CODE_REQUIRED", "invitation code is required")
	ErrInvitationCodeInvalid   = infraerrors.BadRequest("INVITATION_CODE_INVALID", "invalid or used invitation code")
	ErrOAuthInvitationRequired = infraerrors.Forbidden("OAUTH_INVITATION_REQUIRED", "invitation code required to complete oauth registration")
)

// maxTokenLength
const maxTokenLength = 8192

// refreshTokenPrefix is the prefix for refresh tokens to distinguish them from access tokens.
const refreshTokenPrefix = "rt_"

// JWTClaims JWT
type JWTClaims struct {
	UserID       int64  `json:"user_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	TokenVersion int64  `json:"token_version"` // Used to invalidate tokens on password change
	jwt.RegisteredClaims
}

// AuthService
type AuthService struct {
	entClient             *dbent.Client
	userRepo              UserRepository
	redeemRepo            RedeemCodeRepository
	refreshTokenCache     RefreshTokenCache
	cfg                   *config.Config
	settingService        *SettingService
	emailService          *EmailService
	turnstileService      *TurnstileService
	emailQueueService     *EmailQueueService
	promoService          *PromoService
	affiliateService      *AffiliateService
	defaultSubAssigner    DefaultSubscriptionAssigner
	userPlatformQuotaRepo UserPlatformQuotaRepository
}

type DefaultSubscriptionAssigner interface {
	AssignOrExtendSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error)
}

type signupGrantPlan struct {
	Balance        float64
	Concurrency    int
	Subscriptions  []DefaultSubscriptionSetting
	PlatformQuotas map[string]*DefaultPlatformQuotaSetting
}

// NewAuthService
func NewAuthService(
	entClient *dbent.Client,
	userRepo UserRepository,
	redeemRepo RedeemCodeRepository,
	refreshTokenCache RefreshTokenCache,
	cfg *config.Config,
	settingService *SettingService,
	emailService *EmailService,
	turnstileService *TurnstileService,
	emailQueueService *EmailQueueService,
	promoService *PromoService,
	defaultSubAssigner DefaultSubscriptionAssigner,
	affiliateService *AffiliateService,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
) *AuthService {
	return &AuthService{
		entClient:             entClient,
		userRepo:              userRepo,
		redeemRepo:            redeemRepo,
		refreshTokenCache:     refreshTokenCache,
		cfg:                   cfg,
		settingService:        settingService,
		emailService:          emailService,
		turnstileService:      turnstileService,
		emailQueueService:     emailQueueService,
		promoService:          promoService,
		affiliateService:      affiliateService,
		defaultSubAssigner:    defaultSubAssigner,
		userPlatformQuotaRepo: userPlatformQuotaRepo,
	}
}

func (s *AuthService) EntClient() *dbent.Client {
	if s == nil {
		return nil
	}
	return s.entClient
}

// Register
func (s *AuthService) Register(ctx context.Context, email, password string) (string, *User, error) {
	return s.RegisterWithVerification(ctx, email, password, "", "", "", "")
}

// RegisterWithVerification
func (s *AuthService) RegisterWithVerification(ctx context.Context, email, password, verifyCode, promoCode, invitationCode, affiliateCode string) (string, *User, error) {
	//
	if s.settingService == nil || !s.settingService.IsRegistrationEnabled(ctx) {
		return "", nil, ErrRegDisabled
	}

	//
	if isReservedEmail(email) {
		return "", nil, ErrEmailReserved
	}
	if err := s.validateRegistrationEmailPolicy(ctx, email); err != nil {
		return "", nil, err
	}

	var invitationRedeemCode *RedeemCode
	if s.settingService != nil && s.settingService.IsInvitationCodeEnabled(ctx) {
		if invitationCode == "" {
			return "", nil, ErrInvitationCodeRequired
		}
		redeemCode, err := s.redeemRepo.GetByCode(ctx, invitationCode)
		if err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Invalid invitation code: %s, error: %v", invitationCode, err)
			return "", nil, ErrInvitationCodeInvalid
		}
		if redeemCode.Type != RedeemTypeInvitation || !redeemCode.CanUse() {
			logger.LegacyPrintf("service.auth", "[Auth] Invitation code invalid: type=%s, status=%s", redeemCode.Type, redeemCode.Status)
			return "", nil, ErrInvitationCodeInvalid
		}
		invitationRedeemCode = redeemCode
	}

	if s.settingService != nil && s.settingService.IsEmailVerifyEnabled(ctx) {
		if s.emailService == nil {
			logger.LegacyPrintf("service.auth", "%s", "[Auth] Email verification enabled but email service not configured, rejecting registration")
			return "", nil, ErrServiceUnavailable
		}
		if verifyCode == "" {
			return "", nil, ErrEmailVerifyRequired
		}
		if err := s.emailService.VerifyCode(ctx, email, verifyCode); err != nil {
			return "", nil, fmt.Errorf("verify code: %w", err)
		}
	}

	existsEmail, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Database error checking email exists: %v", err)
		return "", nil, ErrServiceUnavailable
	}
	if existsEmail {
		return "", nil, ErrEmailExists
	}

	hashedPassword, err := s.HashPassword(password)
	if err != nil {
		return "", nil, fmt.Errorf("hash password: %w", err)
	}

	grantPlan := s.resolveSignupGrantPlan(ctx, "email")

	// =
	var defaultRPMLimit int
	if s.settingService != nil {
		defaultRPMLimit = s.settingService.GetDefaultUserRPMLimit(ctx)
	}

	user := &User{
		Email:        email,
		PasswordHash: hashedPassword,
		Role:         RoleUser,
		Balance:      grantPlan.Balance,
		Concurrency:  grantPlan.Concurrency,
		RPMLimit:     defaultRPMLimit,
		Status:       StatusActive,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, ErrEmailExists) {
			return "", nil, ErrEmailExists
		}
		logger.LegacyPrintf("service.auth", "[Auth] Database error creating user: %v", err)
		return "", nil, ErrServiceUnavailable
	}
	s.postAuthUserBootstrap(ctx, user, "email", true)
	s.assignSubscriptions(ctx, user.ID, grantPlan.Subscriptions, "auto assigned by signup defaults")
	// snapshot user × platform quota（fail-open）
	_ = s.snapshotPlatformQuotaDefaults(ctx, user.ID, &grantPlan)
	if s.affiliateService != nil {
		if _, err := s.affiliateService.EnsureUserAffiliate(ctx, user.ID); err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to initialize affiliate profile for user %d: %v", user.ID, err)
		}
		if code := strings.TrimSpace(affiliateCode); code != "" {
			if err := s.affiliateService.BindInviterByCode(ctx, user.ID, code); err != nil {
				logger.LegacyPrintf("service.auth", "[Auth] Failed to bind affiliate inviter for user %d: %v", user.ID, err)
			}
		}
	}

	if invitationRedeemCode != nil {
		if err := s.redeemRepo.Use(ctx, invitationRedeemCode.ID, user.ID); err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to mark invitation code as used for user %d: %v", user.ID, err)
		}
	}
	if promoCode != "" && s.promoService != nil && s.settingService != nil && s.settingService.IsPromoCodeEnabled(ctx) {
		if err := s.promoService.ApplyPromoCode(ctx, user.ID, promoCode); err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to apply promo code for user %d: %v", user.ID, err)
		} else {
			if updatedUser, err := s.userRepo.GetByID(ctx, user.ID); err == nil {
				user = updatedUser
			}
		}
	}

	//
	token, err := s.GenerateToken(user)
	if err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}

	return token, user, nil
}

// SendVerifyCodeResult
type SendVerifyCodeResult struct {
	Countdown int `json:"countdown"` // 倒计时seconds数
}

// SendVerifyCode
func (s *AuthService) SendVerifyCode(ctx context.Context, email string, locale ...string) error {
	if s.settingService == nil || !s.settingService.IsRegistrationEnabled(ctx) {
		return ErrRegDisabled
	}

	if isReservedEmail(email) {
		return ErrEmailReserved
	}
	if err := s.validateRegistrationEmailPolicy(ctx, email); err != nil {
		return err
	}

	existsEmail, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Database error checking email exists: %v", err)
		return ErrServiceUnavailable
	}
	if existsEmail {
		return ErrEmailExists
	}

	if s.emailService == nil {
		return errors.New("email service not configured")
	}

	siteName := "Sub2API"
	if s.settingService != nil {
		siteName = s.settingService.GetSiteName(ctx)
	}

	return s.emailService.SendVerifyCode(ctx, email, siteName, firstEmailLocale(locale))
}

// SendVerifyCodeAsync
func (s *AuthService) SendVerifyCodeAsync(ctx context.Context, email string, locale ...string) (*SendVerifyCodeResult, error) {
	logger.LegacyPrintf("service.auth", "[Auth] SendVerifyCodeAsync called for email: %s", email)

	if s.settingService == nil || !s.settingService.IsRegistrationEnabled(ctx) {
		logger.LegacyPrintf("service.auth", "%s", "[Auth] Registration is disabled")
		return nil, ErrRegDisabled
	}

	if isReservedEmail(email) {
		return nil, ErrEmailReserved
	}
	if err := s.validateRegistrationEmailPolicy(ctx, email); err != nil {
		return nil, err
	}

	existsEmail, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Database error checking email exists: %v", err)
		return nil, ErrServiceUnavailable
	}
	if existsEmail {
		logger.LegacyPrintf("service.auth", "[Auth] Email already exists: %s", email)
		return nil, ErrEmailExists
	}

	if s.emailQueueService == nil {
		logger.LegacyPrintf("service.auth", "%s", "[Auth] Email queue service not configured")
		return nil, errors.New("email queue service not configured")
	}

	siteName := "Sub2API"
	if s.settingService != nil {
		siteName = s.settingService.GetSiteName(ctx)
	}

	logger.LegacyPrintf("service.auth", "[Auth] Enqueueing verify code for: %s", email)
	if err := s.emailQueueService.EnqueueVerifyCode(email, siteName, firstEmailLocale(locale)); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to enqueue: %v", err)
		return nil, fmt.Errorf("enqueue verify code: %w", err)
	}

	logger.LegacyPrintf("service.auth", "[Auth] Verify code enqueued successfully for: %s", email)
	return &SendVerifyCodeResult{
		Countdown: 60, // 60seconds倒计时
	}, nil
}

// VerifyTurnstileForRegister
//
//
func (s *AuthService) VerifyTurnstileForRegister(ctx context.Context, token, remoteIP, verifyCode string) error {
	if s.IsEmailVerifyEnabled(ctx) && strings.TrimSpace(verifyCode) != "" {
		logger.LegacyPrintf("service.auth", "%s", "[Auth] Email verify flow detected, skip duplicate Turnstile check on register")
		return nil
	}
	return s.VerifyTurnstile(ctx, token, remoteIP)
}

// VerifyTurnstile
func (s *AuthService) VerifyTurnstile(ctx context.Context, token string, remoteIP string) error {
	required := s.cfg != nil && s.cfg.Server.Mode == "release" && s.cfg.Turnstile.Required

	if required {
		if s.settingService == nil {
			logger.LegacyPrintf("service.auth", "%s", "[Auth] Turnstile required but settings service is not configured")
			return ErrTurnstileNotConfigured
		}
		enabled := s.settingService.IsTurnstileEnabled(ctx)
		secretConfigured := s.settingService.GetTurnstileSecretKey(ctx) != ""
		if !enabled || !secretConfigured {
			logger.LegacyPrintf("service.auth", "[Auth] Turnstile required but not configured (enabled=%v, secret_configured=%v)", enabled, secretConfigured)
			return ErrTurnstileNotConfigured
		}
	}

	if s.turnstileService == nil {
		if required {
			logger.LegacyPrintf("service.auth", "%s", "[Auth] Turnstile required but service not configured")
			return ErrTurnstileNotConfigured
		}
		return nil // 服务未configuration则跳过validation
	}

	if !required && s.settingService != nil && s.settingService.IsTurnstileEnabled(ctx) && s.settingService.GetTurnstileSecretKey(ctx) == "" {
		logger.LegacyPrintf("service.auth", "%s", "[Auth] Turnstile enabled but secret key not configured")
	}

	return s.turnstileService.VerifyToken(ctx, token, remoteIP)
}

// IsTurnstileEnabled
func (s *AuthService) IsTurnstileEnabled(ctx context.Context) bool {
	if s.turnstileService == nil {
		return false
	}
	return s.turnstileService.IsEnabled(ctx)
}

// IsRegistrationEnabled
func (s *AuthService) IsRegistrationEnabled(ctx context.Context) bool {
	if s.settingService == nil {
		return false // 安全默认：settingService 未configuration时shutting down注册
	}
	return s.settingService.IsRegistrationEnabled(ctx)
}

// IsEmailVerifyEnabled
func (s *AuthService) IsEmailVerifyEnabled(ctx context.Context) bool {
	if s.settingService == nil {
		return false
	}
	return s.settingService.IsEmailVerifyEnabled(ctx)
}

// Login
func (s *AuthService) Login(ctx context.Context, email, password string) (string, *User, error) {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", nil, ErrInvalidCredentials
		}
		logger.LegacyPrintf("service.auth", "[Auth] Database error during login: %v", err)
		return "", nil, ErrServiceUnavailable
	}

	if !s.CheckPassword(password, user.PasswordHash) {
		return "", nil, ErrInvalidCredentials
	}

	if !user.IsActive() {
		return "", nil, ErrUserNotActive
	}

	//
	token, err := s.GenerateToken(user)
	if err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}

	return token, user, nil
}

// LoginOrRegisterOAuth
//
//
func (s *AuthService) LoginOrRegisterOAuth(ctx context.Context, email, username string) (string, *User, error) {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 255 {
		return "", nil, infraerrors.BadRequest("INVALID_EMAIL", "invalid email")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", nil, infraerrors.BadRequest("INVALID_EMAIL", "invalid email")
	}

	username = strings.TrimSpace(username)
	if len([]rune(username)) > 100 {
		username = string([]rune(username)[:100])
	}

	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// OAuth
			if s.settingService == nil || !s.settingService.IsRegistrationEnabled(ctx) {
				return "", nil, ErrRegDisabled
			}

			randomPassword, err := randomHexString(32)
			if err != nil {
				logger.LegacyPrintf("service.auth", "[Auth] Failed to generate random password for oauth signup: %v", err)
				return "", nil, ErrServiceUnavailable
			}
			hashedPassword, err := s.HashPassword(randomPassword)
			if err != nil {
				return "", nil, fmt.Errorf("hash password: %w", err)
			}

			signupSource := inferLegacySignupSource(email)
			grantPlan := s.resolveSignupGrantPlan(ctx, signupSource)
			var defaultRPMLimit int
			if s.settingService != nil {
				defaultRPMLimit = s.settingService.GetDefaultUserRPMLimit(ctx)
			}

			newUser := &User{
				Email:        email,
				Username:     username,
				PasswordHash: hashedPassword,
				Role:         RoleUser,
				Balance:      grantPlan.Balance,
				Concurrency:  grantPlan.Concurrency,
				RPMLimit:     defaultRPMLimit,
				Status:       StatusActive,
				SignupSource: signupSource,
			}

			if err := s.userRepo.Create(ctx, newUser); err != nil {
				if errors.Is(err, ErrEmailExists) {
					//
					user, err = s.userRepo.GetByEmail(ctx, email)
					if err != nil {
						logger.LegacyPrintf("service.auth", "[Auth] Database error getting user after conflict: %v", err)
						return "", nil, ErrServiceUnavailable
					}
				} else {
					logger.LegacyPrintf("service.auth", "[Auth] Database error creating oauth user: %v", err)
					return "", nil, ErrServiceUnavailable
				}
			} else {
				user = newUser
				s.postAuthUserBootstrap(ctx, user, signupSource, false)
				s.assignSubscriptions(ctx, user.ID, grantPlan.Subscriptions, "auto assigned by signup defaults")
				// snapshot user × platform quota（fail-open）
				_ = s.snapshotPlatformQuotaDefaults(ctx, user.ID, &grantPlan)
			}
		} else {
			logger.LegacyPrintf("service.auth", "[Auth] Database error during oauth login: %v", err)
			return "", nil, ErrServiceUnavailable
		}
	}

	if !user.IsActive() {
		return "", nil, ErrUserNotActive
	}

	if user.Username == "" && username != "" {
		user.Username = username
		if err := s.userRepo.Update(ctx, user); err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to update username after oauth login: %v", err)
		}
	}
	token, err := s.GenerateToken(user)
	if err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	return token, user, nil
}

// canBypassRegistrationDisabledForOAuth
// dingtalk_connect_bypass_registration=true
func (s *AuthService) canBypassRegistrationDisabledForOAuth(ctx context.Context, signupSource string) bool {
	if signupSource != "dingtalk" {
		return false
	}
	cfg, err := s.settingService.GetDingTalkConnectOAuthConfig(ctx)
	if err != nil || !cfg.Enabled || !cfg.BypassRegistration {
		return false
	}
	return cfg.CorpRestrictionPolicy == "internal_only"
}

// LoginOrRegisterOAuthWithTokenPair
//
// invitationCode
// affiliateCode
// signupSource "dingtalk"/"linuxdo"/"wechat"/"oidc"
func (s *AuthService) LoginOrRegisterOAuthWithTokenPair(ctx context.Context, email, username, invitationCode, affiliateCode, signupSource string) (*TokenPair, *User, error) {
	//
	if s.refreshTokenCache == nil {
		return nil, nil, errors.New("refresh token cache not configured")
	}

	email = strings.TrimSpace(email)
	if email == "" || len(email) > 255 {
		return nil, nil, infraerrors.BadRequest("INVALID_EMAIL", "invalid email")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, nil, infraerrors.BadRequest("INVALID_EMAIL", "invalid email")
	}

	username = strings.TrimSpace(username)
	if len([]rune(username)) > 100 {
		username = string([]rune(username)[:100])
	}

	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// OAuth
			if s.settingService == nil || (!s.settingService.IsRegistrationEnabled(ctx) && !s.canBypassRegistrationDisabledForOAuth(ctx, signupSource)) {
				return nil, nil, ErrRegDisabled
			}

			var invitationRedeemCode *RedeemCode
			if s.settingService != nil && s.settingService.IsInvitationCodeEnabled(ctx) {
				if invitationCode == "" {
					return nil, nil, ErrOAuthInvitationRequired
				}
				redeemCode, err := s.redeemRepo.GetByCode(ctx, invitationCode)
				if err != nil {
					return nil, nil, ErrInvitationCodeInvalid
				}
				if redeemCode.Type != RedeemTypeInvitation || !redeemCode.CanUse() {
					return nil, nil, ErrInvitationCodeInvalid
				}
				invitationRedeemCode = redeemCode
			}

			randomPassword, err := randomHexString(32)
			if err != nil {
				logger.LegacyPrintf("service.auth", "[Auth] Failed to generate random password for oauth signup: %v", err)
				return nil, nil, ErrServiceUnavailable
			}
			hashedPassword, err := s.HashPassword(randomPassword)
			if err != nil {
				return nil, nil, fmt.Errorf("hash password: %w", err)
			}

			// "dingtalk" / "linuxdo" / "oidc" / "wechat"），
			// ——"email"
			if strings.TrimSpace(signupSource) == "" {
				signupSource = inferLegacySignupSource(email)
			}
			grantPlan := s.resolveSignupGrantPlan(ctx, signupSource)
			var defaultRPMLimit int
			if s.settingService != nil {
				defaultRPMLimit = s.settingService.GetDefaultUserRPMLimit(ctx)
			}

			newUser := &User{
				Email:        email,
				Username:     username,
				PasswordHash: hashedPassword,
				Role:         RoleUser,
				Balance:      grantPlan.Balance,
				Concurrency:  grantPlan.Concurrency,
				RPMLimit:     defaultRPMLimit,
				Status:       StatusActive,
				SignupSource: signupSource,
			}

			if s.entClient != nil && invitationRedeemCode != nil {
				tx, err := s.entClient.Tx(ctx)
				if err != nil {
					logger.LegacyPrintf("service.auth", "[Auth] Failed to begin transaction for oauth registration: %v", err)
					return nil, nil, ErrServiceUnavailable
				}
				defer func() { _ = tx.Rollback() }()
				txCtx := dbent.NewTxContext(ctx, tx)

				if err := s.userRepo.Create(txCtx, newUser); err != nil {
					if errors.Is(err, ErrEmailExists) {
						user, err = s.userRepo.GetByEmail(ctx, email)
						if err != nil {
							logger.LegacyPrintf("service.auth", "[Auth] Database error getting user after conflict: %v", err)
							return nil, nil, ErrServiceUnavailable
						}
					} else {
						logger.LegacyPrintf("service.auth", "[Auth] Database error creating oauth user: %v", err)
						return nil, nil, ErrServiceUnavailable
					}
				} else {
					if err := s.redeemRepo.Use(txCtx, invitationRedeemCode.ID, newUser.ID); err != nil {
						return nil, nil, ErrInvitationCodeInvalid
					}
					if err := tx.Commit(); err != nil {
						logger.LegacyPrintf("service.auth", "[Auth] Failed to commit oauth registration transaction: %v", err)
						return nil, nil, ErrServiceUnavailable
					}
					user = newUser
					s.postAuthUserBootstrap(ctx, user, signupSource, false)
					s.assignSubscriptions(ctx, user.ID, grantPlan.Subscriptions, "auto assigned by signup defaults")
					// snapshot user × platform quota（fail-open）
					_ = s.snapshotPlatformQuotaDefaults(ctx, user.ID, &grantPlan)
					s.bindOAuthAffiliate(ctx, user.ID, affiliateCode)
				}
			} else {
				if err := s.userRepo.Create(ctx, newUser); err != nil {
					if errors.Is(err, ErrEmailExists) {
						user, err = s.userRepo.GetByEmail(ctx, email)
						if err != nil {
							logger.LegacyPrintf("service.auth", "[Auth] Database error getting user after conflict: %v", err)
							return nil, nil, ErrServiceUnavailable
						}
					} else {
						logger.LegacyPrintf("service.auth", "[Auth] Database error creating oauth user: %v", err)
						return nil, nil, ErrServiceUnavailable
					}
				} else {
					user = newUser
					s.postAuthUserBootstrap(ctx, user, signupSource, false)
					s.assignSubscriptions(ctx, user.ID, grantPlan.Subscriptions, "auto assigned by signup defaults")
					// snapshot user × platform quota（fail-open）
					_ = s.snapshotPlatformQuotaDefaults(ctx, user.ID, &grantPlan)
					s.bindOAuthAffiliate(ctx, user.ID, affiliateCode)
					if invitationRedeemCode != nil {
						if err := s.redeemRepo.Use(ctx, invitationRedeemCode.ID, user.ID); err != nil {
							return nil, nil, ErrInvitationCodeInvalid
						}
					}
				}
			}
		} else {
			logger.LegacyPrintf("service.auth", "[Auth] Database error during oauth login: %v", err)
			return nil, nil, ErrServiceUnavailable
		}
	}

	if !user.IsActive() {
		return nil, nil, ErrUserNotActive
	}

	if user.Username == "" && username != "" {
		user.Username = username
		if err := s.userRepo.Update(ctx, user); err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to update username after oauth login: %v", err)
		}
	}
	tokenPair, err := s.GenerateTokenPair(ctx, user, "")
	if err != nil {
		return nil, nil, fmt.Errorf("generate token pair: %w", err)
	}
	return tokenPair, user, nil
}

func (s *AuthService) assignSubscriptions(ctx context.Context, userID int64, items []DefaultSubscriptionSetting, notes string) {
	if s.settingService == nil || s.defaultSubAssigner == nil || userID <= 0 {
		return
	}
	for _, item := range items {
		if _, _, err := s.defaultSubAssigner.AssignOrExtendSubscription(ctx, &AssignSubscriptionInput{
			UserID:       userID,
			GroupID:      item.GroupID,
			ValidityDays: item.ValidityDays,
			Notes:        notes,
		}); err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to assign default subscription: user_id=%d group_id=%d err=%v", userID, item.GroupID, err)
		}
	}
}

func (s *AuthService) resolveSignupGrantPlan(ctx context.Context, signupSource string) signupGrantPlan {
	plan := signupGrantPlan{}
	if s != nil && s.cfg != nil {
		plan.Balance = s.cfg.Default.UserBalance
		plan.Concurrency = s.cfg.Default.UserConcurrency
	}
	if s == nil || s.settingService == nil {
		return plan
	}

	plan.Balance = s.settingService.GetDefaultBalance(ctx)
	plan.Concurrency = s.settingService.GetDefaultConcurrency(ctx)
	plan.Subscriptions = s.settingService.GetDefaultSubscriptions(ctx)

	// ============ ============
	// !enabled
	if quotas, err := s.settingService.GetDefaultPlatformQuotas(ctx); err == nil {
		plan.PlatformQuotas = quotas
	} else {
		logger.LegacyPrintf("service.auth", "[Auth] Warning: load default platform quotas failed: %v (fail-open)", err)
	}
	// ============================================================================================

	resolved, enabled, err := s.settingService.ResolveAuthSourceGrantSettings(ctx, signupSource, false)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to load auth source signup defaults for %s: %v", signupSource, err)
		return plan
	}
	if !enabled {
		return plan // plan.PlatformQuotas 已含全局层
	}

	plan.Balance = resolved.Balance
	plan.Concurrency = resolved.Concurrency
	plan.Subscriptions = resolved.Subscriptions

	// ============ auth source quota merge（============
	asQuotas := s.settingService.GetAuthSourcePlatformQuotas(ctx, signupSource)
	if plan.PlatformQuotas != nil {
		for platform, patch := range asQuotas {
			if dst := plan.PlatformQuotas[platform]; dst != nil {
				mergePlatformQuotaDefaults(dst, patch)
			}
		}
	}
	// ==============================================================================

	return plan
}

func authSourceSignupSettings(defaults *AuthSourceDefaultSettings, signupSource string) (ProviderDefaultGrantSettings, bool) {
	if defaults == nil {
		return ProviderDefaultGrantSettings{}, false
	}

	switch strings.ToLower(strings.TrimSpace(signupSource)) {
	case "email":
		return defaults.Email, true
	case "linuxdo":
		return defaults.LinuxDo, true
	case "oidc":
		return defaults.OIDC, true
	case "wechat":
		return defaults.WeChat, true
	case "github":
		return defaults.GitHub, true
	case "google":
		return defaults.Google, true
	case "dingtalk":
		return defaults.DingTalk, true
	default:
		return ProviderDefaultGrantSettings{}, false
	}
}

// bindOAuthAffiliate initializes the affiliate profile and binds the inviter
// for an OAuth-registered user. Failures are logged but never block registration.
func (s *AuthService) bindOAuthAffiliate(ctx context.Context, userID int64, affiliateCode string) {
	if s.affiliateService == nil || userID <= 0 {
		return
	}
	if _, err := s.affiliateService.EnsureUserAffiliate(ctx, userID); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to initialize affiliate profile for user %d: %v", userID, err)
	}
	if code := strings.TrimSpace(affiliateCode); code != "" {
		if err := s.affiliateService.BindInviterByCode(ctx, userID, code); err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to bind affiliate inviter for user %d: %v", userID, err)
		}
	}
}

func (s *AuthService) postAuthUserBootstrap(ctx context.Context, user *User, signupSource string, touchLogin bool) {
	if user == nil || user.ID <= 0 {
		return
	}

	if strings.TrimSpace(signupSource) == "" {
		signupSource = "email"
	}
	s.updateUserSignupSource(ctx, user.ID, signupSource)

	if touchLogin {
		s.touchUserLogin(ctx, user.ID)
	}
}

func (s *AuthService) updateUserSignupSource(ctx context.Context, userID int64, signupSource string) {
	if s == nil || s.entClient == nil || userID <= 0 {
		return
	}
	if strings.TrimSpace(signupSource) == "" {
		return
	}
	if err := s.entClient.User.UpdateOneID(userID).
		SetSignupSource(signupSource).
		Exec(ctx); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to update signup source: user_id=%d source=%s err=%v", userID, signupSource, err)
	}
}

func (s *AuthService) touchUserLogin(ctx context.Context, userID int64) {
	if s == nil || s.entClient == nil || userID <= 0 {
		return
	}
	now := time.Now().UTC()
	if err := s.entClient.User.UpdateOneID(userID).
		SetLastLoginAt(now).
		SetLastActiveAt(now).
		Exec(ctx); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to touch login timestamps: user_id=%d err=%v", userID, err)
	}
}

func (s *AuthService) backfillEmailIdentityOnSuccessfulLogin(ctx context.Context, user *User) {
	if s == nil || user == nil || user.ID <= 0 {
		return
	}
	identity, created := s.ensureEmailAuthIdentity(ctx, user, "auth_service_login_backfill")
	if s.shouldApplyEmailFirstBindDefaults(ctx, user.ID, identity, created) {
		if err := s.ApplyProviderDefaultSettingsOnFirstBind(ctx, user.ID, "email"); err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to apply email first bind defaults: user_id=%d err=%v", user.ID, err)
		}
	}
}

func (s *AuthService) shouldApplyEmailFirstBindDefaults(
	ctx context.Context,
	userID int64,
	identity *dbent.AuthIdentity,
	created bool,
) bool {
	source := emailAuthIdentitySource(identity.Metadata)
	if source == "auth_service_login_backfill" {
		return false
	}
	if created {
		return true
	}
	if s == nil || s.entClient == nil || userID <= 0 || identity == nil || identity.UserID != userID {
		return false
	}
	if source != "auth_service_dual_write" {
		return false
	}

	hasGrant, err := s.hasProviderGrantRecord(ctx, userID, "email", "first_bind")
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to inspect email first bind grant state: user_id=%d err=%v", userID, err)
		return false
	}
	return !hasGrant
}

func emailAuthIdentitySource(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata["source"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func (s *AuthService) hasProviderGrantRecord(
	ctx context.Context,
	userID int64,
	providerType string,
	grantReason string,
) (bool, error) {
	if s == nil || s.entClient == nil || userID <= 0 {
		return false, nil
	}

	rows, err := s.entClient.QueryContext(
		ctx,
		`SELECT 1 FROM user_provider_default_grants WHERE user_id = $1 AND provider_type = $2 AND grant_reason = $3 LIMIT 1`,
		userID,
		strings.TrimSpace(providerType),
		strings.TrimSpace(grantReason),
	)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	return rows.Next(), rows.Err()
}

func (s *AuthService) ensureEmailAuthIdentity(ctx context.Context, user *User, source string) (*dbent.AuthIdentity, bool) {
	if s == nil || s.entClient == nil || user == nil || user.ID <= 0 {
		return nil, false
	}

	email := strings.ToLower(strings.TrimSpace(user.Email))
	if email == "" || isReservedEmail(email) {
		return nil, false
	}
	if strings.TrimSpace(source) == "" {
		source = "auth_service_dual_write"
	}

	client := s.entClient
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}

	buildQuery := func() *dbent.AuthIdentityQuery {
		return client.AuthIdentity.Query().Where(
			authidentity.ProviderTypeEQ("email"),
			authidentity.ProviderKeyEQ("email"),
			authidentity.ProviderSubjectEQ(email),
		)
	}

	existed, err := buildQuery().Exist(ctx)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to inspect email auth identity: user_id=%d email=%s err=%v", user.ID, email, err)
		return nil, false
	}

	if !existed {
		if err := client.AuthIdentity.Create().
			SetUserID(user.ID).
			SetProviderType("email").
			SetProviderKey("email").
			SetProviderSubject(email).
			SetVerifiedAt(time.Now().UTC()).
			SetMetadata(map[string]any{
				"source": strings.TrimSpace(source),
			}).
			OnConflictColumns(
				authidentity.FieldProviderType,
				authidentity.FieldProviderKey,
				authidentity.FieldProviderSubject,
			).
			DoNothing().
			Exec(ctx); err != nil {
			if isSQLNoRowsError(err) {
				return nil, false
			}
		}
		if err != nil {
			logger.LegacyPrintf("service.auth", "[Auth] Failed to ensure email auth identity: user_id=%d email=%s err=%v", user.ID, email, err)
			return nil, false
		}
	}

	identity, err := buildQuery().Only(ctx)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to reload email auth identity: user_id=%d email=%s err=%v", user.ID, email, err)
		return nil, false
	}
	if identity.UserID != user.ID {
		logger.LegacyPrintf("service.auth", "[Auth] Email auth identity ownership mismatch: user_id=%d email=%s owner_id=%d", user.ID, email, identity.UserID)
		return nil, false
	}

	return identity, !existed
}

func inferLegacySignupSource(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	switch {
	case strings.HasSuffix(normalized, DingTalkConnectSyntheticEmailDomain):
		return "dingtalk"
	case strings.HasSuffix(normalized, LinuxDoConnectSyntheticEmailDomain):
		return "linuxdo"
	case strings.HasSuffix(normalized, OIDCConnectSyntheticEmailDomain):
		return "oidc"
	case strings.HasSuffix(normalized, WeChatConnectSyntheticEmailDomain):
		return "wechat"
	default:
		return "email"
	}
}

func (s *AuthService) validateRegistrationEmailPolicy(ctx context.Context, email string) error {
	if s.settingService == nil {
		return nil
	}
	whitelist := s.settingService.GetRegistrationEmailSuffixWhitelist(ctx)
	if !IsRegistrationEmailSuffixAllowed(email, whitelist) {
		return buildEmailSuffixNotAllowedError(whitelist)
	}
	return nil
}

func buildEmailSuffixNotAllowedError(whitelist []string) error {
	if len(whitelist) == 0 {
		return ErrEmailSuffixNotAllowed
	}

	allowed := strings.Join(whitelist, ", ")
	return infraerrors.BadRequest(
		"EMAIL_SUFFIX_NOT_ALLOWED",
		fmt.Sprintf("email suffix is not allowed, allowed suffixes: %s", allowed),
	).WithMetadata(map[string]string{
		"allowed_suffixes":     strings.Join(whitelist, ","),
		"allowed_suffix_count": strconv.Itoa(len(whitelist)),
	})
}

// ValidateToken
func (s *AuthService) ValidateToken(tokenString string) (*JWTClaims, error) {
	//
	if len(tokenString) > maxTokenLength {
		return nil, ErrTokenTooLarge
	}

	parser := jwt.NewParser(jwt.WithValidMethods([]string{
		jwt.SigningMethodHS256.Name,
		jwt.SigningMethodHS384.Name,
		jwt.SigningMethodHS512.Name,
	}))

	//
	token, err := parser.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWT.Secret), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			// token
			// jwt-go
			if claims, ok := token.Claims.(*JWTClaims); ok {
				return claims, ErrTokenExpired
			}
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrInvalidToken
}

func randomHexString(byteLength int) (string, error) {
	if byteLength <= 0 {
		byteLength = 16
	}
	buf := make([]byte, byteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isReservedEmail(email string) bool {
	normalized := strings.ToLower(strings.TrimSpace(email))
	return strings.HasSuffix(normalized, LinuxDoConnectSyntheticEmailDomain) ||
		strings.HasSuffix(normalized, OIDCConnectSyntheticEmailDomain) ||
		strings.HasSuffix(normalized, WeChatConnectSyntheticEmailDomain) ||
		strings.HasSuffix(normalized, DingTalkConnectSyntheticEmailDomain)
}

// GenerateToken
//
func (s *AuthService) GenerateToken(user *User) (string, error) {
	now := time.Now()
	var expiresAt time.Time
	if s.cfg.JWT.AccessTokenExpireMinutes > 0 {
		expiresAt = now.Add(time.Duration(s.cfg.JWT.AccessTokenExpireMinutes) * time.Minute)
	} else {
		//
		expiresAt = now.Add(time.Duration(s.cfg.JWT.ExpireHour) * time.Hour)
	}

	claims := &JWTClaims{
		UserID:       user.ID,
		Email:        user.Email,
		Role:         user.Role,
		TokenVersion: resolvedTokenVersion(user),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return tokenString, nil
}

// GetAccessTokenExpiresIn
func (s *AuthService) GetAccessTokenExpiresIn() int {
	if s.cfg.JWT.AccessTokenExpireMinutes > 0 {
		return s.cfg.JWT.AccessTokenExpireMinutes * 60
	}
	return s.cfg.JWT.ExpireHour * 3600
}

// HashPassword
func (s *AuthService) HashPassword(password string) (string, error) {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedBytes), nil
}

// CheckPassword
func (s *AuthService) CheckPassword(password, hashedPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// RefreshToken
func (s *AuthService) RefreshToken(ctx context.Context, oldTokenString string) (string, error) {
	//
	claims, err := s.ValidateToken(oldTokenString)
	if err != nil && !errors.Is(err, ErrTokenExpired) {
		return "", err
	}

	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", ErrInvalidToken
		}
		logger.LegacyPrintf("service.auth", "[Auth] Database error refreshing token: %v", err)
		return "", ErrServiceUnavailable
	}

	if !user.IsActive() {
		return "", ErrUserNotActive
	}

	// Security: Check TokenVersion to prevent refreshing revoked tokens
	// This ensures tokens issued before a password change cannot be refreshed
	if claims.TokenVersion != resolvedTokenVersion(user) {
		return "", ErrTokenRevoked
	}

	//
	return s.GenerateToken(user)
}

// IsPasswordResetEnabled
//
func (s *AuthService) IsPasswordResetEnabled(ctx context.Context) bool {
	if s.settingService == nil {
		return false
	}
	// Must have email verification enabled and SMTP configured
	if !s.settingService.IsEmailVerifyEnabled(ctx) {
		return false
	}
	return s.settingService.IsPasswordResetEnabled(ctx)
}

// preparePasswordReset validates the password reset request and returns necessary data
// Returns (siteName, resetURL, shouldProceed)
// shouldProceed is false when we should silently return success (to prevent enumeration)
func (s *AuthService) preparePasswordReset(ctx context.Context, email, frontendBaseURL string) (string, string, bool) {
	// Check if user exists (but don't reveal this to the caller)
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Security: Log but don't reveal that user doesn't exist
			logger.LegacyPrintf("service.auth", "[Auth] Password reset requested for non-existent email: %s", email)
			return "", "", false
		}
		logger.LegacyPrintf("service.auth", "[Auth] Database error checking email for password reset: %v", err)
		return "", "", false
	}

	// Check if user is active
	if !user.IsActive() {
		logger.LegacyPrintf("service.auth", "[Auth] Password reset requested for inactive user: %s", email)
		return "", "", false
	}

	// Get site name
	siteName := "Sub2API"
	if s.settingService != nil {
		siteName = s.settingService.GetSiteName(ctx)
	}

	// Build reset URL base
	resetURL := fmt.Sprintf("%s/reset-password", strings.TrimSuffix(frontendBaseURL, "/"))

	return siteName, resetURL, true
}

// RequestPasswordReset
// Security: Returns the same response regardless of whether the email exists (prevent user enumeration)
func (s *AuthService) RequestPasswordReset(ctx context.Context, email, frontendBaseURL string, locale ...string) error {
	if !s.IsPasswordResetEnabled(ctx) {
		return infraerrors.Forbidden("PASSWORD_RESET_DISABLED", "password reset is not enabled")
	}
	if s.emailService == nil {
		return ErrServiceUnavailable
	}

	siteName, resetURL, shouldProceed := s.preparePasswordReset(ctx, email, frontendBaseURL)
	if !shouldProceed {
		return nil // Silent success to prevent enumeration
	}

	if err := s.emailService.SendPasswordResetEmail(ctx, email, siteName, resetURL, firstEmailLocale(locale)); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to send password reset email to %s: %v", email, err)
		return nil // Silent success to prevent enumeration
	}

	logger.LegacyPrintf("service.auth", "[Auth] Password reset email sent to: %s", email)
	return nil
}

// RequestPasswordResetAsync
// Security: Returns the same response regardless of whether the email exists (prevent user enumeration)
func (s *AuthService) RequestPasswordResetAsync(ctx context.Context, email, frontendBaseURL string, locale ...string) error {
	if !s.IsPasswordResetEnabled(ctx) {
		return infraerrors.Forbidden("PASSWORD_RESET_DISABLED", "password reset is not enabled")
	}
	if s.emailQueueService == nil {
		return ErrServiceUnavailable
	}

	siteName, resetURL, shouldProceed := s.preparePasswordReset(ctx, email, frontendBaseURL)
	if !shouldProceed {
		return nil // Silent success to prevent enumeration
	}

	if err := s.emailQueueService.EnqueuePasswordReset(email, siteName, resetURL, firstEmailLocale(locale)); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to enqueue password reset email for %s: %v", email, err)
		return nil // Silent success to prevent enumeration
	}

	logger.LegacyPrintf("service.auth", "[Auth] Password reset email enqueued for: %s", email)
	return nil
}

// ResetPassword
// Security: Increments TokenVersion to invalidate all existing JWT tokens
func (s *AuthService) ResetPassword(ctx context.Context, email, token, newPassword string) error {
	// Check if password reset is enabled
	if !s.IsPasswordResetEnabled(ctx) {
		return infraerrors.Forbidden("PASSWORD_RESET_DISABLED", "password reset is not enabled")
	}

	if s.emailService == nil {
		return ErrServiceUnavailable
	}

	// Verify and consume the reset token (one-time use)
	if err := s.emailService.ConsumePasswordResetToken(ctx, email, token); err != nil {
		return err
	}

	// Get user
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return ErrInvalidResetToken // Token was valid but user was deleted
		}
		logger.LegacyPrintf("service.auth", "[Auth] Database error getting user for password reset: %v", err)
		return ErrServiceUnavailable
	}

	// Check if user is active
	if !user.IsActive() {
		return ErrUserNotActive
	}

	// Hash new password
	hashedPassword, err := s.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// Update password and increment TokenVersion
	user.PasswordHash = hashedPassword
	user.TokenVersion++ // Invalidate all existing tokens

	if err := s.userRepo.Update(ctx, user); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Database error updating password for user %d: %v", user.ID, err)
		return ErrServiceUnavailable
	}

	// Also revoke all refresh tokens for this user
	if err := s.RevokeAllUserSessions(ctx, user.ID); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to revoke refresh tokens for user %d: %v", user.ID, err)
		// Don't return error - password was already changed successfully
	}

	logger.LegacyPrintf("service.auth", "[Auth] Password reset successful for user: %s", email)
	return nil
}

// ==================== Refresh Token Methods ====================

// TokenPair
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // Access Tokenvalid期（seconds）
}

// TokenPairWithUser extends TokenPair with user role for backend mode checks
type TokenPairWithUser struct {
	TokenPair
	UserRole string
}

// GenerateTokenPair
// familyID:
func (s *AuthService) GenerateTokenPair(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
	//
	if s.refreshTokenCache == nil {
		return nil, errors.New("refresh token cache not configured")
	}

	//
	accessToken, err := s.GenerateToken(user)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	//
	refreshToken, err := s.generateRefreshToken(ctx, user, familyID)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    s.GetAccessTokenExpiresIn(),
	}, nil
}

// generateRefreshToken
func (s *AuthService) generateRefreshToken(ctx context.Context, user *User, familyID string) (string, error) {
	//
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	rawToken := refreshTokenPrefix + hex.EncodeToString(tokenBytes)

	//
	tokenHash := hashToken(rawToken)

	//
	if familyID == "" {
		familyBytes := make([]byte, 16)
		if _, err := rand.Read(familyBytes); err != nil {
			return "", fmt.Errorf("generate family id: %w", err)
		}
		familyID = hex.EncodeToString(familyBytes)
	}

	now := time.Now()
	ttl := time.Duration(s.cfg.JWT.RefreshTokenExpireDays) * 24 * time.Hour

	data := &RefreshTokenData{
		UserID:       user.ID,
		TokenVersion: resolvedTokenVersion(user),
		FamilyID:     familyID,
		CreatedAt:    now,
		ExpiresAt:    now.Add(ttl),
	}

	//
	if err := s.refreshTokenCache.StoreRefreshToken(ctx, tokenHash, data, ttl); err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}

	//
	if err := s.refreshTokenCache.AddToUserTokenSet(ctx, user.ID, tokenHash, ttl); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to add token to user set: %v", err)
	}

	//
	if err := s.refreshTokenCache.AddToFamilyTokenSet(ctx, familyID, tokenHash, ttl); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to add token to family set: %v", err)
	}

	return rawToken, nil
}

// RefreshTokenPair
//
func (s *AuthService) RefreshTokenPair(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
	//
	if s.refreshTokenCache == nil {
		return nil, ErrRefreshTokenInvalid
	}

	//
	if !strings.HasPrefix(refreshToken, refreshTokenPrefix) {
		return nil, ErrRefreshTokenInvalid
	}

	tokenHash := hashToken(refreshToken)

	//
	data, err := s.refreshTokenCache.GetRefreshToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrRefreshTokenNotFound) {
			// Token
			logger.LegacyPrintf("service.auth", "[Auth] Refresh token not found, possible reuse attack")
			return nil, ErrRefreshTokenInvalid
		}
		logger.LegacyPrintf("service.auth", "[Auth] Error getting refresh token: %v", err)
		return nil, ErrServiceUnavailable
	}

	//
	if time.Now().After(data.ExpiresAt) {
		//
		_ = s.refreshTokenCache.DeleteRefreshToken(ctx, tokenHash)
		return nil, ErrRefreshTokenExpired
	}

	user, err := s.userRepo.GetByID(ctx, data.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			//
			_ = s.refreshTokenCache.DeleteTokenFamily(ctx, data.FamilyID)
			return nil, ErrRefreshTokenInvalid
		}
		logger.LegacyPrintf("service.auth", "[Auth] Database error getting user for token refresh: %v", err)
		return nil, ErrServiceUnavailable
	}

	if !user.IsActive() {
		//
		_ = s.refreshTokenCache.DeleteTokenFamily(ctx, data.FamilyID)
		return nil, ErrUserNotActive
	}

	//
	if data.TokenVersion != resolvedTokenVersion(user) {
		// TokenVersion
		_ = s.refreshTokenCache.DeleteTokenFamily(ctx, data.FamilyID)
		return nil, ErrTokenRevoked
	}

	// Token
	if err := s.refreshTokenCache.DeleteRefreshToken(ctx, tokenHash); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to delete old refresh token: %v", err)
	}

	//
	pair, err := s.GenerateTokenPair(ctx, user, data.FamilyID)
	if err != nil {
		return nil, err
	}
	return &TokenPairWithUser{
		TokenPair: *pair,
		UserRole:  user.Role,
	}, nil
}

// RevokeRefreshToken
func (s *AuthService) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	if s.refreshTokenCache == nil {
		return nil // No-op if cache not configured
	}
	if !strings.HasPrefix(refreshToken, refreshTokenPrefix) {
		return ErrRefreshTokenInvalid
	}

	tokenHash := hashToken(refreshToken)
	return s.refreshTokenCache.DeleteRefreshToken(ctx, tokenHash)
}

// RevokeAllUserSessions
func (s *AuthService) RevokeAllUserSessions(ctx context.Context, userID int64) error {
	if s.refreshTokenCache == nil {
		return nil // No-op if cache not configured
	}
	return s.refreshTokenCache.DeleteUserRefreshTokens(ctx, userID)
}

// RevokeAllUserTokens invalidates both stateless access tokens and refresh sessions.
// Access/refresh token verification both depend on TokenVersion, so bumping it provides
// immediate revocation even if refresh-token cache cleanup later fails.
func (s *AuthService) RevokeAllUserTokens(ctx context.Context, userID int64) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	user.TokenVersion++
	if err := s.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	if err := s.RevokeAllUserSessions(ctx, userID); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to revoke refresh sessions after token invalidation for user %d: %v", userID, err)
	}
	return nil
}

// hashToken
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func resolvedTokenVersion(user *User) int64 {
	if user == nil {
		return 0
	}
	if user.TokenVersionResolved {
		return user.TokenVersion
	}

	material := strings.ToLower(strings.TrimSpace(user.Email)) + "\n" + user.PasswordHash
	sum := sha256.Sum256([]byte(material))
	fingerprint := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	return user.TokenVersion ^ fingerprint
}

// snapshotPlatformQuotaDefaults × 3 window）
// BulkInsertInitial
func (s *AuthService) snapshotPlatformQuotaDefaults(ctx context.Context, userID int64, plan *signupGrantPlan) error {
	if s.userPlatformQuotaRepo == nil || plan == nil || len(plan.PlatformQuotas) == 0 {
		return nil
	}
	records := make([]UserPlatformQuotaRecord, 0, len(plan.PlatformQuotas))
	for platform, q := range plan.PlatformQuotas {
		rec := UserPlatformQuotaRecord{
			UserID:   userID,
			Platform: platform,
		}
		if q != nil {
			rec.DailyLimitUSD = q.DailyLimitUSD
			rec.WeeklyLimitUSD = q.WeeklyLimitUSD
			rec.MonthlyLimitUSD = q.MonthlyLimitUSD
		}
		records = append(records, rec)
	}
	if err := s.userPlatformQuotaRepo.BulkInsertInitial(ctx, records); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Warning: snapshot platform quota failed user=%d: %v (fail-open)", userID, err)
		return nil // fail-open：returned nil，让调用方继续
	}
	return nil
}
