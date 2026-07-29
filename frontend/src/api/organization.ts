/**
 * Current-organization API endpoints.
 *
 * The server derives organization scope from the authenticated user. Do not add
 * an organization ID parameter to this module.
 */

import { apiClient } from './client'
import type {
  AddCurrentOrganizationMemberRequest,
  OrganizationAllocationExecuteRequest,
  OrganizationAllocationOperation,
  OrganizationAllocationPreview,
  OrganizationAllocationPreviewRequest,
  OrganizationAllocationResult,
  OrganizationBalanceSummary,
  OrganizationMember,
  OrganizationMemberRemovalResult,
  OrganizationSummary,
  PaginatedResponse,
  RemoveOrganizationMemberRequest
} from '@/types'

async function getCurrent(): Promise<OrganizationSummary> {
  const { data } = await apiClient.get<OrganizationSummary>('/organization')
  return data
}

async function listMembers(page: number = 1, pageSize: number = 20): Promise<PaginatedResponse<OrganizationMember>> {
  const { data } = await apiClient.get<PaginatedResponse<OrganizationMember>>('/organization/members', {
    params: {
      page,
      page_size: pageSize
    }
  })
  return data
}

async function getBalance(): Promise<OrganizationBalanceSummary> {
  const { data } = await apiClient.get<OrganizationBalanceSummary>('/organization/balance')
  return data
}

async function addMember(request: AddCurrentOrganizationMemberRequest): Promise<OrganizationMember> {
  const { data } = await apiClient.post<OrganizationMember>('/organization/members', request)
  return data
}

async function removeMember(
  userId: number,
  request: RemoveOrganizationMemberRequest,
  idempotencyKey: string
): Promise<OrganizationMemberRemovalResult> {
  const { data } = await apiClient.delete<OrganizationMemberRemovalResult>(`/organization/members/${userId}`, {
    data: request,
    headers: {
      'Idempotency-Key': idempotencyKey
    }
  })
  return data
}

async function previewAllocation(
  operation: OrganizationAllocationOperation,
  request: OrganizationAllocationPreviewRequest = {}
): Promise<OrganizationAllocationPreview> {
  const { data } = await apiClient.post<OrganizationAllocationPreview>(
    `/organization/allocations/${operation}/preview`,
    request
  )
  return data
}

async function executeAllocation(
  operation: OrganizationAllocationOperation,
  request: OrganizationAllocationExecuteRequest,
  idempotencyKey: string
): Promise<OrganizationAllocationResult> {
  const { data } = await apiClient.post<OrganizationAllocationResult>(
    `/organization/allocations/${operation}`,
    request,
    {
      headers: {
        'Idempotency-Key': idempotencyKey
      }
    }
  )
  return data
}

const previewEqualAllocation = (request?: OrganizationAllocationPreviewRequest) =>
  previewAllocation('equal', request)
const previewCustomAllocation = (request: OrganizationAllocationPreviewRequest) =>
  previewAllocation('custom', request)
const previewReclaim = (request: OrganizationAllocationPreviewRequest) =>
  previewAllocation('reclaim', request)
const previewReclaimAllocation = previewReclaim
const executeEqualAllocation = (request: OrganizationAllocationExecuteRequest, idempotencyKey: string) =>
  executeAllocation('equal', request, idempotencyKey)
const executeCustomAllocation = (request: OrganizationAllocationExecuteRequest, idempotencyKey: string) =>
  executeAllocation('custom', request, idempotencyKey)
const executeReclaim = (request: OrganizationAllocationExecuteRequest, idempotencyKey: string) =>
  executeAllocation('reclaim', request, idempotencyKey)
const executeReclaimAllocation = executeReclaim

export const organizationAPI = {
  getCurrent,
  listMembers,
  getBalance,
  addMember,
  removeMember,
  previewAllocation,
  executeAllocation,
  previewEqualAllocation,
  previewCustomAllocation,
  previewReclaim,
  previewReclaimAllocation,
  executeEqualAllocation,
  executeCustomAllocation,
  executeReclaim,
  executeReclaimAllocation
}

export default organizationAPI
