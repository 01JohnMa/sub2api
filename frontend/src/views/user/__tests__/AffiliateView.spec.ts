import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { UserAffiliateDetail } from '@/types'
import AffiliateView from '../AffiliateView.vue'

const {
  getAffiliateDetail,
  transferAffiliateQuota,
  showError,
  showSuccess,
  refreshUser,
  copyToClipboard,
} = vi.hoisted(() => ({
  getAffiliateDetail: vi.fn(),
  transferAffiliateQuota: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  refreshUser: vi.fn(),
  copyToClipboard: vi.fn(),
}))

vi.mock('@/api/user', () => ({
  default: {
    getAffiliateDetail,
    transferAffiliateQuota,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    refreshUser,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const longAffiliateCode = `affiliate-${'x'.repeat(160)}`

const detail: UserAffiliateDetail = {
  user_id: 1,
  aff_code: longAffiliateCode,
  inviter_id: null,
  aff_count: 0,
  aff_quota: 0,
  aff_frozen_quota: 0,
  aff_history_quota: 0,
  effective_rebate_rate_percent: 20,
  invitees: [],
}

const mountView = async (): Promise<VueWrapper> => {
  const wrapper = mount(AffiliateView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

describe('AffiliateView copy fields', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getAffiliateDetail.mockResolvedValue(detail)
    transferAffiliateQuota.mockResolvedValue({ transferred_quota: 0, balance: 0 })
    refreshUser.mockResolvedValue(undefined)
    copyToClipboard.mockResolvedValue(true)
  })

  it('keeps long values shrinkable and copy buttons visible', async () => {
    const wrapper = await mountView()
    const values = wrapper.findAll('code')

    expect(values).toHaveLength(2)
    for (const value of values) {
      expect(value.classes()).toEqual(expect.arrayContaining(['min-w-0', 'truncate']))

      const row = value.element.parentElement
      const field = row?.parentElement
      const button = row?.querySelector('button')

      expect(row?.classList.contains('min-w-0')).toBe(true)
      expect(field?.classList.contains('min-w-0')).toBe(true)
      expect(button?.classList.contains('shrink-0')).toBe(true)
    }
  })

  it('copies the original code and generated invite link', async () => {
    const wrapper = await mountView()
    const codeButton = getButtonByText(wrapper, 'affiliate.copyCode')
    const linkButton = getButtonByText(wrapper, 'affiliate.copyLink')
    const expectedLink = `${window.location.origin}/register?aff=${encodeURIComponent(longAffiliateCode)}`

    await codeButton.trigger('click')
    await linkButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenNthCalledWith(1, longAffiliateCode, 'affiliate.codeCopied')
    expect(copyToClipboard).toHaveBeenNthCalledWith(2, expectedLink, 'affiliate.linkCopied')
  })
})
