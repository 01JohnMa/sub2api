import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../TablePageLayout.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('TablePageLayout responsive table overflow', () => {
  it('keeps wide tables horizontally scrollable inside the mobile layout', () => {
    expect(componentSource).toContain(
      '.table-page-layout.mobile-mode .table-scroll-container :deep(.table-wrapper)'
    )
    expect(componentSource).toContain('@apply overflow-x-auto overflow-y-visible;')
  })
})
