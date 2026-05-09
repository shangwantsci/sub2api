<template>
  <div class="min-h-screen bg-slate-50 text-slate-950 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-slate-200 bg-white/95 dark:border-dark-800 dark:bg-dark-900/95">
      <div class="mx-auto flex max-w-6xl items-center justify-between gap-4 px-4 py-4 sm:px-6">
        <RouterLink to="/home" class="flex min-w-0 items-center gap-3">
          <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center overflow-hidden rounded-lg bg-white shadow-sm ring-1 ring-slate-200 dark:bg-dark-800 dark:ring-dark-700">
            <img src="/logo.png" alt="Logo" class="h-full w-full object-contain" />
          </span>
          <span class="truncate text-base font-semibold tracking-normal text-slate-950 dark:text-white">
            Sub2API
          </span>
        </RouterLink>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="inline-flex h-10 w-10 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-600 transition hover:border-teal-300 hover:text-teal-700 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300 dark:hover:border-teal-500/60 dark:hover:text-teal-200"
            :disabled="loading"
            title="刷新"
            @click="fetchStatus"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          </button>
          <RouterLink
            to="/login"
            class="inline-flex h-10 items-center justify-center rounded-lg bg-teal-600 px-4 text-sm font-semibold text-white transition hover:bg-teal-700"
          >
            登录
          </RouterLink>
        </div>
      </div>
    </header>

    <main class="mx-auto max-w-6xl px-4 py-8 sm:px-6 lg:py-10">
      <section class="mb-6 flex flex-col gap-4 border-b border-slate-200 pb-6 dark:border-dark-800 md:flex-row md:items-end md:justify-between">
        <div>
          <div class="mb-3 inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-sm font-medium" :class="statusBadgeClass">
            <span class="h-2 w-2 rounded-full" :class="statusDotClass"></span>
            {{ statusText }}
          </div>
          <h1 class="text-3xl font-bold tracking-normal text-slate-950 dark:text-white">
            Claude 号池状态
          </h1>
          <p class="mt-2 max-w-2xl text-sm leading-6 text-slate-600 dark:text-dark-300">
            当前展示 Derouter Claude 模型池的公开负载、空闲率和动态系数。
          </p>
        </div>
        <div class="text-sm text-slate-500 dark:text-dark-400">
          <span v-if="status?.updated_at">更新于 {{ updatedAtText }}</span>
          <span v-else>等待数据</span>
        </div>
      </section>

      <div v-if="loading && !status" class="grid gap-4 md:grid-cols-3">
        <div v-for="item in 3" :key="item" class="h-36 rounded-lg border border-slate-200 bg-white p-5 dark:border-dark-800 dark:bg-dark-900">
          <div class="skeleton h-4 w-24"></div>
          <div class="skeleton mt-8 h-10 w-32"></div>
          <div class="skeleton mt-5 h-3 w-full"></div>
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
        <section class="grid gap-4 md:grid-cols-3">
          <div class="rounded-lg border border-slate-200 bg-white p-5 dark:border-dark-800 dark:bg-dark-900">
            <div class="flex items-center justify-between gap-3">
              <span class="text-sm font-medium text-slate-500 dark:text-dark-400">动态系数</span>
              <span class="rounded-lg bg-teal-50 p-2 text-teal-700 dark:bg-teal-500/10 dark:text-teal-200">
                <Icon name="calculator" size="sm" />
              </span>
            </div>
            <div class="mt-5 text-4xl font-bold tracking-normal tabular-nums text-slate-950 dark:text-white">
              {{ coefficientText }}
            </div>
            <p class="mt-3 text-sm text-slate-500 dark:text-dark-400">
              {{ status.state_label || '未知状态' }}
            </p>
          </div>

          <div class="rounded-lg border border-slate-200 bg-white p-5 dark:border-dark-800 dark:bg-dark-900">
            <div class="flex items-center justify-between gap-3">
              <span class="text-sm font-medium text-slate-500 dark:text-dark-400">当前负载</span>
              <span class="rounded-lg bg-amber-50 p-2 text-amber-700 dark:bg-amber-500/10 dark:text-amber-200">
                <Icon name="chartBar" size="sm" />
              </span>
            </div>
            <div class="mt-5 flex items-end gap-3">
              <span class="text-4xl font-bold tracking-normal tabular-nums text-slate-950 dark:text-white">
                {{ percentText(status.load_percent) }}
              </span>
              <span class="pb-1 text-sm text-slate-500 dark:text-dark-400">
                空闲 {{ percentText(status.idle_percent) }}
              </span>
            </div>
            <div class="mt-4 h-2 overflow-hidden rounded bg-slate-100 dark:bg-dark-800">
              <div class="h-full rounded bg-amber-500" :style="{ width: loadBarWidth }"></div>
            </div>
          </div>

          <div class="rounded-lg border border-slate-200 bg-white p-5 dark:border-dark-800 dark:bg-dark-900">
            <div class="flex items-center justify-between gap-3">
              <span class="text-sm font-medium text-slate-500 dark:text-dark-400">参考模型</span>
              <span class="rounded-lg bg-sky-50 p-2 text-sky-700 dark:bg-sky-500/10 dark:text-sky-200">
                <Icon name="server" size="sm" />
              </span>
            </div>
            <div class="mt-5 break-words text-2xl font-bold tracking-normal text-slate-950 dark:text-white">
              {{ status.selected_model || '-' }}
            </div>
            <a
              :href="status.source_url"
              target="_blank"
              rel="noopener noreferrer"
              class="mt-3 inline-flex items-center gap-1 text-sm font-medium text-teal-700 hover:text-teal-800 dark:text-teal-200 dark:hover:text-teal-100"
            >
              Derouter
              <Icon name="externalLink" size="xs" />
            </a>
          </div>
        </section>

        <section class="mt-6 grid gap-6 lg:grid-cols-[minmax(0,1fr)_360px]">
          <div class="rounded-lg border border-slate-200 bg-white dark:border-dark-800 dark:bg-dark-900">
            <div class="flex items-center justify-between gap-3 border-b border-slate-200 px-5 py-4 dark:border-dark-800">
              <h2 class="text-base font-semibold tracking-normal text-slate-950 dark:text-white">Claude 模型</h2>
              <span class="text-sm text-slate-500 dark:text-dark-400">{{ status.models.length }} 个</span>
            </div>
            <div class="overflow-x-auto">
              <table class="w-full min-w-[680px]">
                <thead>
                  <tr class="border-b border-slate-200 bg-slate-50 text-left text-xs font-semibold uppercase text-slate-500 dark:border-dark-800 dark:bg-dark-950 dark:text-dark-400">
                    <th class="px-5 py-3">模型</th>
                    <th class="px-5 py-3 text-right">输入价</th>
                    <th class="px-5 py-3 text-right">输出价</th>
                    <th class="px-5 py-3 text-right">负载</th>
                    <th class="px-5 py-3 text-right">空闲</th>
                    <th class="px-5 py-3 text-right">系数</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="model in status.models"
                    :key="model.name"
                    class="border-b border-slate-100 last:border-0 dark:border-dark-800"
                  >
                    <td class="px-5 py-4 text-sm font-medium text-slate-900 dark:text-white">{{ model.name }}</td>
                    <td class="px-5 py-4 text-right text-sm tabular-nums text-slate-600 dark:text-dark-300">
                      {{ usdText(model.input_price_usd) }}
                    </td>
                    <td class="px-5 py-4 text-right text-sm tabular-nums text-slate-600 dark:text-dark-300">
                      {{ usdText(model.output_price_usd) }}
                    </td>
                    <td class="px-5 py-4 text-right text-sm tabular-nums text-slate-600 dark:text-dark-300">
                      {{ percentText(model.load_percent) }}
                    </td>
                    <td class="px-5 py-4 text-right text-sm tabular-nums text-slate-600 dark:text-dark-300">
                      {{ percentText(model.idle_percent) }}
                    </td>
                    <td class="px-5 py-4 text-right text-sm font-semibold tabular-nums text-slate-950 dark:text-white">
                      {{ coefficientLabel(model.coefficient) }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div class="rounded-lg border border-slate-200 bg-white dark:border-dark-800 dark:bg-dark-900">
            <div class="border-b border-slate-200 px-5 py-4 dark:border-dark-800">
              <h2 class="text-base font-semibold tracking-normal text-slate-950 dark:text-white">动态定价规则</h2>
            </div>
            <div class="divide-y divide-slate-100 dark:divide-dark-800">
              <div
                v-for="rule in status.pricing_rules"
                :key="rule.idle_range"
                class="flex items-center justify-between gap-4 px-5 py-4"
              >
                <div>
                  <p class="text-sm font-medium text-slate-900 dark:text-white">{{ rule.idle_range }} 空闲</p>
                  <p class="mt-1 text-xs text-slate-500 dark:text-dark-400">{{ rule.label }}</p>
                </div>
                <span class="text-sm font-semibold tabular-nums text-slate-950 dark:text-white">
                  {{ coefficientLabel(rule.coefficient) }}
                </span>
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

const status = ref<ClaudePoolStatus | null>(null)
const loading = ref(false)
const loadError = ref(false)
let refreshTimer: ReturnType<typeof setInterval> | null = null

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

const coefficientText = computed(() => coefficientLabel(status.value?.coefficient ?? 0))

const loadBarWidth = computed(() => `${clampPercent(status.value?.load_percent ?? 0)}%`)

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

function coefficientLabel(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '-'
  }
  return `${value.toFixed(2)}x`
}

function percentText(value: number): string {
  if (!Number.isFinite(value)) {
    return '-'
  }
  const safeValue = clampPercent(value)
  return `${Number.isInteger(safeValue) ? safeValue.toFixed(0) : safeValue.toFixed(1)}%`
}

function usdText(value: number): string {
  if (!Number.isFinite(value)) {
    return '-'
  }
  return `$${value.toFixed(2)}`
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
