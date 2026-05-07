<template>
  <div class="space-y-4">
    <div class="card p-4">
      <div class="flex flex-wrap items-center gap-3">
        <input
          v-model="probeModel"
          type="text"
          class="input w-full sm:w-56"
          placeholder="输入检测模型"
          data-testid="openai-oauth-detection-model-input"
        />
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="loading || probing || deleting"
          data-testid="openai-oauth-detection-refresh"
          @click="loadAccounts"
        >
          {{ loading ? '刷新中...' : '刷新' }}
        </button>
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="loading || accounts.length === 0"
          data-testid="openai-oauth-detection-select-all-visible"
          @click="selectAllVisible"
        >
          全选当前列表
        </button>
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="selectedIds.length === 0"
          data-testid="openai-oauth-detection-clear-selection"
          @click="clearSelection"
        >
          清空选择
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="selectedIds.length === 0 || loading || probing || deleting"
          data-testid="openai-oauth-detection-probe-selected"
          @click="handleProbeSelected"
        >
          {{ probing ? '检测中...' : '批量检测' }}
        </button>
        <button
          type="button"
          class="btn btn-danger"
          :disabled="selectedIds.length === 0 || loading || probing || deleting"
          data-testid="openai-oauth-detection-delete-selected"
          @click="handleDeleteSelected"
        >
          {{ deleting ? '删除中...' : '删除' }}
        </button>
      </div>
    </div>

    <div class="card p-4">
      <div class="flex flex-wrap items-center gap-4 text-sm text-gray-600 dark:text-dark-300">
        <span data-testid="openai-oauth-detection-visible-count">当前列表 {{ accounts.length }} 个</span>
        <span data-testid="openai-oauth-detection-selected-count">已选 {{ selectedIds.length }} 个账号</span>
      </div>
      <p class="mt-3 text-xs text-gray-500 dark:text-dark-400">
        点击“刷新”会从账号管理重新拉取 `platform=openai` 且 `type=oauth` 的真实账号，并覆盖当前检测列表。
      </p>
    </div>

    <div class="card overflow-x-auto" data-testid="openai-oauth-detection-table-scroll">
      <table class="min-w-[1280px] divide-y divide-gray-200 dark:divide-dark-700">
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">选择</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">名称</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">邮箱</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">ChatGPT 账号 ID</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">账号状态</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">检测结果</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">最近检测</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">错误原因</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
          <tr v-for="account in accounts" :key="account.id">
            <td class="px-4 py-3">
              <input
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                :checked="selectedIdSet.has(account.id)"
                :data-testid="`openai-oauth-detection-select-${account.id}`"
                @change="toggleSelection(account.id, $event)"
              />
            </td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">{{ account.name }}</td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">{{ accountEmail(account) }}</td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">{{ chatgptAccountID(account) }}</td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">
              <span :class="accountStatusBadgeClass(account.status)">{{ account.status }}</span>
            </td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">
              <span :class="detectionResultBadgeClass(detectionResult(account))">{{ detectionResultLabel(account) }}</span>
            </td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">{{ formatDateTime(detectionCheckedAt(account)) || '-' }}</td>
            <td class="max-w-[360px] px-4 py-3 text-sm text-gray-700 dark:text-gray-200">
              <div class="flex items-center gap-2">
                <span class="min-w-0 flex-1 truncate whitespace-nowrap">
                  {{ reasonPreview(detectionReason(account) || account.error_message || undefined) }}
                </span>
                <button
                  v-if="hasReason(detectionReason(account) || account.error_message || undefined)"
                  type="button"
                  class="shrink-0 text-xs text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
                  :data-testid="`openai-oauth-detection-reason-detail-${account.id}`"
                  @click="openReasonDetail(account)"
                >
                  详情
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="!loading && accounts.length === 0">
            <td colspan="8" class="px-4 py-8 text-center text-sm text-gray-500 dark:text-dark-400">暂无 OpenAI OAuth 账号</td>
          </tr>
        </tbody>
      </table>
    </div>

    <BaseDialog
      :show="Boolean(detailAccount)"
      title="错误详情"
      width="wide"
      close-on-click-outside
      @close="closeReasonDetail"
    >
      <div v-if="detailAccount" class="space-y-4">
        <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
          <div class="text-sm font-medium text-gray-900 dark:text-white">
            {{ detailAccount.name }}
          </div>
          <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ accountEmail(detailAccount) }}
          </div>
        </div>
        <div class="rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900">
          <pre class="whitespace-pre-wrap break-all text-xs text-gray-700 dark:text-dark-200">{{ detectionReason(detailAccount) || detailAccount.error_message || '-' }}</pre>
        </div>
      </div>

      <template #footer>
        <div class="flex justify-end">
          <button class="btn btn-secondary" type="button" @click="closeReasonDetail">
            关闭
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Account } from '@/types'
import { formatDateTime } from '@/utils/format'

const appStore = useAppStore()

