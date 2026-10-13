package service

import "github.com/Wei-Shaw/sub2api/internal/config"

func ProvideOrganizationAllocationService(
	repo OrganizationAllocationRepository,
	cfg *config.Config,
) (*OrganizationAllocationService, error) {
	if cfg == nil {
		return nil, ErrOrganizationAllocationPreviewUnavailable
	}
	return NewOrganizationAllocationService(repo, cfg.JWT.Secret)
}
