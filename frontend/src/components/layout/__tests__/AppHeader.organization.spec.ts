import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { User } from '@/types'
import AppHeader from '../AppHeader.vue'

const { authStore, appStore, routerPush } = vi.hoisted(() => ({
  authStore: {
    user: null as User | null,
    isAdmin: false,
    isOrganizationAdmin: false,
    isSimpleMode: false,
    logout: vi.fn()
  },
  appStore: {
    contactInfo: '',
    docUrl: '',
    cachedPublicSettings: null as null | { custom_menu_items?: Array<{ id: string; label: string }> },
    toggleMobileSidebar: vi.fn()
  },
  routerPush: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({
    name: 'OrganizationOverview',
    params: {},
    meta: {}
  }),
  useRouter: () => ({
    push: routerPush
  })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => appStore,
  useAuthStore: () => authStore,
  useOnboardingStore: () => ({ replay: vi.fn() })
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] })
}))

function createUser(overrides: Partial<User> = {}): User {
  return {
    id: 7,
    username: 'organization-admin',
    email: 'organization-admin@example.com',
    role: 'user',
    balance: 123.45,
    concurrency: 2,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: true,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '2026-07-28T00:00:00Z',
    updated_at: '2026-07-28T00:00:00Z',
    ...overrides
  }
}

function mountHeader() {
  return mount(AppHeader, {
    global: {
      mocks: {
        $t: (key: string) => key
      },
      stubs: {
        AnnouncementBell: true,
        Icon: true,
        LocaleSwitcher: true,
        SubscriptionProgressMini: {
          template: '<div data-test="subscription-progress" />'
        },
        RouterLink: {
          props: ['to'],
          template: '<a :data-route="to"><slot /></a>'
        }
      }
    }
  })
}

async function openUserMenu() {
  const wrapper = mountHeader()
  await wrapper.get('[aria-label="User Menu"]').trigger('click')
  await nextTick()
  return wrapper
}

describe('AppHeader organization authority', () => {
  beforeEach(() => {
    authStore.user = createUser()
    authStore.isAdmin = false
    authStore.isOrganizationAdmin = false
    authStore.isSimpleMode = false
    authStore.logout.mockReset()
    appStore.contactInfo = ''
    appStore.docUrl = ''
    appStore.cachedPublicSettings = null
    appStore.toggleMobileSidebar.mockReset()
    routerPush.mockReset()
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('hides personal balance, subscription progress, and management links for active organization administrators', async () => {
    authStore.isOrganizationAdmin = true

    const wrapper = await openUserMenu()

    expect(wrapper.find('[data-test="subscription-progress"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('$123.45')
    expect(wrapper.find('[data-route="/profile"]').exists()).toBe(false)
    expect(wrapper.find('[data-route="/keys"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('nav.logout')
  })

  it('keeps the complete personal header for platform administrators', async () => {
    authStore.user = createUser({ role: 'admin' })
    authStore.isAdmin = true
    authStore.isOrganizationAdmin = true

    const wrapper = await openUserMenu()

    expect(wrapper.find('[data-test="subscription-progress"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('$123.45')
    expect(wrapper.find('[data-route="/profile"]').exists()).toBe(true)
    expect(wrapper.find('[data-route="/keys"]').exists()).toBe(true)
  })

  it('keeps the personal header for users without an active organization administrator role', async () => {
    const wrapper = await openUserMenu()

    expect(wrapper.find('[data-test="subscription-progress"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('$123.45')
    expect(wrapper.find('[data-route="/profile"]').exists()).toBe(true)
    expect(wrapper.find('[data-route="/keys"]').exists()).toBe(true)
  })

  it('keeps the personal header for organization members', async () => {
    authStore.user = createUser({
      organization_role: 'organization_member',
      organization: {
        id: 3,
        name: 'Engineering',
        code: 'engineering',
        description: '',
        status: 'active'
      }
    })

    const wrapper = await openUserMenu()

    expect(wrapper.find('[data-test="subscription-progress"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('$123.45')
    expect(wrapper.find('[data-route="/profile"]').exists()).toBe(true)
    expect(wrapper.find('[data-route="/keys"]').exists()).toBe(true)
  })
})
