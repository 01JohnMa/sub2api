export const DEFAULT_VISIBLE_SITE_NAME = 'coococode'

const UPSTREAM_SITE_NAME_PATTERN = /^(?:sub2api|su2api)$/i
const UPSTREAM_BRAND_PATTERN = /(?:sub2api|su2api)/gi

export function normalizeVisibleSiteName(value: unknown): string {
  if (typeof value !== 'string') {
    return DEFAULT_VISIBLE_SITE_NAME
  }

  const normalized = value.trim()
  if (!normalized || UPSTREAM_SITE_NAME_PATTERN.test(normalized)) {
    return DEFAULT_VISIBLE_SITE_NAME
  }

  return normalized
}

export function replaceVisibleUpstreamBrand(value: unknown, fallback: string): string {
  if (typeof value !== 'string' || !value.trim()) {
    return fallback
  }

  return value.replace(UPSTREAM_BRAND_PATTERN, DEFAULT_VISIBLE_SITE_NAME)
}
