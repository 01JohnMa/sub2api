<template>
  <span :class="['brand-wordmark', `brand-wordmark--${size}`]" :aria-label="text" role="img">
    <span aria-hidden="true">
      <template v-for="(char, index) in chars" :key="`${char}-${index}`">
        <span v-if="isCoconutLetter(char)" class="brand-wordmark__coconut" data-coconut-letter>
          <span class="brand-wordmark__body"></span>
          <span class="brand-wordmark__shine"></span>
        </span>
        <span v-else class="brand-wordmark__letter">{{ char }}</span>
      </template>
    </span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

type BrandWordmarkSize = 'inline' | 'sm' | 'md' | 'lg' | 'inherit'

const props = withDefaults(
  defineProps<{
    text?: string
    size?: BrandWordmarkSize
  }>(),
  {
    text: 'coococode',
    size: 'inherit',
  }
)

const chars = computed(() => props.text.split(''))

function isCoconutLetter(char: string) {
  return char.toLowerCase() === 'o'
}
</script>

<style scoped>
.brand-wordmark {
  display: inline-flex;
  align-items: baseline;
  letter-spacing: 0;
  font-kerning: normal;
  line-height: 1;
  white-space: nowrap;
}

.brand-wordmark > span {
  display: inline-flex;
  align-items: baseline;
  gap: 0.01em;
}

.brand-wordmark--inline {
  font-size: 1em;
}

.brand-wordmark--sm {
  font-size: 0.875rem;
}

.brand-wordmark--md {
  font-size: 1.5rem;
}

.brand-wordmark--lg {
  font-size: 2.25rem;
}

.brand-wordmark--inherit {
  font-size: 1em;
}

.brand-wordmark__letter {
  display: inline-block;
  color: #183b34;
}

.brand-wordmark__coconut {
  position: relative;
  display: inline-block;
  width: 0.64em;
  height: 0.72em;
  margin: 0 -0.01em;
  transform: translateY(0.08em);
}

.brand-wordmark__body {
  position: absolute;
  left: 0.06em;
  bottom: 0.03em;
  width: 0.52em;
  height: 0.52em;
  border: 0.075em solid #285d4f;
  border-radius: 50%;
  background:
    radial-gradient(circle at 32% 30%, #f1ffd8 0 0.075em, transparent 0.08em),
    linear-gradient(145deg, #b8dc7e 0%, #8db862 58%, #638f54 100%);
  box-shadow:
    inset -0.055em -0.065em 0 rgba(55, 96, 48, 0.26),
    0 0.02em 0 rgba(24, 59, 52, 0.12);
}

.brand-wordmark__shine {
  position: absolute;
  left: 0.43em;
  top: 0.02em;
  width: 0.15em;
  height: 0.1em;
  border-radius: 999px;
  background: #1aa08f;
  transform: rotate(-18deg);
  box-shadow: -0.08em 0.04em 0 -0.035em #0f7d73;
}
</style>
