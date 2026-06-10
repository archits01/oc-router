package service

import "time"

// ProviderRefreshErrorAction
type ProviderRefreshErrorAction int

const (
	// ProviderRefreshErrorReturn
	ProviderRefreshErrorReturn ProviderRefreshErrorAction = iota
	// ProviderRefreshErrorUseExistingToken
	ProviderRefreshErrorUseExistingToken
)

// ProviderLockHeldAction
type ProviderLockHeldAction int

const (
	// ProviderLockHeldUseExistingToken
	ProviderLockHeldUseExistingToken ProviderLockHeldAction = iota
	// ProviderLockHeldWaitForCache
	ProviderLockHeldWaitForCache
)

// ProviderRefreshPolicy
type ProviderRefreshPolicy struct {
	OnRefreshError ProviderRefreshErrorAction
	OnLockHeld     ProviderLockHeldAction
	FailureTTL     time.Duration
}

func ClaudeProviderRefreshPolicy() ProviderRefreshPolicy {
	return ProviderRefreshPolicy{
		OnRefreshError: ProviderRefreshErrorUseExistingToken,
		OnLockHeld:     ProviderLockHeldWaitForCache,
		FailureTTL:     time.Minute,
	}
}

func OpenAIProviderRefreshPolicy() ProviderRefreshPolicy {
	return ProviderRefreshPolicy{
		OnRefreshError: ProviderRefreshErrorUseExistingToken,
		OnLockHeld:     ProviderLockHeldWaitForCache,
		FailureTTL:     time.Minute,
	}
}

func GeminiProviderRefreshPolicy() ProviderRefreshPolicy {
	return ProviderRefreshPolicy{
		OnRefreshError: ProviderRefreshErrorReturn,
		OnLockHeld:     ProviderLockHeldUseExistingToken,
		FailureTTL:     0,
	}
}

func AntigravityProviderRefreshPolicy() ProviderRefreshPolicy {
	return ProviderRefreshPolicy{
		OnRefreshError: ProviderRefreshErrorReturn,
		OnLockHeld:     ProviderLockHeldUseExistingToken,
		FailureTTL:     0,
	}
}

// BackgroundSkipAction “”
type BackgroundSkipAction int

const (
	// BackgroundSkipAsSkipped
	BackgroundSkipAsSkipped BackgroundSkipAction = iota
	// BackgroundSkipAsSuccess
	BackgroundSkipAsSuccess
)

// BackgroundRefreshPolicy
type BackgroundRefreshPolicy struct {
	OnLockHeld       BackgroundSkipAction
	OnAlreadyRefresh BackgroundSkipAction
}

func DefaultBackgroundRefreshPolicy() BackgroundRefreshPolicy {
	return BackgroundRefreshPolicy{
		OnLockHeld:       BackgroundSkipAsSkipped,
		OnAlreadyRefresh: BackgroundSkipAsSkipped,
	}
}

func (p BackgroundRefreshPolicy) handleLockHeld() error {
	if p.OnLockHeld == BackgroundSkipAsSuccess {
		return nil
	}
	return errRefreshSkipped
}

func (p BackgroundRefreshPolicy) handleAlreadyRefreshed() error {
	if p.OnAlreadyRefresh == BackgroundSkipAsSuccess {
		return nil
	}
	return errRefreshSkipped
}
