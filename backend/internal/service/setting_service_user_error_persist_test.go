//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// TestAllowUserViewErrorRequests_PersistsToDB
// AllowUserViewErrorRequests
func TestAllowUserViewErrorRequests_PersistsToDB(t *testing.T) {
	// bmUpdateRepoStub
	//
	repo := &bmUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		AllowUserViewErrorRequests: true,
	})
	require.NoError(t, err)

	// "true"
	val, ok := repo.updates[SettingKeyAllowUserViewErrorRequests]
	require.True(t, ok, "updates map 中应包含 SettingKeyAllowUserViewErrorRequests，但未找到（bug：buildSystemSettingsUpdates 漏写）")
	require.Equal(t, "true", val)
}
