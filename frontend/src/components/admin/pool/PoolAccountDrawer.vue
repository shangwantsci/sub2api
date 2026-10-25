<template>
  <Teleport to="body">
    <div v-if="show && account" class="fixed inset-0 z-50 flex justify-end bg-black/30" @click.self="$emit('close')">
      <aside class="h-full w-full max-w-xl overflow-y-auto bg-white shadow-2xl dark:bg-dark-800">
        <div class="sticky top-0 z-10 flex items-start justify-between border-b border-gray-200 bg-white px-5 py-4 dark:border-dark-700 dark:bg-dark-800">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ account.name || `#${account.id}` }}</h2>
            <p class="mt-0.5 text-xs text-gray-500">#{{ account.id }} · {{ account.platform }} · {{ account.type }}</p>
          </div>
          <button class="text-gray-400 hover:text-gray-600" @click="$emit('close')">✕</button>
        </div>

        <div class="space-y-5 p-5">
          <div class="flex flex-wrap gap-2">
            <span class="badge" :class="account.status === 'active' ? 'badge-success' : 'badge-gray'">{{ account.status }}</span>
            <span v-if="account.schedulable === false" class="badge badge-warning">unschedulable</span>
            <span v-if="account.rate_limit_reset_at" class="badge badge-warning">429 until {{ formatTime(account.rate_limit_reset_at) }}</span>
            <span v-if="activeRun" class="badge badge-primary">{{ activeRun.state }}</span>
          </div>

          <div v-if="account.error_message" class="rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
            {{ account.error_message }}
          </div>

          <div class="grid grid-cols-2 gap-3">
            <div class="card p-3">
              <p class="text-xs text-gray-500">5h</p>
              <p class="mt-1 text-lg font-semibold">{{ percent(usage?.five_hour?.utilization) }}</p>
              <p class="text-xs text-gray-500">{{ usage?.five_hour?.resets_at ? formatTime(usage.five_hour.resets_at) : '—' }}</p>
            </div>
            <div class="card p-3">
              <p class="text-xs text-gray-500">7d</p>
              <p class="mt-1 text-lg font-semibold">{{ percent(usage?.seven_day?.utilization) }}</p>
              <p class="text-xs text-gray-500">{{ usage?.seven_day?.resets_at ? formatTime(usage.seven_day.resets_at) : '—' }}</p>
            </div>
            <div class="card p-3">
              <p class="text-xs text-gray-500">{{ t('admin.pool.capacity') }}</p>
              <p class="mt-1 text-sm">{{ account.current_concurrency ?? 0 }} / {{ account.concurrency ?? '∞' }}</p>
            </div>
            <div class="card p-3">
              <p class="text-xs text-gray-500">{{ t('admin.pool.lastUsed') }}</p>
              <p class="mt-1 text-sm">{{ formatTime(account.last_used_at) }}</p>
            </div>
          </div>

          <div v-if="loadingUsage" class="text-center text-xs text-gray-500">…</div>

          <div v-if="activeRun" class="card p-3 text-xs text-gray-600 dark:text-gray-300">
            <p class="font-medium text-gray-900 dark:text-white">{{ t('admin.pool.runs') }}</p>
            <p class="mt-1 font-mono">{{ activeRun.session }}</p>
            <p class="mt-1">{{ activeRun.state }} · {{ formatTime(activeRun.started_at) }}</p>
          </div>

          <div class="flex justify-end gap-2 border-t border-gray-200 pt-4 dark:border-dark-700">
            <button class="btn btn-secondary" @click="$emit('close')">{{ t('common.cancel') }}</button>
            <RouterLink :to="`/admin/accounts?account=${account.id}`" class="btn btn-primary">{{ t('admin.pool.openAccount') }}</RouterLink>
          </div>
        </div>
      </aside>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Account, AccountUsageInfo } from '@/types'
import type { PoolRun } from '@/api/admin/pool'

const props = defineProps<{ show: boolean; account: Account | null; activeRun?: PoolRun | null }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const usage = ref<AccountUsageInfo | null>(null)
const loadingUsage = ref(false)

function percent(value: number | undefined | null) {
  return typeof value === 'number' ? `${Math.round(value)}%` : '—'
}
function formatTime(value: string | null | undefined) {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

watch(
  () => [props.show, props.account?.id],
  async () => {
    usage.value = null
    if (!props.show || !props.account) return
    loadingUsage.value = true
    try {
      usage.value = await adminAPI.accounts.getUsage(props.account.id)
    } finally {
      loadingUsage.value = false
    }
  },
  { immediate: true },
)
</script>
