<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-6xl space-y-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('organization.overview.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('organization.overview.description') }}
          </p>
        </div>
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="loading || dataLoading"
          :aria-label="t('common.refresh')"
          data-test="organization-refresh"
          @click="reloadAll"
        >
          <Icon name="refresh" size="sm" :class="(loading || dataLoading) ? 'animate-spin' : ''" />
          <span>{{ t('common.refresh') }}</span>
        </button>
      </div>

      <div v-if="loading" class="flex justify-center py-12">
        <LoadingSpinner />
      </div>

      <section
        v-else-if="organizationUnavailable"
        class="border border-amber-200 bg-amber-50 p-6 dark:border-amber-900/50 dark:bg-amber-900/20"
        data-test="organization-unavailable"
      >
        <h2 class="text-lg font-semibold text-amber-900 dark:text-amber-100">
          {{ t('organization.overview.unavailableTitle') }}
        </h2>
        <p class="mt-2 text-sm text-amber-800 dark:text-amber-200">
          {{ t('organization.overview.unavailableDescription') }}
        </p>
      </section>

      <template v-else-if="organization">
        <section
          class="border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-800"
          data-test="organization-identity-section"
        >
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div class="min-w-0">
              <h2 class="break-words text-xl font-semibold text-gray-900 dark:text-white">
                {{ organization.name }}
              </h2>
              <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">
                {{ organization.description || t('organization.overview.noDescription') }}
              </p>
            </div>
            <span :class="['badge', statusBadgeClass]">{{ statusLabel }}</span>
          </div>

          <dl class="mt-6 grid grid-cols-1 gap-5 border-t border-gray-100 pt-5 sm:grid-cols-3 dark:border-dark-700">
            <div>
              <dt class="text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.code') }}</dt>
              <dd class="mt-1">
                <code class="rounded bg-gray-100 px-1.5 py-0.5 text-sm text-gray-700 dark:bg-dark-700 dark:text-gray-200">
                  {{ organization.code }}
                </code>
              </dd>
            </div>
            <div>
              <dt class="text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.status') }}</dt>
              <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ statusLabel }}</dd>
            </div>
            <div>
              <dt class="text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.currency') }}</dt>
              <dd class="mt-1 text-sm font-medium text-gray-900 dark:text-white">USD</dd>
            </div>
          </dl>
        </section>

        <section
          class="border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-800"
          data-test="organization-balance-section"
        >
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('organization.overview.balance.title') }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('organization.overview.balance.description') }}
              </p>
            </div>
            <Icon name="dollar" size="lg" class="text-emerald-500" />
          </div>

          <div v-if="balanceLoading" class="flex justify-center py-8">
            <LoadingSpinner />
          </div>
          <div v-else-if="balance" class="mt-5 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-5">
            <div v-for="stat in balanceStats" :key="stat.key" class="border border-gray-100 p-3 dark:border-dark-700">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ stat.label }}</p>
              <p class="mt-2 whitespace-nowrap font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ stat.value }}</p>
            </div>
          </div>
          <p v-else class="mt-5 text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.balance.unavailable') }}</p>
        </section>

        <section
          class="border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-800"
          data-test="organization-allocation-section"
        >
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('organization.overview.allocations.title') }}
              </h2>
              <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
                {{ t('organization.overview.allocations.description') }}
              </p>
            </div>
            <div class="flex flex-wrap gap-2">
              <button
                type="button"
                class="btn btn-primary"
                data-test="organization-equal-allocation"
                :disabled="!canMutateAllocations || allocationPreviewLoading"
                @click="requestAllocationPreview('equal')"
              >
                <Icon name="users" size="sm" />
                <span>{{ t('organization.overview.allocations.equalAction') }}</span>
              </button>
              <button
                type="button"
                class="btn btn-secondary"
                data-test="organization-add-member"
                :disabled="!canAddMember"
                @click="openAddMemberDialog"
              >
                <Icon name="userPlus" size="sm" />
                <span>{{ t('organization.overview.members.addAction') }}</span>
              </button>
            </div>
          </div>

          <div class="mt-6 border-t border-gray-100 pt-5 dark:border-dark-700">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div class="inline-flex border border-gray-200 dark:border-dark-600" role="tablist">
                <button
                  type="button"
                  role="tab"
                  :aria-selected="allocationMode === 'custom'"
                  :class="allocationMode === 'custom' ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'text-gray-600 dark:text-gray-300'"
                  class="px-3 py-2 text-sm font-medium"
                  data-test="organization-custom-allocation-mode"
                  @click="allocationMode = 'custom'"
                >
                  {{ t('organization.overview.allocations.customTab') }}
                </button>
                <button
                  type="button"
                  role="tab"
                  :aria-selected="allocationMode === 'reclaim'"
                  :class="allocationMode === 'reclaim' ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'text-gray-600 dark:text-gray-300'"
                  class="border-l border-gray-200 px-3 py-2 text-sm font-medium dark:border-dark-600"
                  data-test="organization-reclaim-mode"
                  @click="allocationMode = 'reclaim'"
                >
                  {{ t('organization.overview.allocations.reclaimTab') }}
                </button>
              </div>
              <button
                type="button"
                class="btn btn-secondary"
                :data-test="allocationMode === 'custom' ? 'organization-custom-allocation-preview' : 'organization-reclaim-preview'"
                :disabled="!canMutateAllocations || allocationPreviewLoading"
                @click="requestAllocationPreview(allocationMode)"
              >
                <Icon v-if="allocationPreviewLoading" name="refresh" size="sm" class="animate-spin" />
                <Icon v-else name="eye" size="sm" />
                <span>{{ t('organization.overview.allocations.previewAction') }}</span>
              </button>
            </div>

            <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
              {{ allocationMode === 'custom' ? t('organization.overview.allocations.customHint') : t('organization.overview.allocations.reclaimHint') }}
            </p>

            <div v-if="editableMembers.length" class="mt-4 overflow-x-auto">
              <table class="w-full min-w-[640px] divide-y divide-gray-200 text-sm dark:divide-dark-700">
                <thead>
                  <tr class="text-left text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    <th class="px-3 py-2 font-medium">{{ t('organization.overview.members.member') }}</th>
                    <th class="px-3 py-2 font-medium">{{ t('organization.overview.members.available') }}</th>
                    <th class="px-3 py-2 font-medium">{{ t('organization.overview.allocations.amount') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                  <tr v-for="member in editableMembers" :key="member.user_id">
                    <td class="px-3 py-3">
                      <div class="font-medium text-gray-900 dark:text-white">{{ member.username || member.email }}</div>
                      <div class="text-xs text-gray-500 dark:text-gray-400">{{ member.email }}</div>
                    </td>
                    <td class="px-3 py-3 font-mono text-gray-700 dark:text-gray-200">{{ member.allocation.available }}</td>
                    <td class="px-3 py-3">
                      <input
                        v-model="allocationAmounts[String(member.user_id)]"
                        :data-test="`organization-allocation-amount-${member.user_id}`"
                        class="input w-full max-w-xs font-mono"
                        inputmode="decimal"
                        autocomplete="off"
                        :placeholder="allocationMode === 'custom' ? '0.00000000' : member.allocation.available"
                        :aria-label="t('organization.overview.allocations.amountFor', { member: member.email })"
                      />
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else class="mt-4 border border-dashed border-gray-200 p-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
              {{ t('organization.overview.allocations.noEligibleMembers') }}
            </p>
          </div>

          <div v-if="lastResult" class="mt-5 border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-900/50 dark:bg-emerald-900/20" data-test="organization-allocation-result">
            <div class="flex items-start justify-between gap-3">
              <div>
                <p class="font-medium text-emerald-800 dark:text-emerald-200">{{ t('organization.overview.allocations.resultTitle') }}</p>
                <p class="mt-1 text-sm text-emerald-700 dark:text-emerald-300">
                  {{ t('organization.overview.allocations.resultSummary', { amount: lastResult.total_delta, pool: lastResult.pool_after }) }}
                </p>
              </div>
              <button type="button" class="text-emerald-700 hover:text-emerald-900 dark:text-emerald-300" :aria-label="t('common.close')" @click="lastResult = null">
                <Icon name="x" size="sm" />
              </button>
            </div>
          </div>
        </section>

        <section
          class="border border-gray-200 bg-white p-6 dark:border-dark-700 dark:bg-dark-800"
          data-test="organization-members-section"
        >
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('organization.overview.members.title') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('organization.overview.members.description') }}</p>
            </div>
            <span class="text-sm text-gray-500 dark:text-gray-400">{{ memberTotal }}</span>
          </div>

          <div v-if="membersLoading" class="flex justify-center py-8"><LoadingSpinner /></div>
          <div v-else-if="members.length" class="mt-5 overflow-x-auto">
              <table class="w-full min-w-[840px] divide-y divide-gray-200 text-sm dark:divide-dark-700">
              <thead>
                <tr class="text-left text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                  <th class="px-3 py-2 font-medium">{{ t('organization.overview.members.member') }}</th>
                  <th class="px-3 py-2 font-medium">{{ t('organization.overview.members.role') }}</th>
                  <th class="px-3 py-2 font-medium">{{ t('organization.overview.members.status') }}</th>
                  <th class="px-3 py-2 text-right font-medium">{{ t('organization.overview.members.available') }}</th>
                  <th class="px-3 py-2 text-right font-medium">{{ t('organization.overview.members.reserved') }}</th>
                  <th class="px-3 py-2 text-right font-medium">{{ t('organization.overview.members.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr v-for="member in members" :key="member.user_id">
                  <td class="px-3 py-3">
                    <div class="font-medium text-gray-900 dark:text-white">{{ member.username || member.email }}</div>
                    <div class="text-xs text-gray-500 dark:text-gray-400">{{ member.email }}</div>
                  </td>
                  <td class="px-3 py-3 text-gray-700 dark:text-gray-200">{{ roleLabel(member.organization_role) }}</td>
                  <td class="px-3 py-3"><span :class="['badge', member.status === 'active' ? 'badge-success' : 'badge-gray']">{{ memberStatusLabel(member.status) }}</span></td>
                  <td class="px-3 py-3 text-right font-mono text-gray-700 dark:text-gray-200">{{ member.allocation.available }}</td>
                  <td class="px-3 py-3 text-right font-mono text-gray-700 dark:text-gray-200">{{ member.allocation.reserved }}</td>
                  <td class="px-3 py-3 text-right">
                    <button
                      v-if="canRemoveMember(member)"
                      type="button"
                      class="inline-flex h-9 w-9 items-center justify-center text-red-600 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-950/30"
                      :aria-label="t('organization.overview.members.removeAction')"
                      :title="t('organization.overview.members.removeAction')"
                      :data-test="`organization-remove-member-${member.user_id}`"
                      @click="openMemberRemoval(member)"
                    >
                      <Icon name="trash" size="sm" />
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else class="mt-5 border border-dashed border-gray-200 p-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">{{ t('organization.overview.members.empty') }}</p>

          <Pagination
            v-if="memberTotal > memberPageSize"
            class="mt-4"
            :total="memberTotal"
            :page="memberPage"
            :page-size="memberPageSize"
            @update:page="changeMemberPage"
            @update:page-size="changeMemberPageSize"
          />
        </section>
      </template>
    </div>

    <BaseDialog
      :show="addDialogOpen"
      :title="t('organization.overview.members.addTitle')"
      width="normal"
      data-test="organization-add-member-dialog"
      @close="closeAddMemberDialog"
    >
      <form data-test="organization-add-member-form" class="space-y-4" @submit.prevent="prepareAddMember">
        <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('organization.overview.members.addDescription') }}</p>
        <label class="block">
          <span class="input-label">{{ t('organization.overview.members.email') }}</span>
          <input v-model="addTarget.email" data-test="organization-member-email" class="input mt-1 w-full" type="email" autocomplete="off" />
        </label>
        <div class="flex items-center gap-3 text-xs text-gray-400"><span class="h-px flex-1 bg-gray-200 dark:bg-dark-600" /><span>{{ t('organization.overview.members.or') }}</span><span class="h-px flex-1 bg-gray-200 dark:bg-dark-600" /></div>
        <label class="block">
          <span class="input-label">{{ t('organization.overview.members.userId') }}</span>
          <input v-model="addTarget.userId" data-test="organization-member-user-id" class="input mt-1 w-full" inputmode="numeric" autocomplete="off" />
        </label>
        <p v-if="addError" class="text-sm text-red-600 dark:text-red-400">{{ addError }}</p>
        <div class="flex justify-end gap-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <button type="button" class="btn btn-secondary" @click="closeAddMemberDialog">{{ t('common.cancel') }}</button>
          <button type="submit" class="btn btn-primary"><Icon name="userPlus" size="sm" /><span>{{ t('organization.overview.members.continue') }}</span></button>
        </div>
      </form>
    </BaseDialog>

    <ConfirmDialog
      :show="addWarningOpen"
      :title="t('organization.overview.members.addConfirmTitle')"
      :message="t('organization.overview.members.addWarning')"
      :confirm-text="t('organization.overview.members.addConfirm')"
      data-test="organization-add-member-confirmation"
      @confirm="confirmAddMember"
      @cancel="addWarningOpen = false"
    />

    <ConfirmDialog
      :show="removeDialogOpen"
      :title="t('organization.overview.members.removeTitle')"
      :message="memberRemovalMessage"
      :confirm-text="t('organization.overview.members.removeConfirm')"
      danger
      data-test="organization-remove-member-confirmation"
      @confirm="confirmMemberRemoval"
      @cancel="closeMemberRemoval"
    />

    <BaseDialog
      :show="!!allocationPreview"
      :title="allocationPreview ? allocationPreviewTitle : ''"
      width="wide"
      data-test="organization-allocation-preview-dialog"
      @close="closeAllocationPreview"
    >
      <template v-if="allocationPreview">
        <div class="space-y-5">
          <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div class="border border-gray-100 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('organization.overview.allocations.poolBefore') }}</p><p class="mt-1 font-mono font-semibold">{{ allocationPreview.confirmation.pool_before }}</p></div>
            <div class="border border-gray-100 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('organization.overview.allocations.totalDelta') }}</p><p class="mt-1 font-mono font-semibold">{{ allocationPreview.confirmation.total_delta }}</p></div>
            <div class="border border-gray-100 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('organization.overview.allocations.poolAfter') }}</p><p class="mt-1 font-mono font-semibold">{{ allocationPreview.confirmation.pool_after }}</p></div>
            <div class="border border-gray-100 p-3 dark:border-dark-700"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('organization.overview.allocations.remainder') }}</p><p class="mt-1 font-mono font-semibold">{{ allocationPreview.confirmation.rounding_remainder }}</p></div>
          </div>

          <p v-if="allocationPreview.confirmation.no_op_reason" class="border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-200">
            {{ noOpReasonLabel(allocationPreview.confirmation.no_op_reason) }}
          </p>

          <div v-if="allocationPreview.confirmation.targets.length" class="overflow-x-auto">
            <h3 class="mb-2 text-sm font-semibold text-gray-900 dark:text-white">{{ t('organization.overview.allocations.changes') }}</h3>
            <table class="w-full min-w-[640px] divide-y divide-gray-200 text-sm dark:divide-dark-700">
              <thead><tr class="text-left text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400"><th class="px-3 py-2">{{ t('organization.overview.members.userId') }}</th><th class="px-3 py-2">{{ t('organization.overview.allocations.before') }}</th><th class="px-3 py-2">{{ t('organization.overview.allocations.delta') }}</th><th class="px-3 py-2">{{ t('organization.overview.allocations.after') }}</th></tr></thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="target in allocationPreview.confirmation.targets" :key="`${target.user_id}-${target.allocation_id}`"><td class="px-3 py-2 font-mono">{{ target.user_id }}</td><td class="px-3 py-2 font-mono">{{ target.before }}</td><td class="px-3 py-2 font-mono" :class="target.delta.startsWith('-') ? 'text-amber-600' : 'text-emerald-600'">{{ target.delta }}</td><td class="px-3 py-2 font-mono">{{ target.after }}</td></tr></tbody>
            </table>
          </div>

          <div v-if="allocationPreview.excluded_members.length" class="border-t border-gray-100 pt-4 dark:border-dark-700">
            <h3 class="mb-2 text-sm font-semibold text-gray-900 dark:text-white">{{ t('organization.overview.allocations.excluded') }}</h3>
            <div class="flex flex-wrap gap-2">
              <span v-for="member in allocationPreview.excluded_members" :key="member.user_id" class="inline-flex items-center gap-1 border border-gray-200 px-2 py-1 text-xs text-gray-600 dark:border-dark-600 dark:text-gray-300">
                <span class="font-mono">{{ member.user_id }}</span><span>·</span><span>{{ exclusionReasonLabel(member.excluded_reason) }}</span>
              </span>
            </div>
          </div>

          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('organization.overview.allocations.previewExpiry') }}</p>
          <div class="flex justify-end gap-3 border-t border-gray-100 pt-4 dark:border-dark-700">
            <button type="button" class="btn btn-secondary" @click="closeAllocationPreview">{{ t('common.cancel') }}</button>
            <button
              type="button"
              class="btn btn-primary"
              data-test="organization-allocation-confirm"
              :disabled="allocationExecuting"
              @click="executeAllocationPreview"
            >
              <Icon v-if="allocationExecuting" name="refresh" size="sm" class="animate-spin" />
              <Icon v-else name="check" size="sm" />
              <span>{{ allocationExecuting ? t('organization.overview.allocations.confirming') : t('organization.overview.allocations.confirmAction') }}</span>
            </button>
          </div>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { organizationAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import type {
  AddCurrentOrganizationMemberRequest,
  OrganizationAllocationOperation,
  OrganizationAllocationPreview,
  OrganizationAllocationPreviewRequest,
  OrganizationAllocationResult,
  OrganizationBalanceSummary,
  OrganizationMember,
  OrganizationRole,
  OrganizationSummary
} from '@/types'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()

const organization = ref<OrganizationSummary | null>(null)
const organizationUnavailable = ref(false)
const balance = ref<OrganizationBalanceSummary | null>(null)
const members = ref<OrganizationMember[]>([])
const loading = ref(true)
const dataLoading = ref(false)
const balanceLoading = ref(false)
const membersLoading = ref(false)
const memberPage = ref(1)
const memberPageSize = ref(20)
const memberTotal = ref(0)
const memberPages = ref(0)

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

const allocationMode = ref<'custom' | 'reclaim'>('custom')
const allocationAmounts = ref<Record<string, string>>({})
const allocationPreviewLoading = ref(false)
const allocationExecuting = ref(false)
const allocationPreview = ref<OrganizationAllocationPreview | null>(null)
const allocationPreviewOperation = ref<OrganizationAllocationOperation | null>(null)
const allocationAttempt = ref({ key: '', fingerprint: '' })
const lastResult = ref<OrganizationAllocationResult | null>(null)

const statusLabel = computed(() => organization.value ? t(`admin.organizations.status.${organization.value.status}`) : '')
const statusBadgeClass = computed(() => {
  if (organization.value?.status === 'active') return 'badge-success'
  if (organization.value?.status === 'disabled') return 'badge-warning'
  return 'badge-gray'
})
const canMutateAllocations = computed(() => organization.value?.status === 'active')
const canAddMember = computed(() => organization.value?.status === 'active')
const editableMembers = computed(() => members.value.filter((member) => {
  return member.status === 'active' && member.organization_role === 'organization_member' && member.user_id !== authStore.user?.id
}))
const balanceStats = computed(() => {
  const current = balance.value
  if (!current) return []
  return [
    { key: 'unallocated', label: t('organization.overview.balance.unallocated'), value: current.unallocated },
    { key: 'available', label: t('organization.overview.balance.available'), value: current.available },
    { key: 'reserved', label: t('organization.overview.balance.reserved'), value: current.reserved },
    { key: 'consumed', label: t('organization.overview.balance.consumed'), value: current.consumed },
    { key: 'total_remaining', label: t('organization.overview.balance.totalRemaining'), value: current.total_remaining }
  ]
})
const allocationPreviewTitle = computed(() => {
  if (!allocationPreviewOperation.value) return t('organization.overview.allocations.previewTitle')
  return t(`organization.overview.allocations.${allocationPreviewOperation.value}PreviewTitle`)
})
const memberRemovalMessage = computed(() => {
  const member = pendingRemovalMember.value
  if (!member) return ''
  return t('organization.overview.members.removeWarning', {
    member: member.username || member.email,
    amount: member.allocation.available
  })
})

function errorStatus(error: unknown): number | undefined {
  const typedError = error as { status?: number; response?: { status?: number } }
  return typedError.status ?? typedError.response?.status
}

function errorCode(error: unknown): string {
  const typedError = error as {
    code?: string | number
    reason?: string
    response?: { data?: { code?: string | number; reason?: string } }
  }
  const topLevelCode = typeof typedError.code === 'string' ? typedError.code : ''
  return String(typedError.reason || typedError.response?.data?.reason || topLevelCode || typedError.response?.data?.code || '')
}

function errorMessage(error: unknown, fallback: string): string {
  const typedError = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
  return typedError.response?.data?.detail || typedError.response?.data?.message || typedError.message || fallback
}

function isStrictPositiveInteger(value: string): boolean {
  return /^[1-9]\d*$/.test(value) && Number.isSafeInteger(Number(value))
}

/** Normalize user-entered money without converting it through a JS number. */
function canonicalMoney(value: string): string | null {
  const match = value.trim().match(/^(\d+)(?:\.(\d{1,8}))?$/)
  if (!match) return null
  const whole = match[1].replace(/^0+(?=\d)/, '')
  if (whole.length > 12) return null
  const fraction = (match[2] || '').padEnd(8, '0')
  const result = `${whole}.${fraction}`
  if (/^0\.0{8}$/.test(result)) return null
  return result
}

function createIdempotencyKey(): string {
  if (globalThis.crypto?.randomUUID) return `organization-allocation-${globalThis.crypto.randomUUID()}`
  return `organization-allocation-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function createMemberRemovalIdempotencyKey(): string {
  if (globalThis.crypto?.randomUUID) return `organization-member-removal-${globalThis.crypto.randomUUID()}`
  return `organization-member-removal-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function allocationFingerprint(preview: OrganizationAllocationPreview, operation: OrganizationAllocationOperation): string {
  return `${operation}:${preview.canonical_payload_hash}:${preview.preview_token}`
}

function resetAllocationPreview(): void {
  allocationPreview.value = null
  allocationPreviewOperation.value = null
  allocationAttempt.value = { key: '', fingerprint: '' }
}

function isStaleAllocationConflict(error: unknown): boolean {
  if (errorStatus(error) !== 409) return false
  const code = errorCode(error).toUpperCase()
  return !code || code.includes('STALE') || code.includes('PREVIEW') || code.includes('VERSION') || code.includes('SNAPSHOT')
}

async function returnToDashboard(): Promise<boolean> {
  try {
    await authStore.refreshUser()
  } catch {
    // Keep the original authorization response authoritative for this page.
  }
  if (authStore.isOrganizationAdmin && !authStore.isAdmin) {
    return false
  }
  await router.replace(authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
  return true
}

async function loadOrganization(): Promise<void> {
  loading.value = true
  organizationUnavailable.value = false
  try {
    organization.value = await organizationAPI.getCurrent()
  } catch (error: unknown) {
    const status = errorStatus(error)
    if (status === 403 || status === 404) {
      organization.value = null
      balance.value = null
      members.value = []
      organizationUnavailable.value = !(await returnToDashboard())
      return
    }
    appStore.showError(errorMessage(error, t('organization.overview.failedToLoad')))
  } finally {
    loading.value = false
  }
}

async function loadBalance(): Promise<void> {
  balanceLoading.value = true
  try {
    balance.value = await organizationAPI.getBalance()
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('organization.overview.balance.failedToLoad')))
  } finally {
    balanceLoading.value = false
  }
}

async function loadMembers(): Promise<void> {
  membersLoading.value = true
  try {
    const response = await organizationAPI.listMembers(memberPage.value, memberPageSize.value)
    members.value = response.items
    memberTotal.value = response.total
    memberPages.value = response.pages
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('organization.overview.members.failedToLoad')))
  } finally {
    membersLoading.value = false
  }
}

