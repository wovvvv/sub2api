<template>
  <div class="space-y-4">
    <div class="card p-4">
      <div class="flex flex-wrap items-center gap-3">
        <select
          v-model="livenessFilter"
          class="input w-full sm:w-48"
          data-testid="codex-detection-liveness-filter"
          @change="loadCandidates"
        >
          <option value="all">全部存活状态</option>
          <option value="alive">可用</option>
          <option value="dead">失效</option>
          <option value="invalid">无效</option>
          <option value="error">检测异常</option>
        </select>
        <select
          v-model="workflowFilter"
          class="input w-full sm:w-48"
          @change="loadCandidates"
        >
          <option value="all">全部流程状态</option>
          <option value="detected">已检测</option>
          <option value="staged">已暂存</option>
          <option value="duplicate">重复账号</option>
          <option value="imported">已导入</option>
        </select>
        <input
          v-model="query"
          type="text"
          class="input w-full sm:flex-1"
          placeholder="搜索文件名、邮箱或账号 ID"
          @keyup.enter="loadCandidates"
        />
        <input
          v-model="scanModel"
          type="text"
          class="input w-full sm:w-56"
          placeholder="输入检测模型"
          data-testid="codex-detection-model-input"
        />
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="loading"
          @click="loadCandidates"
        >
          {{ loading ? '刷新中...' : '刷新' }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="scanning"
          data-testid="codex-detection-scan"
          @click="handleScan"
        >
          {{ scanning ? '检测中...' : '重新检测' }}
        </button>
      </div>
      <div
        v-if="scanStatus"
        class="mt-3 rounded-lg border border-primary-200 bg-primary-50/70 px-3 py-2 text-sm text-primary-800 dark:border-primary-900/40 dark:bg-primary-950/20 dark:text-primary-200"
        data-testid="codex-detection-scan-status"
      >
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <span :class="scanStatusBadgeClass(scanStatus.status)">
                {{ scanStatusLabel(scanStatus.status) }}
              </span>
              <span class="break-words">{{ scanStatus.message }}</span>
            </div>
            <div v-if="scanStatus.updatedAt" class="mt-1 text-xs text-primary-700/80 dark:text-primary-200/80">
              最近更新 {{ formatDateTime(scanStatus.updatedAt) }}
            </div>
          </div>
          <button
            v-if="scanStatus.errorDetail"
            type="button"
            class="shrink-0 text-xs text-primary-700 underline decoration-dotted underline-offset-2 hover:text-primary-900 dark:text-primary-200 dark:hover:text-white"
            data-testid="codex-detection-scan-status-detail"
            @click="openScanStatusDetail"
          >
            详情
          </button>
        </div>
      </div>
    </div>

    <div class="card p-4">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div class="flex flex-wrap items-center gap-4 text-sm text-gray-600 dark:text-dark-300">
          <span data-testid="codex-detection-visible-count">当前筛选结果 {{ candidates.length }} 个</span>
          <span data-testid="codex-detection-selected-count">已选 {{ selectedIds.length }} 个账号</span>
        </div>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loading || candidates.length === 0"
            data-testid="codex-detection-select-all-visible"
            @click="selectAllVisible"
          >
            全选当前筛选结果
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="selectedIds.length === 0"
            data-testid="codex-detection-clear-selection"
            @click="clearSelection"
          >
            清空选择
          </button>
          <button
            type="button"
            class="btn btn-danger"
            :disabled="loading || scanning || importing || clearing || candidates.length === 0"
            data-testid="codex-detection-clear-all"
            @click="handleClearAll"
          >
            {{ clearing ? '清空中...' : '清空账号' }}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="selectedIds.length === 0"
            data-testid="codex-detection-batch-import"
            @click="openBatchImportDialog"
          >
            批量导入
          </button>
        </div>
      </div>
      <p class="mt-3 text-xs text-gray-500 dark:text-dark-400">
        所有状态都可以勾选并发起导入，但只有“可用”账号会真正创建；其余状态会在导入结果里按原因跳过。
      </p>
      <div class="mt-4 space-y-3">
        <div>
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">存活状态统计</div>
          <div class="mt-2 flex flex-wrap gap-2">
            <span
              v-for="item in livenessSummaryItems"
              :key="`liveness-${item.label}`"
              class="inline-flex items-center rounded-full bg-gray-100 px-3 py-1 text-xs text-gray-700 dark:bg-dark-700 dark:text-dark-200"
            >
              {{ item.label }} {{ item.count }}
            </span>
          </div>
        </div>
        <div>
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">流程状态统计</div>
          <div class="mt-2 flex flex-wrap gap-2">
            <span
              v-for="item in workflowSummaryItems"
              :key="`workflow-${item.label}`"
              class="inline-flex items-center rounded-full bg-gray-100 px-3 py-1 text-xs text-gray-700 dark:bg-dark-700 dark:text-dark-200"
            >
              {{ item.label }} {{ item.count }}
            </span>
          </div>
        </div>
      </div>
    </div>

    <div class="card overflow-x-auto" data-testid="codex-detection-table-scroll">
      <table
        class="min-w-[1400px] divide-y divide-gray-200 dark:divide-dark-700"
        data-testid="codex-detection-table"
      >
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">选择</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">文件</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">邮箱</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">账号 ID</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">过期时间</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">状态</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">错误摘要</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">最近检测</th>
            <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-300">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
          <tr v-for="candidate in candidates" :key="candidate.id">
            <td class="px-4 py-3">
              <input
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                :checked="selectedIdSet.has(candidate.id)"
                :data-testid="`codex-detection-select-${candidate.id}`"
                @change="toggleSelection(candidate.id, $event)"
              />
            </td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">{{ candidate.source_filename }}</td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">{{ candidate.email || '-' }}</td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">{{ candidate.account_id || '-' }}</td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">{{ formatDateTime(candidate.expires_at) || '-' }}</td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">
              <div class="flex flex-wrap gap-2">
                <span :class="workflowBadgeClass(candidate.workflow_state)">
                  {{ workflowLabel(candidate.workflow_state) }}
                </span>
                <span :class="livenessBadgeClass(candidate.liveness_status)">
                  {{ livenessLabel(candidate.liveness_status) }}
                </span>
              </div>
            </td>
            <td class="max-w-[340px] px-4 py-3 text-sm text-gray-700 dark:text-gray-200">
              <div class="flex items-center gap-2">
                <span class="min-w-0 flex-1 truncate whitespace-nowrap">
                  {{ reasonPreview(candidate.status_reason) }}
                </span>
                <button
                  v-if="hasReason(candidate.status_reason)"
                  type="button"
                  class="shrink-0 text-xs text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
                  :data-testid="`codex-detection-reason-detail-${candidate.id}`"
                  @click="openReasonDetail(candidate)"
                >
                  详情
                </button>
              </div>
            </td>
            <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-200">{{ formatDateTime(candidate.last_checked_at) || '-' }}</td>
            <td class="px-4 py-3">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="importing"
                :data-testid="`codex-detection-import-${candidate.id}`"
                @click="openSingleImportDialog(candidate.id)"
              >
                导入
              </button>
            </td>
          </tr>
          <tr v-if="!loading && candidates.length === 0">
            <td colspan="9" class="px-4 py-8 text-center text-sm text-gray-500 dark:text-dark-400">暂无检测账号</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="resultSummary" class="card space-y-3 p-4 text-sm text-gray-700 dark:text-gray-200">
      <div class="flex flex-wrap items-center gap-4">
        <span class="font-medium text-gray-900 dark:text-white">上次导入结果</span>
        <span>导入成功 {{ resultSummary.imported }} 个</span>
        <span>跳过 {{ resultSummary.skipped }} 个</span>
      </div>
      <div
        v-if="resultSummary.items.length > 0"
        class="max-h-48 overflow-auto rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800"
      >
        <div
          v-for="item in resultSummary.items"
          :key="`codex-import-result-${item.id}`"
          class="border-b border-gray-200 py-2 last:border-b-0 dark:border-dark-700"
        >
          <div class="text-sm font-medium text-gray-900 dark:text-white">{{ item.label }}</div>
          <div class="mt-1 break-all text-xs text-gray-600 dark:text-dark-300">{{ item.reason }}</div>
        </div>
      </div>
    </div>

    <BaseDialog
      :show="showImportDialog"
      title="导入账号"
      width="wide"
      close-on-click-outside
      @close="closeImportDialog"
    >
      <form id="codex-import-form" class="space-y-5" @submit.prevent="handleImport">
        <div class="rounded-lg bg-gray-50 p-3 text-sm text-gray-600 dark:bg-dark-800 dark:text-dark-300">
          已选择 {{ importCandidateIds.length }} 个账号。只有“可用”状态会真正导入，其余状态会自动跳过。
        </div>

        <div
          v-if="importProgress"
          class="space-y-3 rounded-lg border border-primary-200 bg-primary-50/70 p-4 dark:border-primary-900/40 dark:bg-primary-950/20"
        >
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <div class="text-sm font-medium text-gray-900 dark:text-white">导入进度</div>
              <div class="mt-1 text-xs text-gray-600 dark:text-dark-300">
                已完成 {{ importProgress.completed }} / {{ importProgress.total }}
              </div>
            </div>
            <div class="text-xs text-gray-600 dark:text-dark-300">
              当前分块 {{ importProgress.currentChunk }} / {{ importProgress.chunkCount }}
            </div>
          </div>
          <div class="h-2 overflow-hidden rounded-full bg-white/80 dark:bg-dark-800">
            <div
              class="h-full rounded-full bg-primary-500 transition-all duration-300"
              :style="{ width: `${importProgress.percent}%` }"
            />
          </div>
          <div class="flex flex-wrap gap-4 text-xs text-gray-600 dark:text-dark-300">
            <span>成功 {{ importProgress.imported }}</span>
            <span>跳过 {{ importProgress.failed }}</span>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
          <div class="space-y-3">
            <div>
              <label class="input-label">目标分组</label>
              <div
                class="max-h-56 overflow-auto rounded-lg border border-gray-200 bg-gray-50 p-2 dark:border-dark-600 dark:bg-dark-800"
              >
                <label
                  v-for="group in openAIGroupOptions"
                  :key="group.id"
                  class="flex cursor-pointer items-center justify-between gap-3 rounded px-2 py-2 hover:bg-white dark:hover:bg-dark-700"
                >
                  <div class="flex min-w-0 items-center gap-3">
                    <input
                      type="checkbox"
                      class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                      :checked="selectedGroupIds.includes(group.id)"
                      :data-testid="`codex-import-group-${group.id}`"
                      @change="toggleGroup(group.id, $event)"
                    />
                    <span class="truncate text-sm text-gray-700 dark:text-gray-200">{{ group.name }}</span>
                  </div>
                  <span class="shrink-0 text-xs text-gray-400">#{{ group.id }}</span>
                </label>
                <div
                  v-if="openAIGroupOptions.length === 0"
                  class="px-2 py-3 text-center text-sm text-gray-500 dark:text-dark-400"
                >
                  暂无可用的 OpenAI 分组
                </div>
              </div>
              <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">
                同一个账号可以同时绑定到多个分组。
              </p>
            </div>
          </div>

          <div class="space-y-4">
            <div>
              <label class="input-label">代理</label>
              <select
                v-model="selectedProxyId"
                class="input"
                data-testid="codex-import-proxy-select"
              >
                <option value="">不绑定代理</option>
                <option v-for="proxy in proxies" :key="proxy.id" :value="String(proxy.id)">
                  {{ proxy.name }}
                </option>
              </select>
            </div>

            <div>
              <label class="input-label">备注前缀</label>
              <input
                v-model="notes"
                type="text"
                class="input"
                placeholder="可选，例如：4 月批量导入"
                data-testid="codex-import-notes"
              />
            </div>

            <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/80">
              <div class="flex items-start justify-between gap-4">
                <div>
                  <label class="input-label">模型限制（可选）</label>
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                    开启后，会在导入时自动从账号的 `/v1/models` 获取模型并写入限制。
                  </p>
                </div>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  data-testid="codex-import-models-toggle"
                  @click="importModelsEnabled = !importModelsEnabled"
                >
                  {{ importModelsEnabled ? '已开启' : '未开启' }}
                </button>
              </div>
              <p v-if="importModelsEnabled" class="mt-3 text-xs text-emerald-600 dark:text-emerald-400">
                导入时将自动请求 `/v1/models`，并把返回模型写入账号限制。
              </p>

              <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <label class="input-label">并发数</label>
                  <input
                    v-model.number="importConcurrency"
                    type="number"
                    min="1"
                    class="input"
                    data-testid="codex-import-concurrency"
                    @input="importConcurrency = Math.max(1, importConcurrency || 1)"
                  />
                </div>
                <div>
                  <label class="input-label">负载因子</label>
                  <input
                    v-model.number="importLoadFactor"
                    type="number"
                    min="1"
                    class="input"
                    data-testid="codex-import-load-factor"
                    @input="importLoadFactor = Math.max(1, importLoadFactor || 1)"
                  />
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                    提高负载因子可以提高对账号的调度频率
                  </p>
                </div>
                <div>
                  <label class="input-label">优先级</label>
                  <input
                    v-model.number="importPriority"
                    type="number"
                    min="1"
                    class="input"
                    data-testid="codex-import-priority"
                    @input="importPriority = Math.max(1, importPriority || 1)"
                  />
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                    优先级越小的账号优先使用
                  </p>
                </div>
                <div>
                  <label class="input-label">账号计费倍率</label>
                  <input
                    v-model.number="importRateMultiplier"
                    type="number"
                    min="0"
                    step="0.001"
                    class="input"
                    data-testid="codex-import-rate-multiplier"
                    @input="importRateMultiplier = Math.max(0, Number.isFinite(importRateMultiplier) ? importRateMultiplier : 1)"
                  />
                  <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                    0 表示不计费，仅影响账号计费
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div>
          <label class="input-label">本次处理账号</label>
          <div
            class="max-h-48 overflow-auto rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900"
          >
            <div
              v-for="candidate in importCandidates"
              :key="`codex-import-candidate-${candidate.id}`"
              class="flex items-center justify-between gap-3 border-b border-gray-100 px-3 py-2 last:border-b-0 dark:border-dark-700"
            >
              <div class="min-w-0">
                <div class="truncate text-sm text-gray-900 dark:text-white">
                  {{ candidate.email || candidate.account_id || candidate.source_filename }}
                </div>
                <div class="truncate text-xs text-gray-500 dark:text-dark-400">
                  {{ candidate.source_filename }}
                </div>
              </div>
              <span :class="livenessBadgeClass(candidate.liveness_status)">
                {{ livenessLabel(candidate.liveness_status) }}
              </span>
            </div>
          </div>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" type="button" :disabled="importing" @click="closeImportDialog">
            取消
          </button>
          <button
            class="btn btn-primary"
            type="button"
            :disabled="importing"
            data-testid="codex-import-submit"
            @click="handleImport"
          >
            {{ importing ? '导入中...' : '确认导入' }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="Boolean(detailCandidate)"
      title="错误详情"
      width="wide"
      close-on-click-outside
      @close="closeReasonDetail"
    >
      <div v-if="detailCandidate" class="space-y-4">
        <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
          <div class="text-sm font-medium text-gray-900 dark:text-white">
            {{ detailCandidate.email || detailCandidate.account_id || detailCandidate.source_filename }}
          </div>
          <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ detailCandidate.source_filename }}
          </div>
        </div>
        <div class="rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900">
          <pre class="whitespace-pre-wrap break-all text-xs text-gray-700 dark:text-dark-200">{{ detailCandidate.status_reason }}</pre>
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

    <BaseDialog
      :show="showScanStatusDetail"
      title="扫描失败详情"
      @close="closeScanStatusDetail"
    >
      <div v-if="scanStatus?.errorDetail" class="space-y-4">
        <div class="rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900">
          <pre class="whitespace-pre-wrap break-all text-xs text-gray-700 dark:text-dark-200">{{ scanStatus.errorDetail }}</pre>
        </div>
      </div>

      <template #footer>
        <div class="flex justify-end">
          <button class="btn btn-secondary" type="button" @click="closeScanStatusDetail">
            关闭
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import type {
  AdminGroup,
  CodexRegistrationCandidate,
  CodexRegistrationImportRequest,
  CodexRegistrationListFilters,
  CodexRegistrationLivenessStatus,
  CodexRegistrationScanTaskResponse,
  CodexRegistrationWorkflowState,
  Proxy
} from '@/types'
import type {
  CodexImportResultSummary,
  CodexLivenessFilterValue,
  CodexWorkflowFilterValue
} from './types'

