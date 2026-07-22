import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe.each([
  ['en', en, 'Change language', 'Show password', 'Hide password'],
  ['zh', zh, '切换语言', '显示密码', '隐藏密码']
] as const)('%s coococode locale contract', (_locale, messages, language, show, hide) => {
  it('defines the accessibility labels used by shared auth controls', () => {
    expect(messages.common.changeLanguage).toBe(language)
    expect(messages.auth.showPassword).toBe(show)
    expect(messages.auth.hidePassword).toBe(hide)
  })

  it('keeps both onboarding entry points on the coococode brand', () => {
    for (const welcome of [messages.onboarding.admin.welcome, messages.onboarding.user.welcome]) {
      expect(welcome.title).toContain('coococode')
      expect(welcome.description).toContain('coococode')
      expect(welcome.title).not.toContain('Sub2API')
      expect(welcome.description).not.toContain('Sub2API')
    }
  })
})
