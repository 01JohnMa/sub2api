<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="homeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      :title="t('home.coococode.customContentFrameTitle', { siteName })"
      class="h-screen w-full border-0"
      sandbox="allow-forms allow-popups allow-scripts"
      referrerpolicy="strict-origin-when-cross-origin"
      allowfullscreen
    ></iframe>
    <!-- HTML mode -->
    <div v-else v-html="sanitizedHomeContent"></div>
  </div>

  <div v-else class="coconut-page">
    <div class="coconut-browser" :aria-label="t('home.coococode.pageLabel')">
      <main class="page-shell">
        <nav class="main-nav" :aria-label="t('home.coococode.navigationLabel')">
          <div class="brand-lockup">
            <img
              v-if="siteLogo"
              :src="siteLogo"
              :alt="t('home.coococode.siteLogoAlt', { siteName })"
              class="brand-logo"
            />
            <span v-else class="brand-logo-mark">
              <PixelCoconutMark
                size="sm"
                variant="mono"
                :label="t('home.coococode.siteMarkLabel', { siteName })"
              />
            </span>
            <span class="wordmark"><BrandWordmark :text="siteName" size="inherit" /></span>
          </div>

          <div class="nav-actions">
            <a
              v-if="docUrl"
              :href="docUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="nav-link"
            >
              {{ t('home.docs') }}
            </a>
            <LocaleSwitcher fixed-light />
            <router-link class="nav-cta" :to="entryPath">{{ t('home.coococode.start') }}</router-link>
          </div>
        </nav>

        <section id="home" class="hero-wrap" aria-labelledby="hero-title">
          <aside class="side-banner">
            <span>{{ t('home.coococode.sideBanner') }}</span>
          </aside>

          <div class="hero">
            <div class="hero-copy">
              <span class="stamp">coococode.com/v1</span>
              <h1 id="hero-title">
                {{ t('home.coococode.heroTitle') }}
                <span>{{ t('home.coococode.heroEmphasis') }}</span>
              </h1>
              <p class="lead">
                {{ siteSubtitle }}
              </p>
              <div class="actions">
                <router-link class="button primary" :to="entryPath">
                  {{ t('home.coococode.start') }} <span class="arrow">→</span>
                </router-link>
              </div>
            </div>

            <div class="hero-art" aria-hidden="true">
              <div class="terminal-card">
                <div class="terminal-top">
                  <span class="terminal-dots"><i></i><i></i><i></i></span>
                  <strong>{{ t('home.coococode.terminalTitle') }}</strong>
                </div>
                <div class="terminal-lines">
                  <p class="typing-line">$ curl /v1/responses -H "key=***"</p>
                  <p class="typing-line">{{ t('home.coococode.terminalOk') }}</p>
                  <p class="typing-line">input 1,248 · output 386 tokens</p>
                  <p class="typing-line">{{ t('home.coococode.terminalLedger') }}</p>
                </div>
              </div>

              <div class="burst">{{ t('home.coococode.fresh') }}<br />{{ t('home.coococode.direct') }}</div>
              <div class="island">
                <div class="cloud"></div>
                <div class="leaf one"></div>
                <div class="leaf two"></div>
                <div class="leaf three"></div>
                <div class="leaf four"></div>
                <div class="leaf five"></div>
                <div class="palm"></div>
                <div class="sand"></div>
                <div class="sun-line"></div>
                <div class="coconut"></div>
                <div class="code-badge">&lt;/&gt;</div>
                <div class="token-badge">T</div>
              </div>
            </div>
          </div>
        </section>

        <section class="proof-row" :aria-label="t('home.coococode.proofLabel')">
          <article class="proof-card">
            <div class="icon-box">T</div>
            <div>
              <h2>{{ t('home.coococode.proofTokenTitle') }}</h2>
              <p>{{ t('home.coococode.proofTokenDescription') }}</p>
            </div>
          </article>
          <article class="proof-card">
            <div class="icon-box">{}</div>
            <div>
              <h2>{{ t('home.coococode.proofSdkTitle') }}</h2>
              <p>{{ t('home.coococode.proofSdkDescription') }}</p>
            </div>
          </article>
          <article class="proof-card">
            <div class="icon-box">↯</div>
            <div>
              <h2>{{ t('home.coococode.proofErrorTitle') }}</h2>
              <p>{{ t('home.coococode.proofErrorDescription') }}</p>
            </div>
          </article>
        </section>

        <section id="usage" class="usage-panel" :aria-label="t('home.coococode.usageLabel')">
          <div class="panel-head">
            <strong>{{ t('home.coococode.usageTitle') }}</strong>
            <span>{{ t('home.coococode.usageDescription') }}</span>
          </div>
        </section>

        <footer id="pricing" class="ticker" :aria-label="t('home.coococode.capabilitiesLabel')">
          <span><i>/v1</i> {{ t('home.coococode.compatible') }}</span>
          <span><i>T</i> {{ t('home.coococode.tokenLedger') }}</span>
          <span><i>¥</i> {{ t('home.coococode.prepaidBalance') }}</span>
          <span><i>!</i> {{ t('home.coococode.errorLogs') }}</span>
        </footer>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import DOMPurify from 'dompurify'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import BrandWordmark from '@/components/brand/BrandWordmark.vue'
