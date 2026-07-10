import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAdminComplianceStore } from '@/stores/adminCompliance'

const { acceptMock, getStatusMock, localeRef } = vi.hoisted(() => ({
  acceptMock: vi.fn(),
  getStatusMock: vi.fn(),
  localeRef: { value: 'zh' }
}))

vi.mock('@/api/admin/compliance', () => ({
  default: {
    accept: (...args: unknown[]) => acceptMock(...args),
    getStatus: (...args: unknown[]) => getStatusMock(...args)
  }
}))

vi.mock('@/i18n', () => ({
  getLocale: () => localeRef.value
}))

const upstreamStatus = {
  required: true,
  version: 'v2026.06.10',
  document_path_zh: 'docs/legal/admin-compliance.zh.md',
  document_path_en: 'docs/legal/admin-compliance.en.md',
  document_url_zh: 'https://example.com/zh',
  document_url_en: 'https://example.com/en',
  ack_phrase_zh: '我已阅读、理解并同意 Sub2API 部署与运营合规承诺',
  ack_phrase_en:
    'I have read, understood, and agree to the Sub2API Deployment and Operation Compliance Commitment'
}

describe('useAdminComplianceStore visible branding', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localeRef.value = 'zh'
    getStatusMock.mockReset()
    acceptMock.mockReset()
    getStatusMock.mockResolvedValue({ ...upstreamStatus })
    acceptMock.mockResolvedValue({ ...upstreamStatus, required: false })
  })

  it('shows a Coococode phrase for an upstream-branded backend response', async () => {
    const store = useAdminComplianceStore()

    await store.fetchStatus()

    expect(store.expectedPhrase).toBe('我已阅读、理解并同意 coococode 部署与运营合规承诺')
  })

  it('maps the verified display phrase back to the backend phrase on accept', async () => {
    const store = useAdminComplianceStore()
    await store.fetchStatus()

    await store.accept(store.expectedPhrase)

    expect(acceptMock).toHaveBeenCalledWith({
      phrase: upstreamStatus.ack_phrase_zh,
      language: 'zh'
    })
  })

  it('keeps the backend phrase contract when acknowledgement is forced from metadata only', async () => {
    const store = useAdminComplianceStore()

    store.requireAcknowledgement({
      version: upstreamStatus.version,
      document_path_zh: upstreamStatus.document_path_zh,
      document_path_en: upstreamStatus.document_path_en,
      document_url_zh: upstreamStatus.document_url_zh,
      document_url_en: upstreamStatus.document_url_en
    })
    expect(store.expectedPhrase).toBe('我已阅读、理解并同意 coococode 部署与运营合规承诺')

    await store.accept(store.expectedPhrase)

    expect(acceptMock).toHaveBeenCalledWith({
      phrase: upstreamStatus.ack_phrase_zh,
      language: 'zh'
    })
  })
})
