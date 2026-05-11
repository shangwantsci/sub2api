import { onMounted, onUnmounted, ref } from 'vue'
import { claudePoolAPI, type ClaudePoolStatus } from '@/api/claudePool'

const DYNAMIC_GROUP_NAME = 'claude满血默认'
const BASE_COEFFICIENT = 0.8
const REFRESH_INTERVAL_MS = 60_000

const claudePoolStatus = ref<ClaudePoolStatus | null>(null)
let refreshTimer: ReturnType<typeof setInterval> | null = null
let consumerCount = 0
let inflight: Promise<void> | null = null
let lastLoadedAt = 0

export interface ClaudePoolDynamicRateInput {
  name: string
  rateMultiplier?: number | null
  userRateMultiplier?: number | null
}

export interface ClaudePoolDynamicRateInfo {
  isDynamicGroup: boolean
  showDynamicRate: boolean
  baseRate: number | null
  actualRate: number | null
  baseLabel: string
  actualLabel: string
  coefficient: number | null
}

export function useClaudePoolDynamicRate() {
  onMounted(() => {
    consumerCount += 1
    void loadClaudePoolStatus()
    if (!refreshTimer) {
      refreshTimer = setInterval(() => {
        void loadClaudePoolStatus(true)
      }, REFRESH_INTERVAL_MS)
    }
  })

  onUnmounted(() => {
    consumerCount = Math.max(0, consumerCount - 1)
    if (consumerCount === 0 && refreshTimer) {
      clearInterval(refreshTimer)
      refreshTimer = null
    }
  })

  return {
    claudePoolStatus,
    refreshClaudePoolStatus: loadClaudePoolStatus,
  }
}

export function resolveClaudePoolDynamicRate(
  input: ClaudePoolDynamicRateInput,
  snapshot: ClaudePoolStatus | null = claudePoolStatus.value
): ClaudePoolDynamicRateInfo {
  const baseRate = resolveBaseRate(input.rateMultiplier, input.userRateMultiplier)
  const baseLabel = baseRate === null ? '' : formatMultiplier(baseRate)
  const isDynamicGroup = input.name.trim() === DYNAMIC_GROUP_NAME
  const coefficient = snapshot?.coefficient ?? null

  if (
    !isDynamicGroup ||
    baseRate === null ||
    !snapshot ||
    snapshot.status !== 'fresh' ||
    snapshot.stale ||
    coefficient === null ||
    !Number.isFinite(coefficient) ||
    coefficient <= 0
  ) {
    return {
      isDynamicGroup,
      showDynamicRate: false,
      baseRate,
      actualRate: baseRate,
      baseLabel,
      actualLabel: baseLabel,
      coefficient,
    }
  }

  const actualRate = roundMultiplier(baseRate * coefficient / BASE_COEFFICIENT)
  const showDynamicRate = Math.abs(actualRate - baseRate) > 0.000001

  return {
    isDynamicGroup,
    showDynamicRate,
    baseRate,
    actualRate,
    baseLabel,
    actualLabel: formatMultiplier(actualRate),
    coefficient,
  }
}

async function loadClaudePoolStatus(force = false): Promise<void> {
  if (inflight) {
    return inflight
  }
  if (!force && claudePoolStatus.value && Date.now() - lastLoadedAt < REFRESH_INTERVAL_MS) {
    return
  }

  inflight = claudePoolAPI
    .getStatus()
    .then(response => {
      claudePoolStatus.value = response.data
      lastLoadedAt = Date.now()
    })
    .catch(() => {
      // Keep the last known status; the backend billing path also falls back when status is stale.
    })
    .finally(() => {
      inflight = null
    })

  return inflight
}

function resolveBaseRate(rateMultiplier?: number | null, userRateMultiplier?: number | null): number | null {
  const defaultRate = finiteNumberOrNull(rateMultiplier)
  const userRate = finiteNumberOrNull(userRateMultiplier)
  if (userRate !== null && (defaultRate === null || Math.abs(userRate - defaultRate) > 0.000001)) {
    return userRate
  }
  return defaultRate
}

function finiteNumberOrNull(value?: number | null): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function formatMultiplier(value: number): string {
  const rounded = roundMultiplier(value)
  return `${rounded.toFixed(4).replace(/\.?0+$/, '')}x`
}

function roundMultiplier(value: number): number {
  return Math.round(value * 10000) / 10000
}

export function __resetClaudePoolDynamicRateForTests() {
  claudePoolStatus.value = null
  lastLoadedAt = 0
  inflight = null
  consumerCount = 0
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}
