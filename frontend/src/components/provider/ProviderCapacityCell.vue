<template>
  <div v-if="showAny" class="flex flex-col gap-0.5">
    <CapacityBadge
      v-if="showConcurrency"
      data-test="occupancy-concurrency"
      :color-class="concurrencyClass"
      :tooltip="concurrencyTooltip"
      :current="currentConcurrency ?? 0"
      :max="account.concurrency"
    >
      <svg class="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18A2.25 2.25 0 0120.25 6v2.25A2.25 2.25 0 0118 10.5h-2.25a2.25 2.25 0 01-2.25-2.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 012.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z"
        />
      </svg>
    </CapacityBadge>

    <CapacityBadge
      v-if="showSessions"
      data-test="occupancy-sessions"
      :color-class="sessionsClass"
      :tooltip="sessionsTooltip"
      :current="currentSessions ?? 0"
      :max="account.max_sessions"
    >
      <svg class="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M15 19.128a9.38 9.38 0 002.625.372 9.337 9.337 0 004.121-.952 4.125 4.125 0 00-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 018.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0111.964-3.07M12 6.375a3.375 3.375 0 11-6.75 0 3.375 3.375 0 016.75 0zm8.25 2.25a2.625 2.625 0 11-5.25 0 2.625 2.625 0 015.25 0z"
        />
      </svg>
    </CapacityBadge>

    <CapacityBadge
      v-if="showRpm"
      data-test="occupancy-rpm"
      :color-class="rpmClass"
      :tooltip="rpmTooltip"
      :current="currentRpm ?? 0"
      :max="account.base_rpm"
    >
      <svg class="h-2.5 w-2.5" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
      </svg>
    </CapacityBadge>
  </div>
  <span v-else class="text-gray-400">-</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import CapacityBadge from '@/components/account/CapacityBadge.vue'
import type { ProviderAccount } from '@/api/provider'

const props = defineProps<{
  account: ProviderAccount
}>()

const { t } = useI18n()

const currentConcurrency = computed(() => props.account.current_concurrency)
const currentSessions = computed(() => props.account.active_sessions)
const currentRpm = computed(() => props.account.current_rpm)

const showConcurrency = computed(
  () => props.account.concurrency > 0 && currentConcurrency.value != null
)
const showSessions = computed(
  () => props.account.max_sessions > 0 && currentSessions.value != null
)
const showRpm = computed(() => props.account.base_rpm > 0 && currentRpm.value != null)
const showAny = computed(() => showConcurrency.value || showSessions.value || showRpm.value)

const occupancyParams = (current: number, max: number) => ({ current, max })

const concurrencyClass = computed(() => {
  const current = currentConcurrency.value ?? 0
  const max = props.account.concurrency
  if (current >= max) return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  if (current > 0) return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  return 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
})

const concurrencyTooltip = computed(() => {
  const current = currentConcurrency.value ?? 0
  const max = props.account.concurrency
  const params = occupancyParams(current, max)
  if (current >= max) return t('provider.accounts.occupancy.concurrencyFull', params)
  if (current > 0) return t('provider.accounts.occupancy.concurrencyBusy', params)
  return t('provider.accounts.occupancy.concurrencyIdle', params)
})

const sessionsClass = computed(() => {
  const current = currentSessions.value ?? 0
  const max = props.account.max_sessions
  if (current >= max) return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
  if (current >= max * 0.8) return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
})

const sessionsTooltip = computed(() => {
  const current = currentSessions.value ?? 0
  const max = props.account.max_sessions
  const params = occupancyParams(current, max)
  if (current >= max) return t('provider.accounts.occupancy.sessionsFull', params)
  if (current >= max * 0.8) return t('provider.accounts.occupancy.sessionsApproaching', params)
  return t('provider.accounts.occupancy.sessionsIdle', params)
})

const rpmClass = computed(() => {
  const current = currentRpm.value ?? 0
  const max = props.account.base_rpm
  if (current >= max) return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
  if (current >= max * 0.8) return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
  return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
})

const rpmTooltip = computed(() => {
  const current = currentRpm.value ?? 0
  const max = props.account.base_rpm
  const params = occupancyParams(current, max)
  if (current >= max) return t('provider.accounts.occupancy.rpmAtLimit', params)
  if (current >= max * 0.8) return t('provider.accounts.occupancy.rpmApproaching', params)
  return t('provider.accounts.occupancy.rpmIdle', params)
})
</script>
