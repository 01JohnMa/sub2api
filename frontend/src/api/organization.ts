/**
 * Current-organization API endpoints.
 *
 * The server derives organization scope from the authenticated user. Do not add
 * an organization ID parameter to this module.
 */

import { apiClient } from './client'
import type {
  AddCurrentOrganizationMemberRequest,
  OrganizationMember,
  OrganizationMemberRemovalResult,
  OrganizationReport,
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
    params: { page, page_size: pageSize }
  })
  return data
}

async function getReport(start: string, end: string): Promise<OrganizationReport> {
  const { data } = await apiClient.get<OrganizationReport>('/organization/report', { params: { start, end } })
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
    headers: { 'Idempotency-Key': idempotencyKey }
  })
  return data
}

export const organizationAPI = {
  getCurrent,
  listMembers,
  getReport,
  addMember,
  removeMember
}

export default organizationAPI
