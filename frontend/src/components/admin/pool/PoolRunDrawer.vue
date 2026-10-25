<template>
  <Teleport to="body">
    <div v-if="show && run" class="fixed inset-0 z-50 flex justify-end bg-black/30" @click.self="$emit('close')">
      <aside class="h-full w-full max-w-lg overflow-y-auto bg-white shadow-2xl dark:bg-dark-800">
        <div class="flex items-start justify-between border-b border-gray-200 px-5 py-4 dark:border-dark-700">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.pool.runDetail') }}</h2>
            <p class="mt-0.5 font-mono text-xs text-gray-500">{{ run.run_id }}</p>
          </div>
          <button class="text-gray-400 hover:text-gray-600" @click="$emit('close')">✕</button>
        </div>
        <div class="space-y-4 p-5">
          <div class="flex flex-wrap gap-2">
            <span class="badge" :class="run.state === 'waiting_tool' ? 'badge-warning' : 'badge-success'">{{ run.state }}</span>
            <span v-if="run.tool_pending" class="badge badge-warning">{{ run.tool_pending }} tool pending</span>
          </div>
          <dl class="kv text-sm">
            <dt>{{ t('admin.pool.account') }}</dt><dd>{{ run.account_name || `#${run.account_id}` }}</dd>
            <dt>{{ t('admin.pool.session') }}</dt><dd class="font-mono text-xs">{{ run.session }}</dd>
            <dt>{{ t('admin.pool.startedAt') }}</dt><dd>{{ formatTime(run.started_at) }}</dd>
            <dt>{{ t('admin.pool.lastUsed') }}</dt><dd>{{ formatTime(run.last_used_at) }}</dd>
          </dl>
          <div class="flex justify-end gap-2 border-t border-gray-200 pt-4 dark:border-dark-700">
            <button class="btn btn-secondary" @click="$emit('close')">{{ t('common.cancel') }}</button>
            <button class="btn btn-danger" @click="$emit('release', run)">{{ t('admin.pool.release') }}</button>
          </div>
        </div>
      </aside>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PoolRun } from '@/api/admin/pool'

defineProps<{ show: boolean; run: PoolRun | null }>()
defineEmits<{ close: []; release: [run: PoolRun] }>()
const { t } = useI18n()

function formatTime(value: string) {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}
</script>
