import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put, del } = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	put: vi.fn(),
	del: vi.fn()
}))

vi.mock('@/api/client', () => ({
	apiClient: { get, post, put, delete: del }
}))

import {
  addMember,
  create,
	disable,
	reactivate,
	getArchivePreview,
	archive,
  credit,
  executeAllocation,
  getBalance,
	getReport,
	getPendingReconciliation,
  getById,
  list,
  listLedger,
	listPendingReconciliations,
  listMembers,
	previewAllocation,
	removeMember,
	resolvePendingReconciliation,
	update,
  updateMemberRole
} from '@/api/admin/organizations'

describe('admin organizations API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
		put.mockReset()
		del.mockReset()
  })

  it('lists organizations with pagination, filters, sorting, and cancellation', async () => {
    const response = {
      items: [],
      total: 0,
      page: 2,
      page_size: 50,
      pages: 0
    }
    const controller = new AbortController()
    get.mockResolvedValue({ data: response })

    await expect(
      list(
        2,
        50,
        { status: 'disabled', search: 'acme', sort_by: 'name', sort_order: 'asc' },
        { signal: controller.signal }
      )
    ).resolves.toEqual(response)

    expect(get).toHaveBeenCalledWith('/admin/organizations', {
      params: {
        page: 2,
        page_size: 50,
        status: 'disabled',
        search: 'acme',
        sort_by: 'name',
        sort_order: 'asc'
      },
      signal: controller.signal
    })
  })

  it('uses the organization resource endpoints for detail, create, and edit', async () => {
    const organization = {
      id: 7,
      name: 'Acme',
      code: 'acme',
      description: '',
      status: 'active' as const,
      created_by: 1,
      created_at: '2026-07-28T00:00:00Z',
      updated_at: '2026-07-28T00:00:00Z'
    }
    get.mockResolvedValue({ data: organization })
    post.mockResolvedValue({ data: organization })
    put.mockResolvedValue({ data: organization })

    await expect(getById(7)).resolves.toEqual(organization)
    await expect(
      create({ name: 'Acme', code: 'acme', description: 'Platform organization' })
    ).resolves.toEqual(organization)
    await expect(update(7, { description: '' })).resolves.toEqual(organization)

    expect(get).toHaveBeenCalledWith('/admin/organizations/7')
    expect(post).toHaveBeenCalledWith('/admin/organizations', {
      name: 'Acme',
      code: 'acme',
      description: 'Platform organization'
    })
    expect(put).toHaveBeenCalledWith('/admin/organizations/7', { description: '' })
  })

	it('uses explicit lifecycle endpoints and archive confirmation versions', async () => {
		const organization = { id: 7, status: 'disabled' as const }
		const preview = {
			organization_id: 7,
			status: 'disabled' as const,
			unallocated: '12.50000000',
			balance_version: 4,
			updated_at: '2026-07-30T00:00:00Z'
		}
		post.mockResolvedValue({ data: organization })
		get.mockResolvedValue({ data: preview })

		await expect(disable(7)).resolves.toEqual(organization)
		await expect(reactivate(7)).resolves.toEqual(organization)
		await expect(getArchivePreview(7)).resolves.toEqual(preview)
		await expect(archive(7, { balance_version: 4, updated_at: preview.updated_at, confirm_archive: true })).resolves.toEqual(organization)

		expect(post).toHaveBeenNthCalledWith(1, '/admin/organizations/7/disable')
		expect(post).toHaveBeenNthCalledWith(2, '/admin/organizations/7/reactivate')
		expect(get).toHaveBeenCalledWith('/admin/organizations/7/archive-preview')
		expect(post).toHaveBeenNthCalledWith(3, '/admin/organizations/7/archive', {
			balance_version: 4,
			updated_at: preview.updated_at,
			confirm_archive: true
		})
	})

	it('uses only organization-scoped member endpoints and role payloads', async () => {
    const member = {
      user_id: 11,
      email: 'member@example.com',
      username: 'member',
      status: 'active' as const,
      organization_role: 'organization_member' as const,
      allocation: {
        id: 9,
        status: 'active',
        available: '0.00000000',
        reserved: '0.00000000'
      }
    }
    const response = {
      items: [member],
      total: 1,
      page: 2,
      page_size: 50,
      pages: 1
    }
    get.mockResolvedValue({ data: response })
    post.mockResolvedValue({ data: member })
    put.mockResolvedValue({ data: member })

    await expect(listMembers(7, 2, 50)).resolves.toEqual(response)
    await expect(addMember(7, { user_id: 11, organization_role: 'organization_member' })).resolves.toEqual(member)
    await expect(updateMemberRole(7, 11, 'organization_admin')).resolves.toEqual(member)

    expect(get).toHaveBeenCalledWith('/admin/organizations/7/members', {
      params: {
        page: 2,
        page_size: 50
      }
    })
    expect(post).toHaveBeenCalledWith('/admin/organizations/7/members', {
      user_id: 11,
      organization_role: 'organization_member'
    })
    expect(put).toHaveBeenCalledWith('/admin/organizations/7/members/11', {
      organization_role: 'organization_admin'
    })
  })

	it('removes a member with the platform idempotency contract', async () => {
		const result = { user_id: 11, reclaimed: '2.00000000', replayed: true }
		del.mockResolvedValue({ data: result })

		await expect(removeMember(7, 11, { public_reason: 'offboarding' }, 'platform-remove-key')).resolves.toEqual(result)

		expect(del).toHaveBeenCalledWith('/admin/organizations/7/members/11', {
			data: { public_reason: 'offboarding' },
			headers: { 'Idempotency-Key': 'platform-remove-key' }
		})
	})

  it('uses decimal organization funding endpoints with an explicit idempotency header', async () => {
    const summary = {
      organization_id: 7,
      status: 'active' as const,
      currency: 'USD' as const,
      unallocated: '8.00000000',
      available: '2.00000000',
      reserved: '0.50000000',
      consumed: '1.50000000',
      total_remaining: '10.50000000',
      balance_version: 4
    }
    const ledger = {
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1
    }
    const result = {
      operation_id: 'op-1',
      correlation_id: 'corr-1',
      organization_id: 7,
      currency: 'USD' as const,
      amount: '1.25000000',
      pool_before: '8.00000000',
      pool_after: '9.25000000',
      balance_version_before: 4,
      balance_version_after: 5,
      ledger: {} as never,
      replayed: false
    }
    get.mockResolvedValueOnce({ data: summary }).mockResolvedValueOnce({ data: ledger })
    post.mockResolvedValueOnce({ data: result })

    await expect(getBalance(7)).resolves.toEqual(summary)
    await expect(listLedger(7, 2, 50, 'asc')).resolves.toEqual(ledger)
    await expect(
      credit(7, { amount: '1.25000000', internal_reason: 'approved by finance' }, 'credit-retry-1')
    ).resolves.toEqual(result)

    expect(get).toHaveBeenNthCalledWith(1, '/admin/organizations/7/balance')
    expect(get).toHaveBeenNthCalledWith(2, '/admin/organizations/7/ledger', {
      params: { page: 2, page_size: 50, sort_order: 'asc' }
    })
    expect(post).toHaveBeenCalledWith(
      '/admin/organizations/7/credit',
      { amount: '1.25000000', internal_reason: 'approved by finance' },
      { headers: { 'Idempotency-Key': 'credit-retry-1' } }
    )
  })

	it('loads a platform-scoped organization report with an explicit half-open range', async () => {
		const report = { organization_id: 7, request_count_total: 0 }
		get.mockResolvedValue({ data: report })
		await expect(getReport(7, '2026-07-01T00:00:00Z', '2026-08-01T00:00:00Z')).resolves.toEqual(report)
		expect(get).toHaveBeenCalledWith('/admin/organizations/7/report', { params: { start: '2026-07-01T00:00:00Z', end: '2026-08-01T00:00:00Z' } })
	})

  it('uses organization-scoped custom and reclaim allocation preview and execute endpoints', async () => {
    const confirmation = {
      organization_id: 7,
      authorization_scope: 'platform_admin_jwt',
      operation_type: 'custom_allocation' as const,
      currency: 'USD' as const,
      money_scale: 8,
      organization_at: '2026-07-28T00:00:00Z',
      balance_version: 4,
      snapshot_hash: 'snapshot',
      pool_before: '8.00000000',
      pool_after: '6.00000000',
      total_delta: '2.00000000',
      rounding_remainder: '0.00000000',
      targets: [
        {
          user_id: 11,
          allocation_id: 9,
          before: '0.00000000',
          delta: '2.00000000',
          after: '2.00000000'
        }
      ]
    }
    const preview = {
      confirmation,
      eligible_members: [],
      excluded_members: [],
      canonical_payload_hash: 'payload-hash',
      preview_token: 'preview-token',
      expires_at: '2026-07-28T00:05:00Z'
    }
    const result = {
      organization_id: 7,
      operation_type: 'custom_allocation' as const,
      currency: 'USD' as const,
      outcome: 'committed' as const,
      pool_before: '8.00000000',
      pool_after: '6.00000000',
      total_delta: '2.00000000',
      balance_version_before: 4,
      balance_version_after: 5,
      replayed: false
    }
    const reclaimConfirmation = {
      ...confirmation,
      operation_type: 'allocation_reclaim' as const,
      pool_after: '10.00000000',
      total_delta: '-2.00000000',
      targets: [
        {
          user_id: 11,
          allocation_id: 9,
          before: '2.00000000',
          delta: '-2.00000000',
          after: '0.00000000'
        }
      ]
    }
    const reclaimPreview = {
      ...preview,
      confirmation: reclaimConfirmation,
      canonical_payload_hash: 'reclaim-payload-hash',
      preview_token: 'reclaim-preview-token'
    }
    const reclaimResult = {
      ...result,
      operation_type: 'allocation_reclaim' as const,
      pool_before: '8.00000000',
      pool_after: '10.00000000',
      total_delta: '-2.00000000'
    }
    post
      .mockResolvedValueOnce({ data: preview })
      .mockResolvedValueOnce({ data: result })
      .mockResolvedValueOnce({ data: reclaimPreview })
      .mockResolvedValueOnce({ data: reclaimResult })

    await expect(
      previewAllocation(7, 'custom', { targets: [{ user_id: 11, amount: '2.00000000' }] })
    ).resolves.toEqual(preview)
    await expect(
      executeAllocation(7, 'custom', { preview_token: 'preview-token', confirmation }, 'allocation-retry-1')
    ).resolves.toEqual(result)
    await expect(
      previewAllocation(7, 'reclaim', { targets: [{ user_id: 11, amount: '2.00000000' }] })
    ).resolves.toEqual(reclaimPreview)
    await expect(
      executeAllocation(
        7,
        'reclaim',
        { preview_token: 'reclaim-preview-token', confirmation: reclaimConfirmation },
        'reclaim-retry-1'
      )
    ).resolves.toEqual(reclaimResult)

    expect(post).toHaveBeenNthCalledWith(
      1,
      '/admin/organizations/7/allocations/custom/preview',
      { targets: [{ user_id: 11, amount: '2.00000000' }] }
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/admin/organizations/7/allocations/custom',
      { preview_token: 'preview-token', confirmation },
      { headers: { 'Idempotency-Key': 'allocation-retry-1' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      3,
      '/admin/organizations/7/allocations/reclaim/preview',
      { targets: [{ user_id: 11, amount: '2.00000000' }] }
    )
    expect(post).toHaveBeenNthCalledWith(
      4,
      '/admin/organizations/7/allocations/reclaim',
      { preview_token: 'reclaim-preview-token', confirmation: reclaimConfirmation },
      { headers: { 'Idempotency-Key': 'reclaim-retry-1' } }
    )
  })

	it('uses scoped pending reconciliation endpoints and an idempotency header for resolution', async () => {
		const pending = { items: [], total: 0, page: 1, page_size: 20, pages: 0 }
		const resolution = { execution_id: 'execution-id', reservation_state: 'released', replayed: false }
		get.mockResolvedValue({ data: pending })
		post.mockResolvedValue({ data: resolution })

		await expect(listPendingReconciliations(7)).resolves.toEqual(pending)
		get.mockResolvedValueOnce({ data: { execution_id: 'execution-id' } })
		await expect(getPendingReconciliation(7, 'execution-id')).resolves.toEqual({ execution_id: 'execution-id' })
		await expect(resolvePendingReconciliation(7, 'execution-id', 'reconcile-1')).resolves.toEqual(resolution)

		expect(get).toHaveBeenCalledWith('/admin/organizations/7/pending-reconciliations', {
			params: { page: 1, page_size: 20 }
		})
		expect(get).toHaveBeenCalledWith('/admin/organizations/7/pending-reconciliations/execution-id')
		expect(post).toHaveBeenCalledWith(
			'/admin/organizations/7/pending-reconciliations/execution-id/resolve',
			{},
			{ headers: { 'Idempotency-Key': 'reconcile-1' } }
		)
	})
})