async function reloadFunding(): Promise<void> {
  await Promise.all([loadBalance(), loadMembers()])
}

async function reloadAll(): Promise<void> {
  dataLoading.value = true
  try {
    await loadOrganization()
    if (organization.value) await reloadFunding()
  } finally {
    dataLoading.value = false
  }
}

function openAddMemberDialog(): void {
  addError.value = ''
  addTarget.email = ''
  addTarget.userId = ''
  addDialogOpen.value = true
}

function closeAddMemberDialog(): void {
  if (!addSubmitting.value) addDialogOpen.value = false
}

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

async function confirmAddMember(): Promise<void> {
  if (!pendingAddTarget.value || addSubmitting.value) return
  addSubmitting.value = true
  try {
    await organizationAPI.addMember(pendingAddTarget.value)
    appStore.showSuccess(t('organization.overview.members.added'))
    addWarningOpen.value = false
    pendingAddTarget.value = null
    await loadMembers()
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('organization.overview.members.failedToAdd')))
  } finally {
    addSubmitting.value = false
  }
}

function canRemoveMember(member: OrganizationMember): boolean {
  return organization.value?.status === 'active'
    && member.organization_role === 'organization_member'
    && member.user_id !== authStore.user?.id
}

function resetMemberRemoval(): void {
  removeDialogOpen.value = false
  pendingRemovalMember.value = null
  memberRemovalAttempt.value = { userId: 0, key: '' }
}

