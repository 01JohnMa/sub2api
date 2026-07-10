# Coococode Visible Brand Purge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove every user-visible `Sub2API` / `su2api` brand reference from the Coococode frontend while retaining the latest upstream backend and all compatibility identifiers.

**Architecture:** Treat visible copy and links as a presentation-layer concern. Rebrand or remove only strings that can reach rendered UI, exported filenames, downloadable examples, or local legal content; preserve package names, storage keys, WebSocket subprotocols, import types, database defaults, upstream update APIs, and functional third-party URLs.

**Tech Stack:** Vue 3, TypeScript, Pinia, Vue I18n, Vitest, Vite, Docker Compose, GitHub Actions/GHCR.

## Global Constraints

- Do not modify `backend/`, CPA, database state, API contracts, authentication, Nginx, or internal compatibility identifiers.
- User-visible default brand is exactly `coococode`.
- Keep the current Coococode homepage, management UI, CPA management entry, frozen balance display, batch-image permission logic, legal agreement gating, and sidebar scroll restoration.
- Run only focused frontend tests and one production build; do not run the backend or full frontend suite.
- Deploy by rebuilding and recreating only the `sub2api` container. Roll back to `ghcr.io/01johnma/sub2api:ui-redesign-9231ab7` if acceptance fails.

---

### Task 1: Add a focused visible-brand regression test

**Files:**
- Create: `frontend/src/components/layout/__tests__/visibleBranding.spec.ts`

**Interfaces:**
- Consumes: source files containing visible UI copy and links.
- Produces: a Vitest guard that rejects the known upstream brand literals without rejecting internal compatibility identifiers.

- [ ] **Step 1: Write the failing source-surface test**

```ts
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
    const runtimeSource = source('frontend/src/components/layout/AppHeader.vue')
      + source('frontend/src/views/KeyUsageView.vue')
      + source('frontend/src/views/public/LegalDocumentView.vue')
      + source('frontend/src/views/auth/EmailVerifyView.vue')
      + source('frontend/src/views/user/BatchImageGuideView.vue')
      + source('frontend/src/views/admin/ProxiesView.vue')
      + source('frontend/src/views/admin/AccountsView.vue')
      + source('frontend/src/views/admin/SettingsView.vue')
      + source('frontend/src/stores/adminCompliance.ts')

    for (const literal of forbiddenVisibleLiterals) {
      expect(runtimeSource).not.toContain(literal)
    }
    expect(existsSync(resolve(repoRoot, 'frontend/src/components/common/ProxyAdBanner.vue'))).toBe(false)
  })

  it.each(visibleCopyFiles)('%s contains no visible upstream brand name', (path) => {
    expect(source(path)).not.toMatch(/sub2api|su2api/i)
  })
})
```

- [ ] **Step 2: Run the test and confirm it fails on current visible references**

Run:

```bash
cd frontend
npm exec --yes --package=pnpm@9.15.9 -- pnpm exec vitest run src/components/layout/__tests__/visibleBranding.spec.ts
```

Expected: FAIL because current UI sources and locale/legal files still contain upstream brand literals.

---

### Task 2: Remove direct upstream-brand UI entry points and defaults

**Files:**
- Modify: `frontend/src/components/layout/AppHeader.vue`
- Modify: `frontend/src/views/KeyUsageView.vue`
- Delete: `frontend/src/components/common/ProxyAdBanner.vue`
- Modify: `frontend/src/views/admin/ProxiesView.vue`
- Modify: `frontend/src/components/account/CreateAccountModal.vue`
- Modify: `frontend/src/components/account/EditAccountModal.vue`
- Modify: `frontend/src/components/common/VersionBadge.vue`
- Modify: `frontend/src/views/public/LegalDocumentView.vue`
- Modify: `frontend/src/views/auth/EmailVerifyView.vue`
- Modify: `frontend/src/stores/adminCompliance.ts`
- Modify: `docs/legal/admin-compliance.zh.md`
- Modify: `docs/legal/admin-compliance.en.md`
- Test: `frontend/src/views/auth/__tests__/EmailVerifyView.spec.ts`

**Interfaces:**
- Consumes: existing public settings, version APIs, legal documents, and auth flows.
- Produces: the same functional UI without upstream GitHub/promotional links or upstream fallback names.

- [ ] **Step 1: Remove upstream repository and proxy-ad entries from rendered templates**

Apply these exact transformations:

```text
AppHeader.vue:
  delete the admin-only GitHub <a> block; retain Profile, API Keys, balance and support blocks.

KeyUsageView.vue:
  change fallback site name from 'Sub2API' to 'coococode';
  delete the GitHub footer <a> and the githubUrl constant; retain sanitized docUrl.

ProxiesView.vue, CreateAccountModal.vue, EditAccountModal.vue:
  delete <ProxyAdBanner /> and its import.

ProxyAdBanner.vue:
  delete the now-unused component.
```

