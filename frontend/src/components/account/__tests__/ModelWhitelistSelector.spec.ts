import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showInfo: vi.fn()
  })
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

describe('ModelWhitelistSelector', () => {
  it('adds multiple custom models split by commas and newlines', async () => {
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        'onUpdate:modelValue': (value: string[]) => {
          wrapper.setProps({ modelValue: value })
        }
      },
      global: {
        stubs: {
          ModelIcon: true,
          Icon: true
        }
      }
    })

    const input = wrapper.get('textarea[placeholder="admin.accounts.enterCustomModelName"]')
    await input.setValue('gpt-5.4, gpt-5.3-codex\ncustom-model，gpt-5.4')
    const addButton = wrapper.findAll('button').find((btn) => btn.text() === 'admin.accounts.addModel')
    expect(addButton).toBeDefined()
    await addButton!.trigger('click')

    expect(wrapper.props('modelValue')).toEqual(['gpt-5.4', 'gpt-5.3-codex', 'custom-model'])
  })
})