import PixelCoconutMark from '@/components/brand/PixelCoconutMark.vue'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

// Site settings - directly from appStore (already initialized from injected config)
const siteName = computed(() => {
  const name = appStore.cachedPublicSettings?.site_name || appStore.siteName || ''
  return name && name !== 'Sub2API' ? name : 'coococode'
})
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || t('home.coococode.defaultSubtitle'))
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')

const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})
const sanitizedHomeContent = computed(() =>
  DOMPurify.sanitize(homeContent.value, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ['iframe', 'object', 'embed'],
    FORBID_ATTR: ['srcdoc']
  })
)

const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
const entryPath = computed(() => (isAuthenticated.value ? dashboardPath.value : '/login'))

onMounted(() => {
  authStore.checkAuth()

  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
.coconut-page {
  min-height: 100vh;
  background:
    linear-gradient(90deg, rgba(0, 0, 0, 0.08) 1px, transparent 1px) 0 0 / 28px 28px,
    linear-gradient(rgba(0, 0, 0, 0.08) 1px, transparent 1px) 0 0 / 28px 28px,
    #ffe500;
  color: #050505;
  overflow-x: hidden;
  padding: 22px 0;
}

.coconut-browser {
  width: min(1540px, 100vw);
  margin: 0 auto;
  overflow: hidden;
  border: 0;
  border-radius: 0;
  background: #ffe500;
  box-shadow: none;
}

.page-shell {
  padding: 24px;
}

.main-nav {
  display: grid;
  grid-template-columns: minmax(260px, 1fr) auto;
  align-items: center;
  gap: 18px;
  margin-bottom: 18px;
}

.brand-lockup {
  display: inline-flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.brand-logo,
.brand-logo-mark {
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  width: 58px;
  height: 58px;
  border: 4px solid #050505;
  border-radius: 8px;
  background: #fff;
  box-shadow: 4px 5px 0 rgba(0, 0, 0, 0.28);
  overflow: hidden;
}

.brand-logo {
  object-fit: contain;
}

.wordmark {
  font-size: clamp(36px, 4vw, 58px);
  font-weight: 1000;
  letter-spacing: 0;
  line-height: 0.9;
}

.nav-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
}

.nav-link,
.nav-cta {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 50px;
  border: 3px solid #050505;
  border-radius: 6px;
  background: #fff;
  color: #050505;
  padding: 0 16px;
  box-shadow: 4px 5px 0 rgba(0, 0, 0, 0.28);
  font-weight: 1000;
  text-decoration: none;
  white-space: nowrap;
}

.nav-link {
  font-size: 16px;
}

.nav-cta {
  background: #e51912;
  color: #fff;
  padding: 0 27px;
  font-size: 20px;
}

.hero-wrap {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr);
  gap: 12px;
  align-items: stretch;
}

.side-banner {
  display: grid;
  place-items: center;
  border: 4px solid #050505;
  border-radius: 8px;
  background: #050505;
  padding: 10px;
}

.side-banner span {
  display: block;
  border: 3px solid #fff;
  border-radius: 8px;
  background: #e51912;
  color: #fff;
  padding: 18px 9px;
  font-size: 25px;
  font-weight: 1000;
  line-height: 1.25;
  text-align: center;
  writing-mode: vertical-rl;
  letter-spacing: 0.08em;
}

.hero {
  position: relative;
  display: grid;
  grid-template-columns: 1.05fr 0.95fr;
  gap: 20px;
  min-height: 490px;
  overflow: hidden;
  border: 4px solid #050505;
  border-radius: 8px;
  background:
    radial-gradient(circle at 76% 70%, rgba(255, 229, 0, 0.75) 0 16%, transparent 17%),
    linear-gradient(135deg, #005fff 0%, #0032b8 64%, #061d6b 100%);
  color: #fff;
  padding: clamp(24px, 4vw, 58px);
}

.hero::before {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(rgba(255, 255, 255, 0.08) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255, 255, 255, 0.08) 1px, transparent 1px);
  background-size: 44px 44px;
  content: "";
}

.hero-copy,
.hero-art {
  position: relative;
  z-index: 1;
}

.stamp {
  display: inline-block;
  margin-bottom: 18px;
  border: 3px solid #050505;
  border-radius: 6px;
  background: #e51912;
  box-shadow: 4px 5px 0 rgba(0, 0, 0, 0.35);
  color: #fff;
  padding: 8px 16px 9px;
  font-size: clamp(24px, 3vw, 40px);
  font-weight: 1000;
  line-height: 1;
  letter-spacing: 0;
}

h1 {
  margin: 0;
  max-width: 780px;
  color: #fff;
  font-size: clamp(50px, 5.4vw, 86px);
  font-weight: 1000;
  letter-spacing: 0;
  line-height: 0.96;
  text-shadow: 5px 6px 0 rgba(0, 0, 0, 0.4);
}

h1 span {
  display: block;
  color: #ffe500;
}

.lead {
  max-width: 690px;
  margin: 24px 0 28px;
  color: #fffef4;
  font-size: clamp(18px, 1.7vw, 23px);
  font-weight: 850;
  line-height: 1.45;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
}

.button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 64px;
  border: 4px solid #050505;
  border-radius: 6px;
  padding: 0 34px;
  box-shadow: 5px 7px 0 rgba(0, 0, 0, 0.35);
  font-size: clamp(20px, 2.2vw, 29px);
  font-weight: 1000;
  line-height: 1;
  text-decoration: none;
}

