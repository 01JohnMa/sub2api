<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-6xl space-y-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('organization.overview.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading || dataLoading" :aria-label="t('common.refresh')" data-test="organization-refresh" @click="reloadAll">
          <Icon name="refresh" size="sm" :class="(loading || dataLoading) ? 'animate-spin' : ''" />
          <span>{{ t('common.refresh') }}</span>
        </button>
      </div>

      <div v-if="loading" class="flex justify-center py-12"><LoadingSpinner /></div>
      <section v-else-if="organizationUnavailable" class="border border-amber-200 bg-amber-50 p-6 dark:border-amber-900/50 dark:bg-amber-900/20" data-test="organization-unavailable">
        <h2 class="text-lg font-semibold text-amber-900 dark:text-amber-100">{{ t('organization.overview.unavailableTitle') }}</h2>
        <p class="mt-2 text-sm text-amber-800 dark:text-amber-200">{{ t('organization.overview.unavailableDescription') }}</p>
      </section>

      <template v-else-if="organization">
        <section class="border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-800" data-test="organization-identity-section">
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div class="min-w-0">
              <h2 class="break-words text-xl font-semibold text-gray-900 dark:text-white">{{ organization.name }}</h2>
              <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ organization.description || t('organization.overview.noDescription') }}</p>
            </div>
            <span :class="['badge', statusBadgeClass]">{{ statusLabel }}</span>
          </div>
          <dl class="mt-6 grid grid-cols-1 gap-5 border-t border-gray-100 pt-5 sm:grid-cols-2 dark:border-dark-700">
            <div><dt class="text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.code') }}</dt><dd class="mt-1"><code class="rounded bg-gray-100 px-1.5 py-0.5 text-sm text-gray-700 dark:bg-dark-700 dark:text-gray-200">{{ organization.code }}</code></dd></div>
            <div><dt class="text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.status') }}</dt><dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ statusLabel }}</dd></div>
          </dl>
        </section>

        <section class="border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-800" data-test="organization-report-section">
          <div class="flex flex-wrap items-end justify-between gap-4">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('organization.overview.reporting.title') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.reporting.description') }}</p>
            </div>
            <div class="flex flex-wrap items-end gap-2">
              <label class="text-xs text-gray-500 dark:text-gray-400">{{ t('organization.overview.reporting.start') }}<input v-model="reportStartDate" type="date" class="input mt-1 block" data-test="organization-report-start" /></label>
              <label class="text-xs text-gray-500 dark:text-gray-400">{{ t('organization.overview.reporting.end') }}<input v-model="reportEndDate" type="date" class="input mt-1 block" data-test="organization-report-end" /></label>
              <button type="button" class="btn btn-secondary" :disabled="reportLoading" data-test="organization-report-apply" @click="loadReport"><Icon v-if="reportLoading" name="refresh" size="sm" class="animate-spin" /><Icon v-else name="chartBar" size="sm" /><span>{{ t('organization.overview.reporting.apply') }}</span></button>
            </div>
          </div>
          <div v-if="reportLoading" class="flex justify-center py-8"><LoadingSpinner /></div>
          <template v-else-if="report">
            <div class="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
              <div v-for="stat in reportStats" :key="stat.key" class="border border-gray-100 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-gray-400">{{ stat.label }}</p><p class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ stat.value }}</p></div>
            </div>
            <div class="mt-6 grid gap-6 lg:grid-cols-2">
              <div>
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('organization.overview.reporting.models') }}</h3>
                <div v-if="report.models.length" class="mt-3 overflow-x-auto"><table class="w-full text-sm"><thead class="text-left text-xs text-gray-500 dark:text-gray-400"><tr><th class="py-2">{{ t('organization.overview.reporting.columns.model') }}</th><th class="py-2 text-right">{{ t('organization.overview.reporting.columns.requests') }}</th><th class="py-2 text-right">{{ t('organization.overview.reporting.columns.input') }}</th><th class="py-2 text-right">{{ t('organization.overview.reporting.columns.output') }}</th><th class="py-2 text-right">{{ t('organization.overview.reporting.columns.cost') }}</th></tr></thead><tbody><tr v-for="model in report.models" :key="model.model" class="border-t border-gray-100 dark:border-dark-700"><td class="py-2 text-gray-900 dark:text-white">{{ model.model }}</td><td class="py-2 text-right font-mono">{{ model.request_count }}</td><td class="py-2 text-right font-mono">{{ model.input_tokens }}</td><td class="py-2 text-right font-mono">{{ model.output_tokens }}</td><td class="py-2 text-right font-mono">{{ model.cost }}</td></tr></tbody></table></div>
                <p v-else class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.reporting.empty') }}</p>
              </div>
              <div>
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('organization.overview.reporting.trends') }}</h3>
                <div v-if="report.trends.length" class="mt-3 overflow-x-auto"><table class="w-full text-sm"><thead class="text-left text-xs text-gray-500 dark:text-gray-400"><tr><th class="py-2">{{ t('organization.overview.reporting.columns.date') }}</th><th class="py-2 text-right">{{ t('organization.overview.reporting.columns.requests') }}</th><th class="py-2 text-right">{{ t('organization.overview.reporting.columns.success') }}</th><th class="py-2 text-right">{{ t('organization.overview.reporting.columns.errors') }}</th><th class="py-2 text-right">{{ t('organization.overview.reporting.columns.cost') }}</th></tr></thead><tbody><tr v-for="trend in report.trends" :key="trend.occurred_on" class="border-t border-gray-100 dark:border-dark-700"><td class="py-2 text-gray-900 dark:text-white">{{ trend.occurred_on }}</td><td class="py-2 text-right font-mono">{{ trend.request_count }}</td><td class="py-2 text-right font-mono">{{ trend.success_count }}</td><td class="py-2 text-right font-mono">{{ trend.error_count_total }}</td><td class="py-2 text-right font-mono">{{ trend.cost }}</td></tr></tbody></table></div>
                <p v-else class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.reporting.empty') }}</p>
              </div>
            </div>
          </template>
          <p v-else class="mt-5 text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.reporting.unavailable') }}</p>
        </section>

        <section class="border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-800" data-test="organization-members-section">
          <div class="flex flex-wrap items-center justify-between gap-3"><div><h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('organization.overview.members.title') }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.members.description') }}</p></div><div class="flex items-center gap-3"><span class="text-sm text-gray-500 dark:text-gray-400">{{ memberTotal }}</span><button v-if="canAddMember" type="button" class="btn btn-secondary" data-test="organization-add-member" @click="openAddMemberDialog"><Icon name="userPlus" size="sm" /><span>{{ t('organization.overview.members.addAction') }}</span></button></div></div>
          <div v-if="membersLoading" class="flex justify-center py-8"><LoadingSpinner /></div>
          <div v-else-if="members.length" class="mt-5 overflow-x-auto"><table class="w-full min-w-[640px] divide-y divide-gray-200 text-sm dark:divide-dark-700"><thead><tr class="text-left text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400"><th class="px-3 py-2 font-medium">{{ t('organization.overview.members.member') }}</th><th class="px-3 py-2 font-medium">{{ t('organization.overview.members.role') }}</th><th class="px-3 py-2 font-medium">{{ t('organization.overview.members.status') }}</th><th class="px-3 py-2 text-right font-medium">{{ t('organization.overview.members.actions') }}</th></tr></thead><tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="member in members" :key="member.user_id"><td class="px-3 py-3"><div class="font-medium text-gray-900 dark:text-white">{{ member.username || member.email }}</div><div class="text-xs text-gray-500 dark:text-gray-400">{{ member.email }}</div></td><td class="px-3 py-3 text-gray-700 dark:text-gray-200">{{ roleLabel(member.organization_role) }}</td><td class="px-3 py-3"><span :class="['badge', member.status === 'active' ? 'badge-success' : 'badge-gray']">{{ memberStatusLabel(member.status) }}</span></td><td class="px-3 py-3 text-right"><button v-if="canRemoveMember(member)" type="button" class="inline-flex h-9 w-9 items-center justify-center text-red-600 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-950/30" :aria-label="t('organization.overview.members.removeAction')" :title="t('organization.overview.members.removeAction')" :data-test="`organization-remove-member-${member.user_id}`" @click="openMemberRemoval(member)"><Icon name="trash" size="sm" /></button></td></tr></tbody></table></div>
          <p v-else class="mt-5 border border-dashed border-gray-200 p-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">{{ t('organization.overview.members.empty') }}</p>
          <Pagination v-if="memberTotal > memberPageSize" class="mt-4" :total="memberTotal" :page="memberPage" :page-size="memberPageSize" @update:page="changeMemberPage" @update:page-size="changeMemberPageSize" />
        </section>
      </template>
    </div>

    <BaseDialog :show="addDialogOpen" :title="t('organization.overview.members.addTitle')" width="normal" data-test="organization-add-member-dialog" @close="closeAddMemberDialog">
      <form data-test="organization-add-member-form" class="space-y-4" @submit.prevent="prepareAddMember"><p class="text-sm text-gray-600 dark:text-gray-300">{{ t('organization.overview.members.addDescription') }}</p><label class="block"><span class="input-label">{{ t('organization.overview.members.email') }}</span><input v-model="addTarget.email" data-test="organization-member-email" class="input mt-1 w-full" type="email" autocomplete="off" /></label><div class="flex items-center gap-3 text-xs text-gray-400"><span class="h-px flex-1 bg-gray-200 dark:bg-dark-600" /><span>{{ t('organization.overview.members.or') }}</span><span class="h-px flex-1 bg-gray-200 dark:bg-dark-600" /></div><label class="block"><span class="input-label">{{ t('organization.overview.members.userId') }}</span><input v-model="addTarget.userId" data-test="organization-member-user-id" class="input mt-1 w-full" inputmode="numeric" autocomplete="off" /></label><p v-if="addError" class="text-sm text-red-600 dark:text-red-400">{{ addError }}</p><div class="flex justify-end gap-3 border-t border-gray-100 pt-4 dark:border-dark-700"><button type="button" class="btn btn-secondary" @click="closeAddMemberDialog">{{ t('common.cancel') }}</button><button type="submit" class="btn btn-primary"><Icon name="userPlus" size="sm" /><span>{{ t('organization.overview.members.continue') }}</span></button></div></form>
    </BaseDialog>
    <ConfirmDialog :show="addWarningOpen" :title="t('organization.overview.members.addConfirmTitle')" :message="t('organization.overview.members.addWarning')" :confirm-text="t('organization.overview.members.addConfirm')" data-test="organization-add-member-confirmation" @confirm="confirmAddMember" @cancel="addWarningOpen = false" />
    <ConfirmDialog :show="removeDialogOpen" :title="t('organization.overview.members.removeTitle')" :message="memberRemovalMessage" :confirm-text="t('organization.overview.members.removeConfirm')" danger data-test="organization-remove-member-confirmation" @confirm="confirmMemberRemoval" @cancel="closeMemberRemoval" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { organizationAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import type { AddCurrentOrganizationMemberRequest, OrganizationMember, OrganizationReport, OrganizationRole, OrganizationSummary } from '@/types'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const organization = ref<OrganizationSummary | null>(null)
const organizationUnavailable = ref(false)
const report = ref<OrganizationReport | null>(null)
const members = ref<OrganizationMember[]>([])
const memberPage = ref(1)
const memberPageSize = ref(20)
const memberTotal = ref(0)
const memberPages = ref(0)
const loading = ref(true)
const dataLoading = ref(false)
const membersLoading = ref(false)
const reportLoading = ref(false)
const reportStartDate = ref(utcDateOffset(-29))
const reportEndDate = ref(utcDateOffset(1))
const addDialogOpen = ref(false)
const addWarningOpen = ref(false)
const addSubmitting = ref(false)
const addError = ref('')
const addTarget = reactive({ email: '', userId: '' })
const pendingAddTarget = ref<AddCurrentOrganizationMemberRequest | null>(null)
const removeDialogOpen = ref(false)
const removeSubmitting = ref(false)
const pendingRemovalMember = ref<OrganizationMember | null>(null)
const memberRemovalAttempt = ref({ userId: 0, key: '' })

const statusLabel = computed(() => organization.value ? t(`admin.organizations.status.${organization.value.status}`) : '')
const statusBadgeClass = computed(() => organization.value?.status === 'active' ? 'badge-success' : 'badge-warning')
const canAddMember = computed(() => organization.value?.status === 'active')
const reportStats = computed(() => {
  const current = report.value
  if (!current) return []
  return [
    { key: 'requests', label: t('organization.overview.reporting.requests'), value: current.request_count_total },
    { key: 'success', label: t('organization.overview.reporting.success'), value: current.success_count },
    { key: 'errors', label: t('organization.overview.reporting.errors'), value: current.error_count_total },
    { key: 'active', label: t('organization.overview.reporting.activeMembers'), value: current.active_member_count },
    { key: 'current-members', label: t('organization.overview.reporting.currentMembers'), value: current.current_member_count },
    { key: 'keys', label: t('organization.overview.reporting.apiKeys'), value: current.api_key_count },
    { key: 'success-rate', label: t('organization.overview.reporting.successRate'), value: formatRate(current.success_rate) },
    { key: 'error-rate', label: t('organization.overview.reporting.errorRate'), value: formatRate(current.error_rate) },
    { key: 'input', label: t('organization.overview.reporting.input'), value: current.input_tokens },
    { key: 'output', label: t('organization.overview.reporting.output'), value: current.output_tokens },
    { key: 'cost', label: t('organization.overview.reporting.cost'), value: current.cost }
  ]
})
const memberRemovalMessage = computed(() => pendingRemovalMember.value ? t('organization.overview.members.removeWarning', { member: pendingRemovalMember.value.username || pendingRemovalMember.value.email, amount: '0' }) : '')

function errorStatus(error: unknown): number | undefined { const typed = error as { status?: number; response?: { status?: number } }; return typed.status ?? typed.response?.status }
function errorMessage(error: unknown, fallback: string): string { const typed = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }; return typed.response?.data?.detail || typed.response?.data?.message || typed.message || fallback }
function utcDateOffset(offset: number): string { const date = new Date(); date.setUTCDate(date.getUTCDate() + offset); return date.toISOString().slice(0, 10) }
function formatRate(value: number): string { return `${(value * 100).toFixed(2)}%` }
function reportRange(): { start: string; end: string } | null { if (!/^\d{4}-\d{2}-\d{2}$/.test(reportStartDate.value) || !/^\d{4}-\d{2}-\d{2}$/.test(reportEndDate.value)) return null; const start = `${reportStartDate.value}T00:00:00Z`; const end = `${reportEndDate.value}T00:00:00Z`; return start < end ? { start, end } : null }
function isStrictPositiveInteger(value: string): boolean { return /^[1-9]\d*$/.test(value) && Number.isSafeInteger(Number(value)) }
function createMemberRemovalIdempotencyKey(): string { return globalThis.crypto?.randomUUID ? `organization-member-removal-${globalThis.crypto.randomUUID()}` : `organization-member-removal-${Date.now()}-${Math.random().toString(36).slice(2)}` }
function roleLabel(role: OrganizationRole): string { return t(`organization.overview.members.roles.${role}`) }
function memberStatusLabel(status: 'active' | 'disabled'): string { return t(`organization.overview.members.statuses.${status}`) }

