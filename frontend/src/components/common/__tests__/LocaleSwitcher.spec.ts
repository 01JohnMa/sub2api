import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: {
        changeLanguage: () => 'Change language',
      },
    },
  },
})

describe('LocaleSwitcher', () => {
  it('keeps the home-page variant legible and exposes menu state', async () => {
    const wrapper = mount(LocaleSwitcher, {
      props: {
        fixedLight: true,
      },
      global: {
        plugins: [i18n],
        stubs: {
          Icon: true,
          transition: false,
        },
      },
    })
    const trigger = wrapper.get('button')

    expect(trigger.attributes('aria-label')).toBe('Change language')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(trigger.classes()).toContain('text-gray-950')
    expect(trigger.classes()).not.toContain('dark:text-gray-300')

    await trigger.trigger('click')

    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('[role="group"]').classes()).not.toContain('dark:bg-dark-800')
    expect(wrapper.findAll('#locale-options button[aria-pressed]')).toHaveLength(2)
    expect(wrapper.findAll('#locale-options button[aria-pressed="true"]')).toHaveLength(1)
  })
})
