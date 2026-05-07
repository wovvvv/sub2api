import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import OpenAIOAuthDetectionTab from '../OpenAIOAuthDetectionTab.vue'

const {
  accountsList,
  accountsDelete,
  detectionProbe,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  accountsList: vi.fn(),
  accountsDelete: vi.fn(),
  detectionProbe: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: accountsList,
      delete: accountsDelete
    },
    openaiOAuthDetection: {
      probe: detectionProbe
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showError
  })
}))

describe('OpenAIOAuthDetectionTab', () => {
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

  beforeEach(() => {
    accountsList.mockReset()
    accountsDelete.mockReset()
    detectionProbe.mockReset()
    showSuccess.mockReset()
    showError.mockReset()

    const populatedList = {
      items: [
        {
          id: 11,
          name: 'OpenAI OAuth A',
          platform: 'openai',
          type: 'oauth',
          status: 'active',
          error_message: '',
          credentials: {
            email: 'a@example.com',
            chatgpt_account_id: 'acct-a'
          },
          extra: {
            openai_oauth_detection_last_result: 'healthy',
            openai_oauth_detection_last_checked_at: '2026-04-22T08:00:00Z',
            openai_oauth_detection_last_reason: ''
          }
        },
        {
          id: 12,
          name: 'OpenAI OAuth B',
          platform: 'openai',
          type: 'oauth',
          status: 'error',
          error_message: 'OpenAI OAuth detection (401): revoked',
          credentials: {
            email: 'b@example.com',
            chatgpt_account_id: 'acct-b'
          },
          extra: {
            openai_oauth_detection_last_result: 'unauthorized',
            openai_oauth_detection_last_checked_at: '2026-04-22T09:00:00Z',
            openai_oauth_detection_last_reason: 'upstream returned 429: {"error":{"type":"usage_limit","message":"too many requests from this account and the response body is intentionally very long for detail dialog testing"}}'
          }
        }
      ],
      total: 2,
      page: 1,
      page_size: 10000,
      pages: 1
    }

    accountsList
      .mockResolvedValueOnce(populatedList)
      .mockResolvedValueOnce(populatedList)
      .mockResolvedValue({
        items: [],
        total: 0,
        page: 1,
        page_size: 10000,
        pages: 0
      })

    detectionProbe.mockResolvedValue({
      checked: 2,
      healthy: 1,
      unauthorized: 1,
      failed: {}
    })
    accountsDelete.mockResolvedValue({ message: 'ok' })
  })

  it('refreshes from accounts API and supports bulk detect/delete', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)

    const wrapper = mount(OpenAIOAuthDetectionTab, {
      global: {
        stubs: {
          BaseDialog: BaseDialogStub
        }
      }
    })
    await flushPromises()

    expect(accountsList).toHaveBeenCalledWith(1, 10000, {
      platform: 'openai',
      type: 'oauth'
    })
    expect(wrapper.text()).toContain('OpenAI OAuth A')
    expect(wrapper.text()).toContain('a@example.com')
    expect(wrapper.text()).toContain('acct-a')
    expect((wrapper.get('[data-testid="openai-oauth-detection-model-input"]').element as HTMLInputElement).value).toBe('gpt-5.4-mini')

    await wrapper.get('[data-testid="openai-oauth-detection-select-all-visible"]').trigger('click')
    await wrapper.get('[data-testid="openai-oauth-detection-model-input"]').setValue('gpt-5.4')
    await wrapper.get('[data-testid="openai-oauth-detection-probe-selected"]').trigger('click')
    await flushPromises()

    expect(detectionProbe).toHaveBeenCalledWith({ account_ids: [11, 12], model: 'gpt-5.4' })
    expect(accountsList).toHaveBeenCalledTimes(2)

    await wrapper.get('[data-testid="openai-oauth-detection-delete-selected"]').trigger('click')
    await flushPromises()

    expect(confirmSpy).toHaveBeenCalled()
    expect(accountsDelete).toHaveBeenNthCalledWith(1, 11)
    expect(accountsDelete).toHaveBeenNthCalledWith(2, 12)
    expect(accountsList).toHaveBeenCalledTimes(3)

    confirmSpy.mockRestore()
  })

  it('shows truncated detection reason with detail dialog for long content', async () => {
    const wrapper = mount(OpenAIOAuthDetectionTab, {
      global: {
        stubs: {
          BaseDialog: BaseDialogStub
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('upstream returned 429')
    expect(wrapper.text()).toContain('详情')

    await wrapper.get('[data-testid="openai-oauth-detection-reason-detail-12"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('错误详情')
    expect(wrapper.text()).toContain('too many requests from this account')
  })
})