function openMemberRemoval(member: OrganizationMember): void {
  if (!canRemoveMember(member)) return
  pendingRemovalMember.value = member
  memberRemovalAttempt.value = {
    userId: member.user_id,
    key: createMemberRemovalIdempotencyKey()
  }
  removeDialogOpen.value = true
}

function closeMemberRemoval(): void {
  if (!removeSubmitting.value) resetMemberRemoval()
}

function refreshPendingRemovalMember(): void {
  const userId = pendingRemovalMember.value?.user_id
  if (!userId) return
  const refreshedMember = members.value.find((member) => member.user_id === userId)
  if (refreshedMember) {
    pendingRemovalMember.value = refreshedMember
  } else {
    resetMemberRemoval()
  }
}

async function confirmMemberRemoval(): Promise<void> {
  const member = pendingRemovalMember.value
  if (!member || removeSubmitting.value || !canRemoveMember(member)) return
  if (memberRemovalAttempt.value.userId !== member.user_id || !memberRemovalAttempt.value.key) {
    memberRemovalAttempt.value = {
      userId: member.user_id,
      key: createMemberRemovalIdempotencyKey()
    }
  }

  removeSubmitting.value = true
  try {
    await organizationAPI.removeMember(member.user_id, {}, memberRemovalAttempt.value.key)
    resetMemberRemoval()
    appStore.showSuccess(t('organization.overview.members.removed'))
    await reloadFunding()
  } catch (error: unknown) {
    const status = errorStatus(error)
    const code = errorCode(error).toUpperCase()
    if (status === 409 && code.includes('ALLOCATION_HAS_RESERVATIONS')) {
      await reloadFunding()
      refreshPendingRemovalMember()
      appStore.showError(t('organization.overview.members.removeReservations'))
    } else if (status === 403 || status === 404) {
      resetMemberRemoval()
      await reloadAll()
      appStore.showError(t(status === 403
        ? 'organization.overview.members.removeForbidden'
        : 'organization.overview.members.removeNotFound'))
    } else {
      appStore.showError(errorMessage(error, t('organization.overview.members.removeFailed')))
    }
  } finally {
    removeSubmitting.value = false
  }
}

