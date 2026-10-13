package ent

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"entgo.io/ent/dialect"
	"github.com/Wei-Shaw/sub2api/ent/organizationallocation"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

var (
	ErrUserDeletionRequiresTransaction = errors.New("user deletion guard requires a transaction")
	ErrUserDeletionOrganizationMember  = errors.New("user organization membership must be removed before deletion")
)

// GuardUserDeletion locks the mutation targets and revalidates their current
// organization state on the same transactional client used by the mutation.
func (m *UserMutation) GuardUserDeletion(ctx context.Context) error {
	if _, err := m.Tx(); err != nil {
		return fmt.Errorf("%w: %v", ErrUserDeletionRequiresTransaction, err)
	}

	ids, err := m.IDs(ctx)
	if err != nil {
		return fmt.Errorf("resolve user deletion targets: %w", err)
	}
	if len(ids) == 0 {
		// Under READ COMMITTED, rows matching the original predicate can be
		// inserted after IDs returns. Pin this mutation to an empty target set.
		m.Where(user.IDIn())
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	// Prevent a broad mutation predicate from matching rows that appeared after
	// the guarded target snapshot was resolved.
	m.Where(user.IDIn(ids...))

	client := m.Client()
	query := client.User.Query().
		Where(user.IDIn(ids...)).
		Order(user.ByID())
	if client.Driver().Dialect() != dialect.SQLite {
		query = query.ForUpdate()
	}
	users, err := query.All(ctx)
	if err != nil {
		return fmt.Errorf("lock user deletion targets: %w", err)
	}
	for _, entity := range users {
		if entity.OrganizationID != nil {
			return ErrUserDeletionOrganizationMember
		}
	}

	hasActiveAllocation, err := client.OrganizationAllocation.Query().
		Where(
			organizationallocation.UserRefIDIn(ids...),
			organizationallocation.StatusEQ(domain.OrganizationAllocationStatusActive),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check user deletion allocations: %w", err)
	}
	if hasActiveAllocation {
		return ErrUserDeletionOrganizationMember
	}

	return nil
}
