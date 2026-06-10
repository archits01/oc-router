//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

// GatewayRoutingSuite
type GatewayRoutingSuite struct {
	suite.Suite
	ctx         context.Context
	client      *dbent.Client
	accountRepo *accountRepository
}

func (s *GatewayRoutingSuite) SetupTest() {
	s.ctx = context.Background()
	tx := testEntTx(s.T())
	s.client = tx.Client()
	s.accountRepo = newAccountRepositoryWithSQL(s.client, tx, nil)
}

func TestGatewayRoutingSuite(t *testing.T) {
	suite.Run(t, new(GatewayRoutingSuite))
}

// TestListSchedulableByPlatforms_GeminiAndAntigravity
func (s *GatewayRoutingSuite) TestListSchedulableByPlatforms_GeminiAndAntigravity() {
	geminiAcc := mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "gemini-oauth",
		Platform:    service.PlatformGemini,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Priority:    1,
	})

	antigravityAcc := mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "antigravity-oauth",
		Platform:    service.PlatformAntigravity,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Priority:    2,
		Credentials: map[string]any{
			"access_token":  "test-token",
			"refresh_token": "test-refresh",
			"project_id":    "test-project",
		},
	})

	//
	mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "anthropic-oauth",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Priority:    0,
	})

	// + antigravity
	accounts, err := s.accountRepo.ListSchedulableByPlatforms(s.ctx, []string{
		service.PlatformGemini,
		service.PlatformAntigravity,
	})

	s.Require().NoError(err)
	s.Require().Len(accounts, 2, "should return both gemini and antigravity accounts")

	platforms := make(map[string]bool)
	for _, acc := range accounts {
		platforms[acc.Platform] = true
	}
	s.Require().True(platforms[service.PlatformGemini], "should include gemini account")
	s.Require().True(platforms[service.PlatformAntigravity], "should include antigravity account")
	s.Require().False(platforms[service.PlatformAnthropic], "should not include anthropic account")

	ids := make(map[int64]bool)
	for _, acc := range accounts {
		ids[acc.ID] = true
	}
	s.Require().True(ids[geminiAcc.ID])
	s.Require().True(ids[antigravityAcc.ID])
}

// TestListSchedulableByGroupIDAndPlatforms_WithGroupBinding
func (s *GatewayRoutingSuite) TestListSchedulableByGroupIDAndPlatforms_WithGroupBinding() {
	//
	group := mustCreateGroup(s.T(), s.client, &service.Group{
		Name:     "gemini-group",
		Platform: service.PlatformGemini,
		Status:   service.StatusActive,
	})

	boundAcc := mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "bound-antigravity",
		Platform:    service.PlatformAntigravity,
		Status:      service.StatusActive,
		Schedulable: true,
	})
	unboundAcc := mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "unbound-antigravity",
		Platform:    service.PlatformAntigravity,
		Status:      service.StatusActive,
		Schedulable: true,
	})

	mustBindAccountToGroup(s.T(), s.client, boundAcc.ID, group.ID, 1)

	accounts, err := s.accountRepo.ListSchedulableByGroupIDAndPlatforms(s.ctx, group.ID, []string{
		service.PlatformGemini,
		service.PlatformAntigravity,
	})

	s.Require().NoError(err)
	s.Require().Len(accounts, 1, "should only return accounts bound to group")
	s.Require().Equal(boundAcc.ID, accounts[0].ID)

	for _, acc := range accounts {
		s.Require().NotEqual(unboundAcc.ID, acc.ID, "should not include unbound account")
	}
}

// TestListSchedulableByPlatform_Antigravity
func (s *GatewayRoutingSuite) TestListSchedulableByPlatform_Antigravity() {
	mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "gemini-1",
		Platform:    service.PlatformGemini,
		Status:      service.StatusActive,
		Schedulable: true,
	})

	antigravity := mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "antigravity-1",
		Platform:    service.PlatformAntigravity,
		Status:      service.StatusActive,
		Schedulable: true,
	})

	//
	accounts, err := s.accountRepo.ListSchedulableByPlatform(s.ctx, service.PlatformAntigravity)

	s.Require().NoError(err)
	s.Require().Len(accounts, 1)
	s.Require().Equal(antigravity.ID, accounts[0].ID)
	s.Require().Equal(service.PlatformAntigravity, accounts[0].Platform)
}

// TestSchedulableFilter_ExcludesInactive
func (s *GatewayRoutingSuite) TestSchedulableFilter_ExcludesInactive() {
	activeAcc := mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "active-antigravity",
		Platform:    service.PlatformAntigravity,
		Status:      service.StatusActive,
		Schedulable: true,
	})

	// =true）
	inactiveAcc := mustCreateAccount(s.T(), s.client, &service.Account{
		Name:     "inactive-antigravity",
		Platform: service.PlatformAntigravity,
		Status:   service.StatusActive,
	})
	s.Require().NoError(s.client.Account.UpdateOneID(inactiveAcc.ID).SetSchedulable(false).Exec(s.ctx))

	mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "error-antigravity",
		Platform:    service.PlatformAntigravity,
		Status:      service.StatusError,
		Schedulable: true,
	})

	accounts, err := s.accountRepo.ListSchedulableByPlatform(s.ctx, service.PlatformAntigravity)

	s.Require().NoError(err)
	s.Require().Len(accounts, 1, "should only return schedulable active accounts")
	s.Require().Equal(activeAcc.ID, accounts[0].ID)
}

// TestPlatformRoutingDecision
//
func (s *GatewayRoutingSuite) TestPlatformRoutingDecision() {
	geminiAcc := mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "gemini-route-test",
		Platform:    service.PlatformGemini,
		Status:      service.StatusActive,
		Schedulable: true,
	})

	antigravityAcc := mustCreateAccount(s.T(), s.client, &service.Account{
		Name:        "antigravity-route-test",
		Platform:    service.PlatformAntigravity,
		Status:      service.StatusActive,
		Schedulable: true,
	})

	tests := []struct {
		name            string
		accountID       int64
		expectedService string
	}{
		{
			name:            "Gemini account routes to ForwardNative",
			accountID:       geminiAcc.ID,
			expectedService: "GeminiMessagesCompatService.ForwardNative",
		},
		{
			name:            "Antigravity account routes to ForwardGemini",
			accountID:       antigravityAcc.ID,
			expectedService: "AntigravityGatewayService.ForwardGemini",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			account, err := s.accountRepo.GetByID(s.ctx, tt.accountID)
			s.Require().NoError(err)

			//
			var routedService string
			if account.Platform == service.PlatformAntigravity {
				routedService = "AntigravityGatewayService.ForwardGemini"
			} else {
				routedService = "GeminiMessagesCompatService.ForwardNative"
			}

			s.Require().Equal(tt.expectedService, routedService)
		})
	}
}
