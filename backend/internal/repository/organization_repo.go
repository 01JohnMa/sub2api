package repository

import (
	"context"
	"errors"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/organization"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
)

type organizationRepository struct {
	client *dbent.Client
}

func NewOrganizationRepository(client *dbent.Client) service.OrganizationRepository {
	return &organizationRepository{client: client}
}

func (r *organizationRepository) Create(ctx context.Context, input *service.Organization) error {
	entity, err := clientFromContext(ctx, r.client).Organization.Create().
		SetName(input.Name).
		SetCode(input.Code).
		SetDescription(input.Description).
		SetStatus(input.Status).
		SetCreatedBy(input.CreatedBy).
		Save(ctx)
	if err != nil {
		return translatePersistenceError(err, nil, service.ErrOrganizationCodeExists)
	}
	copyOrganizationEntity(input, entity)
	return nil
}

func (r *organizationRepository) GetByID(ctx context.Context, id int64) (*service.Organization, error) {
	entity, err := clientFromContext(ctx, r.client).Organization.Query().
		Where(organization.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, nil)
	}
	return organizationEntityToService(entity), nil
}

func (r *organizationRepository) List(ctx context.Context, params pagination.PaginationParams, filters service.OrganizationListFilters) ([]service.Organization, *pagination.PaginationResult, error) {
	query := clientFromContext(ctx, r.client).Organization.Query()
	if filters.Status != "" {
		query = query.Where(organization.StatusEQ(filters.Status))
	}
	if filters.Search != "" {
		query = query.Where(organization.Or(
			organization.NameContainsFold(filters.Search),
			organization.CodeContainsFold(filters.Search),
		))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	itemsQuery := query.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range organizationListOrder(params) {
		itemsQuery = itemsQuery.Order(order)
	}
	entities, err := itemsQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}
	items := make([]service.Organization, 0, len(entities))
	for _, entity := range entities {
		items = append(items, *organizationEntityToService(entity))
	}
	return items, paginationResultFromTotal(int64(total), params), nil
}

func organizationListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	field := organization.FieldCreatedAt
	switch strings.ToLower(strings.TrimSpace(params.SortBy)) {
	case "name":
		field = organization.FieldName
	case "code":
		field = organization.FieldCode
	case "status":
		field = organization.FieldStatus
	case "created_at", "":
		field = organization.FieldCreatedAt
	}

	if params.NormalizedSortOrder(pagination.SortOrderDesc) == pagination.SortOrderAsc {
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(organization.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(organization.FieldID)}
}

func (r *organizationRepository) Update(ctx context.Context, id int64, input service.UpdateOrganizationInput) (*service.Organization, error) {
	builder := clientFromContext(ctx, r.client).Organization.UpdateOneID(id).
		Where(organization.StatusNEQ(service.OrganizationStatusArchived))
	if input.Name != nil {
		builder.SetName(*input.Name)
	}
	if input.Code != nil {
		builder.SetCode(*input.Code)
	}
	if input.Description != nil {
		builder.SetDescription(*input.Description)
	}

	updated, err := builder.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			existing, lookupErr := r.GetByID(ctx, id)
			if lookupErr == nil && existing.Status == service.OrganizationStatusArchived {
				return nil, service.ErrOrganizationArchived
			}
			if lookupErr != nil && !errors.Is(lookupErr, service.ErrOrganizationNotFound) {
				return nil, lookupErr
			}
			return nil, service.ErrOrganizationNotFound
		}
		return nil, translatePersistenceError(err, service.ErrOrganizationNotFound, service.ErrOrganizationCodeExists)
	}
	return organizationEntityToService(updated), nil
}

func organizationEntityToService(entity *dbent.Organization) *service.Organization {
	if entity == nil {
		return nil
	}
	return &service.Organization{
		ID:          entity.ID,
		Name:        entity.Name,
		Code:        entity.Code,
		Description: entity.Description,
		Status:      entity.Status,
		CreatedBy:   entity.CreatedBy,
		CreatedAt:   entity.CreatedAt,
		UpdatedAt:   entity.UpdatedAt,
	}
}

func copyOrganizationEntity(destination *service.Organization, source *dbent.Organization) {
	if destination == nil || source == nil {
		return
	}
	*destination = *organizationEntityToService(source)
}
