import { describe, expect, it } from 'vitest'

import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
  getDisplayBillingMode,
} from '../billingMode'

describe('getDisplayBillingMode', () => {
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
