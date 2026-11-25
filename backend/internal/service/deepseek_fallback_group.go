package service

import "context"

// ResolveDeepSeekFallbackGroup checks the target without rebinding the authenticated key.
func (s *APIKeyService) ResolveDeepSeekFallbackGroup(ctx context.Context, user *User, groupID int64, model string) (*Group, error) {
	if s == nil || s.groupRepo == nil || user == nil || groupID <= 0 {
		return nil, ErrGroupNotAllowed
	}
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil || !group.IsActive() || group.Platform != PlatformDeepseek || group.IsSubscriptionType() ||
		!s.canUserBindGroup(ctx, user, group) || !group.ModelAllowlist.Allows(model) {
		return nil, ErrGroupNotAllowed
	}
	return group, nil
}
