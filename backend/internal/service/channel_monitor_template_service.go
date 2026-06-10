package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// ChannelMonitorRequestTemplateRepository
type ChannelMonitorRequestTemplateRepository interface {
	Create(ctx context.Context, t *ChannelMonitorRequestTemplate) error
	GetByID(ctx context.Context, id int64) (*ChannelMonitorRequestTemplate, error)
	Update(ctx context.Context, t *ChannelMonitorRequestTemplate) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params ChannelMonitorRequestTemplateListParams) ([]*ChannelMonitorRequestTemplate, error)
	// ApplyToMonitors
	// = id，
	//
	ApplyToMonitors(ctx context.Context, id int64, monitorIDs []int64) (int64, error)
	// CountAssociatedMonitors = id 「」）。
	CountAssociatedMonitors(ctx context.Context, id int64) (int64, error)
	// ListAssociatedMonitors = id
	// +filter。
	ListAssociatedMonitors(ctx context.Context, id int64) ([]*AssociatedMonitorBrief, error)
}

// AssociatedMonitorBrief
type AssociatedMonitorBrief struct {
	ID       int64
	Name     string
	Provider string
	APIMode  string
	Enabled  bool
}

// ChannelMonitorRequestTemplateService
type ChannelMonitorRequestTemplateService struct {
	repo ChannelMonitorRequestTemplateRepository
}

// NewChannelMonitorRequestTemplateService
func NewChannelMonitorRequestTemplateService(repo ChannelMonitorRequestTemplateRepository) *ChannelMonitorRequestTemplateService {
	return &ChannelMonitorRequestTemplateService{repo: repo}
}

// ---------- CRUD ----------

// List =
func (s *ChannelMonitorRequestTemplateService) List(ctx context.Context, params ChannelMonitorRequestTemplateListParams) ([]*ChannelMonitorRequestTemplate, error) {
	if params.Provider != "" {
		if err := validateProvider(params.Provider); err != nil {
			return nil, err
		}
	}
	if params.APIMode != "" {
		if params.Provider == "" {
			if err := validateAPIMode(MonitorProviderOpenAI, params.APIMode); err != nil {
				return nil, err
			}
		} else if err := validateAPIMode(params.Provider, params.APIMode); err != nil {
			return nil, err
		}
	}
	return s.repo.List(ctx, params)
}

// Get
func (s *ChannelMonitorRequestTemplateService) Get(ctx context.Context, id int64) (*ChannelMonitorRequestTemplate, error) {
	return s.repo.GetByID(ctx, id)
}

// Create
func (s *ChannelMonitorRequestTemplateService) Create(ctx context.Context, p ChannelMonitorRequestTemplateCreateParams) (*ChannelMonitorRequestTemplate, error) {
	if err := validateTemplateCreateParams(p); err != nil {
		return nil, err
	}
	t := &ChannelMonitorRequestTemplate{
		Name:             strings.TrimSpace(p.Name),
		Provider:         p.Provider,
		APIMode:          defaultAPIMode(p.APIMode),
		Description:      strings.TrimSpace(p.Description),
		ExtraHeaders:     emptyHeadersIfNil(p.ExtraHeaders),
		BodyOverrideMode: defaultBodyMode(p.BodyOverrideMode),
		BodyOverride:     p.BodyOverride,
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	return t, nil
}

// Update
func (s *ChannelMonitorRequestTemplateService) Update(ctx context.Context, id int64, p ChannelMonitorRequestTemplateUpdateParams) (*ChannelMonitorRequestTemplate, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := applyTemplateUpdate(existing, p); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	return existing, nil
}

// Delete
func (s *ChannelMonitorRequestTemplateService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

// ApplyToMonitors
// monitorIDs = id；
func (s *ChannelMonitorRequestTemplateService) ApplyToMonitors(ctx context.Context, id int64, monitorIDs []int64) (int64, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return 0, err
	}
	if len(monitorIDs) == 0 {
		return 0, ErrChannelMonitorTemplateApplyEmpty
	}
	affected, err := s.repo.ApplyToMonitors(ctx, id, monitorIDs)
	if err != nil {
		return 0, fmt.Errorf("apply template to monitors: %w", err)
	}
	return affected, nil
}

// CountAssociatedMonitors
func (s *ChannelMonitorRequestTemplateService) CountAssociatedMonitors(ctx context.Context, id int64) (int64, error) {
	return s.repo.CountAssociatedMonitors(ctx, id)
}

// ListAssociatedMonitors
//
func (s *ChannelMonitorRequestTemplateService) ListAssociatedMonitors(ctx context.Context, id int64) ([]*AssociatedMonitorBrief, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.ListAssociatedMonitors(ctx, id)
}

// ---------- &

// validateTemplateCreateParams
func validateTemplateCreateParams(p ChannelMonitorRequestTemplateCreateParams) error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrChannelMonitorTemplateMissingName
	}
	if err := validateProvider(p.Provider); err != nil {
		return ErrChannelMonitorTemplateInvalidProvider
	}
	if err := validateAPIMode(p.Provider, p.APIMode); err != nil {
		return ErrChannelMonitorTemplateInvalidAPIMode
	}
	if err := validateBodyModeForProtocol(p.Provider, p.APIMode, p.BodyOverrideMode, p.BodyOverride); err != nil {
		return err
	}
	if err := validateExtraHeaders(p.ExtraHeaders); err != nil {
		return err
	}
	return nil
}

