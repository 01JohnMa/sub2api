import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { OrganizationMember, OrganizationReport, OrganizationSummary } from '@/types'
import OverviewView from '../OverviewView.vue'

const mocks = vi.hoisted(() => ({
  getCurrent: vi.fn(), listMembers: vi.fn(), getReport: vi.fn(), addMember: vi.fn(), removeMember: vi.fn(),
  showError: vi.fn(), showSuccess: vi.fn(), replace: vi.fn(), authStore: { user: { id: 1 }, isAdmin: false, isOrganizationAdmin: true }
}))

vi.mock('@/api', () => ({ organizationAPI: { getCurrent: mocks.getCurrent, listMembers: mocks.listMembers, getReport: mocks.getReport, addMember: mocks.addMember, removeMember: mocks.removeMember } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.authStore }))
vi.mock('vue-router', () => ({ useRouter: () => ({ replace: mocks.replace }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const organization = (): OrganizationSummary => ({ id: 7, name: 'Acme', code: 'acme', description: 'Engineering', status: 'active' })
const members = (): { items: OrganizationMember[]; total: number; page: number; page_size: number; pages: number } => ({ items: [{ user_id: 11, email: 'member@example.com', username: 'member', organization_role: 'organization_member', status: 'active' }], total: 1, page: 1, page_size: 20, pages: 1 })
const report = (): OrganizationReport => ({ organization_id: 7, start: '', end: '', current_member_count: 1, active_member_count: 1, api_key_count: 1, request_count_total: 2, success_count: 1, error_count_total: 1, business_limited_count: 0, error_count_sla: 1, success_rate: 0.5, error_rate: 0.5, input_tokens: 3, output_tokens: 4, cache_creation_tokens: 0, cache_read_tokens: 0, cost: '1.00000000', models: [], trends: [] })
const mountOptions = {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      BaseDialog: { template: '<div v-if="show"><slot /></div>', props: ['show'] },
      ConfirmDialog: { template: '<div v-if="show" />', props: ['show'] },
      Icon: true,
      LoadingSpinner: true,
      Pagination: true
    }
  }
}

describe('organization overview', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getCurrent.mockResolvedValue(organization())
    mocks.listMembers.mockResolvedValue(members())
    mocks.getReport.mockResolvedValue(report())
  })

  it('renders identity, reporting, and members only', async () => {
    const wrapper = mount(OverviewView, mountOptions)
    await flushPromises()
    expect(wrapper.find('[data-test="organization-identity-section"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="organization-report-section"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="organization-members-section"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="organization-balance-section"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="organization-public-ledger-section"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('organization.overview.allocations')
  })

  it('loads only server-scoped identity, member, and report data', async () => {
    mount(OverviewView, mountOptions)
    await flushPromises()
    expect(mocks.getCurrent).toHaveBeenCalledTimes(1)
    expect(mocks.listMembers).toHaveBeenCalledWith(1, 20)
    expect(mocks.getReport).toHaveBeenCalledTimes(1)
  })
})
