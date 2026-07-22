import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import BaseDialog from '../BaseDialog.vue'
import ConfirmDialog from '../ConfirmDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      if (key === 'common.confirm') return 'Confirm'
      if (key === 'common.cancel') return 'Cancel'
      return key
    },
  }),
}))

const mountedWrappers = new Set<{ unmount: () => void }>()

function track<T extends { unmount: () => void }>(wrapper: T): T {
  mountedWrappers.add(wrapper)
  return wrapper
}

function mountBaseDialog(props: Record<string, unknown> = {}) {
  return track(mount(BaseDialog, {
    attachTo: document.body,
    props: {
      show: true,
      title: 'Test dialog',
      ...props,
    },
    slots: {
      default: '<button data-testid="body-action" type="button">Body action</button>',
      footer: '<button data-testid="footer-action" type="button">Footer action</button>',
    },
    global: {
      stubs: {
        Icon: true,
      },
    },
  }))
}

afterEach(() => {
  for (const wrapper of mountedWrappers) wrapper.unmount()
  mountedWrappers.clear()
  document.body.classList.remove('modal-open')
  document.body.innerHTML = ''
})

describe('BaseDialog', () => {
  it('renders the semantic dialog with body and footer in order', async () => {
    mountBaseDialog()
    await nextTick()

    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')
    const content = dialog?.querySelector<HTMLElement>('.modal-content')
    const title = dialog?.querySelector<HTMLElement>('.modal-title')

    expect(dialog).not.toBeNull()
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(dialog?.getAttribute('aria-labelledby')).toBe(title?.id)
    expect(Array.from(content?.children ?? []).map((child) => child.className)).toEqual([
      'modal-header',
      'modal-body',
      'modal-footer',
    ])
    expect(content?.querySelector('[data-testid="body-action"]')).not.toBeNull()
    expect(content?.querySelector('[data-testid="footer-action"]')).not.toBeNull()
  })

  it('locks body scroll, focuses the first action, and restores focus after close', async () => {
    const opener = document.createElement('button')
    opener.textContent = 'Open dialog'
    document.body.appendChild(opener)
    opener.focus()

    const wrapper = mountBaseDialog({ show: false, showCloseButton: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(document.activeElement).toBe(document.querySelector('[data-testid="body-action"]'))

    await wrapper.setProps({ show: false })
    await nextTick()

    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(document.activeElement).toBe(opener)

    await wrapper.setProps({ show: true })
    await nextTick()
    expect(document.body.classList.contains('modal-open')).toBe(true)

    wrapper.unmount()
    mountedWrappers.delete(wrapper)
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })

  it('honors Escape and backdrop dismissal options without changing close events', async () => {
    const wrapper = mountBaseDialog()
    await nextTick()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(1)

    await wrapper.setProps({ closeOnEscape: false })
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(1)

    const overlay = document.querySelector<HTMLElement>('.modal-overlay')
    overlay?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(wrapper.emitted('close')).toHaveLength(1)

    await wrapper.setProps({ closeOnClickOutside: true })
    overlay?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(wrapper.emitted('close')).toHaveLength(2)

    const closeButton = document.querySelector<HTMLButtonElement>('button[aria-label="Close modal"]')
    closeButton?.click()
    expect(wrapper.emitted('close')).toHaveLength(3)
  })
})

describe('ConfirmDialog', () => {
  it('keeps cancel, confirm, close, and danger behavior mapped to the same events', async () => {
    const wrapper = track(mount(ConfirmDialog, {
      attachTo: document.body,
      props: {
        show: true,
        title: 'Confirm action',
        message: 'This is a synthetic fixture.',
        danger: true,
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    }))
    await nextTick()

    const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>('.modal-footer button'))
    expect(buttons.map((button) => button.textContent?.trim())).toEqual(['Cancel', 'Confirm'])
    expect(buttons[1]?.className).toContain('bg-red-600')

    buttons[0]?.click()
    buttons[1]?.click()
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(wrapper.emitted('confirm')).toHaveLength(1)

    document.querySelector<HTMLButtonElement>('button[aria-label="Close modal"]')?.click()
    expect(wrapper.emitted('cancel')).toHaveLength(2)
  })
})
