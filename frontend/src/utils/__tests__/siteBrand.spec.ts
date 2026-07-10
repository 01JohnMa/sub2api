import { describe, expect, it } from 'vitest'
import {
  DEFAULT_VISIBLE_SITE_NAME,
  normalizeVisibleSiteName,
  replaceVisibleUpstreamBrand
} from '@/utils/siteBrand'

describe('siteBrand', () => {
  it.each([undefined, null, '', '  ', 'Sub2API', 'sub2api', 'su2api'])(
    'normalizes upstream or empty site name %s',
    (value) => {
      expect(normalizeVisibleSiteName(value)).toBe(DEFAULT_VISIBLE_SITE_NAME)
    }
  )

  it('preserves and trims a custom site name', () => {
    expect(normalizeVisibleSiteName('  My Gateway  ')).toBe('My Gateway')
  })

  it('rebrands upstream names inside visible copy', () => {
    expect(replaceVisibleUpstreamBrand('I agree to the Sub2API terms', 'fallback')).toBe(
      'I agree to the coococode terms'
    )
    expect(replaceVisibleUpstreamBrand('su2api 使用协议', 'fallback')).toBe(
      'coococode 使用协议'
    )
  })

  it('uses the supplied fallback for empty copy', () => {
    expect(replaceVisibleUpstreamBrand(' ', 'fallback')).toBe('fallback')
  })
})
