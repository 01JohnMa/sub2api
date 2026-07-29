package dto

import "github.com/Wei-Shaw/sub2api/internal/service"

type OrganizationMember struct {
	UserID           int64                        `json:"user_id"`
	Email            string                       `json:"email"`
	Username         string                       `json:"username"`
	Status           string                       `json:"status"`
	OrganizationRole string                       `json:"organization_role"`
	Allocation       OrganizationMemberAllocation `json:"allocation"`
}

type OrganizationMemberAllocation struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	Available string `json:"available"`
	Reserved  string `json:"reserved"`
}

type OrganizationMemberRemovalResult struct {
	OperationID          string `json:"operation_id"`
	CorrelationID        string `json:"correlation_id"`
	OrganizationID       int64  `json:"organization_id"`
	UserID               int64  `json:"user_id"`
	AllocationID         int64  `json:"allocation_id"`
	OperationType        string `json:"operation_type"`
	Currency             string `json:"currency"`
	Outcome              string `json:"outcome"`
	Reclaimed            string `json:"reclaimed"`
	PoolBefore           string `json:"pool_before"`
	PoolAfter            string `json:"pool_after"`
	BalanceVersionBefore int64  `json:"balance_version_before"`
	BalanceVersionAfter  int64  `json:"balance_version_after"`
	Replayed             bool   `json:"replayed"`
}

func OrganizationMemberFromService(member *service.OrganizationMember) *OrganizationMember {
	if member == nil {
		return nil
	}
	return &OrganizationMember{
		UserID:           member.UserID,
		Email:            member.Email,
		Username:         member.Username,
		Status:           member.Status,
		OrganizationRole: member.OrganizationRole,
		Allocation: OrganizationMemberAllocation{
			ID:        member.Allocation.ID,
			Status:    member.Allocation.Status,
			Available: member.Allocation.Available,
			Reserved:  member.Allocation.Reserved,
		},
	}
}

func OrganizationMemberRemovalResultFromService(result *service.OrganizationMemberRemovalResult) *OrganizationMemberRemovalResult {
	if result == nil {
		return nil
	}
	return &OrganizationMemberRemovalResult{
		OperationID:          result.OperationID,
		CorrelationID:        result.CorrelationID,
		OrganizationID:       result.OrganizationID,
		UserID:               result.UserID,
		AllocationID:         result.AllocationID,
		OperationType:        result.OperationType,
		Currency:             result.Currency,
		Outcome:              result.Outcome,
		Reclaimed:            result.Reclaimed,
		PoolBefore:           result.PoolBefore,
		PoolAfter:            result.PoolAfter,
		BalanceVersionBefore: result.BalanceVersionBefore,
		BalanceVersionAfter:  result.BalanceVersionAfter,
		Replayed:             result.Replayed,
	}
}
