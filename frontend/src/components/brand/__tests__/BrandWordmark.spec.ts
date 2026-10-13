import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import BrandWordmark from '../BrandWordmark.vue'

describe('BrandWordmark', () => {
  it('replaces every o in coococode with a young coconut letter', () => {
    const wrapper = mount(BrandWordmark, {
      props: {
        text: 'coococode',
        size: 'inline',
      },
    })

    expect(wrapper.attributes('role')).toBe('img')
    expect(wrapper.attributes('aria-label')).toBe('coococode')
    expect(wrapper.attributes('class')).toContain('brand-wordmark--inline')
    expect(wrapper.findAll('[data-coconut-letter]').length).toBe(4)
  })
})
