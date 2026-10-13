import { afterEach, describe, expect, it } from 'vitest'

import { getPersistedPageSize } from '@/composables/usePersistedPageSize'

describe('usePersistedPageSize', () => {
  afterEach(() => {
    localStorage.clear()
    delete window.__APP_CONFIG__
  })

  it('uses the persisted user page size when a system default is configured', () => {
    window.__APP_CONFIG__ = {
      table_default_page_size: 1000,
      table_page_size_options: [20, 50, 1000]
    } as any
    localStorage.setItem('table-page-size', '50')

    expect(getPersistedPageSize()).toBe(50)
  })
})