.button.primary {
  background: #e51912;
  color: #fff;
}

.arrow {
  margin-left: 20px;
  font-size: 42px;
  line-height: 0.6;
}

.burst {
  position: absolute;
  top: 28px;
  right: 32px;
  display: grid;
  place-items: center;
  width: 154px;
  height: 154px;
  background: #ffe500;
  clip-path: polygon(50% 0%, 58% 17%, 74% 7%, 76% 26%, 96% 21%, 87% 39%, 100% 50%, 87% 61%, 96% 79%, 76% 74%, 74% 93%, 58% 83%, 50% 100%, 42% 83%, 26% 93%, 24% 74%, 4% 79%, 13% 61%, 0% 50%, 13% 39%, 4% 21%, 24% 26%, 26% 7%, 42% 17%);
  color: #e51912;
  filter: drop-shadow(5px 6px 0 rgba(0, 0, 0, 0.38));
  transform: rotate(9deg);
  text-align: center;
  font-size: 31px;
  font-weight: 1000;
  line-height: 1.08;
}

.hero-art {
  display: grid;
  align-items: end;
  min-height: 380px;
  padding-top: 70px;
}

.terminal-card {
  position: absolute;
  top: 40px;
  left: 0;
  z-index: 3;
  width: min(430px, 82%);
  overflow: hidden;
  border: 4px solid #050505;
  border-radius: 8px;
  background: #080b10;
  box-shadow: 7px 9px 0 rgba(0, 0, 0, 0.34);
  font-family: "JetBrains Mono", "SFMono-Regular", "Menlo", monospace;
}

