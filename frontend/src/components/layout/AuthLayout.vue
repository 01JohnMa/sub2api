<template>
  <div class="auth-shell relative min-h-screen overflow-hidden bg-[#F8FFF6] text-accent-950">
    <!-- Background -->
    <div
      class="absolute inset-0 bg-[linear-gradient(rgba(47,128,116,0.08)_1px,transparent_1px),linear-gradient(90deg,rgba(47,128,116,0.08)_1px,transparent_1px)] bg-[size:72px_72px]"
    ></div>

    <div class="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-[#9fd8cb] to-transparent"></div>

    <!-- Content Container -->
    <div class="relative z-10 flex min-h-screen items-center justify-center px-5 py-8">
      <section class="w-full max-w-md">
      <!-- Logo/Brand -->
      <div class="mb-8 text-center">
        <!-- Custom Logo or Default Logo -->
        <template v-if="settingsLoaded">
          <div
            v-if="siteLogo"
            class="mb-5 inline-flex h-14 w-14 items-center justify-center overflow-hidden rounded-lg border border-primary-200 bg-white shadow-sm"
          >
            <img :src="siteLogo" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <PixelCoconutMark v-else class="mb-5" size="lg" variant="sprout" :label="`${siteName} young coconut mark`" />
          <div class="text-xs font-medium uppercase tracking-[0.22em] text-primary-700">
            Pixel coconut gateway
          </div>
          <h1 class="mt-2 text-4xl font-semibold tracking-tight text-accent-950">
            <BrandWordmark :text="siteName" size="inherit" />
          </h1>
          <p class="mt-3 text-sm leading-6 text-accent-600">
            {{ siteSubtitle }}
          </p>
        </template>
      </div>

      <!-- Card Container -->
      <div class="card-glass rounded-lg p-6 shadow-glass sm:p-8">
        <slot />
      </div>

      <!-- Footer Links -->
      <div class="mt-6 text-center text-sm">
        <slot name="footer" />
      </div>

      <!-- Copyright -->
      <div class="mt-8 text-center text-xs text-accent-500">
        &copy; {{ currentYear }} <BrandWordmark :text="siteName" size="inline" />. All rights reserved.
      </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import BrandWordmark from '@/components/brand/BrandWordmark.vue'
import PixelCoconutMark from '@/components/brand/PixelCoconutMark.vue'

const appStore = useAppStore()

const siteName = computed(() => {
  const name = appStore.siteName || ''
  return name && name !== 'Sub2API' ? name : 'coococode'
})
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Minimal pixel coconut access with calm operational control.')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>

<style scoped>
.text-gradient {
  @apply bg-gradient-to-r from-primary-600 to-primary-500 bg-clip-text text-transparent;
}

.auth-shell :deep(.card-glass) {
  border-color: rgba(159, 216, 203, 0.78) !important;
  background: rgba(255, 255, 255, 0.86) !important;
  box-shadow:
    0 18px 42px rgba(47, 128, 116, 0.12),
    0 1px 0 rgba(255, 255, 255, 0.88) inset !important;
}

.auth-shell :deep(h2) {
  color: #17231f !important;
}

.auth-shell :deep(p),
.auth-shell :deep(.input-label) {
  color: #5c6c66 !important;
}

.auth-shell :deep(.input) {
  border-color: #bdded4 !important;
  background: rgba(255, 255, 255, 0.92) !important;
  color: #17231f !important;
}

.auth-shell :deep(.input::placeholder) {
  color: #7b9690 !important;
}

.auth-shell :deep(.input:focus) {
  border-color: #0f7d73 !important;
  box-shadow: 0 0 0 3px rgba(15, 125, 115, 0.16) !important;
}

.auth-shell :deep(.btn-primary) {
  background: #0f8f83 !important;
  color: #ffffff !important;
  box-shadow: 0 10px 24px rgba(15, 125, 115, 0.18) !important;
}

.auth-shell :deep(.btn-primary:hover) {
  background: #0b6f66 !important;
}

.auth-shell :deep(a) {
  color: #0f7d73 !important;
}

.auth-shell :deep(svg) {
  color: currentColor;
}
</style>
