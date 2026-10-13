<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-col justify-between gap-3 lg:flex-row lg:items-center">
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-64">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="search"
                :placeholder="t('admin.organizations.searchOrganizations')"
                class="input pl-10"
                @input="handleSearch"
              />
            </div>
            <Select
              v-model="filters.status"
              data-test="organization-status-filter"
              :options="statusFilterOptions"
              class="w-full sm:w-40"
              @change="handleStatusChange"
            />
          </div>

          <div class="flex items-center justify-end gap-2">
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="loadOrganizations"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button
              type="button"
              class="btn btn-primary"
              data-test="organization-create"
              @click="openCreateDialog"
            >
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('admin.organizations.createOrganization') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="organizations"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
        >
          <template #cell-name="{ value, row }">
            <button
              type="button"
              class="max-w-full truncate text-left font-medium text-gray-900 transition-colors hover:text-primary-600 dark:text-white dark:hover:text-primary-400"
              :title="value"
              @click="openDetailDialog(row)"
            >
              {{ value }}
            </button>
          </template>

          <template #cell-code="{ value }">
            <code class="rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-700 dark:bg-dark-700 dark:text-gray-200">
              {{ value }}
            </code>
          </template>

          <template #cell-description="{ value }">
            <span class="block max-w-sm truncate text-sm text-gray-600 dark:text-gray-300" :title="value || ''">
              {{ value || '-' }}
            </span>
          </template>

          <template #cell-status="{ value }">
            <span :class="['badge', statusBadgeClass(value)]">
              {{ statusLabel(value) }}
            </span>
          </template>

          <template #cell-updated_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button
                type="button"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400"
                :title="t('common.view')"
                data-test="organization-view"
                @click="openDetailDialog(row)"
              >
                <Icon name="eye" size="sm" />
              </button>
              <button
                v-if="row.status !== 'archived'"
                type="button"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                :title="t('common.edit')"
                data-test="organization-edit"
                @click="openEditDialog(row)"
              >
                <Icon name="edit" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.organizations.noOrganizationsYet')"
              :description="t('admin.organizations.createFirstOrganization')"
              :action-text="t('admin.organizations.createOrganization')"
              @action="openCreateDialog"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="showDetailDialog"
      :title="t('admin.organizations.organizationDetails')"
      width="wide"
      @close="closeDetailDialog"
    >
      <div v-if="detailLoading" class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('common.loading') }}
      </div>
      <dl v-else-if="selectedOrganization" class="space-y-4">
        <div>
          <dt class="input-label">{{ t('admin.organizations.form.name') }}</dt>
          <dd class="text-sm text-gray-900 dark:text-white">{{ selectedOrganization.name }}</dd>
        </div>
        <div>
          <dt class="input-label">{{ t('admin.organizations.form.code') }}</dt>
          <dd>
            <code class="rounded bg-gray-100 px-1.5 py-0.5 text-sm text-gray-700 dark:bg-dark-700 dark:text-gray-200">
              {{ selectedOrganization.code }}
            </code>
          </dd>
        </div>
        <div>
          <dt class="input-label">{{ t('admin.organizations.form.status') }}</dt>
          <dd>
            <span :class="['badge', statusBadgeClass(selectedOrganization.status)]">
              {{ statusLabel(selectedOrganization.status) }}
            </span>
          </dd>
        </div>
        <div>
          <dt class="input-label">{{ t('admin.organizations.form.description') }}</dt>
          <dd class="whitespace-pre-wrap text-sm text-gray-700 dark:text-gray-300">
            {{ selectedOrganization.description || '-' }}
          </dd>
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <dt class="input-label">{{ t('admin.organizations.createdAt') }}</dt>
            <dd class="text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(selectedOrganization.created_at) }}</dd>
          </div>
          <div>
            <dt class="input-label">{{ t('admin.organizations.updatedAt') }}</dt>
            <dd class="text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(selectedOrganization.updated_at) }}</dd>
          </div>
        </div>
      </dl>

      <section
        v-if="selectedOrganization"
        class="mt-6 border-t border-gray-200 pt-5 dark:border-dark-700"
        data-test="organization-lifecycle-section"
      >
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.organizations.lifecycle.title') }}</h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.lifecycle.description') }}</p>
          </div>
          <div v-if="selectedOrganization.status !== 'archived'" class="flex flex-wrap gap-2">
            <button
              v-if="selectedOrganization.status === 'active'"
              type="button"
              class="btn btn-secondary"
              :disabled="lifecycleSubmitting"
              data-test="organization-disable"
              @click="requestLifecycleAction('disable')"
            >
              {{ t('admin.organizations.lifecycle.disable') }}
            </button>
            <button
              v-else
              type="button"
              class="btn btn-secondary"
              :disabled="lifecycleSubmitting"
              data-test="organization-reactivate"
              @click="requestLifecycleAction('reactivate')"
            >
              {{ t('admin.organizations.lifecycle.reactivate') }}
            </button>
            <button
              type="button"
              class="btn btn-danger"
              :disabled="lifecycleSubmitting"
              data-test="organization-archive"
              @click="requestLifecycleAction('archive')"
            >
              {{ t('admin.organizations.lifecycle.archive') }}
            </button>
          </div>
        </div>
        <dl v-if="archivePreview" class="mt-4 grid grid-cols-2 gap-3 border-y border-red-100 py-3 text-sm dark:border-red-950/50">
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.lifecycle.frozenUnallocated') }}</dt>
            <dd class="mt-1 font-mono font-semibold text-red-700 dark:text-red-300">{{ archivePreview.unallocated }} USD</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.lifecycle.balanceVersion') }}</dt>
            <dd class="mt-1 font-mono text-gray-900 dark:text-white">{{ archivePreview.balance_version }}</dd>
          </div>
        </dl>
      </section>

      <section
        v-if="selectedOrganization"
        class="mt-6 border-t border-gray-200 pt-5 dark:border-dark-700"
        data-test="organization-funding-section"
      >
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.organizations.funding.title') }}
            </h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.organizations.funding.description') }}
            </p>
          </div>
          <button
            v-if="fundingWritable"
            type="button"
            class="btn btn-primary"
            data-test="organization-credit-open"
            @click="openCreditDialog"
          >
            <Icon name="plus" size="sm" class="mr-1.5" />
            {{ t('admin.organizations.funding.credit') }}
          </button>
        </div>

        <div v-if="balanceLoading" class="py-5 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('common.loading') }}
        </div>
        <dl
          v-else-if="balanceSummary"
          class="grid grid-cols-2 gap-x-5 gap-y-4 border-y border-gray-100 py-4 sm:grid-cols-3 dark:border-dark-700"
        >
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.funding.unallocated') }}</dt>
            <dd class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">
              {{ balanceSummary.unallocated }} {{ balanceSummary.currency }}
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.funding.available') }}</dt>
            <dd class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">
              {{ balanceSummary.available }} {{ balanceSummary.currency }}
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.funding.reserved') }}</dt>
            <dd class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">
              {{ balanceSummary.reserved }} {{ balanceSummary.currency }}
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.funding.consumed') }}</dt>
            <dd class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">
              {{ balanceSummary.consumed }} {{ balanceSummary.currency }}
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.funding.totalRemaining') }}</dt>
            <dd class="mt-1 font-mono text-sm font-semibold text-gray-900 dark:text-white">
              {{ balanceSummary.total_remaining }} {{ balanceSummary.currency }}
            </dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.funding.version') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">
              {{ balanceSummary.balance_version }}
            </dd>
          </div>
        </dl>

        <div class="mt-5 flex items-center justify-between gap-3">
          <h5 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('admin.organizations.funding.ledger') }}
          </h5>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="ledgerLoading"
            :title="t('common.refresh')"
            @click="() => reloadFunding()"
          >
            <Icon name="refresh" size="sm" :class="ledgerLoading ? 'animate-spin' : ''" />
          </button>
        </div>
        <div v-if="ledgerLoading" class="py-5 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('common.loading') }}
        </div>
        <div
          v-else-if="ledgerEntries.length === 0"
          class="mt-3 border border-dashed border-gray-300 px-4 py-5 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
          data-test="organization-ledger-empty"
        >
          {{ t('admin.organizations.funding.noLedger') }}
        </div>
        <div v-else class="mt-3 overflow-x-auto">
          <table class="w-full min-w-[760px] text-left text-sm" data-test="organization-ledger-table">
            <thead class="border-b border-gray-200 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
              <tr>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.funding.columns.time') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.funding.columns.operation') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.funding.columns.amount') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.funding.columns.balance') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.funding.columns.actor') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.funding.columns.reason') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="entry in ledgerEntries" :key="entry.posting_id">
                <td class="whitespace-nowrap px-2 py-3 text-xs text-gray-600 dark:text-gray-300">
                  {{ formatDateTime(entry.occurred_at) }}
                </td>
                <td class="px-2 py-3">
                  <div class="font-medium text-gray-900 dark:text-white">{{ entry.operation_type }}</div>
                  <div class="mt-0.5 max-w-40 truncate font-mono text-xs text-gray-500" :title="entry.operation_id">
                    {{ entry.operation_id }}
                  </div>
                </td>
                <td class="whitespace-nowrap px-2 py-3 font-mono text-sm font-semibold text-gray-900 dark:text-white">
                  {{ entry.delta_amount }} {{ entry.currency }}
                </td>
                <td class="whitespace-nowrap px-2 py-3 font-mono text-xs text-gray-600 dark:text-gray-300">
                  {{ entry.before_amount }} → {{ entry.after_amount }}
                </td>
                <td class="px-2 py-3 text-xs text-gray-600 dark:text-gray-300">
                  <div>{{ entry.actor_display_snapshot }}</div>
                  <div class="mt-0.5">{{ entry.authorization_scope }}</div>
                </td>
                <td class="max-w-56 px-2 py-3 text-xs text-gray-600 dark:text-gray-300">
                  <div class="break-words">{{ entry.public_reason || '-' }}</div>
                  <div class="mt-1 break-words text-gray-500 dark:text-gray-400">{{ entry.internal_reason }}</div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="ledgerPagination.total > 0" class="mt-4">
          <Pagination
            :page="ledgerPagination.page"
            :total="ledgerPagination.total"
            :page-size="ledgerPagination.page_size"
            @update:page="handleLedgerPageChange"
            @update:pageSize="handleLedgerPageSizeChange"
          />
        </div>
      </section>

      <section
        v-if="selectedOrganization"
        class="mt-6 border-t border-gray-200 pt-5 dark:border-dark-700"
        data-test="organization-reconciliation-section"
      >
        <div class="mb-4 flex items-center justify-between gap-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.organizations.reconciliation.title') }}</h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.reconciliation.description') }}</p>
          </div>
          <button type="button" class="btn btn-secondary" :disabled="reconciliationLoading" :title="t('common.refresh')" @click="loadPendingReconciliations(selectedOrganization.id)">
            <Icon name="refresh" size="sm" :class="reconciliationLoading ? 'animate-spin' : ''" />
          </button>
        </div>
        <div v-if="reconciliationLoading" class="py-4 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</div>
        <div v-else-if="pendingReconciliations.length === 0" class="border border-dashed border-gray-300 px-4 py-4 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400" data-test="organization-reconciliation-empty">
          {{ t('admin.organizations.reconciliation.empty') }}
        </div>
        <div v-else class="divide-y divide-gray-100 border-y border-gray-100 dark:divide-dark-700 dark:border-dark-700">
          <div v-for="item in pendingReconciliations" :key="item.execution_id" class="py-3" data-test="organization-reconciliation-item">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <button type="button" class="min-w-0 text-left" @click="loadPendingReconciliationDetail(item.execution_id)">
                <span class="block truncate font-mono text-xs text-gray-700 dark:text-gray-200">{{ item.execution_id }}</span>
                <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ item.reserved_amount }} USD · {{ formatDateTime(item.accepted_at) }} · {{ t('admin.organizations.reconciliation.attemptCount', { count: item.attempt_count }) }}</span>
              </button>
              <div class="flex items-center gap-2">
                <span v-if="item.conflict" class="badge badge-warning">{{ t('admin.organizations.reconciliation.conflict') }}</span>
                <span v-else-if="!item.allowed_resolution" class="badge badge-gray">{{ t('admin.organizations.reconciliation.awaitingEvidence') }}</span>
                <button v-else type="button" class="btn btn-primary" data-test="organization-reconciliation-resolve" @click="requestPendingReconciliationResolution(item)">
                  {{ t('admin.organizations.reconciliation.resolve') }}
                </button>
              </div>
            </div>
            <div v-if="item.allowed_resolution" class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-600 dark:text-gray-300">
              <span>{{ t('admin.organizations.reconciliation.allowedResolution') }}: {{ item.allowed_resolution }}</span>
              <span>{{ t('admin.organizations.reconciliation.computedAmount') }}: {{ item.computed_organization_amount || '0.00000000' }} USD</span>
              <span>{{ t('admin.organizations.reconciliation.candidateCeiling') }}: {{ item.candidate_ceiling }} USD</span>
            </div>
            <dl v-if="pendingReconciliationDetail?.execution_id === item.execution_id" class="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-gray-100 pt-3 text-xs dark:border-dark-700">
              <template v-for="attempt in pendingReconciliationDetail.attempts" :key="attempt.attempt_id">
                <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.organizations.reconciliation.attempt') }} #{{ attempt.attempt_sequence }}</dt>
                <dd class="font-mono text-gray-700 dark:text-gray-200">{{ reconciliationAttemptStatusLabel(attempt.status) }} · {{ attempt.attempt_id }}</dd>
              </template>
              <template v-for="evidence in pendingReconciliationDetail.evidence" :key="`${evidence.attempt_id}-${evidence.evidence_source}`">
                <dt class="text-gray-500 dark:text-gray-400">{{ reconciliationEvidenceSourceLabel(evidence.evidence_source) }} · {{ reconciliationOutcomeLabel(evidence.terminal_outcome) }}</dt>
                <dd class="font-mono text-gray-700 dark:text-gray-200">{{ evidence.raw_actual_cost }} USD · {{ evidence.input_tokens + evidence.output_tokens }} tokens</dd>
              </template>
            </dl>
          </div>
        </div>
        <div
          v-if="pendingReconciliationPagination.total > 0"
          class="mt-4"
          data-test="organization-reconciliation-pagination"
        >
          <Pagination
            :page="pendingReconciliationPagination.page"
            :total="pendingReconciliationPagination.total"
            :page-size="pendingReconciliationPagination.page_size"
            :page-size-options="[10, 20, 50, 100]"
            @update:page="handlePendingReconciliationPageChange"
            @update:pageSize="handlePendingReconciliationPageSizeChange"
          />
        </div>
      </section>

      <section
        v-if="selectedOrganization"
        class="mt-6 border-t border-gray-200 pt-5 dark:border-dark-700"
        data-test="organization-allocation-section"
      >
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.organizations.allocations.title') }}
            </h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.organizations.allocations.description') }}
            </p>
          </div>
          <span
            v-if="selectedOrganization.status === 'disabled'"
            class="badge badge-warning"
            data-test="organization-allocation-reclaim-only"
          >
            {{ t('admin.organizations.allocations.reclaimOnly') }}
          </span>
        </div>

        <template v-if="allocationActionsEnabled">
          <div class="mt-5 flex flex-wrap items-center justify-between gap-3">
            <div
              v-if="allocationWritable"
              class="inline-flex border border-gray-200 dark:border-dark-600"
              role="tablist"
            >
              <button
                type="button"
                role="tab"
                :aria-selected="allocationMode === 'custom'"
                :class="allocationMode === 'custom' ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'text-gray-600 dark:text-gray-300'"
                class="px-3 py-2 text-sm font-medium"
                data-test="organization-allocation-custom-mode"
                @click="allocationMode = 'custom'"
              >
                {{ t('admin.organizations.allocations.customTab') }}
              </button>
              <button
                type="button"
                role="tab"
                :aria-selected="allocationMode === 'reclaim'"
                :class="allocationMode === 'reclaim' ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'text-gray-600 dark:text-gray-300'"
                class="border-l border-gray-200 px-3 py-2 text-sm font-medium dark:border-dark-600"
                data-test="organization-allocation-reclaim-mode"
                @click="allocationMode = 'reclaim'"
              >
                {{ t('admin.organizations.allocations.reclaimTab') }}
              </button>
            </div>
            <p v-else class="text-sm font-medium text-gray-700 dark:text-gray-200">
              {{ t('admin.organizations.allocations.reclaimTab') }}
            </p>
            <button
              type="button"
              class="btn btn-secondary"
              :data-test="`organization-allocation-${activeAllocationOperation}-preview`"
              :disabled="allocationPreviewLoading"
              @click="requestAllocationPreview"
            >
              <Icon v-if="allocationPreviewLoading" name="refresh" size="sm" class="animate-spin" />
              <Icon v-else name="eye" size="sm" />
              <span>{{ t('admin.organizations.allocations.previewAction') }}</span>
            </button>
          </div>

          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            {{
              activeAllocationOperation === 'custom'
                ? t('admin.organizations.allocations.customHint')
                : t('admin.organizations.allocations.reclaimHint')
            }}
          </p>

          <div v-if="allocationEligibleMembers.length" class="mt-4 overflow-x-auto">
            <table class="w-full min-w-[720px] text-left text-sm">
              <thead class="border-b border-gray-200 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
                <tr>
                  <th class="px-2 py-2 font-medium">{{ t('admin.organizations.members.columns.member') }}</th>
                  <th class="px-2 py-2 font-medium">{{ t('admin.organizations.members.columns.status') }}</th>
                  <th class="px-2 py-2 font-medium">{{ t('admin.organizations.members.columns.role') }}</th>
                  <th class="px-2 py-2 font-medium">{{ t('admin.organizations.members.available') }}</th>
                  <th class="px-2 py-2 font-medium">{{ t('admin.organizations.allocations.amount') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr v-for="member in allocationEligibleMembers" :key="member.user_id">
                  <td class="px-2 py-3">
                    <div class="font-medium text-gray-900 dark:text-white">{{ member.username || '-' }}</div>
                    <div class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">#{{ member.user_id }} · {{ member.email || '-' }}</div>
                  </td>
                  <td class="px-2 py-3">
                    <span :class="['badge', member.status === 'active' ? 'badge-success' : 'badge-warning']">
                      {{ memberStatusLabel(member.status) }}
                    </span>
                  </td>
                  <td class="px-2 py-3 text-xs text-gray-600 dark:text-gray-300">
                    {{ organizationRoleLabel(member.organization_role) }}
                  </td>
                  <td class="px-2 py-3 font-mono text-xs text-gray-700 dark:text-gray-200">
                    {{ member.allocation.available }}
                  </td>
                  <td class="px-2 py-3">
                    <input
                      v-model="allocationAmounts[String(member.user_id)]"
                      :data-test="`organization-allocation-amount-${member.user_id}`"
                      class="input w-full max-w-xs font-mono"
                      inputmode="decimal"
                      autocomplete="off"
                      :placeholder="activeAllocationOperation === 'custom' ? '0.00000000' : member.allocation.available"
                      :aria-label="t('admin.organizations.allocations.amountFor', { member: member.email })"
                    />
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p
            v-else
            class="mt-4 border border-dashed border-gray-300 px-4 py-5 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
          >
            {{ t('admin.organizations.allocations.noEligibleMembers') }}
          </p>

          <div
            v-if="lastAllocationResult"
            class="mt-5 border border-emerald-200 bg-emerald-50 p-4 text-sm dark:border-emerald-900/50 dark:bg-emerald-900/20"
            data-test="organization-allocation-result"
          >
            <div class="flex items-start justify-between gap-3">
              <p class="text-emerald-800 dark:text-emerald-200">
                {{
                  t('admin.organizations.allocations.resultSummary', {
                    amount: lastAllocationResult.total_delta,
                    pool: lastAllocationResult.pool_after
                  })
                }}
              </p>
              <button
                type="button"
                class="text-emerald-700 hover:text-emerald-900 dark:text-emerald-300"
                :aria-label="t('common.close')"
                @click="lastAllocationResult = null"
              >
                <Icon name="x" size="sm" />
              </button>
            </div>
          </div>
        </template>
        <p v-else class="mt-4 text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.organizations.allocations.archivedReadOnly') }}
        </p>
      </section>

      <section
        v-if="selectedOrganization"
        class="mt-6 border-t border-gray-200 pt-5 dark:border-dark-700"
        data-test="organization-members-section"
      >
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.organizations.members.title') }}
            </h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{
                membersWritable
                  ? t('admin.organizations.members.safeDataHint')
                  : t('admin.organizations.members.archivedReadOnly')
              }}
            </p>
          </div>
          <button
            v-if="membersWritable"
            type="button"
            class="btn btn-secondary"
            data-test="organization-add-member"
            @click="openAddMemberDialog"
          >
            <Icon name="plus" size="sm" class="mr-1.5" />
            {{ t('admin.organizations.members.addMember') }}
          </button>
        </div>

        <div
          v-if="membersLoading"
          class="py-5 text-center text-sm text-gray-500 dark:text-gray-400"
          data-test="organization-members-loading"
        >
          {{ t('common.loading') }}
        </div>
        <div
          v-else-if="members.length === 0"
          class="rounded-md border border-dashed border-gray-300 px-4 py-5 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
          data-test="organization-members-empty"
        >
          {{ t('admin.organizations.members.noMembers') }}
        </div>
        <div v-else class="overflow-x-auto">
          <table class="w-full min-w-[720px] text-left text-sm">
            <thead class="border-b border-gray-200 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
              <tr>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.members.columns.member') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.members.columns.status') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.members.columns.role') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.organizations.members.columns.allocation') }}</th>
                <th class="px-2 py-2 text-right font-medium">{{ t('admin.organizations.members.columns.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="member in members" :key="member.user_id">
                <td class="px-2 py-3">
                  <div class="font-medium text-gray-900 dark:text-white">{{ member.username || '-' }}</div>
                  <div class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                    #{{ member.user_id }} · {{ member.email || '-' }}
                  </div>
                </td>
                <td class="px-2 py-3">
                  <span :class="['badge', member.status === 'active' ? 'badge-success' : 'badge-warning']">
                    {{ memberStatusLabel(member.status) }}
                  </span>
                </td>
                <td class="px-2 py-3">
                  <Select
                    v-if="membersWritable"
                    :model-value="member.organization_role"
                    :options="organizationRoleOptions"
                    :disabled="memberRoleSavingUserId === member.user_id"
                    :data-test="`organization-member-role-${member.user_id}`"
                    class="min-w-44"
                    @update:model-value="changeMemberRole(member, $event)"
                  />
                  <span v-else class="text-gray-700 dark:text-gray-300">{{ organizationRoleLabel(member.organization_role) }}</span>
                </td>
                <td class="px-2 py-3 text-xs text-gray-600 dark:text-gray-300">
                  <div>{{ t('admin.organizations.members.available') }}: {{ member.allocation.available }}</div>
                  <div class="mt-0.5">{{ t('admin.organizations.members.reserved') }}: {{ member.allocation.reserved }}</div>
                </td>
                <td class="px-2 py-3 text-right">
                  <button
                    type="button"
                    class="inline-flex h-9 w-9 items-center justify-center text-red-600 hover:bg-red-50 hover:text-red-700 disabled:cursor-not-allowed disabled:opacity-40 dark:text-red-400 dark:hover:bg-red-950/30"
                    :disabled="!canRemoveMember(member) || removeSubmitting"
                    :aria-label="t('admin.organizations.members.removeMember')"
                    :title="t('admin.organizations.members.removeMember')"
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
        <div v-if="memberPagination.total > 0" class="mt-4">
          <Pagination
            :page="memberPagination.page"
            :total="memberPagination.total"
            :page-size="memberPagination.page_size"
            @update:page="handleMemberPageChange"
            @update:pageSize="handleMemberPageSizeChange"
          />
        </div>
      </section>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeDetailDialog">
            {{ t('common.close') }}
          </button>
          <button
            v-if="selectedOrganization && selectedOrganization.status !== 'archived'"
            type="button"
            class="btn btn-primary"
            data-test="organization-detail-edit"
            @click="openEditFromDetail"
          >
            <Icon name="edit" size="sm" class="mr-2" />
            {{ t('common.edit') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="!!allocationPreview"
      :title="allocationPreviewTitle"
      width="wide"
      @close="closeAllocationPreview"
    >
      <template v-if="allocationPreview">
        <div class="space-y-5" data-test="organization-allocation-preview-dialog">
          <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div class="border border-gray-100 p-3 dark:border-dark-700">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.allocations.poolBefore') }}</p>
              <p class="mt-1 font-mono font-semibold">{{ allocationPreview.confirmation.pool_before }}</p>
            </div>
            <div class="border border-gray-100 p-3 dark:border-dark-700">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.allocations.totalDelta') }}</p>
              <p class="mt-1 font-mono font-semibold">{{ allocationPreview.confirmation.total_delta }}</p>
            </div>
            <div class="border border-gray-100 p-3 dark:border-dark-700">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.allocations.poolAfter') }}</p>
              <p class="mt-1 font-mono font-semibold">{{ allocationPreview.confirmation.pool_after }}</p>
            </div>
            <div class="border border-gray-100 p-3 dark:border-dark-700">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.organizations.allocations.remainder') }}</p>
              <p class="mt-1 font-mono font-semibold">{{ allocationPreview.confirmation.rounding_remainder }}</p>
            </div>
          </div>

          <div v-if="allocationPreview.confirmation.targets.length" class="overflow-x-auto">
            <table class="w-full min-w-[560px] text-left text-sm">
              <thead class="border-b border-gray-200 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
                <tr>
                  <th class="px-2 py-2 font-medium">{{ t('admin.organizations.allocations.member') }}</th>
                  <th class="px-2 py-2 font-medium">{{ t('admin.organizations.allocations.before') }}</th>
                  <th class="px-2 py-2 font-medium">{{ t('admin.organizations.allocations.delta') }}</th>
                  <th class="px-2 py-2 font-medium">{{ t('admin.organizations.allocations.after') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr v-for="target in allocationPreview.confirmation.targets" :key="`${target.user_id}-${target.allocation_id}`">
                  <td class="px-2 py-3 font-mono">{{ target.user_id }}</td>
                  <td class="px-2 py-3 font-mono">{{ target.before }}</td>
                  <td
                    class="px-2 py-3 font-mono"
                    :class="target.delta.startsWith('-') ? 'text-amber-600' : 'text-emerald-600'"
                  >
                    {{ target.delta }}
                  </td>
                  <td class="px-2 py-3 font-mono">{{ target.after }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.organizations.allocations.previewExpiry') }}
          </p>
        </div>
      </template>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="allocationExecuting" @click="closeAllocationPreview">
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            data-test="organization-allocation-confirm"
            :disabled="allocationExecuting"
            @click="executeAllocationPreview"
          >
            <Icon v-if="allocationExecuting" name="refresh" size="sm" class="animate-spin" />
            <Icon v-else name="check" size="sm" />
            <span>{{ allocationExecuting ? t('admin.organizations.allocations.confirming') : t('admin.organizations.allocations.confirmAction') }}</span>
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showCreditDialog"
      :title="t('admin.organizations.funding.creditTitle')"
      width="normal"
      @close="closeCreditDialog"
    >
      <form id="organization-credit-form" class="space-y-5" data-test="organization-credit-form" @submit.prevent="submitCredit">
        <div>
          <label class="input-label" for="organization-credit-amount">
            {{ t('admin.organizations.funding.amount') }}
          </label>
          <input
            id="organization-credit-amount"
            v-model="creditForm.amount"
            data-test="organization-credit-amount"
            type="text"
            inputmode="decimal"
            autocomplete="off"
            class="input font-mono"
            placeholder="0.00000000"
            required
          />
          <p class="input-hint">{{ t('admin.organizations.funding.amountHint') }}</p>
        </div>
        <div>
          <label class="input-label" for="organization-credit-public-reason">
            {{ t('admin.organizations.funding.publicReason') }}
          </label>
          <input
            id="organization-credit-public-reason"
            v-model="creditForm.publicReason"
            data-test="organization-credit-public-reason"
            type="text"
            class="input"
          />
        </div>
        <div>
          <label class="input-label" for="organization-credit-internal-reason">
            {{ t('admin.organizations.funding.internalReason') }}
          </label>
          <textarea
            id="organization-credit-internal-reason"
            v-model="creditForm.internalReason"
            data-test="organization-credit-internal-reason"
            rows="3"
            class="input"
            required
          ></textarea>
        </div>
        <div class="border-l-2 border-primary-500 pl-3 text-sm text-gray-600 dark:text-gray-300">
          {{ t('admin.organizations.funding.creditConfirmation', {
            organization: selectedOrganization?.name || '-',
            amount: creditForm.amount || '0.00000000'
          }) }}
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="creditSubmitting" @click="closeCreditDialog">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="organization-credit-form"
            class="btn btn-primary"
            data-test="organization-credit-submit"
            :disabled="creditSubmitting"
          >
            {{ creditSubmitting ? t('common.saving') : t('admin.organizations.funding.confirmCredit') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showFormDialog"
      :title="isEditing ? t('admin.organizations.editOrganization') : t('admin.organizations.createOrganization')"
      width="normal"
      @close="closeFormDialog"
    >
      <form id="organization-form" data-test="organization-form" class="space-y-5" @submit.prevent="saveOrganization">
        <div>
          <label class="input-label" for="organization-name">{{ t('admin.organizations.form.name') }}</label>
          <input
            id="organization-name"
            v-model="form.name"
            data-test="organization-name"
            type="text"
            class="input"
            required
            :placeholder="t('admin.organizations.form.namePlaceholder')"
          />
        </div>
        <div>
          <label class="input-label" for="organization-code">{{ t('admin.organizations.form.code') }}</label>
          <input
            id="organization-code"
            v-model="form.code"
            data-test="organization-code"
            type="text"
            class="input"
            required
            autocomplete="off"
            :placeholder="t('admin.organizations.form.codePlaceholder')"
          />
          <p class="input-hint">{{ t('admin.organizations.form.codeHint') }}</p>
        </div>
        <div>
          <label class="input-label" for="organization-description">{{ t('admin.organizations.form.description') }}</label>
          <textarea
            id="organization-description"
            v-model="form.description"
            data-test="organization-description"
            rows="3"
            class="input"
            :placeholder="t('admin.organizations.form.descriptionPlaceholder')"
          ></textarea>
        </div>
        <div v-if="isEditing && editingOrganization" class="rounded-lg bg-gray-50 px-3 py-2.5 dark:bg-dark-700/60">
          <div class="input-label">{{ t('admin.organizations.form.status') }}</div>
          <span :class="['badge', statusBadgeClass(editingOrganization.status)]">
            {{ statusLabel(editingOrganization.status) }}
          </span>
          <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.organizations.statusManagedSeparately') }}
          </p>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeFormDialog">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" form="organization-form" class="btn btn-primary" :disabled="saving">
            {{ saving ? t('common.saving') : isEditing ? t('common.update') : t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showAddMemberDialog"
      :title="t('admin.organizations.members.addMember')"
      width="normal"
      @close="closeAddMemberDialog"
    >
      <form
        id="organization-add-member-form"
        data-test="organization-add-member-form"
        class="space-y-5"
        @submit.prevent="requestMemberAddition"
      >
        <div>
          <label class="input-label" for="organization-member-user-id">
            {{ t('admin.organizations.members.userId') }}
          </label>
          <input
            id="organization-member-user-id"
            v-model="memberForm.userId"
            data-test="organization-member-user-id"
            type="text"
            inputmode="numeric"
            autocomplete="off"
            class="input"
            :placeholder="t('admin.organizations.members.userIdPlaceholder')"
          />
          <p class="input-hint">{{ t('admin.organizations.members.userIdHint') }}</p>
        </div>
        <div>
          <label class="input-label" for="organization-member-role">
            {{ t('admin.organizations.members.role') }}
          </label>
          <Select
            id="organization-member-role"
            v-model="memberForm.organizationRole"
            :options="organizationRoleOptions"
          />
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeAddMemberDialog">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" form="organization-add-member-form" class="btn btn-primary">
            {{ t('common.continue') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="lifecycleAction !== null"
      :title="lifecycleConfirmationTitle"
      :message="lifecycleConfirmationMessage"
      :confirm-text="lifecycleConfirmationText"
      :danger="lifecycleAction === 'archive'"
      data-test="organization-lifecycle-confirmation"
      @confirm="confirmLifecycleAction"
      @cancel="cancelLifecycleAction"
    />

    <ConfirmDialog
      :show="showMemberAdditionConfirmation"
      :title="t('admin.organizations.members.addMemberWarningTitle')"
      :message="t('admin.organizations.members.addMemberWarning')"
      :confirm-text="t('admin.organizations.members.confirmAddMember')"
      data-test="organization-add-member-confirmation"
      @confirm="confirmMemberAddition"
      @cancel="cancelMemberAdditionConfirmation"
    />

    <ConfirmDialog
      :show="pendingReconciliationResolution !== null"
      :title="t('admin.organizations.reconciliation.confirmTitle')"
      :message="t('admin.organizations.reconciliation.confirmMessage')"
      :confirm-text="t('admin.organizations.reconciliation.resolve')"
      data-test="organization-reconciliation-confirmation"
      @confirm="resolvePendingReconciliation"
      @cancel="pendingReconciliationResolution = null"
    />

    <ConfirmDialog
      :show="removeDialogOpen"
      :title="t('admin.organizations.members.removeMemberWarningTitle')"
      :message="memberRemovalMessage"
      :confirm-text="t('admin.organizations.members.confirmRemoveMember')"
      danger
      data-test="organization-remove-member-confirmation"
      @confirm="confirmMemberRemoval"
      @cancel="closeMemberRemoval"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import type {
  Organization,
  OrganizationAllocationPreview,
  OrganizationAllocationResult,
	OrganizationArchivePreview,
  OrganizationBalanceSummary,
  OrganizationLedgerEntry,
  OrganizationMember,
	OrganizationPendingReconciliation,
  OrganizationRole,
  OrganizationStatus
} from '@/types'
import type { AdminOrganizationAllocationOperation } from '@/api/admin/organizations'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()

const organizations = ref<Organization[]>([])
const loading = ref(false)
const saving = ref(false)
const searchQuery = ref('')
const filters = reactive({ status: '' as OrganizationStatus | '' })
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const showDetailDialog = ref(false)
const detailLoading = ref(false)
const selectedOrganization = ref<Organization | null>(null)
const lifecycleAction = ref<'disable' | 'reactivate' | 'archive' | null>(null)
const lifecycleSubmitting = ref(false)
const archivePreview = ref<OrganizationArchivePreview | null>(null)
let detailRequestGeneration = 0
let archivePreviewGeneration = 0
const balanceLoading = ref(false)
const balanceSummary = ref<OrganizationBalanceSummary | null>(null)
const ledgerLoading = ref(false)
const ledgerEntries = ref<OrganizationLedgerEntry[]>([])
const pendingReconciliations = ref<OrganizationPendingReconciliation[]>([])
const pendingReconciliationPagination = reactive({ page: 1, page_size: 20, total: 0, pages: 0 })
const pendingReconciliationDetail = ref<OrganizationPendingReconciliation | null>(null)
const reconciliationLoading = ref(false)
const reconciliationResolving = ref(false)
const pendingReconciliationResolution = ref<OrganizationPendingReconciliation | null>(null)
const reconciliationAttempt = ref({ executionID: '', idempotencyKey: '' })
let pendingReconciliationDetailRequest = 0
const ledgerPagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})
const showCreditDialog = ref(false)
const creditSubmitting = ref(false)
const creditForm = reactive({
  amount: '',
  publicReason: '',
  internalReason: ''
})
const creditAttempt = reactive({ idempotencyKey: '', payloadFingerprint: '' })
const allocationMode = ref<AdminOrganizationAllocationOperation>('custom')
const allocationAmounts = ref<Record<string, string>>({})
const allocationPreviewLoading = ref(false)
const allocationExecuting = ref(false)
const allocationPreview = ref<OrganizationAllocationPreview | null>(null)
const allocationPreviewOperation = ref<AdminOrganizationAllocationOperation | null>(null)
const allocationAttempt = ref({ idempotencyKey: '', fingerprint: '' })
const lastAllocationResult = ref<OrganizationAllocationResult | null>(null)
const members = ref<OrganizationMember[]>([])
const membersLoading = ref(false)
const memberPagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})
const memberSaving = ref(false)
const memberRoleSavingUserId = ref<number | null>(null)
const showAddMemberDialog = ref(false)
const showMemberAdditionConfirmation = ref(false)
const memberForm = reactive({ userId: '', organizationRole: 'organization_member' as OrganizationRole })
const removeDialogOpen = ref(false)
const removeSubmitting = ref(false)
const pendingRemovalMember = ref<OrganizationMember | null>(null)
const memberRemovalAttempt = reactive({ userId: 0, idempotencyKey: '' })
const showFormDialog = ref(false)
const editingOrganization = ref<Organization | null>(null)
const form = reactive({ name: '', code: '', description: '' })

const isEditing = computed(() => editingOrganization.value !== null)
const membersWritable = computed(() => {
  const status = selectedOrganization.value?.status
  return status === 'active' || status === 'disabled'
})
const fundingWritable = computed(() => {
  const status = selectedOrganization.value?.status
  return status === 'active' || status === 'disabled'
})
const allocationWritable = computed(() => selectedOrganization.value?.status === 'active')
const allocationActionsEnabled = computed(() => {
  const status = selectedOrganization.value?.status
  return status === 'active' || status === 'disabled'
})
const activeAllocationOperation = computed<AdminOrganizationAllocationOperation>(() => {
  return allocationWritable.value && allocationMode.value === 'custom' ? 'custom' : 'reclaim'
})
const allocationEligibleMembers = computed(() => {
  return members.value.filter((member) => member.allocation.status === 'active')
})
const allocationPreviewTitle = computed(() => {
  if (!allocationPreviewOperation.value) return t('admin.organizations.allocations.previewTitle')
  return t(`admin.organizations.allocations.${allocationPreviewOperation.value}PreviewTitle`)
})
const memberRemovalMessage = computed(() => {
  const member = pendingRemovalMember.value
  if (!member) return ''
  return t('admin.organizations.members.removeMemberWarning', {
    member: member.username || member.email || `#${member.user_id}`,
    amount: member.allocation.available
  })
})
const lifecycleConfirmationTitle = computed(() => {
  if (lifecycleAction.value === 'disable') return t('admin.organizations.lifecycle.disableTitle')
  if (lifecycleAction.value === 'reactivate') return t('admin.organizations.lifecycle.reactivateTitle')
  return t('admin.organizations.lifecycle.archiveTitle')
})
const lifecycleConfirmationText = computed(() => {
  if (lifecycleAction.value === 'disable') return t('admin.organizations.lifecycle.disable')
  if (lifecycleAction.value === 'reactivate') return t('admin.organizations.lifecycle.reactivate')
  return t('admin.organizations.lifecycle.archive')
})
const lifecycleConfirmationMessage = computed(() => {
  if (lifecycleAction.value === 'disable') return t('admin.organizations.lifecycle.disableConfirmation')
  if (lifecycleAction.value === 'reactivate') return t('admin.organizations.lifecycle.reactivateConfirmation')
  return t('admin.organizations.lifecycle.archiveConfirmation', { amount: archivePreview.value?.unallocated || '0.00000000' })
})
const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.organizations.columns.name'), sortable: true },
  { key: 'code', label: t('admin.organizations.columns.code'), sortable: true },
  { key: 'description', label: t('admin.organizations.columns.description') },
  { key: 'status', label: t('admin.organizations.columns.status'), sortable: true },
  { key: 'created_at', label: t('admin.organizations.columns.createdAt'), sortable: true },
  { key: 'updated_at', label: t('admin.organizations.columns.updatedAt') },
  { key: 'actions', label: t('admin.organizations.columns.actions') }
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('admin.organizations.allStatus') },
  { value: 'active', label: t('admin.organizations.status.active') },
  { value: 'disabled', label: t('admin.organizations.status.disabled') },
  { value: 'archived', label: t('admin.organizations.status.archived') }
])

const organizationRoleOptions = computed(() => [
  { value: 'organization_member', label: t('admin.organizations.members.roles.organization_member') },
  { value: 'organization_admin', label: t('admin.organizations.members.roles.organization_admin') }
])

function statusLabel(status: OrganizationStatus): string {
  return t(`admin.organizations.status.${status}`)
}

function statusBadgeClass(status: OrganizationStatus): string {
  if (status === 'active') return 'badge-success'
  if (status === 'disabled') return 'badge-warning'
  return 'badge-gray'
}

function organizationRoleLabel(role: OrganizationRole): string {
  return t(`admin.organizations.members.roles.${role}`)
}

function memberStatusLabel(status: OrganizationMember['status']): string {
  return status === 'active' ? t('common.active') : t('admin.users.disabled')
}

function reconciliationAttemptStatusLabel(status: string): string {
  return t(`admin.organizations.reconciliation.attemptStatuses.${status}`)
}

function reconciliationEvidenceSourceLabel(source: string): string {
  return t(`admin.organizations.reconciliation.evidenceSources.${source}`)
}

function reconciliationOutcomeLabel(outcome: string): string {
  return t(`admin.organizations.reconciliation.outcomes.${outcome}`)
}

let listController: AbortController | null = null
let searchDebounceTimer: number | null = null

async function loadOrganizations() {
  listController?.abort()
  const requestController = new AbortController()
  listController = requestController

  loading.value = true
  try {
    const response = await adminAPI.organizations.list(
      pagination.page,
      pagination.page_size,
      {
        status: filters.status || undefined,
        search: searchQuery.value.trim() || undefined,
        sort_by: sortState.sort_by,
        sort_order: sortState.sort_order
      },
      { signal: requestController.signal }
    )
    if (requestController.signal.aborted || listController !== requestController) return

    organizations.value = response.items
    pagination.total = response.total
    pagination.page = response.page
    pagination.page_size = response.page_size
    pagination.pages = response.pages
  } catch (error: unknown) {
    const requestError = error as { name?: string; code?: string; message?: string; response?: { data?: { detail?: string } } }
    if (
      requestController.signal.aborted ||
      listController !== requestController ||
      requestError.name === 'AbortError' ||
      requestError.code === 'ERR_CANCELED'
    ) {
      return
    }
    appStore.showError(requestError.response?.data?.detail || requestError.message || t('admin.organizations.failedToLoad'))
  } finally {
    if (listController === requestController) {
      loading.value = false
      listController = null
    }
  }
}

function handleSearch() {
  if (searchDebounceTimer !== null) window.clearTimeout(searchDebounceTimer)
  searchDebounceTimer = window.setTimeout(() => {
    pagination.page = 1
    void loadOrganizations()
  }, 300)
}

function handleStatusChange() {
  pagination.page = 1
  void loadOrganizations()
}

function handleSort(key: string, order: 'asc' | 'desc') {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  void loadOrganizations()
}

function handlePageChange(page: number) {
  pagination.page = Math.max(1, Math.min(page, pagination.pages || 1))
  void loadOrganizations()
}

function handlePageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  void loadOrganizations()
}

function resetForm() {
  form.name = ''
  form.code = ''
  form.description = ''
}

function openCreateDialog() {
  editingOrganization.value = null
  resetForm()
  showFormDialog.value = true
}

function openEditDialog(organization: Organization) {
  editingOrganization.value = organization
  form.name = organization.name
  form.code = organization.code
  form.description = organization.description || ''
  showFormDialog.value = true
}

function closeFormDialog() {
  showFormDialog.value = false
  editingOrganization.value = null
  resetForm()
}

async function openDetailDialog(organization: Organization) {
  const requestGeneration = ++detailRequestGeneration
  archivePreviewGeneration += 1
  pendingReconciliationDetailRequest += 1
  selectedOrganization.value = organization
	archivePreview.value = null
	lifecycleAction.value = null
  resetAllocationState()
  balanceSummary.value = null
  balanceLoading.value = true
  ledgerEntries.value = []
  pendingReconciliations.value = []
  pendingReconciliationDetail.value = null
  pendingReconciliationPagination.page = 1
  pendingReconciliationPagination.total = 0
  pendingReconciliationPagination.pages = 0
  ledgerPagination.page = 1
  ledgerPagination.total = 0
  ledgerPagination.pages = 0
  ledgerLoading.value = true
  members.value = []
  memberPagination.page = 1
  memberPagination.total = 0
  memberPagination.pages = 0
  membersLoading.value = true
  showDetailDialog.value = true
  detailLoading.value = true
  try {
    const loadedOrganization = await adminAPI.organizations.getById(organization.id)
    if (
      requestGeneration !== detailRequestGeneration ||
      !showDetailDialog.value ||
      selectedOrganization.value?.id !== organization.id
    ) {
      return
    }
    selectedOrganization.value = loadedOrganization
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    if (
      requestGeneration === detailRequestGeneration &&
      showDetailDialog.value &&
      selectedOrganization.value?.id === organization.id
    ) {
      appStore.showError(requestError.response?.data?.detail || requestError.message || t('admin.organizations.failedToLoadDetail'))
    }
  } finally {
    if (requestGeneration === detailRequestGeneration && selectedOrganization.value?.id === organization.id) {
      detailLoading.value = false
    }
  }
	if (
		requestGeneration !== detailRequestGeneration ||
		!showDetailDialog.value ||
		selectedOrganization.value?.id !== organization.id
	) {
		return
	}

	if (
		requestGeneration === detailRequestGeneration &&
		showDetailDialog.value &&
		selectedOrganization.value?.id === organization.id
	) {
	await Promise.all([loadMembers(organization.id), reloadFunding(organization.id), loadPendingReconciliations(organization.id)])
  } else {
    balanceLoading.value = false
    ledgerLoading.value = false
    membersLoading.value = false
  }
}

function closeDetailDialog() {
  detailRequestGeneration += 1
  archivePreviewGeneration += 1
  pendingReconciliationDetailRequest += 1
  showDetailDialog.value = false
  selectedOrganization.value = null
	archivePreview.value = null
	lifecycleAction.value = null
  detailLoading.value = false
  balanceSummary.value = null
  balanceLoading.value = false
  ledgerEntries.value = []
  pendingReconciliations.value = []
  pendingReconciliationDetail.value = null
  pendingReconciliationResolution.value = null
  pendingReconciliationPagination.page = 1
  pendingReconciliationPagination.total = 0
  pendingReconciliationPagination.pages = 0
  ledgerLoading.value = false
  ledgerPagination.page = 1
  ledgerPagination.total = 0
  ledgerPagination.pages = 0
  members.value = []
  membersLoading.value = false
  memberPagination.page = 1
  memberPagination.total = 0
  memberPagination.pages = 0
  resetAllocationState()
  closeAddMemberDialog()
  closeCreditDialog()
  showMemberAdditionConfirmation.value = false
  resetMemberRemoval()
}

async function requestLifecycleAction(action: 'disable' | 'reactivate' | 'archive') {
  const organization = selectedOrganization.value
  if (!organization || lifecycleSubmitting.value) return
  if (action === 'archive') {
    const organizationID = organization.id
    const requestGeneration = ++archivePreviewGeneration
    archivePreview.value = null
    try {
      const preview = await adminAPI.organizations.getArchivePreview(organizationID)
      if (
        requestGeneration !== archivePreviewGeneration ||
        !showDetailDialog.value ||
        selectedOrganization.value?.id !== organizationID
      ) {
        return
      }
      if (preview.organization_id !== organizationID) {
        appStore.showError(t('admin.organizations.lifecycle.failedToLoadPreview'))
        return
      }
      archivePreview.value = preview
    } catch (error: unknown) {
      const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
      if (
        requestGeneration === archivePreviewGeneration &&
        showDetailDialog.value &&
        selectedOrganization.value?.id === organizationID
      ) {
        appStore.showError(requestError.response?.data?.detail || requestError.message || t('admin.organizations.lifecycle.failedToLoadPreview'))
      }
      return
    }
  } else {
    archivePreviewGeneration += 1
    archivePreview.value = null
  }
  lifecycleAction.value = action
}

function cancelLifecycleAction() {
  if (!lifecycleSubmitting.value) lifecycleAction.value = null
}

async function refreshLifecycleState(organizationID: number) {
  const [organization] = await Promise.all([
    adminAPI.organizations.getById(organizationID),
    loadOrganizations(),
    reloadFunding(organizationID),
    loadMembers(organizationID),
    loadPendingReconciliations(organizationID)
  ])
  if (selectedOrganization.value?.id === organizationID) selectedOrganization.value = organization
}

async function confirmLifecycleAction() {
  const organization = selectedOrganization.value
  const action = lifecycleAction.value
  if (!organization || !action || lifecycleSubmitting.value) return
  if (
    action === 'archive' &&
    (!archivePreview.value || archivePreview.value.organization_id !== organization.id)
  ) {
    lifecycleAction.value = null
    archivePreview.value = null
    appStore.showError(t('admin.organizations.lifecycle.failedToLoadPreview'))
    return
  }
  lifecycleSubmitting.value = true
  try {
    let updated: Organization
    if (action === 'disable') {
      updated = await adminAPI.organizations.disable(organization.id)
    } else if (action === 'reactivate') {
      updated = await adminAPI.organizations.reactivate(organization.id)
    } else {
      updated = await adminAPI.organizations.archive(organization.id, {
        balance_version: archivePreview.value!.balance_version,
        updated_at: archivePreview.value!.updated_at,
        confirm_archive: true
      })
    }
    if (selectedOrganization.value?.id === organization.id) selectedOrganization.value = updated
    lifecycleAction.value = null
    archivePreview.value = null
    await refreshLifecycleState(organization.id)
    appStore.showSuccess(t(`admin.organizations.lifecycle.${action}Succeeded`))
  } catch (error: unknown) {
    const status = errorStatus(error)
    const code = errorCode(error).toUpperCase()
    if (status === 403) {
      lifecycleAction.value = null
      closeDetailDialog()
      await loadOrganizations()
    } else if (status === 409) {
      lifecycleAction.value = null
      if (action === 'archive') {
        await requestLifecycleAction('archive')
      } else {
        await refreshLifecycleState(organization.id)
      }
    }
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    appStore.showError(requestError.response?.data?.detail || requestError.message || (code.includes('VERSION') ? t('admin.organizations.lifecycle.versionConflict') : t('admin.organizations.lifecycle.failed')))
  } finally {
    lifecycleSubmitting.value = false
  }
}

async function loadPendingReconciliations(organizationId: number) {
	reconciliationLoading.value = true
	try {
    const response = await adminAPI.organizations.listPendingReconciliations(
      organizationId,
      pendingReconciliationPagination.page,
      pendingReconciliationPagination.page_size
    )
    if (selectedOrganization.value?.id === organizationId) {
      pendingReconciliations.value = response.items
      pendingReconciliationPagination.total = response.total
      pendingReconciliationPagination.page = response.page
      pendingReconciliationPagination.page_size = response.page_size
      pendingReconciliationPagination.pages = response.pages
    }
	} catch (error: unknown) {
		const requestError = error as { message?: string; response?: { status?: number; data?: { detail?: string } } }
		appStore.showError(requestError.response?.data?.detail || requestError.message || t('admin.organizations.reconciliation.failedToLoad'))
	} finally {
		if (selectedOrganization.value?.id === organizationId) reconciliationLoading.value = false
	}
}

function handlePendingReconciliationPageChange(page: number) {
  const organization = selectedOrganization.value
  if (!organization) return
  pendingReconciliationPagination.page = Math.max(1, Math.min(page, pendingReconciliationPagination.pages || 1))
  void loadPendingReconciliations(organization.id)
}

function handlePendingReconciliationPageSizeChange(pageSize: number) {
  const organization = selectedOrganization.value
  if (!organization) return
  pendingReconciliationPagination.page_size = pageSize
  pendingReconciliationPagination.page = 1
  void loadPendingReconciliations(organization.id)
}

async function loadPendingReconciliationDetail(executionID: string) {
  const organization = selectedOrganization.value
  if (!organization) return
  const requestID = ++pendingReconciliationDetailRequest
  try {
    const detail = await adminAPI.organizations.getPendingReconciliation(organization.id, executionID)
    if (requestID === pendingReconciliationDetailRequest && selectedOrganization.value?.id === organization.id) {
      pendingReconciliationDetail.value = detail
    }
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    if (requestID === pendingReconciliationDetailRequest && selectedOrganization.value?.id === organization.id) {
      appStore.showError(requestError.response?.data?.detail || requestError.message || t('admin.organizations.reconciliation.failedToLoad'))
    }
  }
}

function requestPendingReconciliationResolution(item: OrganizationPendingReconciliation) {
	if (!item.allowed_resolution || item.conflict) return
	if (reconciliationAttempt.value.executionID !== item.execution_id) {
		reconciliationAttempt.value = { executionID: item.execution_id, idempotencyKey: `organization-reconciliation-${globalThis.crypto.randomUUID?.() || Date.now()}` }
	}
	pendingReconciliationResolution.value = item
}

async function resolvePendingReconciliation() {
  const organization = selectedOrganization.value
  const item = pendingReconciliationResolution.value
  if (!organization || !item || !reconciliationAttempt.value.idempotencyKey || reconciliationResolving.value) return
  reconciliationResolving.value = true
  try {
    await adminAPI.organizations.resolvePendingReconciliation(
      organization.id,
      item.execution_id,
      reconciliationAttempt.value.idempotencyKey
    )
    pendingReconciliationResolution.value = null
    pendingReconciliationDetail.value = null
    await loadPendingReconciliations(organization.id)
    appStore.showSuccess(t('admin.organizations.reconciliation.resolved'))
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { status?: number; data?: { detail?: string } } }
    const status = requestError.response?.status
    if (status === 403 || status === 409) {
      pendingReconciliationResolution.value = null
      pendingReconciliationDetail.value = null
      await loadPendingReconciliations(organization.id)
      if (status === 409 && pendingReconciliations.value.some((entry) => entry.execution_id === item.execution_id)) {
        await loadPendingReconciliationDetail(item.execution_id)
      }
    }
    appStore.showError(requestError.response?.data?.detail || requestError.message || t('admin.organizations.reconciliation.failedToResolve'))
  } finally {
    reconciliationResolving.value = false
  }
}

async function loadBalance(organizationId: number) {
  balanceLoading.value = true
  try {
    const summary = await adminAPI.organizations.getBalance(organizationId)
    if (selectedOrganization.value?.id === organizationId) {
      balanceSummary.value = summary
    }
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    appStore.showError(
      requestError.response?.data?.detail || requestError.message || t('admin.organizations.funding.failedToLoadBalance')
    )
  } finally {
    if (selectedOrganization.value?.id === organizationId) {
      balanceLoading.value = false
    }
  }
}

async function loadLedger(organizationId: number) {
  ledgerLoading.value = true
  try {
    const response = await adminAPI.organizations.listLedger(
      organizationId,
      ledgerPagination.page,
      ledgerPagination.page_size
    )
    if (selectedOrganization.value?.id === organizationId) {
      ledgerEntries.value = response.items
      ledgerPagination.total = response.total
      ledgerPagination.page = response.page
      ledgerPagination.page_size = response.page_size
      ledgerPagination.pages = response.pages
    }
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    appStore.showError(
      requestError.response?.data?.detail || requestError.message || t('admin.organizations.funding.failedToLoadLedger')
    )
  } finally {
    if (selectedOrganization.value?.id === organizationId) {
      ledgerLoading.value = false
    }
  }
}

async function reloadFunding(organizationId: number = selectedOrganization.value?.id || 0) {
  if (organizationId <= 0) return
  await Promise.all([loadBalance(organizationId), loadLedger(organizationId)])
}

function handleLedgerPageChange(page: number) {
  const organization = selectedOrganization.value
  if (!organization) return
  ledgerPagination.page = Math.max(1, Math.min(page, ledgerPagination.pages || 1))
  void loadLedger(organization.id)
}

function handleLedgerPageSizeChange(pageSize: number) {
  const organization = selectedOrganization.value
  if (!organization) return
  ledgerPagination.page_size = pageSize
  ledgerPagination.page = 1
  void loadLedger(organization.id)
}

function resetCreditForm() {
  creditForm.amount = ''
  creditForm.publicReason = ''
  creditForm.internalReason = ''
  creditAttempt.idempotencyKey = ''
  creditAttempt.payloadFingerprint = ''
}

function openCreditDialog() {
  if (!fundingWritable.value) return
  resetCreditForm()
  showCreditDialog.value = true
}

function closeCreditDialog() {
  if (creditSubmitting.value) return
  showCreditDialog.value = false
  resetCreditForm()
}

function createCreditIdempotencyKey(): string {
  if (globalThis.crypto?.randomUUID) {
    return `organization-credit-${globalThis.crypto.randomUUID()}`
  }
  return `organization-credit-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function isPositiveOrganizationMoney(value: string): boolean {
  if (!/^(?:[0-9]+(?:\.[0-9]{1,8})?|\.[0-9]{1,8})$/.test(value)) return false
  return value.replace('.', '').split('').some((digit) => digit !== '0')
}

async function submitCredit() {
  const organization = selectedOrganization.value
  const amount = creditForm.amount.trim()
  const publicReason = creditForm.publicReason.trim()
  const internalReason = creditForm.internalReason.trim()
  if (!organization || !fundingWritable.value || creditSubmitting.value) return
  if (!isPositiveOrganizationMoney(amount)) {
    appStore.showError(t('admin.organizations.funding.invalidAmount'))
    return
  }
  if (!internalReason) {
    appStore.showError(t('admin.organizations.funding.internalReasonRequired'))
    return
  }

  const request = {
    amount,
    ...(publicReason ? { public_reason: publicReason } : {}),
    internal_reason: internalReason
  }
  const payloadFingerprint = JSON.stringify(request)
  if (!creditAttempt.idempotencyKey || creditAttempt.payloadFingerprint !== payloadFingerprint) {
    creditAttempt.idempotencyKey = createCreditIdempotencyKey()
    creditAttempt.payloadFingerprint = payloadFingerprint
  }

  creditSubmitting.value = true
  try {
    await adminAPI.organizations.credit(organization.id, request, creditAttempt.idempotencyKey)
    showCreditDialog.value = false
    resetCreditForm()
    appStore.showSuccess(t('admin.organizations.funding.creditSucceeded'))
    ledgerPagination.page = 1
    await reloadFunding(organization.id)
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    appStore.showError(
      requestError.response?.data?.detail || requestError.message || t('admin.organizations.funding.failedToCredit')
    )
  } finally {
    creditSubmitting.value = false
  }
}

function resetAllocationPreview() {
  allocationPreview.value = null
  allocationPreviewOperation.value = null
  allocationAttempt.value.idempotencyKey = ''
  allocationAttempt.value.fingerprint = ''
}

function resetAllocationState() {
  allocationMode.value = 'custom'
  allocationAmounts.value = {}
  lastAllocationResult.value = null
  resetAllocationPreview()
}

function createAllocationIdempotencyKey(): string {
  if (globalThis.crypto?.randomUUID) {
    return `organization-allocation-${globalThis.crypto.randomUUID()}`
  }
  return `organization-allocation-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function canonicalAllocationAmount(value: string): string | null {
  const match = value.trim().match(/^(\d+)(?:\.(\d{1,8}))?$/)
  if (!match) return null

  const whole = match[1].replace(/^0+(?=\d)/, '')
  if (whole.length > 12) return null

  const result = `${whole}.${(match[2] || '').padEnd(8, '0')}`
  return /^0\.0{8}$/.test(result) ? null : result
}

function buildAllocationTargets(): { targets: { user_id: number; amount: string }[]; invalid: boolean } {
  const targets: { user_id: number; amount: string }[] = []
  let invalid = false

  for (const member of allocationEligibleMembers.value) {
    const raw = allocationAmounts.value[String(member.user_id)]?.trim() || ''
    if (!raw) continue

    const amount = canonicalAllocationAmount(raw)
    if (!amount) {
      invalid = true
      continue
    }
    targets.push({ user_id: member.user_id, amount })
  }

  return { targets, invalid }
}

function allocationPreviewFingerprint(preview: OrganizationAllocationPreview, operation: AdminOrganizationAllocationOperation): string {
  return `${operation}:${preview.canonical_payload_hash}:${preview.preview_token}`
}

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

function isStaleAllocationConflict(error: unknown): boolean {
  if (errorStatus(error) !== 409) return false
  const typedError = error as {
    code?: string
    reason?: string
    response?: { data?: { code?: string; reason?: string } }
  }
  const code = String(
    typedError.reason || typedError.response?.data?.reason || typedError.code || typedError.response?.data?.code || ''
  ).toUpperCase()
  return !code || code.includes('STALE') || code.includes('PREVIEW') || code.includes('VERSION') || code.includes('SNAPSHOT')
}

function allocationErrorMessage(error: unknown, fallback: string): string {
  const typedError = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
  return typedError.response?.data?.detail || typedError.response?.data?.message || typedError.message || fallback
}

async function requestAllocationPreview() {
  const organization = selectedOrganization.value
  const operation = activeAllocationOperation.value
  if (!organization || !allocationActionsEnabled.value || allocationPreviewLoading.value) return

  const { targets, invalid } = buildAllocationTargets()
  if (invalid) {
    appStore.showError(t('admin.organizations.allocations.invalidAmount'))
    return
  }
  if (targets.length === 0) {
    appStore.showError(t('admin.organizations.allocations.targetRequired'))
    return
  }

  allocationPreviewLoading.value = true
  try {
    const nextPreview = await adminAPI.organizations.previewAllocation(organization.id, operation, { targets })
    allocationPreview.value = nextPreview
    allocationPreviewOperation.value = operation
    lastAllocationResult.value = null
    const fingerprint = allocationPreviewFingerprint(nextPreview, operation)
    if (allocationAttempt.value.fingerprint !== fingerprint) {
      allocationAttempt.value.idempotencyKey = createAllocationIdempotencyKey()
      allocationAttempt.value.fingerprint = fingerprint
    }
  } catch (error: unknown) {
    appStore.showError(allocationErrorMessage(error, t('admin.organizations.allocations.previewFailed')))
  } finally {
    allocationPreviewLoading.value = false
  }
}

function closeAllocationPreview() {
  if (!allocationExecuting.value) resetAllocationPreview()
}

async function executeAllocationPreview() {
  const organization = selectedOrganization.value
  const preview = allocationPreview.value
  const operation = allocationPreviewOperation.value
  if (!organization || !preview || !operation || allocationExecuting.value) return

  const fingerprint = allocationPreviewFingerprint(preview, operation)
  if (allocationAttempt.value.fingerprint !== fingerprint || !allocationAttempt.value.idempotencyKey) {
    allocationAttempt.value.idempotencyKey = createAllocationIdempotencyKey()
    allocationAttempt.value.fingerprint = fingerprint
  }

  allocationExecuting.value = true
  try {
    const result = await adminAPI.organizations.executeAllocation(
      organization.id,
      operation,
      { preview_token: preview.preview_token, confirmation: preview.confirmation },
      allocationAttempt.value.idempotencyKey
    )
    lastAllocationResult.value = result
    allocationAmounts.value = {}
    resetAllocationPreview()
    appStore.showSuccess(t('admin.organizations.allocations.executed'))
    ledgerPagination.page = 1
    await Promise.all([reloadFunding(organization.id), loadMembers(organization.id)])
  } catch (error: unknown) {
    if (isStaleAllocationConflict(error)) {
      resetAllocationPreview()
      await Promise.all([reloadFunding(organization.id), loadMembers(organization.id)])
      appStore.showError(t('admin.organizations.allocations.stalePreview'))
    } else {
      appStore.showError(allocationErrorMessage(error, t('admin.organizations.allocations.executeFailed')))
    }
  } finally {
    allocationExecuting.value = false
  }
}

async function loadMembers(organizationId: number) {
  membersLoading.value = true
  try {
    const response = await adminAPI.organizations.listMembers(
      organizationId,
      memberPagination.page,
      memberPagination.page_size
    )
    if (selectedOrganization.value?.id === organizationId) {
      members.value = response.items
      memberPagination.total = response.total
      memberPagination.page = response.page
      memberPagination.page_size = response.page_size
      memberPagination.pages = response.pages
    }
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    appStore.showError(requestError.response?.data?.detail || requestError.message || t('admin.organizations.members.failedToLoad'))
  } finally {
    if (selectedOrganization.value?.id === organizationId) {
      membersLoading.value = false
    }
  }
}

function handleMemberPageChange(page: number) {
  const organization = selectedOrganization.value
  if (!organization) return
  memberPagination.page = Math.max(1, Math.min(page, memberPagination.pages || 1))
  void loadMembers(organization.id)
}

function handleMemberPageSizeChange(pageSize: number) {
  const organization = selectedOrganization.value
  if (!organization) return
  memberPagination.page_size = pageSize
  memberPagination.page = 1
  void loadMembers(organization.id)
}

function resetMemberForm() {
  memberForm.userId = ''
  memberForm.organizationRole = 'organization_member'
}

function openAddMemberDialog() {
  if (!membersWritable.value) return
  resetMemberForm()
  showAddMemberDialog.value = true
}

function closeAddMemberDialog() {
  showAddMemberDialog.value = false
  resetMemberForm()
}

function memberUserID(): number | null {
  const value = memberForm.userId.trim()
  if (!/^[1-9]\d*$/.test(value)) return null

  const userId = Number(value)
  return Number.isSafeInteger(userId) ? userId : null
}

function requestMemberAddition() {
  if (memberUserID() === null) {
    appStore.showError(t('admin.organizations.members.userIdRequired'))
    return
  }
  if (!membersWritable.value) return

  showAddMemberDialog.value = false
  showMemberAdditionConfirmation.value = true
}

function cancelMemberAdditionConfirmation() {
  showMemberAdditionConfirmation.value = false
  if (membersWritable.value) showAddMemberDialog.value = true
}

async function confirmMemberAddition() {
  const organization = selectedOrganization.value
  const userId = memberUserID()
  if (!organization || !membersWritable.value || userId === null || memberSaving.value) {
    return
  }

  memberSaving.value = true
  try {
    await adminAPI.organizations.addMember(organization.id, {
      user_id: userId,
      organization_role: memberForm.organizationRole
    })
    showMemberAdditionConfirmation.value = false
    resetMemberForm()
    appStore.showSuccess(t('admin.organizations.members.memberAdded'))
    memberPagination.page = 1
    await loadMembers(organization.id)
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    appStore.showError(requestError.response?.data?.detail || requestError.message || t('admin.organizations.members.failedToAdd'))
  } finally {
    memberSaving.value = false
  }
}

function isZeroOrganizationMoney(value: string): boolean {
  return value === '0.00000000'
}

function canRemoveMember(member: OrganizationMember): boolean {
  const status = selectedOrganization.value?.status
  if (status === 'active' || status === 'disabled') return true
  return status === 'archived'
    && isZeroOrganizationMoney(member.allocation.available)
    && isZeroOrganizationMoney(member.allocation.reserved)
}

function createMemberRemovalIdempotencyKey(): string {
  if (globalThis.crypto?.randomUUID) {
    return `organization-member-removal-${globalThis.crypto.randomUUID()}`
  }
  return `organization-member-removal-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function resetMemberRemoval() {
  removeDialogOpen.value = false
  pendingRemovalMember.value = null
  memberRemovalAttempt.userId = 0
  memberRemovalAttempt.idempotencyKey = ''
}

function openMemberRemoval(member: OrganizationMember) {
  if (!canRemoveMember(member)) return
  pendingRemovalMember.value = member
  memberRemovalAttempt.userId = member.user_id
  memberRemovalAttempt.idempotencyKey = createMemberRemovalIdempotencyKey()
  removeDialogOpen.value = true
}

function closeMemberRemoval() {
  if (!removeSubmitting.value) resetMemberRemoval()
}

function refreshPendingRemovalMember() {
  const userId = pendingRemovalMember.value?.user_id
  if (!userId) return
  const refreshedMember = members.value.find((member) => member.user_id === userId)
  if (refreshedMember) {
    pendingRemovalMember.value = refreshedMember
  } else {
    resetMemberRemoval()
  }
}

async function confirmMemberRemoval() {
  const organization = selectedOrganization.value
  const member = pendingRemovalMember.value
  if (!organization || !member || removeSubmitting.value || !canRemoveMember(member)) return
  if (memberRemovalAttempt.userId !== member.user_id || !memberRemovalAttempt.idempotencyKey) {
    memberRemovalAttempt.userId = member.user_id
    memberRemovalAttempt.idempotencyKey = createMemberRemovalIdempotencyKey()
  }

  removeSubmitting.value = true
  try {
    await adminAPI.organizations.removeMember(
      organization.id,
      member.user_id,
      {},
      memberRemovalAttempt.idempotencyKey
    )
    resetMemberRemoval()
    appStore.showSuccess(t('admin.organizations.members.memberRemoved'))
    await Promise.all([loadMembers(organization.id), reloadFunding(organization.id)])
  } catch (error: unknown) {
    const status = errorStatus(error)
    const code = errorCode(error).toUpperCase()
    if (status === 409 && code.includes('ALLOCATION_HAS_RESERVATIONS')) {
      await Promise.all([loadMembers(organization.id), reloadFunding(organization.id)])
      refreshPendingRemovalMember()
      appStore.showError(t('admin.organizations.members.removeReservations'))
    } else if (status === 403) {
      closeDetailDialog()
      await loadOrganizations()
      appStore.showError(t('admin.organizations.members.removeForbidden'))
    } else if (status === 404) {
      resetMemberRemoval()
      await loadMembers(organization.id)
      appStore.showError(t('admin.organizations.members.removeNotFound'))
    } else {
      appStore.showError(allocationErrorMessage(error, t('admin.organizations.members.failedToRemove')))
    }
  } finally {
    removeSubmitting.value = false
  }
}

async function changeMemberRole(member: OrganizationMember, value: unknown) {
  const organization = selectedOrganization.value
  const role = value as OrganizationRole
  if (
    !organization ||
    !membersWritable.value ||
    (role !== 'organization_admin' && role !== 'organization_member') ||
    role === member.organization_role ||
    memberRoleSavingUserId.value !== null
  ) {
    return
  }

  memberRoleSavingUserId.value = member.user_id
  try {
    const updatedMember = await adminAPI.organizations.updateMemberRole(organization.id, member.user_id, role)
    const index = members.value.findIndex((currentMember) => currentMember.user_id === member.user_id)
    if (index >= 0) members.value[index] = updatedMember
    appStore.showSuccess(t('admin.organizations.members.memberRoleUpdated'))
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    appStore.showError(
      requestError.response?.data?.detail || requestError.message || t('admin.organizations.members.failedToUpdateRole')
    )
  } finally {
    memberRoleSavingUserId.value = null
  }
}

function openEditFromDetail() {
  if (!selectedOrganization.value) return
  const organization = selectedOrganization.value
  closeDetailDialog()
  openEditDialog(organization)
}

async function saveOrganization() {
  const name = form.name.trim()
  const code = form.code.trim()
  if (!name || !code) {
    appStore.showError(t('admin.organizations.identityRequired'))
    return
  }

  const payload = {
    name,
    code,
    description: form.description.trim()
  }

  saving.value = true
  try {
    if (editingOrganization.value) {
      await adminAPI.organizations.update(editingOrganization.value.id, payload)
      appStore.showSuccess(t('admin.organizations.organizationUpdated'))
    } else {
      await adminAPI.organizations.create(payload)
      appStore.showSuccess(t('admin.organizations.organizationCreated'))
    }
    closeFormDialog()
    await loadOrganizations()
  } catch (error: unknown) {
    const requestError = error as { message?: string; response?: { data?: { detail?: string } } }
    appStore.showError(
      requestError.response?.data?.detail ||
        requestError.message ||
        (editingOrganization.value
          ? t('admin.organizations.failedToUpdate')
          : t('admin.organizations.failedToCreate'))
    )
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void loadOrganizations()
})

onUnmounted(() => {
  listController?.abort()
  if (searchDebounceTimer !== null) window.clearTimeout(searchDebounceTimer)
})
</script>