const appStore = useAppStore()

const loading = ref(false)
const scanning = ref(false)
const importing = ref(false)
const clearing = ref(false)
const scanModel = ref('gpt-5.4-mini')
const candidates = ref<CodexRegistrationCandidate[]>([])
const query = ref('')
const livenessFilter = ref<CodexLivenessFilterValue>('all')
const workflowFilter = ref<CodexWorkflowFilterValue>('all')
const selectedIds = ref<number[]>([])
const showImportDialog = ref(false)
const importCandidateIds = ref<number[]>([])
const groups = ref<AdminGroup[]>([])
const proxies = ref<Proxy[]>([])
const selectedGroupIds = ref<number[]>([])
const selectedProxyId = ref('')
const notes = ref('')
const importConcurrency = ref(1)
const importLoadFactor = ref(1)
const importPriority = ref(1)
const importRateMultiplier = ref(1)
const importModelsEnabled = ref(false)
const resultSummary = ref<CodexImportResultSummary | null>(null)
const detailCandidate = ref<CodexRegistrationCandidate | null>(null)
const importProgress = ref<{
  total: number
  completed: number
  imported: number
  failed: number
  currentChunk: number
  chunkCount: number
  percent: number
} | null>(null)
type CodexScanUiStatus = 'queued' | 'running' | 'succeeded' | 'failed'

