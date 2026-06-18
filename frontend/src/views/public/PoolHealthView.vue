<template>
  <main class="min-h-screen bg-slate-50 px-4 py-4 text-slate-900 dark:bg-dark-950 dark:text-white sm:px-6">
    <section class="mx-auto max-w-7xl">
      <div class="mb-3 flex flex-col gap-3 border-b border-slate-200 pb-3 dark:border-dark-800 sm:flex-row sm:items-center sm:justify-between">
        <div class="flex min-w-0 items-center gap-3">
          <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-teal-500 text-white shadow-sm">
            <Icon name="chartBar" size="sm" :stroke-width="2" />
          </div>
          <div class="min-w-0">
            <h1 class="truncate text-base font-semibold text-slate-950 dark:text-white">可用额度监控</h1>
            <p class="text-xs text-slate-500 dark:text-dark-400">{{ groupName }} · Setup Token</p>
          </div>
          <div class="ml-1 flex shrink-0 rounded-lg border border-slate-200 bg-white p-0.5 text-xs dark:border-dark-700 dark:bg-dark-900">
            <button class="rounded-md bg-teal-500 px-3 py-1.5 font-medium text-white shadow-sm">Claude</button>
            <button class="rounded-md px-3 py-1.5 font-medium text-slate-500 dark:text-dark-400" disabled>OpenAI</button>
          </div>
        </div>

        <div class="flex items-center gap-2 text-xs text-slate-500 dark:text-dark-400">
          <span>更新于：{{ updatedLabel }}</span>
          <button
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-slate-200 bg-white text-teal-600 transition hover:border-teal-300 hover:bg-teal-50 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-700 dark:bg-dark-900 dark:text-teal-300 dark:hover:border-teal-700 dark:hover:bg-dark-800"
            :disabled="loading"
            title="刷新"
            @click="refresh"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          </button>
        </div>
      </div>

      <div v-if="errorMessage" class="mb-3 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-200">
        {{ errorMessage }}
      </div>

      <div class="grid gap-3 lg:grid-cols-12">
        <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-dark-800 dark:bg-dark-900 lg:col-span-3">
          <div class="mb-2 flex items-center justify-between text-xs text-slate-500 dark:text-dark-400">
            <span>账号数（有效/总）</span>
            <span v-if="snapshot" class="text-slate-400">{{ snapshot.accounts.available }} 可调度</span>
          </div>
          <div class="mb-2 text-2xl font-bold tabular-nums text-slate-950 dark:text-white">
            <span v-if="snapshot">{{ snapshot.accounts.effective }}</span>
            <span v-else>--</span>
            <span class="text-base font-semibold text-slate-400"> / {{ snapshot?.accounts.total ?? '--' }}</span>
          </div>
          <p class="text-xs leading-5 text-slate-500 dark:text-dark-400">
            使用中 {{ snapshot?.accounts.in_use ?? 0 }} · 闲置 {{ snapshot?.accounts.idle ?? 0 }} · 已打满 {{ snapshot?.accounts.exhausted ?? 0 }}
          </p>
          <p class="mt-1 text-xs leading-5 text-rose-500">
            异常除不可用账号 {{ snapshot?.accounts.unavailable ?? 0 }} 个<span v-if="breakdownText">（{{ breakdownText }}）</span>
          </p>
        </div>

        <div class="rounded-lg border border-indigo-200 bg-indigo-50/70 p-4 shadow-sm dark:border-indigo-900/60 dark:bg-indigo-950/30 lg:col-span-3">
          <div class="mb-2 text-xs font-medium text-indigo-700 dark:text-indigo-300">剩余容量</div>
          <div class="mb-3 text-3xl font-bold tabular-nums text-orange-500">
            {{ formatPercent(snapshot?.capacity.remaining_percent) }}
          </div>
          <div class="h-2 overflow-hidden rounded-full bg-indigo-100 dark:bg-indigo-950">
            <div
              class="h-full rounded-full bg-indigo-500 transition-all"
              :style="{ width: `${progressWidth(snapshot?.capacity.remaining_percent)}%` }"
            ></div>
          </div>
        </div>

        <div class="rounded-lg border border-amber-200 bg-amber-50/70 p-4 shadow-sm dark:border-amber-900/60 dark:bg-amber-950/25 lg:col-span-3">
          <div class="mb-2 flex items-center justify-between text-xs text-amber-700 dark:text-amber-300">
            <span class="font-medium">池子负载</span>
            <span>基于 {{ snapshot?.accounts.measured ?? 0 }} 个账号实测</span>
          </div>
          <div class="mb-2 text-3xl font-bold tabular-nums text-orange-500">
            {{ formatPercent(snapshot?.capacity.pool_load_percent) }}
          </div>
          <p class="text-xs leading-5 text-orange-600 dark:text-orange-300">
            等待队列 {{ snapshot?.capacity.waiting ?? 0 }} · 当前压力{{ loadHint }}
          </p>
        </div>

        <div class="rounded-lg border border-orange-200 bg-orange-50/70 p-4 shadow-sm dark:border-orange-900/60 dark:bg-orange-950/25 lg:col-span-3">
          <div class="mb-2 text-xs font-medium text-orange-700 dark:text-orange-300">池子健康度</div>
          <div class="mb-2 text-3xl font-bold" :class="healthTextClass">
            {{ snapshot?.health.label ?? '--' }}
          </div>
          <p class="text-xs leading-5 text-orange-700 dark:text-orange-300">
            {{ snapshot?.health.message ?? '正在加载健康状态' }}
          </p>
        </div>
      </div>

      <section class="mt-3 rounded-lg border border-teal-200 bg-teal-50/80 px-4 py-3 text-sm leading-7 text-teal-900 shadow-sm dark:border-teal-900/60 dark:bg-teal-950/30 dark:text-teal-100">
        <div class="flex flex-wrap items-start gap-x-4 gap-y-1">
          <span class="inline-flex items-center gap-1 font-semibold">
            <Icon name="refresh" size="xs" />
            4h 内恢复：
          </span>
          <template v-if="snapshot && snapshot.recovery_buckets.length > 0">
            <span
              v-for="bucket in snapshot.recovery_buckets"
              :key="bucket.after_seconds"
              class="whitespace-nowrap"
            >
              {{ bucket.label }} +{{ bucket.count }}
              <span class="text-teal-600 dark:text-teal-300">（{{ formatSegments(bucket.segments) }}）</span>
            </span>
          </template>
          <span v-else class="text-teal-700 dark:text-teal-300">暂无恢复批次</span>
        </div>
      </section>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getPoolHealth, type PublicPoolHealthRecoverySegment, type PublicPoolHealthSnapshot } from '@/api/poolHealth'