.terminal-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 9px 12px;
  border-bottom: 3px solid #1d2635;
  background: #141922;
  color: #dbe7ff;
  font-size: 12px;
  font-weight: 900;
}

.terminal-dots {
  display: inline-flex;
  gap: 6px;
}

.terminal-dots i {
  width: 10px;
  height: 10px;
  border-radius: 999px;
  background: #ff4b41;
}

.terminal-dots i:nth-child(2) {
  background: #ffd54a;
}

.terminal-dots i:nth-child(3) {
  background: #31cf55;
}

.terminal-lines {
  padding: 14px 15px 16px;
  color: #e9f3ff;
  font-size: clamp(12px, 1.15vw, 15px);
  font-weight: 800;
  line-height: 1.55;
}

.typing-line {
  width: 0;
  max-width: max-content;
  margin: 0;
  overflow: hidden;
  border-right: 3px solid transparent;
  white-space: nowrap;
  animation:
    terminal-type 1.35s steps(var(--chars)) forwards,
    terminal-caret-once 1.35s steps(1) forwards;
  animation-delay: var(--delay), var(--delay);
}

.typing-line + .typing-line {
  margin-top: 4px;
}

.typing-line:nth-child(1) {
  --chars: 38;
  --delay: 0.25s;
  color: #fff;
}

.typing-line:nth-child(2) {
  --chars: 32;
  --delay: 1.85s;
  color: #7ee787;
}

.typing-line:nth-child(3) {
  --chars: 35;
  --delay: 3.15s;
  color: #ffd84a;
}

.typing-line:nth-child(4) {
  --chars: 22;
  --delay: 4.35s;
  color: #9ddcff;
  animation:
    terminal-type 1.35s steps(var(--chars)) forwards,
    terminal-caret-once 1.35s steps(1) forwards,
    terminal-caret-loop 0.7s steps(1) infinite;
  animation-delay: var(--delay), var(--delay), calc(var(--delay) + 1.35s);
}

@keyframes terminal-type {
  from {
    width: 0;
  }
  to {
    width: calc(var(--chars) * 1ch);
  }
}

@keyframes terminal-caret-once {
  0%,
  24%,
  48%,
  72% {
    border-right-color: #ffe500;
  }
  12%,
  36%,
  60%,
  100% {
    border-right-color: transparent;
  }
}

@keyframes terminal-caret-loop {
  0%,
  48% {
    border-right-color: #ffe500;
  }
  50%,
  100% {
    border-right-color: transparent;
  }
}

.island {
  position: relative;
  height: 350px;
}

.sun-line {
  position: absolute;
  right: 30px;
  bottom: 18px;
  left: 20px;
  height: 78px;
  border: 6px solid #58d5e8;
  border-top-color: transparent;
  border-radius: 50%;
  transform: rotate(-4deg);
}

.sand {
  position: absolute;
  right: 22px;
  bottom: 38px;
  width: min(520px, 90%);
  height: 96px;
  border: 4px solid rgba(0, 0, 0, 0.28);
  border-radius: 50%;
  background: #ffd337;
  box-shadow: inset 0 -13px 0 rgba(218, 142, 28, 0.24);
}