const scanStatus = ref<{
  status: CodexScanUiStatus
  message: string
  updatedAt: string
  errorDetail?: string
} | null>(null)
const showScanStatusDetail = ref(false)

const codexImportChunkSize = 5
const codexImportTimeoutMs = 2 * 60 * 1000
const codexScanPollIntervalMs = 1000
let scanPollTimer: number | null = null

const selectedIdSet = computed(() => new Set(selectedIds.value))
const importCandidateIdSet = computed(() => new Set(importCandidateIds.value))
const openAIGroupOptions = computed(() => {
  return groups.value.filter((group) => group.platform === 'openai' || typeof group.platform === 'undefined')
})
const importCandidates = computed(() => {
  return candidates.value.filter((candidate) => importCandidateIdSet.value.has(candidate.id))
})
const livenessSummaryItems = computed(() => {
  const counts = {
    alive: 0,
    dead: 0,
    invalid: 0,
    error: 0
  }
  for (const candidate of candidates.value) {
    counts[candidate.liveness_status] += 1
  }
  return [
    { label: '可用', count: counts.alive },
    { label: '失效', count: counts.dead },
    { label: '无效', count: counts.invalid },
    { label: '检测异常', count: counts.error }
  ]
})
const workflowSummaryItems = computed(() => {
  const counts = {
    detected: 0,
    staged: 0,
    duplicate: 0,
    imported: 0
  }
  for (const candidate of candidates.value) {
    counts[candidate.workflow_state] += 1
  }
  return [
    { label: '已检测', count: counts.detected },
    { label: '已暂存', count: counts.staged },
    { label: '重复账号', count: counts.duplicate },
    { label: '已导入', count: counts.imported }
  ]
})

