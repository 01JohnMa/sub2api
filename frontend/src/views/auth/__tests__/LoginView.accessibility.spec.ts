import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LoginView from '@/views/auth/LoginView.vue'

const { getPublicSettingsMock } = vi.hoisted(() => ({ getPublicSettingsMock: vi.fn() }))

vi.mock('vue-router', () => ({
  useRouter: () => ({ currentRoute: { value: { query: {} } }, push: vi.fn() }),
}))
vi.mock('@/stores', () => ({
  useAuthStore: () => ({ login: vi.fn(), login2FA: vi.fn() }),
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }),
}))
vi.mock('@/api/auth', () => ({
  getPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args),
  isTotp2FARequired: () => false,
  isWeChatWebOAuthEnabled: () => false,
}))
vi.mock('@/utils/oauthAffiliate', () => ({ clearAllAffiliateReferralCodes: vi.fn() }))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      auth: {
        welcomeBack: () => 'Welcome back',
        signInToAccount: () => 'Sign in to continue',
        emailLabel: () => 'Email', emailPlaceholder: () => 'Enter email',
        passwordLabel: () => 'Password', passwordPlaceholder: () => 'Enter password',
        showPassword: () => 'Show password', hidePassword: () => 'Hide password',
        signIn: () => 'Sign in', signingIn: () => 'Signing in', forgotPassword: () => 'Forgot password?',
        oauthOrContinue: () => 'Or continue with', dontHaveAccount: () => "Don't have an account?", signUp: () => 'Sign up',
      },
    },
  },
})

describe('LoginView password visibility control', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    getPublicSettingsMock.mockResolvedValue({
      turnstile_enabled: false, turnstile_site_key: '', linuxdo_oauth_enabled: false,
      dingtalk_oauth_enabled: false, backend_mode_enabled: false, oidc_oauth_enabled: false,
      oidc_oauth_provider_name: 'OIDC', github_oauth_enabled: false, google_oauth_enabled: false,
      password_reset_enabled: false, login_agreement_enabled: false,
    })
  })

  it('announces its action and pressed state', async () => {
    const wrapper = mount(LoginView, {
      global: {
        plugins: [i18n],
        stubs: {
          AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' }, Icon: true,
          TurnstileWidget: true, LoginAgreementPrompt: true, TotpLoginModal: true,
          EmailOAuthButtons: true, LinuxDoOAuthSection: true, DingTalkOAuthSection: true,
          WechatOAuthSection: true, OidcOAuthSection: true, RouterLink: true,
        },
      },
    })
    await flushPromises()

    const password = wrapper.get('#password')
    const toggle = wrapper.get('button[aria-label="Show password"]')
    expect(password.attributes('type')).toBe('password')
    expect(toggle.attributes('aria-pressed')).toBe('false')

    await toggle.trigger('click')
    expect(password.attributes('type')).toBe('text')
    expect(toggle.attributes('aria-label')).toBe('Hide password')
    expect(toggle.attributes('aria-pressed')).toBe('true')
  })
})