.palm {
  position: absolute;
  right: 120px;
  bottom: 92px;
  width: 34px;
  height: 198px;
  border: 3px solid rgba(0, 0, 0, 0.35);
  border-radius: 50% 50% 16px 16px;
  background: linear-gradient(90deg, #75461e, #b97735 55%, #6f431f);
  transform: rotate(11deg);
  transform-origin: bottom center;
}

.leaf {
  position: absolute;
  bottom: 274px;
  right: 108px;
  width: 130px;
  height: 42px;
  border-radius: 100% 10% 100% 10%;
  border: 2px solid rgba(0, 0, 0, 0.3);
  background: linear-gradient(90deg, #0f8e35, #7bd22f);
  transform-origin: left center;
}

.leaf.one {
  transform: rotate(-18deg);
}

.leaf.two {
  right: 82px;
  transform: rotate(-62deg);
}

.leaf.three {
  right: 99px;
  transform: rotate(34deg);
}

.leaf.four {
  right: 150px;
  transform: rotate(147deg);
}

.leaf.five {
  right: 157px;
  transform: rotate(205deg);
}

.coconut {
  position: absolute;
  right: 232px;
  bottom: 78px;
  width: 178px;
  height: 178px;
  border: 5px solid #2a1408;
  border-radius: 49% 51% 54% 46%;
  background:
    radial-gradient(circle at 73% 25%, #fff8e4 0 23%, #d9bf8f 24% 42%, transparent 43%),
    linear-gradient(135deg, #b76d24, #6c3813);
  box-shadow: inset -16px -18px 0 rgba(46, 22, 8, 0.22), 8px 10px 0 rgba(0, 0, 0, 0.25);
  transform: rotate(20deg);
}

.coconut::before,
.coconut::after {
  position: absolute;
  width: 42px;
  height: 4px;
  border-radius: 99px;
  background: rgba(45, 20, 8, 0.34);
  content: "";
}

.coconut::before {
  top: 100px;
  left: 28px;
  transform: rotate(70deg);
}

.coconut::after {
  top: 122px;
  left: 76px;
  transform: rotate(68deg);
}

.code-badge,
.token-badge {
  position: absolute;
  display: grid;
  place-items: center;
  border: 4px solid #fff;
  border-radius: 8px;
  background: #060606;
  box-shadow: 5px 6px 0 rgba(0, 0, 0, 0.32);
  font-weight: 1000;
}

.code-badge {
  right: 392px;
  bottom: 205px;
  width: 76px;
  height: 62px;
  font-family: "JetBrains Mono", "SFMono-Regular", monospace;
  font-size: 30px;
}

.token-badge {
  right: 458px;
  bottom: 126px;
  width: 82px;
  height: 64px;
  border-color: #0b0b0b;
  background: #ffe500;
  color: #050505;
  font-family: "JetBrains Mono", "SFMono-Regular", monospace;
  font-size: 28px;
}

.cloud {
  position: absolute;
  right: 500px;
  bottom: 255px;
  width: 102px;
  height: 30px;
  border-radius: 99px;
  background: #fff;
  animation: cloud-drift 11s cubic-bezier(0.46, 0.03, 0.52, 0.96) infinite;
  box-shadow: 8px 10px 0 rgba(0, 57, 160, 0.18);
  will-change: transform;
}

.cloud::before,
.cloud::after {
  position: absolute;
  bottom: 8px;
  border-radius: 50%;
  background: #fff;
  content: "";
}

.cloud::before {
  left: 17px;
  width: 38px;
  height: 38px;
}

.cloud::after {
  left: 45px;
  width: 48px;
  height: 48px;
}

@keyframes cloud-drift {
  0% {
    transform: translate3d(-58px, 6px, 0);
  }
  17% {
    transform: translate3d(-18px, -11px, 0);
  }
  34% {
    transform: translate3d(42px, 4px, 0);
  }
  50% {
    transform: translate3d(72px, -15px, 0);
  }
  67% {
    transform: translate3d(24px, -3px, 0);
  }
  84% {
    transform: translate3d(-36px, -18px, 0);
  }
  100% {
    transform: translate3d(-58px, 6px, 0);
  }
}

@media (prefers-reduced-motion: reduce) {
  .cloud {
    animation: none;
  }
}

.proof-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-top: 14px;
}

.proof-card {
  display: grid;
  grid-template-columns: 78px 1fr;
  align-items: center;
  min-height: 102px;
  border: 4px solid #050505;
  border-radius: 8px;
  background: #fffdf2;
  padding: 16px;
  box-shadow: inset 0 0 0 2px rgba(255, 255, 255, 0.55);
}

.icon-box {
  display: grid;
  place-items: center;
  width: 58px;
  height: 58px;
  border: 3px solid #050505;
  border-radius: 8px;
  background: #004eea;
  color: #fff;
  font-size: 32px;
  font-weight: 1000;
}

.proof-card h2 {
  margin: 0 0 4px;
  font-size: clamp(21px, 2vw, 31px);
  line-height: 1.1;
}

.proof-card p {
  margin: 0;
  font-size: 16px;
  font-weight: 750;
  line-height: 1.45;
}

.usage-panel {
  margin-top: 14px;
  overflow: hidden;
  border: 4px solid #050505;
  border-radius: 8px;
  background: #fff;
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 22px;
  border-bottom: 4px solid #050505;
  background: #fffdf2;
  font-size: 18px;
  font-weight: 950;
}

.panel-head strong {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: clamp(23px, 2vw, 31px);
}

.panel-head strong::before {
  display: inline-block;
  width: 7px;
  height: 28px;
  border-radius: 99px;
  background: #e51912;
  content: "";
}

.ticker {
  display: grid;
  grid-template-columns: repeat(4, auto);
  justify-content: center;
  gap: clamp(18px, 5vw, 62px);
  margin-top: 14px;
  border: 4px solid #050505;
  border-radius: 8px;
  background: #004eea;
  color: #fff;
  padding: 18px 22px;
  font-size: clamp(17px, 2vw, 22px);
  font-weight: 950;
}

.ticker span {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  white-space: nowrap;
}

.ticker i {
  display: inline-grid;
  place-items: center;
  min-width: 35px;
  height: 35px;
  border: 3px solid #050505;
  border-radius: 7px;
  background: #ffe500;
  color: #050505;
  font-family: "JetBrains Mono", "SFMono-Regular", monospace;
  font-style: normal;
  font-size: 18px;
  font-weight: 1000;
}

@media (max-width: 1100px) {
  .hero {
    grid-template-columns: 1fr;
  }

  .hero-art {
    min-height: 340px;
    padding-top: 150px;
  }

  .proof-row {
    grid-template-columns: 1fr 1fr;
  }

  .main-nav {
    grid-template-columns: 1fr auto;
  }

}

@media (max-width: 720px) {
  .coconut-page {
    padding: 7px 0;
  }

  .coconut-browser {
    width: 100vw;
  }

  .page-shell {
    padding: 12px;
  }

  .main-nav {
    grid-template-columns: 1fr;
  }

  .brand-lockup {
    display: flex;
  }

  .wordmark {
    font-size: clamp(34px, 12vw, 46px);
  }

  .brand-logo,
  .brand-logo-mark {
    width: 50px;
    height: 50px;
  }

  .nav-actions {
    justify-content: stretch;
  }

  .nav-cta {
    flex: 1;
  }

  .hero-wrap {
    grid-template-columns: 1fr;
  }

  .side-banner span {
    writing-mode: horizontal-tb;
    font-size: 20px;
    padding: 10px 14px;
  }

  .hero {
    min-height: 0;
    padding: 22px;
  }

  h1 {
    font-size: clamp(42px, 14vw, 66px);
  }

  .button {
    width: 100%;
    min-height: 58px;
  }

  .burst {
    top: auto;
    right: 18px;
    bottom: 120px;
    width: 104px;
    height: 104px;
    font-size: 21px;
  }

  .hero-art {
    min-height: 405px;
    padding-top: 150px;
  }

  .terminal-card {
    top: 12px;
    left: 0;
    width: 100%;
  }

  .terminal-lines {
    font-size: 12px;
  }

  .island {
    height: 285px;
    transform: scale(0.78);
    transform-origin: right bottom;
  }

  .proof-row,
  .ticker {
    grid-template-columns: 1fr;
  }

  .proof-card {
    grid-template-columns: 62px 1fr;
    padding: 13px;
  }

  .icon-box {
    width: 48px;
    height: 48px;
    font-size: 25px;
  }

  .panel-head {
    align-items: flex-start;
    flex-direction: column;
  }
}

:global(.dark) .coconut-page {
  color: #050505;
}
</style>
