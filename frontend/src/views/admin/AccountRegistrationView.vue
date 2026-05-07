<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="sticky top-0 z-10 overflow-x-auto settings-tabs-scroll">
        <nav class="settings-tabs">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            :data-testid="`account-registration-tab-${tab.key}`"
            :class="['settings-tab', activeTab === tab.key && 'settings-tab-active']"
            @click="activeTab = tab.key"
          >
            <span>{{ tab.label }}</span>
          </button>
        </nav>
      </div>

      <CodexDetectionTab v-if="activeTab === 'cpa-import'" />
      <OpenAIOAuthDetectionTab v-else-if="activeTab === 'account-detection'" />
      <CodexPendingImportTab v-else />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import CodexDetectionTab from '@/components/admin/codex-registration/CodexDetectionTab.vue'
import CodexPendingImportTab from '@/components/admin/codex-registration/CodexPendingImportTab.vue'
import OpenAIOAuthDetectionTab from '@/components/admin/account-registration/OpenAIOAuthDetectionTab.vue'
import type { AccountRegistrationTabKey } from '@/components/admin/codex-registration/types'

const tabs: Array<{ key: AccountRegistrationTabKey; label: string }> = [
  { key: 'cpa-import', label: 'CPA导入' },
  { key: 'account-detection', label: '账号检测' },
  { key: 'tasks', label: '注册任务' }
]

const activeTab = ref<AccountRegistrationTabKey>('cpa-import')
</script>

<style scoped>
.settings-tabs-scroll {
  scrollbar-width: thin;
  scrollbar-color: transparent transparent;
}

.settings-tabs-scroll:hover {
  scrollbar-color: rgb(0 0 0 / 0.15) transparent;
}

:root.dark .settings-tabs-scroll:hover {
  scrollbar-color: rgb(255 255 255 / 0.2) transparent;
}

.settings-tabs-scroll::-webkit-scrollbar {
  height: 3px;
}

.settings-tabs-scroll::-webkit-scrollbar-track {
  background: transparent;
}

.settings-tabs-scroll::-webkit-scrollbar-thumb {
  background: transparent;
  border-radius: 3px;
}

.settings-tabs-scroll:hover::-webkit-scrollbar-thumb {
  background: rgb(0 0 0 / 0.15);
}

:root.dark .settings-tabs-scroll:hover::-webkit-scrollbar-thumb {
  background: rgb(255 255 255 / 0.2);
}

.settings-tabs {
  @apply inline-flex min-w-full gap-0.5 rounded-2xl border border-gray-100 bg-white/80 p-1 backdrop-blur-sm dark:border-dark-700/50 dark:bg-dark-800/80;
  box-shadow: 0 1px 3px rgb(0 0 0 / 0.04), 0 1px 2px rgb(0 0 0 / 0.02);
}

@media (min-width: 640px) {
  .settings-tabs {
    @apply flex;
  }
}

.settings-tab {
  @apply relative flex flex-1 items-center justify-center whitespace-nowrap rounded-xl px-3 py-2 text-sm font-medium text-gray-500 transition-all duration-200 ease-out dark:text-dark-400;
}

.settings-tab:hover:not(.settings-tab-active) {
  @apply text-gray-700 dark:text-gray-300;
  background: rgb(0 0 0 / 0.03);
}

:root.dark .settings-tab:hover:not(.settings-tab-active) {
  background: rgb(255 255 255 / 0.04);
}

.settings-tab-active {
  @apply text-primary-600 dark:text-primary-400;
  background: linear-gradient(135deg, rgba(20, 184, 166, 0.08), rgba(20, 184, 166, 0.03));
  box-shadow: 0 1px 2px rgba(20, 184, 166, 0.1);
}

:root.dark .settings-tab-active {
  background: linear-gradient(135deg, rgba(45, 212, 191, 0.12), rgba(45, 212, 191, 0.05));
  box-shadow: 0 1px 3px rgb(0 0 0 / 0.25);
}
</style>