- [ ] **Step 2: Keep automated version operations but remove visible upstream release/manual-command links**

In `VersionBadge.vue`:

```text
delete all three releaseInfo.html_url <a> blocks;
delete the manual rollback command block and its deploy-method tabs;
delete GITHUB_REPO, DOCKER_IMAGE, useClipboard, copied, copyToClipboard,
manualTab, manualTabs, scriptRollbackCommand, dockerRollbackCommand,
activeManualCommand, and the manualTab reset;
retain version refresh, automated update, restart, rollback version selection,
rollback warning, rollback error, and rollback confirmation button.
```

- [ ] **Step 3: Replace legal/auth fallback branding without changing agreement behavior**

Use these exact values:

```ts
// LegalDocumentView.vue and EmailVerifyView.vue
'coococode'

// adminCompliance.ts
const FALLBACK_ZH_PHRASE = '我已阅读、理解并同意 coococode 部署与运营合规承诺'
const FALLBACK_EN_PHRASE = 'I have read, understood, and agree to the coococode Deployment and Operation Compliance Commitment'
```

In both local compliance Markdown files, replace product-specific sentences with `coococode` for headings and `本平台` / `this platform` for repeated prose. Preserve all legal duties, risk categories, and acceptance semantics.

- [ ] **Step 4: Update affected EmailVerify tests**

Change test defaults and public settings fixtures from `Sub2API` to `coococode`, while retaining assertions for code sending, pending OAuth adoption, suffix validation, and Turnstile behavior.

- [ ] **Step 5: Run the focused auth test**

Run:

```bash
cd frontend
npm exec --yes --package=pnpm@9.15.9 -- pnpm exec vitest run src/views/auth/__tests__/EmailVerifyView.spec.ts
```

Expected: PASS.

---

### Task 3: Rebrand remaining visible copy, examples, filenames, and settings

**Files:**
- Modify: `frontend/src/i18n/locales/en/misc.ts`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/landing.ts`
- Modify: `frontend/src/i18n/locales/zh/landing.ts`
- Modify: `frontend/src/i18n/locales/en/admin/accounts.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/accounts.ts`
- Modify: `frontend/src/i18n/locales/en/admin/overview.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/overview.ts`
- Modify: `frontend/src/i18n/locales/en/admin/settings.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/settings.ts`
- Modify: `frontend/src/views/user/BatchImageGuideView.vue`
- Modify: `frontend/src/views/user/KeysView.vue`
- Modify: `frontend/src/views/admin/ProxiesView.vue`
- Modify: `frontend/src/views/admin/AccountsView.vue`
- Modify: `frontend/src/views/admin/SettingsView.vue`
- Test: `frontend/src/views/user/__tests__/KeysView.spec.ts`
- Test: `frontend/src/views/admin/__tests__/SettingsView.spec.ts`

**Interfaces:**
- Consumes: existing i18n keys and export/config generators.
- Produces: Coococode-branded copy and examples while retaining the same data shapes and backend fields.

- [ ] **Step 1: Replace visible locale copy using generic gateway language where appropriate**

Use this mapping:

```text
Sub2API / sub2api as product name -> coococode
"core concept of Sub2API" -> "core concept of this gateway"
"Sub2API 的核心概念" -> "本平台的核心概念"
"another sub2api instance" -> "another compatible gateway"
"另一个 sub2api 实例" -> "另一个兼容网关实例"
"Sub2API orders" -> "gateway orders"
"Sub2API 订单" -> "平台订单"
sub2api-backups -> coococode-backups
```

Do not rename i18n keys or change HTML structure inside onboarding descriptions.

- [ ] **Step 2: Replace visible examples and downloaded filenames**

Use these exact replacements:

```text
<你的 Sub2API API 端点> -> <你的 coococode API 端点>
name: sub2api-batch-image -> name: coococode-batch-image
`sub2api-proxy-${timestamp}.json` -> `coococode-proxy-${timestamp}.json`
`sub2api-account-${timestamp}.json` -> `coococode-account-${timestamp}.json`
KeysView provider fallback sub2api -> coococode
```

Retain `sub2api-batch-image-preview-cache`, `sub2api-ui-*` request IDs, import data type markers, and WebSocket subprotocols.

- [ ] **Step 3: Replace visible settings defaults and placeholders**

In `SettingsView.vue` and locale settings files:

```text
site name placeholder/default -> coococode
payment product prefix fallback -> coococode
mail sender placeholder -> coococode
OAuth descriptions -> coococode end-user login
```

Retain upstream payment documentation URLs because they remain the functional documentation source.

- [ ] **Step 4: Update focused KeysView and SettingsView fixtures/assertions**

Replace only fixtures and expectations representing visible defaults. Keep API request shapes, auth-source defaults, payment fields, and upstream documentation URL assertions unchanged.

- [ ] **Step 5: Run the focused branding/settings tests**

Run:

```bash
cd frontend
npm exec --yes --package=pnpm@9.15.9 -- pnpm exec vitest run \
  src/components/layout/__tests__/visibleBranding.spec.ts \
  src/views/user/__tests__/KeysView.spec.ts \
  src/views/admin/__tests__/SettingsView.spec.ts
