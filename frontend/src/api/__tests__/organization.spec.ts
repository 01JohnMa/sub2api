import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, del } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), del: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post, delete: del } }))

import { organizationAPI } from '@/api/organization'

describe('current organization API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    del.mockReset()
  })

  it('uses server-derived organization scope for identity, members, and reports', async () => {
    const organization = { id: 7, name: 'Acme', code: 'acme', description: 'Engineering', status: 'active' as const }
    const members = { items: [], total: 0, page: 2, page_size: 25, pages: 0 }
    const report = { organization_id: 7, request_count_total: 0 }
    get.mockResolvedValueOnce({ data: organization }).mockResolvedValueOnce({ data: members }).mockResolvedValueOnce({ data: report })

    await expect(organizationAPI.getCurrent()).resolves.toEqual(organization)
    await expect(organizationAPI.listMembers(2, 25)).resolves.toEqual(members)
    await expect(organizationAPI.getReport('2026-07-01T00:00:00Z', '2026-08-01T00:00:00Z')).resolves.toEqual(report)

    expect(get).toHaveBeenNthCalledWith(1, '/organization')
    expect(get).toHaveBeenNthCalledWith(2, '/organization/members', { params: { page: 2, page_size: 25 } })
    expect(get).toHaveBeenNthCalledWith(3, '/organization/report', { params: { start: '2026-07-01T00:00:00Z', end: '2026-08-01T00:00:00Z' } })
  })

  it('adds and removes members through scoped endpoints', async () => {
    const member = { user_id: 11, email: 'member@example.com' }
    const result = { user_id: 11, replayed: false }
    post.mockResolvedValue({ data: member })
    del.mockResolvedValue({ data: result })

    await expect(organizationAPI.addMember({ email: 'member@example.com' })).resolves.toEqual(member)
    await expect(organizationAPI.removeMember(11, { public_reason: 'offboarding' }, 'remove-key')).resolves.toEqual(result)

    expect(post).toHaveBeenCalledWith('/organization/members', { email: 'member@example.com' })
    expect(del).toHaveBeenCalledWith('/organization/members/11', {
      data: { public_reason: 'offboarding' },
      headers: { 'Idempotency-Key': 'remove-key' }
    })
  })

  it('does not expose balance, ledger, allocation, or reconciliation calls', () => {
    expect(organizationAPI).not.toHaveProperty('getBalance')
    expect(organizationAPI).not.toHaveProperty('listPublicLedger')
    expect(organizationAPI).not.toHaveProperty('previewAllocation')
    expect(organizationAPI).not.toHaveProperty('getPendingReconciliationSummary')
  })
})
