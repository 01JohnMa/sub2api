/**
 * Admin organization API endpoints.
 * Organization tenancy is independent from API key Groups and upstream OAuth organizations.
 */

import { apiClient } from '../client'
import type {
  AddOrganizationMemberRequest,
  CreditOrganizationRequest,
  CreateOrganizationRequest,
  OrganizationAllocationExecuteRequest,
  OrganizationAllocationPreview,
  OrganizationAllocationPreviewRequest,
  OrganizationAllocationResult,
  Organization,
  OrganizationBalanceSummary,
	OrganizationArchivePreview,
  OrganizationFundingResult,
  OrganizationLedgerEntry,
  OrganizationMember,
  OrganizationMemberRemovalResult,
	OrganizationPendingReconciliation,
	OrganizationPendingReconciliationResult,
	OrganizationReport,
  OrganizationRole,
  OrganizationStatus,
  PaginatedResponse,
  UpdateOrganizationRequest,
  RemoveOrganizationMemberRequest,
	ArchiveOrganizationRequest,
  UpdateOrganizationMemberRoleRequest
} from '@/types'

export type AdminOrganizationAllocationOperation = 'custom' | 'reclaim'

export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    status?: OrganizationStatus
    search?: string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<Organization>> {
  const { data } = await apiClient.get<PaginatedResponse<Organization>>('/admin/organizations', {
    params: {
      page,
      page_size: pageSize,
      ...filters
    },
    signal: options?.signal
  })
  return data
}

export async function getById(id: number): Promise<Organization> {
  const { data } = await apiClient.get<Organization>(`/admin/organizations/${id}`)
  return data
}

export async function create(request: CreateOrganizationRequest): Promise<Organization> {
  const { data } = await apiClient.post<Organization>('/admin/organizations', request)
  return data
}

export async function update(
  id: number,
  request: UpdateOrganizationRequest
): Promise<Organization> {
  const { data } = await apiClient.put<Organization>(`/admin/organizations/${id}`, request)
  return data
}

export async function disable(id: number): Promise<Organization> {
  const { data } = await apiClient.post<Organization>(`/admin/organizations/${id}/disable`)
  return data
}

export async function reactivate(id: number): Promise<Organization> {
  const { data } = await apiClient.post<Organization>(`/admin/organizations/${id}/reactivate`)
  return data
}

export async function getArchivePreview(id: number): Promise<OrganizationArchivePreview> {
  const { data } = await apiClient.get<OrganizationArchivePreview>(`/admin/organizations/${id}/archive-preview`)
  return data
}

export async function archive(id: number, request: ArchiveOrganizationRequest): Promise<Organization> {
  const { data } = await apiClient.post<Organization>(`/admin/organizations/${id}/archive`, request)
  return data
}

export async function listMembers(
  id: number,
  page: number = 1,
  pageSize: number = 20
): Promise<PaginatedResponse<OrganizationMember>> {
  const { data } = await apiClient.get<PaginatedResponse<OrganizationMember>>(`/admin/organizations/${id}/members`, {
    params: {
      page,
      page_size: pageSize
    }
  })
  return data
}

export async function addMember(
  id: number,
  request: AddOrganizationMemberRequest
): Promise<OrganizationMember> {
  const { data } = await apiClient.post<OrganizationMember>(`/admin/organizations/${id}/members`, request)
  return data
}

export async function updateMemberRole(
  id: number,
  userId: number,
  organizationRole: OrganizationRole
): Promise<OrganizationMember> {
  const request: UpdateOrganizationMemberRoleRequest = { organization_role: organizationRole }
  const { data } = await apiClient.put<OrganizationMember>(
    `/admin/organizations/${id}/members/${userId}`,
    request
  )
  return data
}

export async function removeMember(
  id: number,
  userId: number,
  request: RemoveOrganizationMemberRequest,
  idempotencyKey: string
): Promise<OrganizationMemberRemovalResult> {
  const { data } = await apiClient.delete<OrganizationMemberRemovalResult>(
    `/admin/organizations/${id}/members/${userId}`,
    {
      data: request,
      headers: {
        'Idempotency-Key': idempotencyKey
      }
    }
  )
  return data
}

export async function getBalance(id: number): Promise<OrganizationBalanceSummary> {
  const { data } = await apiClient.get<OrganizationBalanceSummary>(`/admin/organizations/${id}/balance`)
  return data
}

