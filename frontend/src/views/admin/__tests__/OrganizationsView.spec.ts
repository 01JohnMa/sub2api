import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { Organization, OrganizationMember } from '@/types'
import OrganizationsView from '../OrganizationsView.vue'

const {
  listOrganizations,
  getOrganization,
  createOrganization,
  updateOrganization,
	disableOrganization,
	reactivateOrganization,
	getOrganizationArchivePreview,
	archiveOrganization,
  listOrganizationMembers,
  addOrganizationMember,
  removeOrganizationMember,
  updateOrganizationMemberRole,
  getOrganizationBalance,
  listOrganizationLedger,
  creditOrganization,
  previewOrganizationAllocation,
  executeOrganizationAllocation,
  listPendingReconciliations,
  getPendingReconciliation,
  resolvePendingReconciliation
} = vi.hoisted(() => ({
  listOrganizations: vi.fn(),
  getOrganization: vi.fn(),
  createOrganization: vi.fn(),
  updateOrganization: vi.fn(),
	disableOrganization: vi.fn(),
	reactivateOrganization: vi.fn(),
	getOrganizationArchivePreview: vi.fn(),
	archiveOrganization: vi.fn(),
  listOrganizationMembers: vi.fn(),
  addOrganizationMember: vi.fn(),
  removeOrganizationMember: vi.fn(),
  updateOrganizationMemberRole: vi.fn(),
  getOrganizationBalance: vi.fn(),
  listOrganizationLedger: vi.fn(),
  creditOrganization: vi.fn(),
  previewOrganizationAllocation: vi.fn(),
  executeOrganizationAllocation: vi.fn(),
  listPendingReconciliations: vi.fn(),
  getPendingReconciliation: vi.fn(),
  resolvePendingReconciliation: vi.fn()
}))