function changeMemberPage(page: number): void {
  if (page < 1 || page > Math.max(1, memberPages.value)) return
  memberPage.value = page
  void loadMembers()
}

function changeMemberPageSize(pageSize: number): void {
  memberPageSize.value = pageSize
  memberPage.value = 1
  void loadMembers()
}

function buildExplicitTargets(): { targets: { user_id: number; amount: string }[]; invalid: boolean } {
  const targets: { user_id: number; amount: string }[] = []
  let invalid = false
  for (const member of editableMembers.value) {
    const raw = allocationAmounts.value[String(member.user_id)]?.trim() || ''
    if (!raw) continue
    const amount = canonicalMoney(raw)
    if (!amount) {
      invalid = true
      continue
    }
    targets.push({ user_id: member.user_id, amount })
  }
  return { targets, invalid }
}

async function requestAllocationPreview(operation: OrganizationAllocationOperation): Promise<void> {
  if (!canMutateAllocations.value || allocationPreviewLoading.value) return
  let request: OrganizationAllocationPreviewRequest = {}
  if (operation !== 'equal') {
    const { targets, invalid } = buildExplicitTargets()
    if (invalid) {
      appStore.showError(t('organization.overview.allocations.invalidAmount'))
      return
    }
    if (!targets.length) {
      appStore.showError(t('organization.overview.allocations.targetRequired'))
      return
    }
    request = { targets }
  }
  allocationPreviewLoading.value = true
  try {
    const nextPreview = await organizationAPI.previewAllocation(operation, request)
    allocationPreview.value = nextPreview
    allocationPreviewOperation.value = operation
    lastResult.value = null
    const fingerprint = allocationFingerprint(nextPreview, operation)
    if (allocationAttempt.value.fingerprint !== fingerprint) {
      allocationAttempt.value = { key: createIdempotencyKey(), fingerprint }
    }
  } catch (error: unknown) {
    appStore.showError(errorMessage(error, t('organization.overview.allocations.previewFailed')))
  } finally {
    allocationPreviewLoading.value = false
  }
}

