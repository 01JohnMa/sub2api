import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent } from 'vue'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2, getUserUsageTrend, getUserSpendingRanking } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2,
      getUserUsageTrend,
      getUserSpendingRanking
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  total_account_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  today_account_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0
})

const DateRangePickerStub = defineComponent({
  name: 'DateRangePicker',
  props: {
    startDate: { type: String, default: '' },
    endDate: { type: String, default: '' }
  },
  emits: ['update:startDate', 'update:endDate', 'change'],
  setup(_, { emit }) {
    const emitUpdatedRange = () => {
      emit('update:startDate', '2026-04-10')
      emit('update:endDate', '2026-04-10')
      emit('change', {
        startDate: '2026-04-10',
        endDate: '2026-04-10',
        preset: null
      })
    }

    return { emitUpdatedRange }
  },
  template: '<button type="button" data-test="date-range-picker" @click="emitUpdatedRange">date range</button>'
})

const mountDashboard = () => mount(DashboardView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      LoadingSpinner: true,
      Icon: true,
      DateRangePicker: DateRangePickerStub,
      Select: true,
      ModelDistributionChart: true,
      TokenUsageTrend: true,
      Line: true
    }
  }
})

describe('admin DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())

    getSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getUserSpendingRanking.mockReset()

    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getUserUsageTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '',
      end_date: ''
    })
  })

  it('uses last 24 hours as default dashboard range', async () => {
    mountDashboard()

    await flushPromises()

    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      granularity: 'hour'
    }))
  })

  it('applies responsive date controls and reloads all chart requests for an emitted range', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    expect(wrapper.get('[data-test="admin-dashboard-date-range"]').classes()).toEqual(
      expect.arrayContaining([
        'flex',
        'w-full',
        'flex-col',
        'items-start',
        'gap-2',
        'sm:w-auto',
        'sm:flex-row',
        'sm:items-center'
      ])
    )
    expect(wrapper.get('[data-test="date-range-picker"]').classes()).toEqual(
      expect.arrayContaining(['w-full', 'sm:w-auto'])
    )

    getSnapshotV2.mockClear()
    getUserUsageTrend.mockClear()
    getUserSpendingRanking.mockClear()

    await wrapper.get('[data-test="date-range-picker"]').trigger('click')
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: '2026-04-10',
      end_date: '2026-04-10',
      granularity: 'hour'
    }))
    expect(getUserUsageTrend).toHaveBeenCalledWith(expect.objectContaining({
      start_date: '2026-04-10',
      end_date: '2026-04-10',
      granularity: 'hour'
    }))
    expect(getUserSpendingRanking).toHaveBeenCalledWith(expect.objectContaining({
      start_date: '2026-04-10',
      end_date: '2026-04-10'
    }))
  })
})