const { showError, showSuccess } = vi.hoisted(() => ({
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    organizations: {
      list: listOrganizations,
      getById: getOrganization,
      create: createOrganization,
      update: updateOrganization,
		disable: disableOrganization,
		reactivate: reactivateOrganization,
		getArchivePreview: getOrganizationArchivePreview,
		archive: archiveOrganization,
      listMembers: listOrganizationMembers,
      addMember: addOrganizationMember,
      removeMember: removeOrganizationMember,
      updateMemberRole: updateOrganizationMemberRole,
      getBalance: getOrganizationBalance,
      listLedger: listOrganizationLedger,
      credit: creditOrganization,
      previewAllocation: previewOrganizationAllocation,
      executeAllocation: executeOrganizationAllocation,
      listPendingReconciliations,
      getPendingReconciliation,
      resolvePendingReconciliation
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess
  })
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

const organization = (): Organization => ({
  id: 7,
  name: 'Acme',
  code: 'acme',
  description: 'Platform organization',
  status: 'active',
  created_by: 1,
  created_at: '2026-07-28T00:00:00Z',
  updated_at: '2026-07-28T01:00:00Z'
})

const organizationMember = (): OrganizationMember => ({
  user_id: 11,
  email: 'member@example.com',
  username: 'member',
  status: 'active',
  organization_role: 'organization_member',
  allocation: {
    id: 9,
    status: 'active',
    available: '0.00000000',
    reserved: '0.00000000'
  }
})

function deferred<T>() {
	let resolve!: (value: T) => void
	const promise = new Promise<T>((resolvePromise) => {
		resolve = resolvePromise
	})
	return { promise, resolve }
}

function allocationPreview(operation: 'custom' | 'reclaim' = 'custom') {
  const delta = operation === 'custom' ? '2.50000000' : '-2.50000000'
  const before = operation === 'custom' ? '0.00000000' : '5.00000000'
  const after = operation === 'custom' ? '2.50000000' : '2.50000000'
  return {
    confirmation: {
      organization_id: 7,
      authorization_scope: 'platform_admin_jwt',
      operation_type: operation === 'custom' ? 'custom_allocation' : 'allocation_reclaim',
      currency: 'USD',
      money_scale: 8,
      organization_at: '2026-07-28T00:00:00Z',
      balance_version: 1,
      snapshot_hash: 'snapshot-hash',
      pool_before: '100.00000000',
      pool_after: operation === 'custom' ? '97.50000000' : '102.50000000',
      total_delta: delta,
      rounding_remainder: '0.00000000',
      targets: [{ user_id: 11, allocation_id: 9, before, delta, after }]
    },
    eligible_members: [],
    excluded_members: [],
    canonical_payload_hash: `${operation}-payload-hash`,
    preview_token: `${operation}-preview-token`,
    expires_at: '2026-07-28T00:05:00Z'
  }
}

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((column) => column.key).join(',') }}</div>
      <button data-test="sort-name" @click="$emit('sort', 'name', 'asc')">sort</button>
      <template v-for="row in data" :key="row.id">
        <slot name="cell-name" :row="row" :value="row.name" />
        <slot name="cell-actions" :row="row" :value="row.actions" />
      </template>
    </div>
  `
}

const SelectStub = {
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  template: `
    <select :value="modelValue" @change="$emit('update:modelValue', $event.target.value); $emit('change')">
      <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
    </select>
  `
}

const BaseDialogStub = {
  props: ['show', 'title'],
  template: '<div v-if="show"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>'
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

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: '<button data-test="pagination-next" @click="$emit(\'update:page\', page + 1)">next</button>'
}

function mountView() {
  return mount(OrganizationsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        Select: SelectStub,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: ConfirmDialogStub,
        Pagination: PaginationStub,
        EmptyState: true,
        Icon: true
      }
    }
  })
}

describe('admin OrganizationsView', () => {
  beforeEach(() => {
    localStorage.clear()
    listOrganizations.mockReset()
    getOrganization.mockReset()
    createOrganization.mockReset()
    updateOrganization.mockReset()
		disableOrganization.mockReset()
		reactivateOrganization.mockReset()
		getOrganizationArchivePreview.mockReset()
		archiveOrganization.mockReset()
    listOrganizationMembers.mockReset()
    addOrganizationMember.mockReset()
    removeOrganizationMember.mockReset()
    updateOrganizationMemberRole.mockReset()
    getOrganizationBalance.mockReset()
    listOrganizationLedger.mockReset()
    creditOrganization.mockReset()
    previewOrganizationAllocation.mockReset()
    executeOrganizationAllocation.mockReset()
		listPendingReconciliations.mockReset()
		getPendingReconciliation.mockReset()
		resolvePendingReconciliation.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    listOrganizations.mockResolvedValue({
      items: [organization()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    getOrganization.mockResolvedValue(organization())
    createOrganization.mockResolvedValue(organization())
    updateOrganization.mockResolvedValue(organization())
		disableOrganization.mockResolvedValue({ ...organization(), status: 'disabled' })
		reactivateOrganization.mockResolvedValue(organization())
		getOrganizationArchivePreview.mockResolvedValue({ organization_id: 7, status: 'active', unallocated: '12.50000000', balance_version: 3, updated_at: '2026-07-28T01:00:00Z' })
		archiveOrganization.mockResolvedValue({ ...organization(), status: 'archived' })
    listOrganizationMembers.mockResolvedValue({
      items: [organizationMember()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    addOrganizationMember.mockResolvedValue(organizationMember())
    removeOrganizationMember.mockResolvedValue({
      operation_id: 'remove-operation-id',
      correlation_id: 'remove-correlation-id',
      organization_id: 7,
      user_id: 11,
      allocation_id: 9,
      operation_type: 'member_removal_reclaim',
      currency: 'USD',
      outcome: 'committed',
      reclaimed: '0.00000000',
      pool_before: '100.00000000',
      pool_after: '100.00000000',
      balance_version_before: 1,
      balance_version_after: 2,
      replayed: false
    })
    updateOrganizationMemberRole.mockResolvedValue(organizationMember())
    getOrganizationBalance.mockResolvedValue({
      organization_id: 7,
      status: 'active',
      currency: 'USD',
      unallocated: '100.00000000',
      available: '0.00000000',
      reserved: '0.00000000',
      consumed: '0.00000000',
      total_remaining: '100.00000000',
      balance_version: 1
    })
    listOrganizationLedger.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 0
    })
    creditOrganization.mockResolvedValue({})
    previewOrganizationAllocation.mockResolvedValue(allocationPreview())
    executeOrganizationAllocation.mockResolvedValue({
      organization_id: 7,
      operation_type: 'custom_allocation',
      currency: 'USD',
      outcome: 'committed',
      pool_before: '100.00000000',
      pool_after: '97.50000000',
      total_delta: '2.50000000',
      balance_version_before: 1,
      balance_version_after: 2,
      replayed: false
    })
		listPendingReconciliations.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 0 })
		getPendingReconciliation.mockResolvedValue(null)
		resolvePendingReconciliation.mockResolvedValue({ replayed: false })
  })

  it('loads an organization table with the default created-at sort and supports server sort', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="columns"]').text()).toBe(
      'name,code,description,status,created_at,updated_at,actions'
    )
    const columns = wrapper.getComponent(DataTableStub).props('columns') as Array<{
      key: string
      sortable?: boolean
    }>
    expect(columns.find((column) => column.key === 'created_at')?.sortable).toBe(true)
    expect(columns.find((column) => column.key === 'updated_at')?.sortable).toBeUndefined()
    expect(listOrganizations).toHaveBeenLastCalledWith(
      1,
      20,
      {
        status: undefined,
        search: undefined,
        sort_by: 'created_at',
        sort_order: 'desc'
      },
      expect.any(Object)
    )

    await wrapper.get('[data-test="sort-name"]').trigger('click')
    await flushPromises()

    expect(listOrganizations).toHaveBeenLastCalledWith(
      1,
      20,
      expect.objectContaining({ sort_by: 'name', sort_order: 'asc' }),
      expect.any(Object)
    )
  })

  it('creates an organization without exposing status in the create payload', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-create"]').trigger('click')
    await wrapper.get('[data-test="organization-name"]').setValue(' Acme Platform ')
    await wrapper.get('[data-test="organization-code"]').setValue(' ACME ')
    await wrapper.get('[data-test="organization-description"]').setValue(' Internal platform organization ')
    await wrapper.get('[data-test="organization-form"]').trigger('submit')
    await flushPromises()

    expect(createOrganization).toHaveBeenCalledWith({
      name: 'Acme Platform',
      code: 'ACME',
      description: 'Internal platform organization'
    })
  })

  it('opens a detail view from the table and edits only identity fields', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()
    expect(getOrganization).toHaveBeenCalledWith(7)
    expect(getOrganizationBalance).toHaveBeenCalledWith(7)
    expect(listOrganizationLedger).toHaveBeenCalledWith(7, 1, 20)
    expect(wrapper.text()).toContain('Acme')

    await wrapper.get('[data-test="organization-detail-edit"]').trigger('click')
    await wrapper.get('[data-test="organization-name"]').setValue('Acme Updated')
    await wrapper.get('[data-test="organization-form"]').trigger('submit')
    await flushPromises()

    expect(updateOrganization).toHaveBeenCalledWith(7, {
      name: 'Acme Updated',
      code: 'acme',
      description: 'Platform organization'
    })
  })

  it('lists only safe organization member data and requires the allocation warning before adding', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()

    expect(listOrganizationMembers).toHaveBeenCalledWith(7, 1, 20)
    expect(wrapper.get('[data-test="organization-members-section"]').text()).toContain('member@example.com')

    await wrapper.get('[data-test="organization-add-member"]').trigger('click')
    await wrapper.get('[data-test="organization-member-user-id"]').setValue('12')
    await wrapper.get('[data-test="organization-add-member-form"]').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-test="organization-add-member-confirmation"]').text()).toContain(
      'admin.organizations.members.addMemberWarning'
    )
    expect(addOrganizationMember).not.toHaveBeenCalled()

    await wrapper.get('[data-test="organization-add-member-confirm"]').trigger('click')
    await flushPromises()

    expect(addOrganizationMember).toHaveBeenCalledWith(7, {
      user_id: 12,
      organization_role: 'organization_member'
    })
  })

  it('requires a decimal user ID before showing the member conversion confirmation', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="organization-add-member"]').trigger('click')
    await wrapper.get('[data-test="organization-member-user-id"]').setValue('1e2')
    await wrapper.get('[data-test="organization-add-member-form"]').trigger('submit')
    await flushPromises()

    expect(wrapper.find('[data-test="organization-add-member-confirmation"]').exists()).toBe(false)
    expect(addOrganizationMember).not.toHaveBeenCalled()
  })

  it('updates an organization role without touching platform user controls', async () => {
    const updatedMember = { ...organizationMember(), organization_role: 'organization_admin' as const }
    updateOrganizationMemberRole.mockResolvedValueOnce(updatedMember)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="organization-member-role-11"]').setValue('organization_admin')
    await flushPromises()

    expect(updateOrganizationMemberRole).toHaveBeenCalledWith(7, 11, 'organization_admin')
    expect(wrapper.text()).not.toContain('admin.users.balance')
  })

  it('removes a member only after showing reclaimed funds and personal Key recovery consequences', async () => {
    const fundedMember = {
      ...organizationMember(),
      allocation: { ...organizationMember().allocation, available: '4.00000000' }
    }
    listOrganizationMembers.mockResolvedValueOnce({
      items: [fundedMember],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="organization-remove-member-11"]').trigger('click')
    const confirmation = wrapper.get('[data-test="organization-remove-member-confirmation"]')
    expect(confirmation.text()).toContain('admin.organizations.members.removeMemberWarning')
    expect(confirmation.text()).toContain('4.00000000')
    expect(confirmation.text()).toContain('member')

    await wrapper.get('[data-test="organization-remove-member-confirmation-confirm"]').trigger('click')
    await flushPromises()

    expect(removeOrganizationMember).toHaveBeenCalledWith(
      7,
      11,
      {},
      expect.stringMatching(/^organization-member-removal-/)
    )
    expect(getOrganizationBalance).toHaveBeenCalledTimes(2)
    expect(listOrganizationMembers).toHaveBeenCalledTimes(2)
  })

  it('allows only zero-allocation member removal after archival', async () => {
    const archived = { ...organization(), status: 'archived' as const }
    const removable = organizationMember()
    const funded = {
      ...organizationMember(),
      user_id: 12,
      allocation: { ...organizationMember().allocation, available: '1.00000000' }
    }
    listOrganizations.mockResolvedValueOnce({ items: [archived], total: 1, page: 1, page_size: 20, pages: 1 })
    getOrganization.mockResolvedValueOnce(archived)
    listOrganizationMembers.mockResolvedValueOnce({
      items: [removable, funded],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="organization-remove-member-11"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-test="organization-remove-member-12"]').attributes('disabled')).toBeDefined()
  })

  it('reuses the removal idempotency key and refreshes the displayed amount after reservation conflicts', async () => {
    const initialMember = organizationMember()
    initialMember.allocation.available = '4.00000000'
    const refreshedMember = organizationMember()
    refreshedMember.allocation.available = '2.00000000'
    listOrganizationMembers
      .mockResolvedValueOnce({ items: [initialMember], total: 1, page: 1, page_size: 20, pages: 1 })
      .mockResolvedValueOnce({ items: [refreshedMember], total: 1, page: 1, page_size: 20, pages: 1 })
    removeOrganizationMember
      .mockRejectedValueOnce(new Error('temporary network failure'))
      .mockRejectedValueOnce({ response: { status: 409, data: { reason: 'ALLOCATION_HAS_RESERVATIONS' } } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="organization-remove-member-11"]').trigger('click')
    await wrapper.get('[data-test="organization-remove-member-confirmation-confirm"]').trigger('click')
    await flushPromises()
    const firstKey = removeOrganizationMember.mock.calls[0]?.[3]

    await wrapper.get('[data-test="organization-remove-member-confirmation-confirm"]').trigger('click')
    await flushPromises()

    expect(removeOrganizationMember.mock.calls[1]?.[3]).toBe(firstKey)
    expect(listOrganizationMembers).toHaveBeenCalledTimes(2)
    expect(showError).toHaveBeenLastCalledWith('admin.organizations.members.removeReservations')
    expect(wrapper.get('[data-test="organization-remove-member-confirmation"]').text()).toContain('2.00000000')
  })

  it('shows the organization funding summary and sends decimal credit with idempotency', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="organization-funding-section"]').text()).toContain('100.00000000')

    await wrapper.get('[data-test="organization-credit-open"]').trigger('click')
    await wrapper.get('[data-test="organization-credit-amount"]').setValue('12.50000000')
    await wrapper.get('[data-test="organization-credit-public-reason"]').setValue('Quarterly credit')
    await wrapper.get('[data-test="organization-credit-internal-reason"]').setValue('approved by finance')
    await wrapper.get('[data-test="organization-credit-form"]').trigger('submit')
    await flushPromises()

    expect(creditOrganization).toHaveBeenCalledWith(
      7,
      {
        amount: '12.50000000',
        public_reason: 'Quarterly credit',
        internal_reason: 'approved by finance'
      },
      expect.stringMatching(/^organization-credit-/)
    )
    expect(getOrganizationBalance).toHaveBeenCalledTimes(2)
    expect(listOrganizationLedger).toHaveBeenCalledTimes(2)
  })

  it('rejects invalid credit amounts before calling the API', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="organization-credit-open"]').trigger('click')
    await wrapper.get('[data-test="organization-credit-amount"]').setValue('1e3')
    await wrapper.get('[data-test="organization-credit-internal-reason"]').setValue('approved by finance')
    await wrapper.get('[data-test="organization-credit-form"]').trigger('submit')
    await flushPromises()

    expect(creditOrganization).not.toHaveBeenCalled()
  })

  it('reuses the same idempotency key when the same failed credit is retried', async () => {
    creditOrganization.mockRejectedValueOnce(new Error('temporary failure')).mockResolvedValueOnce({})
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="organization-credit-open"]').trigger('click')
    await wrapper.get('[data-test="organization-credit-amount"]').setValue('3.00000000')
    await wrapper.get('[data-test="organization-credit-internal-reason"]').setValue('approved by finance')

    await wrapper.get('[data-test="organization-credit-form"]').trigger('submit')
    await flushPromises()
    const firstKey = creditOrganization.mock.calls[0]?.[2]
    expect(firstKey).toMatch(/^organization-credit-/)
    expect(showError).toHaveBeenCalled()

    await wrapper.get('[data-test="organization-credit-form"]').trigger('submit')
    await flushPromises()
    expect(creditOrganization.mock.calls[1]?.[2]).toBe(firstKey)
  })

  it('previews and confirms a custom platform allocation without rebuilding the server plan', async () => {
    const preview = allocationPreview()
    previewOrganizationAllocation.mockResolvedValueOnce(preview)
    const organizationAdmin = { ...organizationMember(), organization_role: 'organization_admin' as const }
    listOrganizationMembers.mockResolvedValueOnce({
      items: [organizationAdmin],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="organization-allocation-amount-11"]').setValue('2.5')
    await wrapper.get('[data-test="organization-allocation-custom-preview"]').trigger('click')
    await flushPromises()

    expect(previewOrganizationAllocation).toHaveBeenCalledWith(7, 'custom', {
      targets: [{ user_id: 11, amount: '2.50000000' }]
    })
    expect(wrapper.get('[data-test="organization-allocation-preview-dialog"]').text()).toContain('2.50000000')

    await wrapper.get('[data-test="organization-allocation-confirm"]').trigger('click')
    await flushPromises()

    expect(executeOrganizationAllocation).toHaveBeenCalledWith(
      7,
      'custom',
      { preview_token: 'custom-preview-token', confirmation: preview.confirmation },
      expect.stringMatching(/^organization-allocation-/)
    )
    expect(wrapper.get('[data-test="organization-allocation-result"]').text()).toContain(
      'admin.organizations.allocations.resultSummary'
    )
  })

  it('allows custom and reclaim previews for a disabled current member in an active organization', async () => {
    const disabledMember = { ...organizationMember(), status: 'disabled' as const }
    listOrganizationMembers.mockResolvedValueOnce({
      items: [disabledMember],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="organization-allocation-section"]').text()).toContain('admin.users.disabled')
    await wrapper.get('[data-test="organization-allocation-amount-11"]').setValue('1')
    await wrapper.get('[data-test="organization-allocation-custom-preview"]').trigger('click')
    await flushPromises()
    expect(previewOrganizationAllocation).toHaveBeenLastCalledWith(7, 'custom', {
      targets: [{ user_id: 11, amount: '1.00000000' }]
    })

    await wrapper.get('[data-test="organization-allocation-reclaim-mode"]').trigger('click')
    await wrapper.get('[data-test="organization-allocation-reclaim-preview"]').trigger('click')
    await flushPromises()
    expect(previewOrganizationAllocation).toHaveBeenLastCalledWith(7, 'reclaim', {
      targets: [{ user_id: 11, amount: '1.00000000' }]
    })
  })

  it('reuses the idempotency key for a failed allocation confirmation and clears stale previews', async () => {
    previewOrganizationAllocation.mockResolvedValueOnce(allocationPreview())
    executeOrganizationAllocation
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockRejectedValueOnce({ response: { status: 409, data: { code: 'STALE_PREVIEW' } } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="organization-allocation-amount-11"]').setValue('2.5')
    await wrapper.get('[data-test="organization-allocation-custom-preview"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="organization-allocation-confirm"]').trigger('click')
    await flushPromises()
    const firstKey = executeOrganizationAllocation.mock.calls[0]?.[3]
    expect(firstKey).toMatch(/^organization-allocation-/)

    await wrapper.get('[data-test="organization-allocation-confirm"]').trigger('click')
    await flushPromises()
    expect(executeOrganizationAllocation.mock.calls[1]?.[3]).toBe(firstKey)
    expect(wrapper.find('[data-test="organization-allocation-preview-dialog"]').exists()).toBe(false)
    expect(showError).toHaveBeenLastCalledWith('admin.organizations.allocations.stalePreview')
  })

  it('limits disabled organizations to reclaim and blocks allocation controls for archived organizations', async () => {
    const disabled = { ...organization(), status: 'disabled' as const }
    const disabledMember = { ...organizationMember(), status: 'disabled' as const }
    listOrganizations.mockResolvedValueOnce({ items: [disabled], total: 1, page: 1, page_size: 20, pages: 1 })
    getOrganization.mockResolvedValueOnce(disabled)
    listOrganizationMembers.mockResolvedValueOnce({
      items: [disabledMember],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const disabledWrapper = mountView()
    await flushPromises()
    await disabledWrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()

    expect(disabledWrapper.find('[data-test="organization-allocation-custom-mode"]').exists()).toBe(false)
    expect(disabledWrapper.get('[data-test="organization-allocation-reclaim-only"]').exists()).toBe(true)
    expect(disabledWrapper.get('[data-test="organization-allocation-reclaim-preview"]').exists()).toBe(true)
    await disabledWrapper.get('[data-test="organization-allocation-amount-11"]').setValue('1')
    await disabledWrapper.get('[data-test="organization-allocation-reclaim-preview"]').trigger('click')
    await flushPromises()
    expect(previewOrganizationAllocation).toHaveBeenLastCalledWith(7, 'reclaim', {
      targets: [{ user_id: 11, amount: '1.00000000' }]
    })

    const archived = { ...organization(), status: 'archived' as const }
    listOrganizations.mockResolvedValueOnce({ items: [archived], total: 1, page: 1, page_size: 20, pages: 1 })
    getOrganization.mockResolvedValueOnce(archived)
    const archivedWrapper = mountView()
    await flushPromises()
    await archivedWrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()

    expect(archivedWrapper.find('[data-test="organization-allocation-custom-preview"]').exists()).toBe(false)
    expect(archivedWrapper.find('[data-test="organization-allocation-reclaim-preview"]').exists()).toBe(false)
  })

  it('keeps membership writes available for disabled organizations', async () => {
    const disabled = { ...organization(), status: 'disabled' as const }
    listOrganizations.mockResolvedValueOnce({
      items: [disabled],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    getOrganization.mockResolvedValueOnce(disabled)

    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="organization-add-member"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="organization-member-role-11"]').exists()).toBe(true)
  })

  it('keeps archived organizations read-only in table and detail actions', async () => {
    const archived = { ...organization(), status: 'archived' as const }
    listOrganizations.mockResolvedValueOnce({
      items: [archived],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    getOrganization.mockResolvedValueOnce(archived)

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-test="organization-edit"]').exists()).toBe(false)
    await wrapper.get('[data-test="organization-view"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="organization-detail-edit"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="organization-add-member"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="organization-member-role-11"]').exists()).toBe(false)
  })

	it('shows server-computed reconciliation actions and keeps conflict evidence read-only', async () => {
    const pending = {
      execution_id: 'execution-1', organization_id: 7, allocation_id: 9, reservation_id: 12,
      candidate_ceiling: '2.00000000', reserved_amount: '2.00000000', accepted_at: '2026-07-28T00:00:00Z',
      attempt_count: 1, attempts: [], evidence: [], allowed_resolution: 'settled' as const,
      computed_organization_amount: '1.00000000', conflict: false
    }
    listPendingReconciliations
      .mockResolvedValueOnce({ items: [pending], total: 21, page: 1, page_size: 20, pages: 2 })
      .mockResolvedValue({ items: [pending], total: 21, page: 2, page_size: 20, pages: 2 })
    getPendingReconciliation.mockResolvedValue({
      ...pending,
      attempts: [{ attempt_id: 'attempt-1', attempt_sequence: 1, status: 'final_usage_observed' }],
      evidence: [{ attempt_id: 'attempt-1', attempt_sequence: 1, evidence_source: 'adapter_direct', terminal_outcome: 'success', raw_actual_cost: '1.00000000', input_tokens: 2, output_tokens: 3, cache_creation_tokens: 0, cache_read_tokens: 0, provider_observed_at: '2026-07-28T00:00:00Z' }]
    })
		const wrapper = mountView()
		await flushPromises()
		await wrapper.get('[data-test="organization-view"]').trigger('click')
		await flushPromises()

    expect(wrapper.get('[data-test="organization-reconciliation-section"]').text()).toContain('execution-1')
    expect(wrapper.get('[data-test="organization-reconciliation-section"]').text()).toContain('1.00000000')
    await wrapper.get('[data-test="organization-reconciliation-pagination"] [data-test="pagination-next"]').trigger('click')
    await flushPromises()
    expect(listPendingReconciliations).toHaveBeenCalledWith(7, 2, 20)
		await wrapper.get('[data-test="organization-reconciliation-item"] button').trigger('click')
		await flushPromises()
		expect(wrapper.text()).toContain('adapter_direct')
		await wrapper.get('[data-test="organization-reconciliation-resolve"]').trigger('click')
		await wrapper.get('[data-test="organization-reconciliation-confirmation-confirm"]').trigger('click')
    await flushPromises()
    expect(resolvePendingReconciliation).toHaveBeenCalledWith(7, 'execution-1', expect.stringMatching(/^organization-reconciliation-/))

    resolvePendingReconciliation.mockRejectedValueOnce({ response: { status: 409, data: { detail: 'evidence conflict' } } })
    listPendingReconciliations.mockResolvedValueOnce({
      items: [{ ...pending, conflict: true, allowed_resolution: undefined }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    await wrapper.get('[data-test="organization-reconciliation-resolve"]').trigger('click')
    await wrapper.get('[data-test="organization-reconciliation-confirmation-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="organization-reconciliation-confirmation"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="organization-reconciliation-resolve"]').exists()).toBe(false)
  })

	it('confirms lifecycle transitions and binds archive to the refreshed balance version', async () => {
		const active = organization()
		const disabled = { ...active, status: 'disabled' as const }
		const archived = { ...active, status: 'archived' as const }
		getOrganization
			.mockReset()
			.mockResolvedValueOnce(active)
			.mockResolvedValueOnce(disabled)
			.mockResolvedValueOnce(active)
			.mockResolvedValueOnce(archived)
		const wrapper = mountView()
		await flushPromises()
		await wrapper.get('[data-test="organization-view"]').trigger('click')
		await flushPromises()

		await wrapper.get('[data-test="organization-disable"]').trigger('click')
		await wrapper.get('[data-test="organization-lifecycle-confirmation-confirm"]').trigger('click')
		await flushPromises()
		expect(disableOrganization).toHaveBeenCalledWith(7)

		await wrapper.get('[data-test="organization-reactivate"]').trigger('click')
		await wrapper.get('[data-test="organization-lifecycle-confirmation-confirm"]').trigger('click')
		await flushPromises()
		expect(reactivateOrganization).toHaveBeenCalledWith(7)

		await wrapper.get('[data-test="organization-archive"]').trigger('click')
		await flushPromises()
		expect(getOrganizationArchivePreview).toHaveBeenCalledWith(7)
		expect(wrapper.get('[data-test="organization-lifecycle-section"]').text()).toContain('12.50000000')
		await wrapper.get('[data-test="organization-lifecycle-confirmation-confirm"]').trigger('click')
		await flushPromises()
		expect(archiveOrganization).toHaveBeenCalledWith(7, {
			balance_version: 3,
			updated_at: '2026-07-28T01:00:00Z',
			confirm_archive: true
		})
	})

	it('ignores stale detail responses after rapidly selecting another organization or closing', async () => {
		const organizationA = organization()
		const organizationB = { ...organization(), id: 8, name: 'Beta', code: 'beta' }
		const detailA = deferred<Organization>()
		const detailB = deferred<Organization>()
		listOrganizations.mockResolvedValue({ items: [organizationA, organizationB], total: 2, page: 1, page_size: 20, pages: 1 })
		getOrganization.mockImplementation((id: number) => id === 7 ? detailA.promise : detailB.promise)

		const wrapper = mountView()
		await flushPromises()
		const viewButtons = wrapper.findAll('[data-test="organization-view"]')
		await viewButtons[0].trigger('click')
		await viewButtons[1].trigger('click')
		detailA.resolve({ ...organizationA, name: 'Stale Alpha' })
		await flushPromises()
		expect(wrapper.text()).toContain('Beta')
		expect(wrapper.text()).not.toContain('Stale Alpha')

		const openDialog = wrapper.findAllComponents(BaseDialogStub).find((dialog) => dialog.props('show'))
		expect(openDialog).toBeTruthy()
		openDialog!.vm.$emit('close')
		detailB.resolve({ ...organizationB, name: 'Late Beta' })
		await flushPromises()
		expect(wrapper.text()).not.toContain('Late Beta')
	})

	it('discards an archive preview when selection changes and never submits it for another organization', async () => {
		const organizationA = organization()
		const organizationB = { ...organization(), id: 8, name: 'Beta', code: 'beta' }
		const previewA = deferred<{
			organization_id: number
			status: 'active'
			unallocated: string
			balance_version: number
			updated_at: string
		}>()
		listOrganizations.mockResolvedValue({ items: [organizationA, organizationB], total: 2, page: 1, page_size: 20, pages: 1 })
		getOrganization.mockImplementation(async (id: number) => id === 7 ? organizationA : organizationB)
		getOrganizationArchivePreview.mockReturnValueOnce(previewA.promise)

		const wrapper = mountView()
		await flushPromises()
		const viewButtons = wrapper.findAll('[data-test="organization-view"]')
		await viewButtons[0].trigger('click')
		await flushPromises()
		await wrapper.get('[data-test="organization-archive"]').trigger('click')
		await viewButtons[1].trigger('click')
		await flushPromises()
		previewA.resolve({
			organization_id: 7,
			status: 'active',
			unallocated: '12.50000000',
			balance_version: 3,
			updated_at: '2026-07-28T01:00:00Z'
		})
		await flushPromises()

		expect(wrapper.text()).toContain('Beta')
		expect(wrapper.text()).not.toContain('12.50000000')
		expect(wrapper.find('[data-test="organization-lifecycle-confirmation"]').exists()).toBe(false)
		expect(archiveOrganization).not.toHaveBeenCalled()
	})
})