```

Expected: PASS.

---

### Task 4: Verify, review, publish, and deploy the frontend-only change

**Files:**
- Verify: all changed frontend/legal files
- Deploy config: `/opt/sub2api/docker-compose.yml` on `root@69.12.86.145`

**Interfaces:**
- Consumes: committed frontend source and the existing manual GHCR workflow.
- Produces: an immutable `linux/amd64` image deployed only to the `sub2api` service.

- [ ] **Step 1: Run the complete focused test set once**

Run:

```bash
cd frontend
npm exec --yes --package=pnpm@9.15.9 -- pnpm exec vitest run \
  src/components/layout/__tests__/visibleBranding.spec.ts \
  src/views/auth/__tests__/EmailVerifyView.spec.ts \
  src/views/user/__tests__/KeysView.spec.ts \
  src/views/admin/__tests__/SettingsView.spec.ts \
  src/components/brand/__tests__/BrandWordmark.spec.ts \
  src/components/brand/__tests__/PixelCoconutMark.spec.ts \
  src/router/__tests__/title.spec.ts
```

Expected: all selected tests PASS.

- [ ] **Step 2: Run one production build**

Run:

```bash
cd frontend
npm run build
```

Expected: exit 0; warnings about existing chunk sizes or stale Browserslist data are non-blocking.

- [ ] **Step 3: Audit residual references and final diff**

Run:

```bash
git diff --check
git diff --name-only 066a85949a37dbe952b1ebd722b0ab55958b5a08..HEAD -- backend
rg -n -i 'sub2api|su2api' frontend/src docs/legal --glob '!**/__tests__/**' --glob '!**/README.md' --glob '!**/VISUAL_GUIDE.md' --glob '!**/USAGE_EXAMPLES.md'
git --no-pager diff --stat
```

Expected: no backend diff; every residual match is an explicitly preserved compatibility identifier, comment, functional upstream documentation URL, or third-party endpoint.

- [ ] **Step 4: Perform mandatory read-only review**

Review inputs: confirmed design, full diff, focused test/build output, residual-reference classification, and the requirement that CPA/backend/database remain untouched. Fix all Critical and Important findings, then rerun only affected tests plus the build when source compilation changed.

- [ ] **Step 5: Commit and push**

```bash
git add frontend docs/legal
git commit -m "feat(frontend): 清理上游品牌信息"
git push origin codex/upstream-coococode-ui-20260710
```

- [ ] **Step 6: Build the immutable image**

```bash
gh workflow run "Build Coococode Image" --ref codex/upstream-coococode-ui-20260710
gh run list --workflow "Build Coococode Image" --branch codex/upstream-coococode-ui-20260710 --limit 1 --json databaseId,headSha,status,conclusion,url
```

Require the workflow `headSha` to equal `git rev-parse HEAD` before deployment. The resulting tag is computed by the workflow as `ui-redesign-$(git rev-parse --short=7 HEAD)`.

- [ ] **Step 7: Preflight and deploy only Sub2API**

On `root@69.12.86.145`, record the current Sub2API image/digest, CPA PID/state, and PostgreSQL/Redis container IDs. Pull the exact new image, back up `/opt/sub2api/docker-compose.yml`, replace only its Sub2API image tag, validate Compose, then run:

```bash
docker compose -f /opt/sub2api/docker-compose.yml \
  -f /opt/sub2api/docker-compose.override.yml \
  up -d --no-deps --force-recreate sub2api
```

- [ ] **Step 8: Verify production and rollback if needed**

Require all of the following:

```text
Sub2API container: running and healthy on the exact new image digest
localhost /health: {"status":"ok"}
public https://coococode.com/health: {"status":"ok"}
homepage and admin dashboard: coococode visible; no Sub2API/su2api branding
CPA: same PID and active state
PostgreSQL/Redis: same container IDs
startup error count: 0
```

If any required check fails, restore `ui-redesign-9231ab7` and recreate only `sub2api`.
