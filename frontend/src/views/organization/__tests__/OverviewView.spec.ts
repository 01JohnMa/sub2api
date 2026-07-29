import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type {
  OrganizationAllocationPreview,
  OrganizationAllocationResult,
  OrganizationBalanceSummary,
  OrganizationMember,
  OrganizationSummary
} from '@/types'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import OverviewView from '../OverviewView.vue'

const {
  getCurrent,
  listMembers,
  getBalance,
  addMember,
  removeMember,
  previewAllocation,
  executeAllocation,
  showError,
  showSuccess,
  replace,
  authStore
} = vi.hoisted(() => ({
  getCurrent: vi.fn(),
  listMembers: vi.fn(),
  getBalance: vi.fn(),
  addMember: vi.fn(),
  removeMember: vi.fn(),
  previewAllocation: vi.fn(),
  executeAllocation: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  replace: vi.fn(),
  authStore: {
    user: { id: 1 },
    isAdmin: false,
    isOrganizationAdmin: true,
    refreshUser: vi.fn()
  }
}))

const refreshUser = authStore.refreshUser

vi.mock('@/api', () => ({
  organizationAPI: {
    getCurrent,
    listMembers,
    getBalance,
    addMember,
    removeMember,
    previewAllocation,
    executeAllocation
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ replace })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

const organization = (): OrganizationSummary => ({
  id: 7,
  name: 'Acme',
  code: 'acme',
  description: 'Engineering',
  status: 'active'
})

const member = (userId = 11): OrganizationMember => ({
  user_id: userId,
  email: `member${userId}@example.com`,
  username: `member-${userId}`,
  status: 'active',
  organization_role: 'organization_member',
  allocation: {
    id: userId + 100,
    status: 'active',
    available: '3.00000000',
    reserved: '0.00000000'
  }
})

const balance = (): OrganizationBalanceSummary => ({
  organization_id: 7,
  status: 'active',
  currency: 'USD',
  unallocated: '10.00000000',
  available: '3.00000000',
  reserved: '0.00000000',
  consumed: '2.00000000',
  total_remaining: '13.00000000',
  balance_version: 3
})

const allocationPreviewFixture = (
  operationType: 'equal_allocation' | 'custom_allocation' | 'allocation_reclaim' = 'equal_allocation'
): OrganizationAllocationPreview => ({
  confirmation: {
    organization_id: 7,
    authorization_scope: 'organization_admin_jwt',
    operation_type: operationType,
    currency: 'USD',
    money_scale: 8,
    organization_at: '2026-07-28T00:00:00Z',
    balance_version: 3,
    snapshot_hash: 'snapshot-hash',
    pool_before: '10.00000000',
    pool_after: '5.00000000',
    total_delta: '5.00000000',
    rounding_remainder: '0.00000000',
    targets: [
      {
        user_id: 11,
        allocation_id: 111,
        before: '3.00000000',
        delta: '5.00000000',
        after: '8.00000000'
      }
    ]
  },
  eligible_members: [
    {
      user_id: 11,
      user_status: 'active',
      organization_role: 'organization_member',
      allocation_id: 111,
      allocation_status: 'active',
      available: '3.00000000',
      reserved: '0.00000000'
    }
  ],
  excluded_members: [
    {
      user_id: 1,
      user_status: 'active',
      organization_role: 'organization_admin',
      allocation_id: 101,
      allocation_status: 'active',
      available: '0.00000000',
      reserved: '0.00000000',
      excluded_reason: 'caller'
    }
  ],
  canonical_payload_hash: `${operationType}-hash`,
  preview_token: `${operationType}-token`,
  expires_at: '2026-07-28T00:05:00Z'
})

const allocationResult = (): OrganizationAllocationResult => ({
  operation_id: 'operation-id',
  correlation_id: 'correlation-id',
  organization_id: 7,
  operation_type: 'equal_allocation',
  currency: 'USD',
  outcome: 'committed',
  pool_before: '10.00000000',
  pool_after: '5.00000000',
  total_delta: '5.00000000',
  balance_version_before: 3,
  balance_version_after: 4,
  replayed: false
})

const BaseDialogStub = {
  props: ['show', 'title'],
  template: '<div v-if="show"><h2>{{ title }}</h2><slot /></div>'
}

const ConfirmDialogStub = {
  inheritAttrs: false,
  props: ['show', 'title', 'message', 'confirmText'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" :data-test="$attrs['data-test']">
      <h2>{{ title }}</h2>
      <p>{{ message }}</p>
      <button :data-test="$attrs['data-test'] === 'organization-add-member-confirmation' ? 'organization-add-member-confirm' : $attrs['data-test'] + '-confirm'" @click="$emit('confirm')">{{ confirmText }}</button>
      <button @click="$emit('cancel')">cancel</button>
    </div>
  `
}

function mountView() {
  return mount(OverviewView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: BaseDialogStub,
        ConfirmDialog: ConfirmDialogStub,
        Icon: true,
        LoadingSpinner: true,
        Pagination: true
      }
    }
  })
}

describe('organization OverviewView', () => {
  beforeEach(() => {
    getCurrent.mockReset()
    listMembers.mockReset()
    getBalance.mockReset()
    addMember.mockReset()
    removeMember.mockReset()
    previewAllocation.mockReset()
    executeAllocation.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    replace.mockReset()
    refreshUser.mockReset()
    authStore.isAdmin = false
    authStore.isOrganizationAdmin = true

    getCurrent.mockResolvedValue(organization())
    listMembers.mockResolvedValue({ items: [member()], total: 1, page: 1, page_size: 20, pages: 1 })
    getBalance.mockResolvedValue(balance())
    addMember.mockResolvedValue(member(12))
    removeMember.mockResolvedValue({
      operation_id: 'remove-operation-id',
      correlation_id: 'remove-correlation-id',
      organization_id: 7,
      user_id: 11,
      allocation_id: 111,
      operation_type: 'member_removal_reclaim',
      currency: 'USD',
      outcome: 'committed',
      reclaimed: '3.00000000',
      pool_before: '10.00000000',
      pool_after: '13.00000000',
      balance_version_before: 3,
      balance_version_after: 4,
      replayed: false
    })
    previewAllocation.mockResolvedValue(allocationPreviewFixture())
    executeAllocation.mockResolvedValue(allocationResult())
  })

  it('publishes organization copy at the root locale path used by the route and page', () => {
    expect(en.organization.overview.title).toBe('Organization Overview')
    expect(zh.organization.overview.title).toBe('组织概览')
    expect((en.admin as Record<string, unknown>).organization).toBeUndefined()
    expect((zh.admin as Record<string, unknown>).organization).toBeUndefined()
  })

  it('loads only server-scoped organization, balance, and member data', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(getCurrent).toHaveBeenCalledTimes(1)
    expect(getBalance).toHaveBeenCalledTimes(1)
    expect(listMembers).toHaveBeenCalledWith(1, 20)
    expect(wrapper.get('[data-test="organization-balance-section"]').text()).toContain('10.00000000')
    expect(wrapper.get('[data-test="organization-members-section"]').text()).toContain('member11@example.com')
    expect(wrapper.find('select[data-test*="role"]').exists()).toBe(false)
  })

  it('shows an unavailable state without routing loops when a non-active organization admin is denied', async () => {
    getCurrent.mockRejectedValueOnce({ status: 403 })
    refreshUser.mockResolvedValueOnce(undefined)

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="organization-unavailable"]').text()).toContain(
      'organization.overview.unavailableTitle'
    )
    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(replace).not.toHaveBeenCalled()
    expect(getBalance).not.toHaveBeenCalled()
    expect(listMembers).not.toHaveBeenCalled()
  })

  it('returns to the personal dashboard after the organization administrator role is revoked', async () => {
    getCurrent.mockRejectedValueOnce({ status: 403 })
    refreshUser.mockImplementationOnce(async () => {
      authStore.isOrganizationAdmin = false
    })

    mountView()
    await flushPromises()

    expect(refreshUser).toHaveBeenCalledTimes(1)
    expect(replace).toHaveBeenCalledWith('/dashboard')
  })

  it('requires the API Key conversion and zero-allocation confirmation before adding by exact email', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-add-member"]').trigger('click')
    await wrapper.get('[data-test="organization-member-email"]').setValue('member12@example.com')
    await wrapper.get('[data-test="organization-add-member-form"]').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-test="organization-add-member-confirmation"]').text()).toContain(
      'organization.overview.members.addWarning'
    )
    expect(addMember).not.toHaveBeenCalled()

    await wrapper.get('[data-test="organization-add-member-confirm"]').trigger('click')
    await flushPromises()

    expect(addMember).toHaveBeenCalledWith({ email: 'member12@example.com' })
    expect(listMembers).toHaveBeenCalledTimes(2)
  })

  it('supports an exact decimal user ID and rejects ambiguous member targets', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-add-member"]').trigger('click')
    await wrapper.get('[data-test="organization-member-email"]').setValue('member12@example.com')
    await wrapper.get('[data-test="organization-member-user-id"]').setValue('12')
    await wrapper.get('[data-test="organization-add-member-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="organization-add-member-confirmation"]').exists()).toBe(false)

    await wrapper.get('[data-test="organization-member-email"]').setValue('')
    await wrapper.get('[data-test="organization-add-member-form"]').trigger('submit')
    await flushPromises()
    await wrapper.get('[data-test="organization-add-member-confirm"]').trigger('click')
    await flushPromises()

    expect(addMember).toHaveBeenCalledWith({ user_id: 12 })
  })

  it('warns about reclaimed funds and personal Key recovery before removing an ordinary member', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-remove-member-11"]').trigger('click')

    const confirmation = wrapper.get('[data-test="organization-remove-member-confirmation"]')
    expect(confirmation.text()).toContain('organization.overview.members.removeWarning')
    expect(confirmation.text()).toContain('3.00000000')
    expect(confirmation.text()).toContain('member-11')
    expect(removeMember).not.toHaveBeenCalled()

    await wrapper.get('[data-test="organization-remove-member-confirmation-confirm"]').trigger('click')
    await flushPromises()

    expect(removeMember).toHaveBeenCalledWith(11, {}, expect.stringMatching(/^organization-member-removal-/))
    expect(getBalance).toHaveBeenCalledTimes(2)
    expect(listMembers).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalledWith('organization.overview.members.removed')
  })

  it('keeps the same member-removal idempotency key for retryable failures and refreshes reservation conflicts', async () => {
    const refreshedMember = member()
    refreshedMember.allocation.available = '2.00000000'
    listMembers
      .mockResolvedValueOnce({ items: [member()], total: 1, page: 1, page_size: 20, pages: 1 })
      .mockResolvedValueOnce({ items: [refreshedMember], total: 1, page: 1, page_size: 20, pages: 1 })
    removeMember
      .mockRejectedValueOnce(new Error('temporary network failure'))
      .mockRejectedValueOnce({ response: { status: 409, data: { reason: 'ALLOCATION_HAS_RESERVATIONS' } } })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-remove-member-11"]').trigger('click')
    await wrapper.get('[data-test="organization-remove-member-confirmation-confirm"]').trigger('click')
    await flushPromises()
    const firstKey = removeMember.mock.calls[0]?.[2]
    expect(firstKey).toMatch(/^organization-member-removal-/)
    expect(wrapper.find('[data-test="organization-remove-member-confirmation"]').exists()).toBe(true)

    await wrapper.get('[data-test="organization-remove-member-confirmation-confirm"]').trigger('click')
    await flushPromises()

    expect(removeMember.mock.calls[1]?.[2]).toBe(firstKey)
    expect(getBalance).toHaveBeenCalledTimes(2)
    expect(listMembers).toHaveBeenCalledTimes(2)
    expect(showError).toHaveBeenLastCalledWith('organization.overview.members.removeReservations')
    expect(wrapper.get('[data-test="organization-remove-member-confirmation"]').text()).toContain('2.00000000')
  })

  it('passes the preview confirmation through unchanged and uses an idempotency key', async () => {
    const preview = allocationPreviewFixture()
    previewAllocation.mockResolvedValueOnce(preview)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-equal-allocation"]').trigger('click')
    await flushPromises()
    expect(previewAllocation).toHaveBeenCalledWith('equal', {})

    await wrapper.get('[data-test="organization-allocation-confirm"]').trigger('click')
    await flushPromises()

    expect(executeAllocation).toHaveBeenCalledTimes(1)
    expect(executeAllocation.mock.calls[0][0]).toBe('equal')
    expect(executeAllocation.mock.calls[0][1].preview_token).toBe(preview.preview_token)
    expect(executeAllocation.mock.calls[0][1].confirmation).toStrictEqual(preview.confirmation)
    expect(executeAllocation.mock.calls[0][2]).toMatch(/^organization-allocation-/)
    expect(wrapper.get('[data-test="organization-allocation-result"]').text()).toContain('5.00000000')
  })

  it('normalizes custom and reclaim amounts as 8-place decimal strings without number conversion', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-allocation-amount-11"]').setValue('1.5')
    await wrapper.get('[data-test="organization-custom-allocation-preview"]').trigger('click')
    await flushPromises()
    expect(previewAllocation).toHaveBeenLastCalledWith('custom', {
      targets: [{ user_id: 11, amount: '1.50000000' }]
    })

    await wrapper.get('[data-test="organization-reclaim-mode"]').trigger('click')
    await wrapper.get('[data-test="organization-allocation-amount-11"]').setValue('2')
    await wrapper.get('[data-test="organization-reclaim-preview"]').trigger('click')
    await flushPromises()
    expect(previewAllocation).toHaveBeenLastCalledWith('reclaim', {
      targets: [{ user_id: 11, amount: '2.00000000' }]
    })
  })

  it('clears a stale 409 preview and requires the user to preview again', async () => {
    const memberPage = { items: [member()], total: 1, page: 1, page_size: 20, pages: 1 }
    let resolveBalanceRefresh!: (value: OrganizationBalanceSummary) => void
    let resolveMembersRefresh!: (value: typeof memberPage) => void
    const balanceRefresh = new Promise<OrganizationBalanceSummary>((resolve) => {
      resolveBalanceRefresh = resolve
    })
    const membersRefresh = new Promise<typeof memberPage>((resolve) => {
      resolveMembersRefresh = resolve
    })
    getBalance.mockResolvedValueOnce(balance()).mockReturnValueOnce(balanceRefresh)
    listMembers.mockResolvedValueOnce(memberPage).mockReturnValueOnce(membersRefresh)
    executeAllocation.mockRejectedValueOnce({
      status: 409,
      code: 409,
      reason: 'ORGANIZATION_ALLOCATION_STALE_PREVIEW'
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-equal-allocation"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="organization-allocation-confirm"]').exists()).toBe(true)

    await wrapper.get('[data-test="organization-allocation-confirm"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="organization-allocation-confirm"]').exists()).toBe(false)
    expect(getBalance).toHaveBeenCalledTimes(2)
    expect(listMembers).toHaveBeenCalledTimes(2)
    expect(showError).not.toHaveBeenCalled()

    resolveBalanceRefresh(balance())
    resolveMembersRefresh(memberPage)
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('organization.overview.allocations.stalePreview')

    await wrapper.get('[data-test="organization-equal-allocation"]').trigger('click')
    await flushPromises()
    expect(previewAllocation).toHaveBeenCalledTimes(2)
  })

  it('reuses the same idempotency key when an identical execute payload is retried', async () => {
    executeAllocation
      .mockRejectedValueOnce(new Error('temporary network failure'))
      .mockResolvedValueOnce(allocationResult())
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-equal-allocation"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="organization-allocation-confirm"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="organization-allocation-confirm"]').trigger('click')
    await flushPromises()

    expect(executeAllocation).toHaveBeenCalledTimes(2)
    expect(executeAllocation.mock.calls[0][2]).toBe(executeAllocation.mock.calls[1][2])
  })
})