const groupName = 'ccmax'
const snapshot = ref<PublicPoolHealthSnapshot | null>(null)
const loading = ref(false)
const errorMessage = ref('')
let refreshTimer: number | null = null

const refresh = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    snapshot.value = await getPoolHealth({
      horizon: '4h',
    })
  } catch (error) {
    const message = error && typeof error === 'object' && 'message' in error
      ? String((error as { message?: unknown }).message || '')
      : ''
    errorMessage.value = message || '监控数据加载失败'
  } finally {
    loading.value = false
  }
}

const updatedLabel = computed(() => {
  if (!snapshot.value?.updated_at) return loading.value ? '加载中' : '--'
  const updatedAt = new Date(snapshot.value.updated_at).getTime()
  if (!Number.isFinite(updatedAt)) return '--'
  const diffSeconds = Math.max(0, Math.floor((Date.now() - updatedAt) / 1000))
  if (diffSeconds < 60) return '刚刚'
  const minutes = Math.floor(diffSeconds / 60)
  if (minutes < 60) return `${minutes} 分钟前`
  return `${Math.floor(minutes / 60)} 小时前`
})

const breakdownText = computed(() => {
  const rows = snapshot.value?.unavailable_breakdown || []
  return rows.map((item) => `${item.label} ${item.count}`).join(' / ')
})

const loadHint = computed(() => {
  const load = snapshot.value?.capacity.pool_load_percent ?? 0
  if (load >= 90) return '较高'
  if (load >= 75) return '偏高'
  return '平稳'
})

const healthTextClass = computed(() => {
  switch (snapshot.value?.health.level) {
    case 'tight':
      return 'text-orange-600 dark:text-orange-300'
    case 'watch':
      return 'text-amber-600 dark:text-amber-300'
    case 'empty':
      return 'text-slate-500 dark:text-dark-400'
    default:
      return 'text-teal-600 dark:text-teal-300'
  }
})

const formatPercent = (value?: number) => {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '--'
  return `${value.toFixed(1)}%`
}

const progressWidth = (value?: number) => {
  if (typeof value !== 'number' || !Number.isFinite(value)) return 0
  return Math.max(0, Math.min(100, value))
}

const formatSegments = (segments: PublicPoolHealthRecoverySegment[]) => {
  if (!segments.length) return 'x1'
  return segments
    .map((segment) => segment.count > 1 ? `${segment.unit}×${segment.count}` : segment.unit)
    .join(' ')
}

onMounted(() => {
  void refresh()
  refreshTimer = window.setInterval(() => {
    void refresh()
  }, 30_000)
})

onBeforeUnmount(() => {
  if (refreshTimer) {
    window.clearInterval(refreshTimer)
  }
})
</script>
