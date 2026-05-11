<template>
  <div class="min-h-screen bg-[#f7f4ef] text-stone-900 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-stone-200 bg-white/90 dark:border-dark-800 dark:bg-dark-900/95">
      <div class="mx-auto flex max-w-3xl items-center justify-between gap-4 px-4 py-4 sm:px-6">
        <RouterLink to="/home" class="flex min-w-0 items-center gap-3">
          <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center overflow-hidden rounded-lg bg-white shadow-sm ring-1 ring-stone-200 dark:bg-dark-800 dark:ring-dark-700">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
          </span>
          <span class="truncate text-base font-semibold tracking-normal text-stone-950 dark:text-white">
            {{ siteName }}
          </span>
        </RouterLink>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="inline-flex h-10 w-10 items-center justify-center rounded-lg border border-stone-200 bg-white text-stone-600 transition hover:border-emerald-300 hover:text-emerald-700 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300 dark:hover:border-emerald-500/60 dark:hover:text-emerald-200"
            :disabled="loading"
            title="刷新"
            @click="fetchStatus"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          </button>
          <RouterLink
            to="/login"
            class="inline-flex h-10 items-center justify-center rounded-lg bg-emerald-600 px-4 text-sm font-semibold text-white transition hover:bg-emerald-700"
          >
            登录
          </RouterLink>
        </div>
      </div>
    </header>

    <main class="mx-auto max-w-3xl px-4 py-8 sm:px-6 lg:py-10">
      <section class="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div class="flex items-center gap-3">
          <h1 class="text-2xl font-semibold tracking-normal text-stone-950 dark:text-white">
            网络负载与定价
          </h1>
        </div>
        <div class="flex items-center gap-3">
          <div class="inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-sm font-medium" :class="statusBadgeClass">
            <span class="h-2 w-2 rounded-full" :class="statusDotClass"></span>
            {{ statusText }}
          </div>
          <div class="text-sm text-stone-500 dark:text-dark-400">
            <span v-if="status?.updated_at">更新于 {{ updatedAtText }}</span>
            <span v-else>等待数据</span>
          </div>
        </div>
      </section>

      <div v-if="loading && !status" class="rounded-lg bg-white p-7 shadow-sm ring-1 ring-stone-100 dark:bg-dark-900 dark:ring-dark-800">
        <div class="skeleton h-4 w-24"></div>
        <div v-for="item in 4" :key="item" class="mt-6">
          <div class="skeleton h-4 w-40"></div>
          <div class="skeleton mt-4 h-1.5 w-full"></div>
        </div>
      </div>

      <section
        v-else-if="loadError && !status"
        class="rounded-lg border border-rose-200 bg-rose-50 p-6 text-rose-800 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-100"
      >
        <div class="flex items-start gap-3">
          <Icon name="exclamationTriangle" size="lg" />
          <div>
            <h2 class="text-lg font-semibold">状态加载失败</h2>
            <p class="mt-2 text-sm leading-6">暂时无法读取 Claude 号池数据，请稍后刷新。</p>
          </div>
        </div>
      </section>

      <template v-else-if="status">
        <section class="rounded-lg bg-white p-7 shadow-sm ring-1 ring-stone-100 dark:bg-dark-900 dark:ring-dark-800">
          <div class="mb-6 flex items-center justify-between gap-4">
            <span class="text-sm font-semibold uppercase tracking-wider text-stone-400 dark:text-dark-400">
              CLAUDE
            </span>
            <span class="rounded-full bg-emerald-50 px-3 py-1 text-sm font-semibold tabular-nums text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-200">
              压力 {{ percentText(status.load_percent) }}
            </span>
          </div>

          <div class="space-y-6">
            <div v-for="model in claudeModels" :key="model.name">
              <div class="flex items-center justify-between gap-3">
                <span class="min-w-0 truncate font-mono text-sm font-semibold text-stone-500 dark:text-dark-300">
                  {{ displayModelName(model.name) }}
                </span>
                <span class="w-14 flex-shrink-0 text-right text-sm tabular-nums text-stone-400 dark:text-dark-400">
                  {{ percentText(model.load_percent) }}
                </span>
              </div>
              <div class="mt-3 h-1.5 overflow-hidden rounded-full bg-stone-100 dark:bg-dark-800">
                <div class="h-full rounded-full bg-emerald-400" :style="{ width: loadBarWidth(model.load_percent) }"></div>
              </div>
            </div>
          </div>
        </section>

        <section
          v-if="status.status === 'stale' || loadError"
          class="mt-6 rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100"
        >
          <div class="flex items-start gap-3">
            <Icon name="exclamationTriangle" size="sm" />
            <p>最新抓取未成功，当前页面可能显示上一次可用数据。</p>
          </div>
        </section>
      </template>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { claudePoolAPI, type ClaudePoolStatus } from '@/api/claudePool'