// applyTemplateUpdate
func applyTemplateUpdate(existing *ChannelMonitorRequestTemplate, p ChannelMonitorRequestTemplateUpdateParams) error {
	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if name == "" {
			return ErrChannelMonitorTemplateMissingName
		}
		existing.Name = name
	}
	if p.Description != nil {
		existing.Description = strings.TrimSpace(*p.Description)
	}
	newAPIMode := defaultAPIMode(existing.APIMode)
	if p.APIMode != nil {
		newAPIMode = defaultAPIMode(*p.APIMode)
	}
	if err := validateAPIMode(existing.Provider, newAPIMode); err != nil {
		return ErrChannelMonitorTemplateInvalidAPIMode
	}
	if p.ExtraHeaders != nil {
		if err := validateExtraHeaders(*p.ExtraHeaders); err != nil {
			return err
		}
		existing.ExtraHeaders = emptyHeadersIfNil(*p.ExtraHeaders)
	}
	// BodyOverrideMode / BodyOverride 「」
	newMode := existing.BodyOverrideMode
	newBody := existing.BodyOverride
	if p.BodyOverrideMode != nil {
		newMode = *p.BodyOverrideMode
	}
	if p.BodyOverride != nil {
		newBody = *p.BodyOverride
	}
	if err := validateBodyModeForProtocol(existing.Provider, newAPIMode, newMode, newBody); err != nil {
		return err
	}
	existing.APIMode = newAPIMode
	existing.BodyOverrideMode = defaultBodyMode(newMode)
	existing.BodyOverride = newBody
	return nil
}

// validateBodyModeForProtocol
func validateBodyModeForProtocol(provider, apiMode, mode string, body map[string]any) error {
	if err := validateBodyModeParams(mode, body); err != nil {
		return err
	}
	if defaultBodyMode(mode) != MonitorBodyOverrideModeReplace {
		return nil
	}
	if err := validateReplaceRequestBody(provider, defaultAPIMode(apiMode), body); err != nil {
		return ErrChannelMonitorInvalidRequestBody
	}
	return nil
}

// validateBodyModeParams
func validateBodyModeParams(mode string, body map[string]any) error {
	switch mode {
	case "", MonitorBodyOverrideModeOff:
		return nil
	case MonitorBodyOverrideModeMerge, MonitorBodyOverrideModeReplace:
		if len(body) == 0 {
			return ErrChannelMonitorTemplateBodyRequired
		}
		return nil
	default:
		return ErrChannelMonitorTemplateInvalidBodyMode
	}
}

// headerNameRegex
var headerNameRegex = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+\-.^_` + "`" + `|~]+$`)

// forbiddenHeaderNames hop-by-hop + HTTP
//
var forbiddenHeaderNames = map[string]bool{
	"host":              true,
	"content-length":    true,
	"content-encoding":  true,
	"transfer-encoding": true,
	"connection":        true,
}

// IsForbiddenHeaderName
func IsForbiddenHeaderName(name string) bool {
	return forbiddenHeaderNames[strings.ToLower(strings.TrimSpace(name))]
}

// validateExtraHeaders +
func validateExtraHeaders(h map[string]string) error {
	for k := range h {
		if !headerNameRegex.MatchString(k) {
			return ErrChannelMonitorTemplateHeaderInvalidName
		}
		if IsForbiddenHeaderName(k) {
			return ErrChannelMonitorTemplateHeaderForbidden
		}
	}
	return nil
}

// emptyHeadersIfNil
func emptyHeadersIfNil(h map[string]string) map[string]string {
	if h == nil {
		return map[string]string{}
	}
	return h
}

// defaultBodyMode
func defaultBodyMode(mode string) string {
	if mode == "" {
		return MonitorBodyOverrideModeOff
	}
	return mode
}
