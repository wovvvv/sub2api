import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import AccountRegistrationView from '../AccountRegistrationView.vue'

const longErrorReason = 'refresh auth failure: error: code=502 reason="OPENAI_OAUTH_TOKEN_REFRESH_FAILED" message="token refresh failed because refresh token reused and upstream rejected request with 401 invalid_request_error"'

const {
  codexList,
  codexScan,
  codexScanStatus,
  codexStage,
  codexUnstage,
  codexClear,
  codexImport,
  groupsGetAll,
  proxiesGetAll,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  codexList: vi.fn(),
  codexScan: vi.fn(),
  codexScanStatus: vi.fn(),
  codexStage: vi.fn(),
  codexUnstage: vi.fn(),
  codexClear: vi.fn(),
  codexImport: vi.fn(),
  groupsGetAll: vi.fn(),
  proxiesGetAll: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    codexRegistration: {
      list: codexList,
      scan: codexScan,
      getScanTask: codexScanStatus,
      stage: codexStage,
      unstage: codexUnstage,
      clear: codexClear,
      importCandidates: codexImport
    },
    groups: {
      getAll: groupsGetAll
    },
    proxies: {
      getAll: proxiesGetAll
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showError
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('AccountRegistrationView', () => {
  beforeEach(() => {
    codexList.mockReset()
    codexScan.mockReset()
    codexScanStatus.mockReset()
    codexStage.mockReset()
    codexUnstage.mockReset()
    codexClear.mockReset()
    codexImport.mockReset()
    groupsGetAll.mockReset()
    proxiesGetAll.mockReset()
    showSuccess.mockReset()
    showError.mockReset()

    codexScan.mockResolvedValue({
      task_id: 'scan-task-1',
      status: 'queued'
    })
    codexScanStatus
      .mockResolvedValueOnce({
        task_id: 'scan-task-1',
        status: 'running',
        scanned: 0
      })
      .mockResolvedValueOnce({
        task_id: 'scan-task-1',
        status: 'succeeded',
        scanned: 2
      })
    codexClear.mockResolvedValue({
      cleared: 2
    })
    codexImport.mockResolvedValue({
      imported_ids: [1],
      failed: {
        '2': longErrorReason
      }
    })
    groupsGetAll.mockResolvedValue([
      { id: 9, name: 'VIP专线' },
      { id: 10, name: 'default' }
    ])
    proxiesGetAll.mockResolvedValue([
      { id: 7, name: 'Proxy-7' }
    ])

    codexList.mockImplementation(async () => {
      return {
        total: 2,
        items: [
          {
            id: 1,
            source_path: '/tmp/detected-1.json',
            source_filename: 'detected-1.json',
            source_fingerprint: 'fp-1',
            email: 'alive@example.com',
            account_id: 'acct-alive',
            type: 'oauth',
            liveness_status: 'alive',
            workflow_state: 'detected',
            status_reason: '',
            last_checked_at: '2026-04-10T10:00:00Z',
            can_stage: true,
            can_unstage: false,
            can_import: true
          },
          {
            id: 2,
            source_path: '/tmp/detected-2.json',
            source_filename: 'detected-2.json',
            source_fingerprint: 'fp-2',
            email: 'dead@example.com',
            account_id: 'acct-dead',
            type: 'oauth',
            liveness_status: 'dead',
            workflow_state: 'detected',
            status_reason: longErrorReason,
            last_checked_at: '2026-04-10T10:00:00Z',
            can_stage: false,
            can_unstage: false,
            can_import: false
          }
        ]
      }
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  const BaseDialogStub = defineComponent({
    name: 'BaseDialog',
    props: {
      show: {
        type: Boolean,
        default: false
      },
      title: {
        type: String,
        default: ''
      }
    },
    template: '<div v-if="show"><div>{{ title }}</div><slot /><slot name="footer" /></div>'
  })

  it('supports scan, current-filter selection, and batch import on detection tab', async () => {
    vi.useFakeTimers()
    const wrapper = mount(AccountRegistrationView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: BaseDialogStub
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('CPA导入')
    expect(wrapper.text()).toContain('账号检测')
    expect(wrapper.text()).toContain('注册任务')
    expect(wrapper.text()).toContain('detected-1.json')
    expect(wrapper.text()).toContain('alive@example.com')
    expect(wrapper.text()).toContain('acct-alive')
    expect(wrapper.text()).toContain('可用 1')
    expect(wrapper.text()).toContain('失效 1')
    expect(wrapper.text()).toContain('已检测 2')
    expect(wrapper.text()).toContain('详情')
    expect(wrapper.text()).not.toContain('展开')
    expect(codexList).toHaveBeenCalled()

    await wrapper.get('[data-testid="codex-detection-scan"]').trigger('click')
    expect(codexScan).toHaveBeenCalledTimes(1)
    await flushPromises()
    expect(codexScanStatus).toHaveBeenCalledWith('scan-task-1')
    expect(wrapper.text()).toContain('检测任务运行中')
    expect(wrapper.text()).toContain('运行中')
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()
    expect(codexScanStatus).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('上次检测刚完成，共处理 2 个账号')
    expect(wrapper.text()).toContain('已完成')

    const livenessFilter = wrapper.get('[data-testid="codex-detection-liveness-filter"]')
    await livenessFilter.setValue('alive')
    await flushPromises()
    expect(codexList).toHaveBeenLastCalledWith(expect.objectContaining({ liveness_status: 'alive' }))

    await wrapper.get('[data-testid="codex-detection-select-1"]').setValue(true)
    expect(wrapper.text()).toContain('已选 1 个账号')

    await wrapper.get('[data-testid="codex-detection-select-all-visible"]').trigger('click')
    expect(wrapper.text()).toContain('当前筛选结果 2 个')
    expect(wrapper.text()).toContain('已选 2 个账号')

    expect(wrapper.get('[data-testid="codex-detection-import-1"]').text()).toContain('导入')

    await wrapper.get('[data-testid="codex-detection-batch-import"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="codex-import-group-9"]').setValue(true)
    await wrapper.get('[data-testid="codex-import-group-10"]').setValue(true)
    await wrapper.get('[data-testid="codex-import-proxy-select"]').setValue('7')
    await wrapper.get('[data-testid="codex-import-notes"]').setValue('批量导入')
    await wrapper.get('[data-testid="codex-import-concurrency"]').setValue('1')
    await wrapper.get('[data-testid="codex-import-load-factor"]').setValue('1')
    await wrapper.get('[data-testid="codex-import-priority"]').setValue('1')
    await wrapper.get('[data-testid="codex-import-rate-multiplier"]').setValue('1')
    await wrapper.get('[data-testid="codex-import-models-toggle"]').trigger('click')
    await wrapper.get('[data-testid="codex-import-submit"]').trigger('click')

    expect(codexImport).toHaveBeenCalledWith({
      candidate_ids: [1, 2],
      group_ids: [9, 10],
      proxy_id: 7,
      notes: '批量导入',
      concurrency: 1,
      load_factor: 1,
      priority: 1,
      rate_multiplier: 1,
      import_models: true
    }, {
      timeout: 120000
    })
    expect(wrapper.text()).toContain('导入成功 1 个')
    expect(wrapper.text()).toContain('跳过 1 个')
    expect(wrapper.text()).toContain('refresh auth failure')

    vi.useRealTimers()
  })

  it('shows failed scan status with detail dialog', async () => {
    vi.useFakeTimers()
    codexScan.mockResolvedValue({
      task_id: 'scan-task-failed',
      status: 'queued'
    })
    codexScanStatus.mockReset()
    codexScanStatus
      .mockResolvedValueOnce({
        task_id: 'scan-task-failed',
        status: 'running',
        scanned: 0
      })
      .mockResolvedValueOnce({
        task_id: 'scan-task-failed',
        status: 'failed',
        scanned: 0,
        error_message: longErrorReason
      })

    const wrapper = mount(AccountRegistrationView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: BaseDialogStub
        }
      }
    })

    await flushPromises()
    await wrapper.get('[data-testid="codex-detection-scan"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()

    expect(wrapper.text()).toContain('失败')
    expect(wrapper.text()).toContain('检测失败：refresh auth failure')
    expect(showError).toHaveBeenCalled()

    await wrapper.get('[data-testid="codex-detection-scan-status-detail"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('扫描失败详情')
    expect(wrapper.text()).toContain(longErrorReason)
  })

  it('shows full failure reason in detail dialog and keeps table horizontally scrollable', async () => {
    const wrapper = mount(AccountRegistrationView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: BaseDialogStub
        }
      }
    })

    await flushPromises()

    const tableScroll = wrapper.get('[data-testid="codex-detection-table-scroll"]')
    expect(tableScroll.classes()).toContain('overflow-x-auto')

    const table = wrapper.get('[data-testid="codex-detection-table"]')
    expect(table.classes().some((className) => className.includes('min-w-['))).toBe(true)

    const detailButton = wrapper.get('[data-testid="codex-detection-reason-detail-2"]')
    expect(detailButton.text()).toContain('详情')

    await detailButton.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('错误详情')
    expect(wrapper.text()).toContain(longErrorReason)
  })

  it('supports single import entry and keeps tasks tab as placeholder', async () => {
    const wrapper = mount(AccountRegistrationView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: BaseDialogStub
        }
      }
    })
    await flushPromises()

    await wrapper.get('[data-testid="codex-detection-import-1"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('已选 1 个账号')

    await wrapper.get('[data-testid="account-registration-tab-tasks"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('注册任务')
    expect(wrapper.text()).toContain('这里暂时留空')
  })

  it('clears all detected candidates after confirmation', async () => {
    codexList
      .mockResolvedValueOnce({
        total: 2,
        items: [
          {
            id: 1,
            source_path: '/tmp/detected-1.json',
            source_filename: 'detected-1.json',
            source_fingerprint: 'fp-1',
            email: 'alive@example.com',
            account_id: 'acct-alive',
            type: 'oauth',
            liveness_status: 'alive',
            workflow_state: 'detected',
            status_reason: '',
            last_checked_at: '2026-04-10T10:00:00Z',
            can_stage: true,
            can_unstage: false,
            can_import: true
          },
          {
            id: 2,
            source_path: '/tmp/detected-2.json',
            source_filename: 'detected-2.json',
            source_fingerprint: 'fp-2',
            email: 'dead@example.com',
            account_id: 'acct-dead',
            type: 'oauth',
            liveness_status: 'dead',
            workflow_state: 'detected',
            status_reason: '',
            last_checked_at: '2026-04-10T10:00:00Z',
            can_stage: false,
            can_unstage: false,
            can_import: false
          }
        ]
      })
      .mockResolvedValueOnce({
        total: 0,
        items: []
      })

    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)

    const wrapper = mount(AccountRegistrationView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: BaseDialogStub
        }
      }
    })

    await flushPromises()

    await wrapper.get('[data-testid="codex-detection-clear-all"]').trigger('click')
    await flushPromises()

    expect(confirmSpy).toHaveBeenCalled()
    expect(codexClear).toHaveBeenCalledTimes(1)
    expect(codexList).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('当前筛选结果 0 个')
    expect(wrapper.text()).toContain('暂无检测账号')

    confirmSpy.mockRestore()
  })

  it('shows import progress and splits large batch imports into chunks', async () => {
    codexList.mockResolvedValue({
      total: 6,
      items: Array.from({ length: 6 }, (_, index) => ({
        id: index + 1,
        source_path: `/tmp/detected-${index + 1}.json`,
        source_filename: `detected-${index + 1}.json`,
        source_fingerprint: `fp-${index + 1}`,
        email: `alive-${index + 1}@example.com`,
        account_id: `acct-${index + 1}`,
        type: 'oauth',
        liveness_status: 'alive',
        workflow_state: 'detected',
        status_reason: '',
        last_checked_at: '2026-04-10T10:00:00Z',
        can_stage: true,
        can_unstage: false,
        can_import: true
      }))
    })

    let resolveFirstChunk: ((value: any) => void) | null = null
    codexImport
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirstChunk = resolve }))
      .mockResolvedValueOnce({
        imported_ids: [6],
        failed: {}
      })

    const wrapper = mount(AccountRegistrationView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          BaseDialog: BaseDialogStub
        }
      }
    })
    await flushPromises()

    await wrapper.get('[data-testid="codex-detection-select-all-visible"]').trigger('click')
    await wrapper.get('[data-testid="codex-detection-batch-import"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="codex-import-group-9"]').setValue(true)
    await wrapper.get('[data-testid="codex-import-submit"]').trigger('click')
    await nextTick()

    expect(wrapper.text()).toContain('导入进度')
    expect(wrapper.text()).toContain('已完成 0 / 6')
    expect(wrapper.text()).toContain('当前分块 1 / 2')

    resolveFirstChunk?.({
      imported_ids: [1, 2, 3, 4, 5],
      failed: {}
    })
    await flushPromises()

    expect(codexImport).toHaveBeenNthCalledWith(1, expect.objectContaining({
      candidate_ids: [1, 2, 3, 4, 5]
    }), {
      timeout: 120000
    })
    expect(codexImport).toHaveBeenNthCalledWith(2, expect.objectContaining({
      candidate_ids: [6]
    }), {
      timeout: 120000
    })
  })
})
