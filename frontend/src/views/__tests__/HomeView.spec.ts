import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import HomeView from '@/views/HomeView.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

type MessageTree = string | { [key: string]: MessageTree }

function toRuntimeMessages(value: MessageTree): unknown {
  if (typeof value === 'string') {
    return (context: { named: (key: string) => unknown }) =>
      value.replace(/\{(\w+)\}/g, (_match, key: string) => String(context.named(key)))
  }

  return Object.fromEntries(
    Object.entries(value).map(([key, message]) => [key, toRuntimeMessages(message)])
  )
}

const { authStore, appStore } = vi.hoisted(() => ({
  authStore: {
    isAuthenticated: false,
    isAdmin: false,
    checkAuth: vi.fn(),
  },
  appStore: {
    cachedPublicSettings: null as null | Record<string, string>,
    siteName: 'coococode',
    siteLogo: '',
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  },
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => authStore,
  useAppStore: () => appStore,
}))

function mountHome(locale: 'en' | 'zh' = 'en') {
  const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: 'en',
    messages: {
      en: toRuntimeMessages(en as MessageTree),
      zh: toRuntimeMessages(zh as MessageTree),
    },
  })

  const wrapper = mount(HomeView, {
    global: {
      plugins: [i18n],
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a :href="to"><slot /></a>',
        },
        LocaleSwitcher: true,
        BrandWordmark: {
          props: ['text'],
          template: '<span>{{ text }}</span>',
        },
        PixelCoconutMark: true,
      },
    },
  })

  return { wrapper, i18n }
}

describe('HomeView', () => {
  beforeEach(() => {
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    authStore.checkAuth.mockReset()
    appStore.cachedPublicSettings = null
    appStore.siteName = 'coococode'
    appStore.siteLogo = ''
    appStore.publicSettingsLoaded = true
    appStore.fetchPublicSettings.mockReset()
  })

  it('sanitizes administrator-provided HTML before rendering it', () => {
    appStore.cachedPublicSettings = {
      home_content:
        '<section class="safe">Allowed</section><script>alert(1)</script><img src="x" onerror="alert(2)"><iframe src="https://example.com"></iframe>',
    }

    const { wrapper } = mountHome()

    expect(wrapper.find('.safe').text()).toBe('Allowed')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.find('img').attributes('onerror')).toBeUndefined()
  })

  it('isolates a URL-based custom home page in a sandboxed iframe', () => {
    appStore.cachedPublicSettings = {
      site_name: 'Coo',
      home_content: 'https://example.com/landing',
    }

    const { wrapper } = mountHome()
    const frame = wrapper.get('iframe')

    expect(frame.attributes('src')).toBe('https://example.com/landing')
    expect(frame.attributes('title')).toBe('Coo custom home page')
    expect(frame.attributes('sandbox')).toContain('allow-scripts')
    expect(frame.attributes('sandbox')).not.toContain('allow-same-origin')
  })

  it('updates all built-in landing-page copy when the locale changes', async () => {
    const { wrapper, i18n } = mountHome('zh')

    expect(wrapper.text()).toContain('正宗海岛中转站')
    expect(wrapper.text()).toContain('真用量看板')
    expect(wrapper.get('pixel-coconut-mark-stub').attributes('label')).toBe('coococode 像素椰子标志')

    i18n.global.locale.value = 'en'
    await nextTick()

    expect(wrapper.text()).toContain('The Genuine Island Relay')
    expect(wrapper.text()).toContain('Real Usage Dashboard')
    expect(wrapper.get('pixel-coconut-mark-stub').attributes('label')).toBe('coococode pixel coconut mark')
    expect(wrapper.text()).not.toContain('正宗海岛中转站')
    expect(wrapper.text()).not.toContain('真用量看板')
  })
})