function closeAllocationPreview(): void {
  if (!allocationExecuting.value) resetAllocationPreview()
}

async function executeAllocationPreview(): Promise<void> {
  const preview = allocationPreview.value
  const operation = allocationPreviewOperation.value
  if (!preview || !operation || allocationExecuting.value) return
  const fingerprint = allocationFingerprint(preview, operation)
  if (allocationAttempt.value.fingerprint !== fingerprint || !allocationAttempt.value.key) {
    allocationAttempt.value = { key: createIdempotencyKey(), fingerprint }
  }
  allocationExecuting.value = true
  try {
    // The confirmation is intentionally passed through unchanged from preview.
    const result = await organizationAPI.executeAllocation(
      operation,
      { preview_token: preview.preview_token, confirmation: preview.confirmation },
      allocationAttempt.value.key
    )
    lastResult.value = result
    allocationAmounts.value = {}
    resetAllocationPreview()
    appStore.showSuccess(t('organization.overview.allocations.executed'))
    await reloadFunding()
  } catch (error: unknown) {
    if (isStaleAllocationConflict(error)) {
      resetAllocationPreview()
      await reloadFunding()
      appStore.showError(t('organization.overview.allocations.stalePreview'))
    } else {
      appStore.showError(errorMessage(error, t('organization.overview.allocations.executeFailed')))
    }
  } finally {
    allocationExecuting.value = false
  }
}

function roleLabel(role: OrganizationRole): string {
  return t(`organization.overview.members.roles.${role}`)
}

function memberStatusLabel(status: 'active' | 'disabled'): string {
  return t(`organization.overview.members.statuses.${status}`)
}

function noOpReasonLabel(reason: string): string {
  return t(`organization.overview.allocations.noOpReasons.${reason}`)
}

function exclusionReasonLabel(reason?: string): string {
  if (!reason) return t('organization.overview.allocations.excludedGeneric')
  return t(`organization.overview.allocations.exclusionReasons.${reason}`)
}

onMounted(() => {
  void reloadAll()
})
</script>
