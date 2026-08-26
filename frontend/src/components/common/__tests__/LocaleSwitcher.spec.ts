import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: { common: { changeLanguage: () => 'Change language' } },
  },
})

describe('LocaleSwitcher', () => {
  it('exposes menu state and a localized accessible name', async () => {
    const wrapper = mount(LocaleSwitcher, {
      global: {
        plugins: [i18n],
        stubs: { Icon: true, transition: false },
      },
    })
    const trigger = wrapper.get('button')

    expect(trigger.attributes('type')).toBe('button')
    expect(trigger.attributes('aria-label')).toBe('Change language')
    expect(trigger.attributes('aria-expanded')).toBe('false')

    await trigger.trigger('click')

    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('[role="group"]').attributes('aria-label')).toBe('Change language')
    expect(wrapper.findAll('#locale-options button[aria-pressed]')).toHaveLength(2)
  })
})
