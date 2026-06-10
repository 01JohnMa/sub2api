<template>
  <div
    :class="['coconut-mark', `coconut-mark--${variant}`, sizeClass]"
    role="img"
    :aria-label="label"
    :data-coconut-variant="variant"
  >
    <svg class="coconut-mark__svg" viewBox="0 0 64 64" aria-hidden="true" focusable="false">
      <g v-if="variant === 'mono'">
        <path class="coconut-mark__outline" d="M22 18h20v4h6v8h4v16h-4v6h-8v4H24v-4h-8v-6h-4V30h4v-8h6z" />
        <path class="coconut-mark__shell" d="M23 22h18v4h6v8h4v11h-4v5h-8v4H25v-4h-8v-5h-4V34h4v-8h6z" />
        <path class="coconut-mark__shade" d="M17 34h6v-8h8v-4h-8v4h-6z" />
        <rect class="coconut-mark__highlight" x="25" y="29" width="15" height="4" />
        <rect class="coconut-mark__eye" x="25" y="39" width="4" height="4" />
        <rect class="coconut-mark__eye" x="38" y="39" width="4" height="4" />
        <rect class="coconut-mark__code" x="31" y="48" width="10" height="3" />
        <rect class="coconut-mark__leaf-dark" x="30" y="12" width="4" height="7" />
        <rect class="coconut-mark__leaf" x="34" y="10" width="12" height="5" />
        <rect class="coconut-mark__leaf" x="42" y="15" width="5" height="4" />
      </g>

      <g v-else-if="variant === 'half'">
        <path class="coconut-mark__outline" d="M10 29h44v7h-4v9h-7v5H21v-5h-7v-9h-4z" />
        <path class="coconut-mark__cream" d="M15 31h34v5h-4v6h-6v4H25v-4h-6v-6h-4z" />
        <path class="coconut-mark__shell" d="M16 36h32v7h-6v5H22v-5h-6z" />
        <rect class="coconut-mark__water" x="25" y="35" width="15" height="4" />
        <rect class="coconut-mark__highlight" x="22" y="40" width="8" height="4" />
        <rect class="coconut-mark__code" x="36" y="43" width="8" height="3" />
        <rect class="coconut-mark__leaf-dark" x="31" y="13" width="4" height="15" />
        <rect class="coconut-mark__leaf" x="34" y="9" width="15" height="6" />
        <rect class="coconut-mark__leaf" x="41" y="15" width="8" height="5" />
        <rect class="coconut-mark__leaf" x="18" y="14" width="13" height="5" />
        <rect class="coconut-mark__leaf" x="15" y="19" width="10" height="4" />
      </g>

      <g v-else-if="variant === 'sprout'">
        <path class="coconut-mark__leaf-dark" d="M18 15h14v5h-4v5H14v-5h4z" />
        <path class="coconut-mark__leaf" d="M33 10h17v5h-5v5H30v-5h3z" />
        <path class="coconut-mark__young" d="M18 25h28v5h6v18h-5v6H17v-6h-5V30h6z" />
        <path class="coconut-mark__young-shade" d="M18 30h8v18h-8v-5h-4v-9h4z" />
        <rect class="coconut-mark__cream" x="28" y="31" width="11" height="5" />
        <rect class="coconut-mark__water" x="29" y="38" width="14" height="4" />
        <rect class="coconut-mark__code" x="33" y="46" width="9" height="3" />
      </g>

      <g v-else>
        <rect class="coconut-mark__code" x="11" y="22" width="5" height="5" />
        <rect class="coconut-mark__code" x="16" y="27" width="5" height="5" />
        <rect class="coconut-mark__code" x="11" y="32" width="5" height="5" />
        <rect class="coconut-mark__code" x="48" y="22" width="5" height="5" />
        <rect class="coconut-mark__code" x="43" y="27" width="5" height="5" />
        <rect class="coconut-mark__code" x="48" y="32" width="5" height="5" />
        <path class="coconut-mark__outline" d="M25 22h14v4h5v14h-5v5H25v-5h-5V26h5z" />
        <path class="coconut-mark__shell" d="M26 25h12v4h4v10h-4v4H26v-4h-4V29h4z" />
        <rect class="coconut-mark__highlight" x="27" y="30" width="7" height="3" />
        <rect class="coconut-mark__eye" x="30" y="37" width="3" height="3" />
        <rect class="coconut-mark__eye" x="36" y="37" width="3" height="3" />
      </g>
    </svg>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

type PixelCoconutSize = 'xs' | 'sm' | 'md' | 'lg'
type PixelCoconutVariant = 'mono' | 'half' | 'sprout' | 'terminal'

const props = withDefaults(
  defineProps<{
    label?: string
    size?: PixelCoconutSize
    variant?: PixelCoconutVariant
  }>(),
  {
    label: 'coococode pixel coconut mark',
    size: 'md',
    variant: 'mono',
  }
)

const sizeClass = computed(() => {
  return {
    xs: 'h-8 w-8',
    sm: 'h-10 w-10',
    md: 'h-16 w-16',
    lg: 'h-24 w-24',
  }[props.size]
})
</script>

<style scoped>
.coconut-mark {
  display: inline-grid;
  place-items: center;
  color: #17231f;
  image-rendering: pixelated;
}

.coconut-mark__svg {
  width: 100%;
  height: 100%;
  overflow: visible;
  shape-rendering: crispEdges;
}

.coconut-mark--mono,
.coconut-mark--terminal {
  filter: drop-shadow(0 6px 14px rgba(23, 35, 31, 0.14));
}

.coconut-mark--half,
.coconut-mark--sprout {
  filter: drop-shadow(0 12px 24px rgba(23, 35, 31, 0.12));
}

.coconut-mark__outline {
  fill: #3d2d24;
}

.coconut-mark__shell {
  fill: #8a6a4f;
}

.coconut-mark__shade {
  fill: #6f523d;
}

.coconut-mark__highlight {
  fill: #c6aa8f;
}

.coconut-mark__cream {
  fill: #f4e8cc;
}

.coconut-mark__water {
  fill: #7fc4ba;
}

.coconut-mark__eye {
  fill: #17231f;
}

.coconut-mark__code {
  fill: #0f7d73;
}

.coconut-mark__leaf {
  fill: #47a79d;
}

.coconut-mark__leaf-dark {
  fill: #0b6a62;
}

.coconut-mark__young {
  fill: #9ab86e;
}

.coconut-mark__young-shade {
  fill: #6f9158;
}

:global(.dark) .coconut-mark--mono,
:global(.dark) .coconut-mark--terminal {
  filter: drop-shadow(0 7px 18px rgba(0, 0, 0, 0.32));
}

:global(.dark) .coconut-mark--half,
:global(.dark) .coconut-mark--sprout {
  filter: drop-shadow(0 14px 28px rgba(0, 0, 0, 0.3));
}
</style>
