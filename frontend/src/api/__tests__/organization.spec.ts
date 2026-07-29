import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, del } = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	del: vi.fn()
}))

vi.mock('@/api/client', () => ({
	apiClient: { get, post, delete: del }
}))

import { organizationAPI } from '@/api/organization'

describe('current organization API', () => {
  beforeEach(() => {
    get.mockReset()
		post.mockReset()
		del.mockReset()
  })

  it('loads the server-derived current organization without a client organization id', async () => {
    const organization = {
      id: 7,
      name: 'Acme',
      code: 'acme',
      description: 'Engineering',
      status: 'active' as const
    }
    get.mockResolvedValue({ data: organization })

    await expect(organizationAPI.getCurrent()).resolves.toEqual(organization)
    expect(get).toHaveBeenCalledWith('/organization')
  })

  it('loads scoped members and balance without accepting an organization id', async () => {
    const members = { items: [], total: 0, page: 2, page_size: 25, pages: 0 }
    const balance = { organization_id: 7, unallocated: '10.00000000' }
    get.mockResolvedValueOnce({ data: members }).mockResolvedValueOnce({ data: balance })

    await expect(organizationAPI.listMembers(2, 25)).resolves.toEqual(members)
    await expect(organizationAPI.getBalance()).resolves.toEqual(balance)

    expect(get).toHaveBeenNthCalledWith(1, '/organization/members', {
      params: { page: 2, page_size: 25 }
    })
    expect(get).toHaveBeenNthCalledWith(2, '/organization/balance')
  })

	it('adds an exact member target without exposing organization role controls', async () => {
    const member = { user_id: 11, email: 'member@example.com' }
    post.mockResolvedValue({ data: member })

    await expect(organizationAPI.addMember({ email: 'member@example.com' })).resolves.toEqual(member)

    expect(post).toHaveBeenCalledWith('/organization/members', {
      email: 'member@example.com'
    })
	})

	it('removes a member through the scoped endpoint with an idempotency header', async () => {
		const result = { user_id: 11, reclaimed: '3.00000000', replayed: false }
		del.mockResolvedValue({ data: result })

		await expect(organizationAPI.removeMember(11, { public_reason: 'offboarding' }, 'remove-key')).resolves.toEqual(result)

		expect(del).toHaveBeenCalledWith('/organization/members/11', {
			data: { public_reason: 'offboarding' },
			headers: { 'Idempotency-Key': 'remove-key' }
		})
	})

  it('uses the shared preview contract for equal, custom, and reclaim operations', async () => {
    const preview = {
      preview_token: 'preview-token',
      confirmation: { targets: [] }
    }
    post.mockResolvedValue({ data: preview })

    await organizationAPI.previewEqualAllocation()
    await organizationAPI.previewCustomAllocation({
      targets: [{ user_id: 11, amount: '2.50000000' }]
    })
    await organizationAPI.previewReclaim({
      targets: [{ user_id: 11, amount: '1.00000000' }]
    })

    expect(post).toHaveBeenNthCalledWith(1, '/organization/allocations/equal/preview', {})
    expect(post).toHaveBeenNthCalledWith(2, '/organization/allocations/custom/preview', {
      targets: [{ user_id: 11, amount: '2.50000000' }]
    })
    expect(post).toHaveBeenNthCalledWith(3, '/organization/allocations/reclaim/preview', {
      targets: [{ user_id: 11, amount: '1.00000000' }]
    })
  })

  it('echoes the canonical confirmation and sends the explicit idempotency header', async () => {
    const request = {
      preview_token: 'preview-token',
      confirmation: {
        organization_id: 7,
        authorization_scope: 'organization_admin_jwt',
        operation_type: 'custom_allocation',
        currency: 'USD',
        money_scale: 8,
        organization_at: '2026-07-28T00:00:00Z',
        balance_version: 2,
        snapshot_hash: 'snapshot-hash',
        pool_before: '10.00000000',
        pool_after: '7.50000000',
        total_delta: '2.50000000',
        rounding_remainder: '0.00000000',
        targets: [
          {
            user_id: 11,
            allocation_id: 9,
            before: '1.00000000',
            delta: '2.50000000',
            after: '3.50000000'
          }
        ]
      }
    }
    post.mockResolvedValue({ data: { outcome: 'committed' } })

    await organizationAPI.executeCustomAllocation(request, 'organization-allocation-key')

    expect(post).toHaveBeenCalledWith('/organization/allocations/custom', request, {
      headers: { 'Idempotency-Key': 'organization-allocation-key' }
    })
  })
})
