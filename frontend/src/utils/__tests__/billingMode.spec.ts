import { describe, expect, it } from 'vitest'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
  BILLING_MODE_VIDEO,
  getDisplayBillingMode,
  isImageUsage
} from '../billingMode'

describe('billingMode helpers', () => {
  it('prefers explicit video mode over image_count', () => {
    expect(
      getDisplayBillingMode({ image_count: 1, billing_mode: BILLING_MODE_VIDEO })
    ).toBe(BILLING_MODE_VIDEO)
    expect(isImageUsage({ image_count: 1, billing_mode: BILLING_MODE_VIDEO })).toBe(false)
  })

  it('infers image when image_count set and mode missing', () => {
    expect(getDisplayBillingMode({ image_count: 2, billing_mode: null })).toBe(BILLING_MODE_IMAGE)
  })

  it('keeps token mode even with image_count', () => {
    expect(
      getDisplayBillingMode({ image_count: 1, billing_mode: BILLING_MODE_TOKEN })
    ).toBe(BILLING_MODE_TOKEN)
  })

  it.each([
    { billing_mode: BILLING_MODE_TOKEN, image_count: 1, expected: BILLING_MODE_TOKEN },
    { billing_mode: BILLING_MODE_PER_REQUEST, image_count: 1, expected: BILLING_MODE_PER_REQUEST },
    { billing_mode: null, image_count: 1, expected: BILLING_MODE_IMAGE },
    { billing_mode: '', image_count: 1, expected: BILLING_MODE_IMAGE },
    { billing_mode: null, image_count: 0, expected: null },
  ])('uses the explicit mode before legacy image_count fallback', ({ billing_mode, image_count, expected }) => {
    expect(getDisplayBillingMode({ billing_mode, image_count })).toBe(expected)
  })
})
