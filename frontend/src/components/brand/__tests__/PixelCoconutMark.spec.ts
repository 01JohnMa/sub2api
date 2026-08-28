import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PixelCoconutMark from '../PixelCoconutMark.vue'

describe('PixelCoconutMark', () => {
  it('renders an accessible pixel coconut mark', () => {
    const wrapper = mount(PixelCoconutMark, {
      props: {
        label: 'coococode pixel coconut',
        size: 'lg',
        variant: 'half',
      },
    })

    expect(wrapper.attributes('role')).toBe('img')
    expect(wrapper.attributes('aria-label')).toBe('coococode pixel coconut')
    expect(wrapper.classes()).toContain('h-24')
    expect(wrapper.attributes('data-coconut-variant')).toBe('half')
    expect(wrapper.find('svg').exists()).toBe(true)
  })

  it('uses the compact mono mark by default', () => {
    const wrapper = mount(PixelCoconutMark)

    expect(wrapper.attributes('data-coconut-variant')).toBe('mono')
    expect(wrapper.classes()).toContain('h-16')
  })
})
