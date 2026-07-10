import { existsSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const frontendRoot = resolve(dir, '../../../..')
const repoRoot = resolve(frontendRoot, '..')

function source(path: string): string {
  return readFileSync(resolve(repoRoot, path), 'utf8')
}

const forbiddenVisibleLiterals = [
  'href="https://github.com/Wei-Shaw/sub2api"',
  "const githubUrl = 'https://github.com/Wei-Shaw/sub2api'",
  'href="https://sub2api.io/proxyip"',
  'releaseInfo?.html_url',
  "const GITHUB_REPO = 'Wei-Shaw/sub2api'",
  "const DOCKER_IMAGE = 'weishaw/sub2api'",
  '<你的 Sub2API API 端点>',
  'name: sub2api-batch-image',
  '`sub2api-proxy-${timestamp}.json`',
  '`sub2api-account-${timestamp}.json`',
  "const siteName = computed(() => settings.value?.site_name || 'Sub2API')",
  "const siteName = ref<string>('Sub2API')",
  "siteName.value = settings.site_name || 'Sub2API'",
  "const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')",
  'placeholder="Sub2API"',
  '(form.payment_product_name_prefix || "Sub2API")',
  'site_name: "Sub2API"',
  "const FALLBACK_ZH_PHRASE = '我已阅读、理解并同意 Sub2API 部署与运营合规承诺'",
  "const FALLBACK_EN_PHRASE = 'I have read, understood, and agree to the Sub2API Deployment and Operation Compliance Commitment'"
]

const visibleCopyFiles = [
  'docs/legal/admin-compliance.zh.md',
  'docs/legal/admin-compliance.en.md',
  'frontend/src/i18n/locales/en/misc.ts',
  'frontend/src/i18n/locales/zh/misc.ts',
  'frontend/src/i18n/locales/en/landing.ts',
  'frontend/src/i18n/locales/zh/landing.ts',
  'frontend/src/i18n/locales/en/admin/accounts.ts',
  'frontend/src/i18n/locales/zh/admin/accounts.ts',
  'frontend/src/i18n/locales/en/admin/overview.ts',
  'frontend/src/i18n/locales/zh/admin/overview.ts',
  'frontend/src/i18n/locales/en/admin/settings.ts',
  'frontend/src/i18n/locales/zh/admin/settings.ts'
]

describe('visible Coococode branding', () => {
  it('removes known visible Sub2API links and literals', () => {
    const runtimeSource =
      source('frontend/src/components/layout/AppHeader.vue') +
      source('frontend/src/components/common/VersionBadge.vue') +
      source('frontend/src/views/KeyUsageView.vue') +
      source('frontend/src/views/public/LegalDocumentView.vue') +
      source('frontend/src/views/auth/EmailVerifyView.vue') +
      source('frontend/src/views/user/BatchImageGuideView.vue') +
      source('frontend/src/views/admin/ProxiesView.vue') +
      source('frontend/src/views/admin/AccountsView.vue') +
      source('frontend/src/views/admin/SettingsView.vue') +
      source('frontend/src/stores/adminCompliance.ts')

    for (const literal of forbiddenVisibleLiterals) {
      expect(runtimeSource).not.toContain(literal)
    }
    expect(
      existsSync(resolve(repoRoot, 'frontend/src/components/common/ProxyAdBanner.vue'))
    ).toBe(false)
  })

  it.each(visibleCopyFiles)('%s contains no visible upstream brand name', (path) => {
    expect(source(path)).not.toMatch(/sub2api|su2api/i)
  })
})