async function loadOrganization(): Promise<void> {
  loading.value = true
  organizationUnavailable.value = false
  try { organization.value = await organizationAPI.getCurrent() } catch (error: unknown) { if (errorStatus(error) === 403 || errorStatus(error) === 404) organizationUnavailable.value = true; else appStore.showError(errorMessage(error, t('organization.overview.failedToLoad'))) } finally { loading.value = false }
}
async function loadMembers(): Promise<void> {
  membersLoading.value = true
  try { const response = await organizationAPI.listMembers(memberPage.value, memberPageSize.value); members.value = response.items; memberTotal.value = response.total; memberPages.value = response.pages } catch (error: unknown) { if (errorStatus(error) === 403 || errorStatus(error) === 404) organizationUnavailable.value = true; else appStore.showError(errorMessage(error, t('organization.overview.members.failedToLoad'))) } finally { membersLoading.value = false }
}
async function loadReport(): Promise<void> {
  const range = reportRange()
  if (!range) { appStore.showError(t('organization.overview.reporting.invalidRange')); return }
  reportLoading.value = true
  try { report.value = await organizationAPI.getReport(range.start, range.end) } catch (error: unknown) { if (errorStatus(error) === 403 || errorStatus(error) === 404) organizationUnavailable.value = true; else appStore.showError(errorMessage(error, t('organization.overview.reporting.failedToLoad'))) } finally { reportLoading.value = false }
}
async function reloadAll(): Promise<void> { dataLoading.value = true; try { await loadOrganization(); if (organization.value) await Promise.all([loadMembers(), loadReport()]) } finally { dataLoading.value = false } }
function openAddMemberDialog(): void { addError.value = ''; addTarget.email = ''; addTarget.userId = ''; addDialogOpen.value = true }
function closeAddMemberDialog(): void { if (!addSubmitting.value) addDialogOpen.value = false }
function prepareAddMember(): void {
  const email = addTarget.email.trim()
  const userId = addTarget.userId.trim()
  addError.value = ''
  if ((email && userId) || (!email && !userId)) {
    addError.value = t('organization.overview.members.targetRequired')
    return
  }
  if (userId && !isStrictPositiveInteger(userId)) {
    addError.value = t('organization.overview.members.invalidUserId')
    return
  }
  pendingAddTarget.value = userId ? { user_id: Number(userId) } : { email }
  addDialogOpen.value = false
  addWarningOpen.value = true
}
async function confirmAddMember(): Promise<void> { if (!pendingAddTarget.value || addSubmitting.value) return; addSubmitting.value = true; try { await organizationAPI.addMember(pendingAddTarget.value); appStore.showSuccess(t('organization.overview.members.added')); addWarningOpen.value = false; pendingAddTarget.value = null; await loadMembers() } catch (error: unknown) { appStore.showError(errorMessage(error, t('organization.overview.members.failedToAdd'))) } finally { addSubmitting.value = false } }
function canRemoveMember(member: OrganizationMember): boolean { return organization.value?.status === 'active' && member.organization_role === 'organization_member' && member.user_id !== authStore.user?.id }
function openMemberRemoval(member: OrganizationMember): void { if (!canRemoveMember(member)) return; pendingRemovalMember.value = member; memberRemovalAttempt.value = { userId: member.user_id, key: createMemberRemovalIdempotencyKey() }; removeDialogOpen.value = true }
function closeMemberRemoval(): void { if (!removeSubmitting.value) { removeDialogOpen.value = false; pendingRemovalMember.value = null } }
async function confirmMemberRemoval(): Promise<void> { const member = pendingRemovalMember.value; if (!member || removeSubmitting.value || !canRemoveMember(member)) return; removeSubmitting.value = true; try { await organizationAPI.removeMember(member.user_id, {}, memberRemovalAttempt.value.key); closeMemberRemoval(); appStore.showSuccess(t('organization.overview.members.removed')); await Promise.all([loadMembers(), loadReport()]) } catch (error: unknown) { appStore.showError(errorMessage(error, t('organization.overview.members.removeFailed'))) } finally { removeSubmitting.value = false } }
function changeMemberPage(page: number): void { if (page >= 1 && page <= Math.max(1, memberPages.value)) { memberPage.value = page; void loadMembers() } }
function changeMemberPageSize(pageSize: number): void { memberPageSize.value = pageSize; memberPage.value = 1; void loadMembers() }
onMounted(() => { void reloadAll() })
</script>