const livenessLabelMap: Record<CodexRegistrationLivenessStatus, string> = {
  alive: '可用',
  dead: '失效',
  invalid: '无效',
  error: '检测异常'
}

const workflowLabelMap: Record<CodexRegistrationWorkflowState, string> = {
  detected: '已检测',
  staged: '已暂存',
  duplicate: '重复账号',
  imported: '已导入'
}

function livenessLabel(status: CodexRegistrationLivenessStatus) {
  return livenessLabelMap[status] || status
}

function workflowLabel(state: CodexRegistrationWorkflowState) {
  return workflowLabelMap[state] || state
}

function livenessBadgeClass(status: CodexRegistrationLivenessStatus) {
  if (status === 'alive') {
    return 'inline-flex items-center rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  }
  if (status === 'dead') {
    return 'inline-flex items-center rounded-full bg-rose-100 px-2 py-0.5 text-xs font-medium text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'
  }
  if (status === 'invalid') {
    return 'inline-flex items-center rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  }
  return 'inline-flex items-center rounded-full bg-slate-200 px-2 py-0.5 text-xs font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-300'
}

function workflowBadgeClass(state: CodexRegistrationWorkflowState) {
  if (state === 'imported') {
    return 'inline-flex items-center rounded-full bg-blue-100 px-2 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
  }
  if (state === 'duplicate') {
    return 'inline-flex items-center rounded-full bg-fuchsia-100 px-2 py-0.5 text-xs font-medium text-fuchsia-700 dark:bg-fuchsia-900/30 dark:text-fuchsia-300'
  }
  if (state === 'staged') {
    return 'inline-flex items-center rounded-full bg-indigo-100 px-2 py-0.5 text-xs font-medium text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300'
  }
  return 'inline-flex items-center rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-300'
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

function openReasonDetail(candidate: CodexRegistrationCandidate) {
  detailCandidate.value = candidate
}

function closeReasonDetail() {
  detailCandidate.value = null
}

function scanStatusLabel(status: CodexScanUiStatus) {
  if (status === 'queued') return '排队中'
  if (status === 'running') return '运行中'
  if (status === 'succeeded') return '已完成'
  return '失败'
}

function scanStatusBadgeClass(status: CodexScanUiStatus) {
  if (status === 'queued') {
    return 'inline-flex items-center rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  }
  if (status === 'running') {
    return 'inline-flex items-center rounded-full bg-sky-100 px-2 py-0.5 text-xs font-medium text-sky-700 dark:bg-sky-900/30 dark:text-sky-300'
  }
  if (status === 'succeeded') {
    return 'inline-flex items-center rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  }
  return 'inline-flex items-center rounded-full bg-rose-100 px-2 py-0.5 text-xs font-medium text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'
}

function setScanStatus(status: CodexScanUiStatus, message: string, errorDetail?: string) {
  scanStatus.value = {
    status,
    message,
    updatedAt: new Date().toISOString(),
    errorDetail
  }
}

function openScanStatusDetail() {
  if (!scanStatus.value?.errorDetail) {
    return
  }
  showScanStatusDetail.value = true
}

function closeScanStatusDetail() {
  showScanStatusDetail.value = false
}

function syncSelectionWithCurrentCandidates(nextCandidates: CodexRegistrationCandidate[]) {
  const visibleIds = new Set(nextCandidates.map((candidate) => candidate.id))
  selectedIds.value = selectedIds.value.filter((id) => visibleIds.has(id))
  importCandidateIds.value = importCandidateIds.value.filter((id) => visibleIds.has(id))
}

function buildImportSummary(importedIds: number[], failed: Record<string, string>): CodexImportResultSummary {
  const items = Object.entries(failed).map(([idRaw, reason]) => {
    const id = Number(idRaw)
    const candidate = candidates.value.find((item) => item.id === id)
    const label = candidate?.email || candidate?.account_id || candidate?.source_filename || `#${id}`

    return {
      id,
      label,
      reason
    }
  })

  return {
    imported: importedIds.length,
    skipped: items.length,
    items
  }
}

function toggleSelection(candidateId: number, event: Event) {
  const checked = (event.target as HTMLInputElement).checked
  selectedIds.value = checked
    ? Array.from(new Set([...selectedIds.value, candidateId]))
    : selectedIds.value.filter((id) => id !== candidateId)
}

function selectAllVisible() {
  selectedIds.value = candidates.value.map((candidate) => candidate.id)
}

function clearSelection() {
  selectedIds.value = []
}

async function handleClearAll() {
  if (candidates.value.length === 0) {
    return
  }
  if (!window.confirm('确认清空下面所有检测到的账号？该操作不可撤销。')) {
    return
  }

  clearing.value = true
  try {
    const response = await adminAPI.codexRegistration.clear()
    selectedIds.value = []
    importCandidateIds.value = []
    detailCandidate.value = null
    showImportDialog.value = false
    appStore.showSuccess(`已清空 ${response.cleared} 个检测账号`)
    await loadCandidates()
  } catch (error: any) {
    appStore.showError(error?.message || '清空检测账号失败')
  } finally {
    clearing.value = false
  }
}

function chunkCandidateIDs(candidateIDs: number[], chunkSize: number) {
  const normalizedChunkSize = Math.max(1, chunkSize)
  const chunks: number[][] = []
  for (let index = 0; index < candidateIDs.length; index += normalizedChunkSize) {
    chunks.push(candidateIDs.slice(index, index + normalizedChunkSize))
  }
  return chunks
}

function openImportDialog(candidateIds: number[]) {
  if (candidateIds.length === 0) {
    appStore.showError('请先选择要导入的账号')
    return
  }
  importCandidateIds.value = [...candidateIds]
  importModelsEnabled.value = false
  showImportDialog.value = true
}

function openBatchImportDialog() {
  openImportDialog(selectedIds.value)
}

function openSingleImportDialog(candidateId: number) {
  selectedIds.value = [candidateId]
  openImportDialog([candidateId])
}

function closeImportDialog() {
  if (importing.value) {
    return
  }
  showImportDialog.value = false
  importProgress.value = null
}

function toggleGroup(groupId: number, event: Event) {
  const checked = (event.target as HTMLInputElement).checked
  selectedGroupIds.value = checked
    ? Array.from(new Set([...selectedGroupIds.value, groupId]))
    : selectedGroupIds.value.filter((id) => id !== groupId)
}

async function loadCandidates() {
  loading.value = true
  try {
    const filters: CodexRegistrationListFilters = {}
    if (livenessFilter.value !== 'all') {
      filters.liveness_status = livenessFilter.value
    }
    if (workflowFilter.value !== 'all') {
      filters.workflow_state = workflowFilter.value
    }
    if (query.value.trim()) {
      filters.q = query.value.trim()
    }
    const response = await adminAPI.codexRegistration.list(filters)
    candidates.value = response.items
    syncSelectionWithCurrentCandidates(response.items)
  } catch (error: any) {
    appStore.showError(error?.message || '加载检测账号失败')
  } finally {
    loading.value = false
  }
}

async function loadOptions() {
  try {
    const [allGroups, allProxies] = await Promise.all([
      adminAPI.groups.getAll(),
      adminAPI.proxies.getAll()
    ])
    groups.value = allGroups
    proxies.value = allProxies
  } catch (error: any) {
    appStore.showError(error?.message || '加载分组或代理失败')
  }
}

function clearScanPollTimer() {
  if (scanPollTimer !== null) {
    clearTimeout(scanPollTimer)
    scanPollTimer = null
  }
}

async function waitForScanTask(taskId: string): Promise<CodexRegistrationScanTaskResponse> {
  while (true) {
    const task = await adminAPI.codexRegistration.getScanTask(taskId)
    if (task.status === 'queued') {
      setScanStatus('queued', '扫描任务已提交，正在排队')
    } else if (task.status === 'running') {
      setScanStatus('running', '检测任务运行中')
    }
    if (task.status === 'succeeded') {
      return task
    }
    if (task.status === 'failed') {
      throw new Error(task.error_message || '检测账号失败')
    }

    await new Promise<void>((resolve) => {
      clearScanPollTimer()
      scanPollTimer = window.setTimeout(() => {
        scanPollTimer = null
        resolve()
      }, codexScanPollIntervalMs)
    })
  }
}

async function handleScan() {
  scanning.value = true
  try {
    clearScanPollTimer()
    showScanStatusDetail.value = false
    const task = await adminAPI.codexRegistration.scan({
      model: scanModel.value.trim() || 'gpt-5.4-mini'
    })
    setScanStatus(task.status === 'queued' ? 'queued' : 'running', task.status === 'queued' ? '扫描任务已提交，正在排队' : '检测任务运行中')
    const response = await waitForScanTask(task.task_id)
    setScanStatus('succeeded', `上次检测刚完成，共处理 ${response.scanned} 个账号`)
    appStore.showSuccess(`检测完成，共处理 ${response.scanned} 个账号`)
    await loadCandidates()
  } catch (error: any) {
    setScanStatus('failed', error?.message ? `检测失败：${error.message}` : '检测失败', error?.message || '')
    appStore.showError(error?.message || '检测账号失败')
  } finally {
    clearScanPollTimer()
    scanning.value = false
  }
}

async function handleImport() {
  if (importCandidateIds.value.length === 0) {
    appStore.showError('请先选择要导入的账号')
    return
  }

  if (selectedGroupIds.value.length === 0) {
    appStore.showError('请至少选择一个目标分组')
    return
  }

  importing.value = true
  try {
    const basePayload: Omit<CodexRegistrationImportRequest, 'candidate_ids'> = {
      group_ids: [...selectedGroupIds.value],
      concurrency: Math.max(1, importConcurrency.value || 1),
      load_factor: Math.max(1, importLoadFactor.value || 1),
      priority: Math.max(1, importPriority.value || 1),
      rate_multiplier: Math.max(0, Number.isFinite(importRateMultiplier.value) ? importRateMultiplier.value : 1)
    }

    if (selectedProxyId.value) {
      basePayload.proxy_id = Number(selectedProxyId.value)
    }

    if (notes.value.trim()) {
      basePayload.notes = notes.value.trim()
    }

    if (importModelsEnabled.value) {
      basePayload.import_models = true
    }

    const candidateChunks = chunkCandidateIDs(importCandidateIds.value, codexImportChunkSize)
    const importedIDs: number[] = []
    const failed: Record<string, string> = {}
    importProgress.value = {
      total: importCandidateIds.value.length,
      completed: 0,
      imported: 0,
      failed: 0,
      currentChunk: candidateChunks.length > 0 ? 1 : 0,
      chunkCount: candidateChunks.length,
      percent: 0
    }

    for (const [chunkIndex, candidateChunk] of candidateChunks.entries()) {
      if (importProgress.value) {
        importProgress.value.currentChunk = chunkIndex + 1
      }
      const response = await adminAPI.codexRegistration.importCandidates(
        {
          ...basePayload,
          candidate_ids: candidateChunk
        },
        {
          timeout: codexImportTimeoutMs
        }
      )
      importedIDs.push(...response.imported_ids)
      Object.assign(failed, response.failed)
      if (importProgress.value) {
        importProgress.value.completed += candidateChunk.length
        importProgress.value.imported = importedIDs.length
        importProgress.value.failed = Object.keys(failed).length
        importProgress.value.percent = Math.min(
          100,
          Math.round((importProgress.value.completed / Math.max(1, importProgress.value.total)) * 100)
        )
      }
    }

    const summary = buildImportSummary(importedIDs, failed)
    resultSummary.value = summary
    showImportDialog.value = false
    selectedIds.value = []
    importCandidateIds.value = []
    appStore.showSuccess(`导入完成：成功 ${summary.imported} 个，跳过 ${summary.skipped} 个`)
    await loadCandidates()
  } catch (error: any) {
    appStore.showError(error?.message || '导入账号失败')
  } finally {
    importing.value = false
    importProgress.value = null
  }
}

onMounted(async () => {
  await Promise.all([loadCandidates(), loadOptions()])
})

onBeforeUnmount(() => {
  clearScanPollTimer()
})
</script>