const loading = ref(false)
const probing = ref(false)
const deleting = ref(false)
const accounts = ref<Account[]>([])
const selectedIds = ref<number[]>([])
const probeModel = ref('gpt-5.4-mini')
const detailAccount = ref<Account | null>(null)

const selectedIdSet = computed(() => new Set(selectedIds.value))

function syncSelection(nextAccounts: Account[]) {
  const visibleIDs = new Set(nextAccounts.map((account) => account.id))
  selectedIds.value = selectedIds.value.filter((id) => visibleIDs.has(id))
}

function accountEmail(account: Account) {
  return String(account.credentials?.email || '-')
}

function chatgptAccountID(account: Account) {
  return String(account.credentials?.chatgpt_account_id || '-')
}

function detectionResult(account: Account) {
  return String(account.extra?.openai_oauth_detection_last_result || '')
}

function detectionResultLabel(account: Account) {
  const value = detectionResult(account)
  if (value === 'healthy') return '正常'
  if (value === 'unauthorized') return '401'
  if (value === 'probe_error') return '探测失败'
  if (value === 'http_error') return 'HTTP异常'
  return '-'
}

function detectionCheckedAt(account: Account) {
  const raw = account.extra?.openai_oauth_detection_last_checked_at
  return typeof raw === 'string' ? raw : ''
}

function detectionReason(account: Account) {
  const raw = account.extra?.openai_oauth_detection_last_reason
  return typeof raw === 'string' ? raw : ''
}

function normalizeReason(reason?: string) {
  return (reason || '').replace(/\s+/g, ' ').trim()
}

function reasonPreview(reason?: string) {
  const normalized = normalizeReason(reason)
  if (!normalized) {
    return '-'
  }
  return normalized.length > 88 ? `${normalized.slice(0, 88)}...` : normalized
}

function hasReason(reason?: string) {
  return normalizeReason(reason).length > 0
}

function openReasonDetail(account: Account) {
  detailAccount.value = account
}

function closeReasonDetail() {
  detailAccount.value = null
}

function detectionResultBadgeClass(value: string) {
  if (value === 'healthy') {
    return 'inline-flex items-center rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  }
  if (value === 'unauthorized') {
    return 'inline-flex items-center rounded-full bg-rose-100 px-2 py-0.5 text-xs font-medium text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'
  }
  if (value === 'probe_error' || value === 'http_error') {
    return 'inline-flex items-center rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  }
  return 'inline-flex items-center rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-300'
}

function accountStatusBadgeClass(status: string) {
  if (status === 'active') {
    return 'inline-flex items-center rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  }
  if (status === 'error') {
    return 'inline-flex items-center rounded-full bg-rose-100 px-2 py-0.5 text-xs font-medium text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'
  }
  return 'inline-flex items-center rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-300'
}

function toggleSelection(accountID: number, event: Event) {
  const checked = (event.target as HTMLInputElement).checked
  selectedIds.value = checked
    ? Array.from(new Set([...selectedIds.value, accountID]))
    : selectedIds.value.filter((id) => id !== accountID)
}

function selectAllVisible() {
  selectedIds.value = accounts.value.map((account) => account.id)
}

function clearSelection() {
  selectedIds.value = []
}

async function loadAccounts() {
  loading.value = true
  try {
    const response = await adminAPI.accounts.list(1, 10000, {
      platform: 'openai',
      type: 'oauth'
    })
    accounts.value = response.items
    syncSelection(response.items)
  } catch (error: any) {
    appStore.showError(error?.message || '加载 OpenAI OAuth 账号失败')
  } finally {
    loading.value = false
  }
}

async function handleProbeSelected() {
  if (selectedIds.value.length === 0) {
    return
  }
  probing.value = true
  try {
    const response = await adminAPI.openaiOAuthDetection.probe({
      account_ids: selectedIds.value,
      model: probeModel.value.trim() || 'gpt-5.4-mini'
    })
    appStore.showSuccess(`检测完成：共 ${response.checked} 个，正常 ${response.healthy} 个，401 ${response.unauthorized} 个`)
    await loadAccounts()
  } catch (error: any) {
    appStore.showError(error?.message || '批量检测 OpenAI OAuth 账号失败')
  } finally {
    probing.value = false
  }
}

async function handleDeleteSelected() {
  if (selectedIds.value.length === 0) {
    return
  }
  if (!window.confirm('确认删除选中的 OpenAI OAuth 账号？该操作将直接删除账号管理中的真实账号。')) {
    return
  }
  deleting.value = true
  try {
    await Promise.all(selectedIds.value.map((id) => adminAPI.accounts.delete(id)))
    clearSelection()
    appStore.showSuccess('删除成功')
    await loadAccounts()
  } catch (error: any) {
    appStore.showError(error?.message || '批量删除 OpenAI OAuth 账号失败')
  } finally {
    deleting.value = false
  }
}

onMounted(async () => {
  await loadAccounts()
})
</script>