export async function getReport(id: number, start: string, end: string): Promise<OrganizationReport> {
	const { data } = await apiClient.get<OrganizationReport>(`/admin/organizations/${id}/report`, { params: { start, end } })
	return data
}

export async function listPendingReconciliations(
	id: number,
	page: number = 1,
	pageSize: number = 20
): Promise<PaginatedResponse<OrganizationPendingReconciliation>> {
	const { data } = await apiClient.get<PaginatedResponse<OrganizationPendingReconciliation>>(
		`/admin/organizations/${id}/pending-reconciliations`,
		{ params: { page, page_size: pageSize } }
	)
	return data
}

export async function getPendingReconciliation(
	id: number,
	executionID: string
): Promise<OrganizationPendingReconciliation> {
	const { data } = await apiClient.get<OrganizationPendingReconciliation>(
		`/admin/organizations/${id}/pending-reconciliations/${executionID}`
	)
	return data
}

export async function resolvePendingReconciliation(
	id: number,
	executionID: string,
	idempotencyKey: string
): Promise<OrganizationPendingReconciliationResult> {
	const { data } = await apiClient.post<OrganizationPendingReconciliationResult>(
		`/admin/organizations/${id}/pending-reconciliations/${executionID}/resolve`,
		{},
		{ headers: { 'Idempotency-Key': idempotencyKey } }
	)
	return data
}

export async function listLedger(
  id: number,
  page: number = 1,
  pageSize: number = 20,
  sortOrder: 'asc' | 'desc' = 'desc'
): Promise<PaginatedResponse<OrganizationLedgerEntry>> {
  const { data } = await apiClient.get<PaginatedResponse<OrganizationLedgerEntry>>(
    `/admin/organizations/${id}/ledger`,
    {
      params: {
        page,
        page_size: pageSize,
        sort_order: sortOrder
      }
    }
  )
  return data
}

export async function credit(
  id: number,
  request: CreditOrganizationRequest,
  idempotencyKey: string
): Promise<OrganizationFundingResult> {
  const { data } = await apiClient.post<OrganizationFundingResult>(
    `/admin/organizations/${id}/credit`,
    request,
    {
      headers: {
        'Idempotency-Key': idempotencyKey
      }
    }
  )
  return data
}

export async function previewAllocation(
  id: number,
  operation: AdminOrganizationAllocationOperation,
  request: OrganizationAllocationPreviewRequest
): Promise<OrganizationAllocationPreview> {
  const { data } = await apiClient.post<OrganizationAllocationPreview>(
    `/admin/organizations/${id}/allocations/${operation}/preview`,
    request
  )
  return data
}

export async function executeAllocation(
  id: number,
  operation: AdminOrganizationAllocationOperation,
  request: OrganizationAllocationExecuteRequest,
  idempotencyKey: string
): Promise<OrganizationAllocationResult> {
  const { data } = await apiClient.post<OrganizationAllocationResult>(
    `/admin/organizations/${id}/allocations/${operation}`,
    request,
    {
      headers: {
        'Idempotency-Key': idempotencyKey
      }
    }
  )
  return data
}

const previewCustomAllocation = (id: number, request: OrganizationAllocationPreviewRequest) =>
  previewAllocation(id, 'custom', request)
const previewReclaimAllocation = (id: number, request: OrganizationAllocationPreviewRequest) =>
  previewAllocation(id, 'reclaim', request)
const executeCustomAllocation = (
  id: number,
  request: OrganizationAllocationExecuteRequest,
  idempotencyKey: string
) => executeAllocation(id, 'custom', request, idempotencyKey)
const executeReclaimAllocation = (
  id: number,
  request: OrganizationAllocationExecuteRequest,
  idempotencyKey: string
) => executeAllocation(id, 'reclaim', request, idempotencyKey)

const organizationsAPI = {
  list,
  getById,
  create,
  update,
	disable,
	reactivate,
	getArchivePreview,
	archive,
  listMembers,
  addMember,
  updateMemberRole,
  removeMember,
  getBalance,
	getReport,
  listPendingReconciliations,
	getPendingReconciliation,
	resolvePendingReconciliation,
  listLedger,
  credit,
  previewAllocation,
  executeAllocation,
  previewCustomAllocation,
  previewReclaimAllocation,
  executeCustomAllocation,
  executeReclaimAllocation
}

export default organizationsAPI
