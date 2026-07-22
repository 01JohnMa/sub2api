import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { UserDashboardStats as DashboardStats } from '@/api/usage'
import UserDashboardStats from '../UserDashboardStats.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const stats: DashboardStats = {
  total_api_keys: 2,
  active_api_keys: 1,
  total_requests: 8,
  total_input_tokens: 100,
  total_output_tokens: 50,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 150,
  total_cost: 4.5678,
  total_actual_cost: 3.4567,
  today_requests: 3,
  today_input_tokens: 40,
  today_output_tokens: 20,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 60,
  today_cost: 2.3456,
  today_actual_cost: 1.2345,
  average_duration_ms: 125,
  rpm: 2,
  tpm: 10,
  by_platform: []
}

describe('UserDashboardStats', () => {
  it('keeps each standard-price separator with its value', () => {
    const wrapper = mount(UserDashboardStats, {
      props: {
        stats,
        balance: 10,
        isSimple: false,
        platformQuotas: []
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    const coreStats = wrapper.findAll('div.grid')[0]
    const actualPrices = coreStats.findAll('span[title="dashboard.actual"]')
    const standardPrices = coreStats.findAll('span[title="dashboard.standard"]')

    expect(actualPrices).toHaveLength(2)
    expect(standardPrices).toHaveLength(2)
    expect(actualPrices.map((price) => price.text())).toEqual(['$1.2345', '$3.4567'])
    expect(standardPrices.map((price) => price.text())).toEqual(['/ $2.3456', '/ $4.5678'])

    for (const price of standardPrices) {
      expect(price.classes()).toEqual(expect.arrayContaining(['inline-block', 'whitespace-nowrap']))
      expect(price.attributes('title')).toBe('dashboard.standard')
    }
    for (const price of actualPrices) {
      expect(price.attributes('title')).toBe('dashboard.actual')
    }
  })
})