import { useAppStore } from '@/stores'

const appStore = useAppStore()
const status = ref<ClaudePoolStatus | null>(null)
const loading = ref(false)
const loadError = ref(false)
let refreshTimer: ReturnType<typeof setInterval> | null = null

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')

const statusText = computed(() => {
  switch (status.value?.status) {
    case 'fresh':
      return '实时更新'
    case 'stale':
      return '数据过期'
    case 'disabled':
      return '未启用'
    case 'unavailable':
      return '暂不可用'
    default:
      return loading.value ? '加载中' : '等待数据'
  }
})

const statusBadgeClass = computed(() => {
  switch (status.value?.status) {
    case 'fresh':
      return 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-100'
    case 'stale':
      return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100'
    case 'disabled':
    case 'unavailable':
      return 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-100'
    default:
      return 'border-slate-200 bg-white text-slate-600 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300'
  }
})

const statusDotClass = computed(() => {
  switch (status.value?.status) {
    case 'fresh':
      return 'bg-emerald-500'
    case 'stale':
      return 'bg-amber-500'
    case 'disabled':
    case 'unavailable':
      return 'bg-rose-500'
    default:
      return 'bg-slate-400'
  }
})

const claudeModels = computed(() => {
  const models = status.value?.models ?? []
  const order = new Map([
    ['claude-opus-4-6', 0],
    ['claude-sonnet-4-6', 1],
    ['claude-haiku-4-5', 2],
    ['claude-opus-4-7', 3],
  ])
  return [...models].sort((left, right) => {
    const leftOrder = order.get(displayModelName(left.name)) ?? 99
    const rightOrder = order.get(displayModelName(right.name)) ?? 99
    return leftOrder - rightOrder
  })
})

function displayModelName(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/\./g, '-')
    .replace(/\s+/g, '-')
}

function loadBarWidth(loadPercent: number): string {
  return `${clampPercent(loadPercent)}%`
}

const updatedAtText = computed(() => {
  if (!status.value?.updated_at) {
    return '-'
  }
  return new Date(status.value.updated_at).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
})

function percentText(value: number): string {
  if (!Number.isFinite(value)) {
    return '-'
  }
  const safeValue = clampPercent(value)
  return `${Number.isInteger(safeValue) ? safeValue.toFixed(0) : safeValue.toFixed(1)}%`
}

function clampPercent(value: number): number {
  if (value < 0) {
    return 0
  }
  if (value > 100) {
    return 100
  }
  return value
}

async function fetchStatus() {
  loading.value = true
  loadError.value = false
  try {
    const response = await claudePoolAPI.getStatus()
    status.value = response.data
  } catch {
    loadError.value = true
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchStatus()
  refreshTimer = setInterval(fetchStatus, 60000)
})

onUnmounted(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
  }
})
</script>

<style scoped>
.tabular-nums {
  font-variant-numeric: tabular-nums;
}

.skeleton {
  border-radius: 8px;
  background: linear-gradient(90deg, #e2e8f0 25%, #f8fafc 50%, #e2e8f0 75%);
  background-size: 200% 100%;
  animation: shimmer 1.4s ease-in-out infinite;
}

:global(.dark) .skeleton {
  background: linear-gradient(90deg, #1f2937 25%, #334155 50%, #1f2937 75%);
  background-size: 200% 100%;
}

@keyframes shimmer {
  0% {
    background-position: -200% 0;
  }
  100% {
    background-position: 200% 0;
  }
}
</style>
