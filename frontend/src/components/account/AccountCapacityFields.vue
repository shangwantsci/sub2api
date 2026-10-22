<script setup lang="ts">
/**
 * 「容量与节流」卡：新建与编辑 Claude OAuth / SetupToken 账号共用的无状态展示。
 *
 * 所有状态都在弹窗 setup 里的 useAccountCapacity 中，这里只通过 v-model 读写，不持有任何状态；
 * 读写 extra 的键一律走 capacityBuilder，这里不出现任何 extra 键。
 * 档位下拉是「动作 + 派生显示」而不是 v-model：显示 matchedKey ?? 自定义，选中后触发 pick，
 * 手改任何数值后下拉自动回到「自定义」。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import type { CapacityPreset } from '@/api/admin/settings'
import {
  presetOptionLabel,
  toPositive,
  type NumberInput,
  type RpmStrategy
} from '@/components/account/capacityBuilder'

const CUSTOM_KEY = 'custom'

const props = withDefaults(
  defineProps<{
    presets: CapacityPreset[]
    /** 模板已加载且非空。未加载时不显示档位下拉，绝不显示「自定义」。 */
    presetsReady: boolean
    presetsFailed?: boolean
    matchedKey: string | null
    nurturingGlobalEnabled: boolean
    idPrefix?: string
  }>(),
  { presetsFailed: false, idPrefix: 'capacity' }
)

const emit = defineEmits<{
  pick: [key: string]
  /** 用户碰了卡里的任意输入控件（用于「只在用户没碰过时才自动预选默认档位」）。 */
  touch: []
}>()

const concurrency = defineModel<number>('concurrency', { required: true })
const maxSessions = defineModel<NumberInput>('maxSessions', { required: true })
const sessionIdleTimeout = defineModel<NumberInput>('sessionIdleTimeout', { required: true })
const windowCostLimit = defineModel<NumberInput>('windowCostLimit', { required: true })
const windowCostStickyReserve = defineModel<NumberInput>('windowCostStickyReserve', { required: true })
const baseRpm = defineModel<NumberInput>('baseRpm', { required: true })
const rpmStrategy = defineModel<RpmStrategy>('rpmStrategy', { required: true })
const rpmStickyBuffer = defineModel<NumberInput>('rpmStickyBuffer', { required: true })
const userMsgQueueMode = defineModel<string>('userMsgQueueMode', { required: true })
const adaptiveThrottle = defineModel<boolean>('adaptiveThrottle', { required: true })
const nurturing = defineModel<boolean>('nurturing', { required: true })

const { t } = useI18n()

const fieldId = (name: string) => `${props.idPrefix}-${name}`

const sessionsOn = computed(() => toPositive(maxSessions.value) > 0)
const windowCostOn = computed(() => toPositive(windowCostLimit.value) > 0)
const rpmOn = computed(() => toPositive(baseRpm.value) > 0)

const umqModeOptions = computed(() => [
  { value: '', label: t('admin.accounts.quotaControl.rpmLimit.umqModeOff') },
  { value: 'throttle', label: t('admin.accounts.quotaControl.rpmLimit.umqModeThrottle') },
  { value: 'serialize', label: t('admin.accounts.quotaControl.rpmLimit.umqModeSerialize') }
])

function onPresetChange(event: Event) {
  const key = (event.target as HTMLSelectElement).value
  // 「自定义」只是当前状态的显示，选它不做任何事
  if (key === CUSTOM_KEY) return
  emit('pick', key)
}

// 并发至少为 1（后端把 0 当成不限并发，填 0 的效果与直觉相反），清空或填 0 立刻纠回 1。
// 不能在 v-model 之后再读 concurrency.value：父组件重渲染前它还是旧值，所以直接读输入框。
function onConcurrencyInput(event: Event) {
  const input = event.target as HTMLInputElement
  const next = Math.max(1, Math.floor(Number(input.value)) || 1)
  concurrency.value = next
  if (input.value !== String(next)) input.value = String(next)
}

function onInteract(event: Event) {
  const target = event.target as HTMLElement | null
  if (target?.closest('input, select, button, textarea')) {
    emit('touch')
  }
}
</script>

<template>
  <div
    class="rounded-lg border border-gray-200 p-4 dark:border-dark-600"
    data-testid="account-capacity-card"
    @input="onInteract"
    @change="onInteract"
    @click="onInteract"
  >
    <div class="mb-4">
      <h3 class="input-label mb-0 text-base font-semibold">
        {{ t('admin.accounts.capacityFields.title') }}
      </h3>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.capacityFields.hint') }}
      </p>
    </div>

    <!-- 档位：复制写入，不存档位名 -->
    <div class="mb-4">
      <label class="input-label" :for="fieldId('preset')">
        {{ t('admin.accounts.capacityFields.preset') }}
      </label>
      <select
        v-if="presetsReady"
        :id="fieldId('preset')"
        data-testid="capacity-preset-select"
        class="input"
        :value="matchedKey ?? CUSTOM_KEY"
        @change="onPresetChange"
      >
        <option v-for="preset in presets" :key="preset.key" :value="preset.key">
          {{ presetOptionLabel(preset, t) }}
        </option>
        <option v-if="matchedKey === null" :value="CUSTOM_KEY">
          {{ t('admin.accounts.capacityFields.presetCustom') }}
        </option>
      </select>
      <p
        v-else-if="presetsFailed"
        data-testid="capacity-presets-unavailable"
        class="input-hint text-amber-600 dark:text-amber-400"
      >
        {{ t('admin.accounts.capacityFields.presetsUnavailable') }}
      </p>
      <p v-else class="input-hint">{{ t('common.loading') }}</p>
      <p v-if="presetsReady" class="input-hint">{{ t('admin.accounts.capacityFields.presetHint') }}</p>
    </div>

    <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <div>
        <label class="input-label" :for="fieldId('concurrency')">
          {{ t('admin.accounts.concurrency') }}
        </label>
        <input
          :id="fieldId('concurrency')"
          :value="concurrency"
          data-testid="capacity-concurrency"
          type="number"
          min="1"
          step="1"
          class="input"
          @input="onConcurrencyInput"
        />
      </div>
      <div>
        <label class="input-label" :for="fieldId('max-sessions')">
          {{ t('admin.accounts.quotaControl.sessionLimit.maxSessions') }}
        </label>
        <input
          :id="fieldId('max-sessions')"
          v-model.number="maxSessions"
          data-testid="capacity-max-sessions"
          type="number"
          min="0"
          step="1"
          class="input"
          placeholder="0"
        />
        <p class="input-hint">{{ t('admin.accounts.quotaControl.sessionLimit.maxSessionsHint') }}</p>
      </div>
      <div>
        <label class="input-label" :for="fieldId('base-rpm')">
          {{ t('admin.accounts.quotaControl.rpmLimit.baseRpm') }}
        </label>
        <input
          :id="fieldId('base-rpm')"
          v-model.number="baseRpm"
          data-testid="capacity-base-rpm"
          type="number"
          min="0"
          max="1000"
          step="1"
          class="input"
          placeholder="0"
        />
        <p class="input-hint">{{ t('admin.accounts.quotaControl.rpmLimit.baseRpmHint') }}</p>
      </div>
      <div>
        <label class="input-label" :for="fieldId('window-cost')">
          {{ t('admin.accounts.quotaControl.windowCost.label') }}
        </label>
        <div class="relative">
          <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500 dark:text-gray-400">$</span>
          <input
            :id="fieldId('window-cost')"
            v-model.number="windowCostLimit"
            data-testid="capacity-window-cost"
            type="number"
            min="0"
            step="1"
            class="input pl-7"
            placeholder="0"
          />
        </div>
        <p class="input-hint">{{ t('admin.accounts.quotaControl.windowCost.limitHint') }}</p>
      </div>
    </div>

    <!-- 只有对应限制开着（> 0）时才出现的从属项 -->
    <div v-if="sessionsOn || windowCostOn" class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
      <div v-if="sessionsOn">
        <label class="input-label" :for="fieldId('idle-timeout')">
          {{ t('admin.accounts.quotaControl.sessionLimit.idleTimeout') }}
        </label>
        <div class="relative">
          <input
            :id="fieldId('idle-timeout')"
            v-model.number="sessionIdleTimeout"
            type="number"
            min="1"
            step="1"
            class="input pr-12"
            :placeholder="t('admin.accounts.quotaControl.sessionLimit.idleTimeoutPlaceholder')"
          />
          <span class="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 dark:text-gray-400">
            {{ t('common.minutes') }}
          </span>
        </div>
        <p class="input-hint">{{ t('admin.accounts.quotaControl.sessionLimit.idleTimeoutHint') }}</p>
      </div>
      <div v-if="windowCostOn">
        <label class="input-label" :for="fieldId('sticky-reserve')">
          {{ t('admin.accounts.quotaControl.windowCost.stickyReserve') }}
        </label>
        <div class="relative">
          <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500 dark:text-gray-400">$</span>
          <input
            :id="fieldId('sticky-reserve')"
            v-model.number="windowCostStickyReserve"
            type="number"
            min="0"
            step="1"
            class="input pl-7"
            :placeholder="t('admin.accounts.quotaControl.windowCost.stickyReservePlaceholder')"
          />
        </div>
        <p class="input-hint">{{ t('admin.accounts.quotaControl.windowCost.stickyReserveHint') }}</p>
      </div>
    </div>

    <div v-if="rpmOn" class="mt-4 space-y-4">
      <div>
        <label class="input-label">{{ t('admin.accounts.quotaControl.rpmLimit.strategy') }}</label>
        <div class="flex gap-2">
          <button
            type="button"
            :class="[
              'flex-1 rounded-lg px-3 py-2 text-sm font-medium transition-all',
              rpmStrategy === 'tiered'
                ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
            ]"
            @click="rpmStrategy = 'tiered'"
          >
            <div class="text-center">
              <div>{{ t('admin.accounts.quotaControl.rpmLimit.strategyTiered') }}</div>
              <div class="mt-0.5 text-[10px] opacity-70">
                {{ t('admin.accounts.quotaControl.rpmLimit.strategyTieredHint') }}
              </div>
            </div>
          </button>
          <button
            type="button"
            :class="[
              'flex-1 rounded-lg px-3 py-2 text-sm font-medium transition-all',
              rpmStrategy === 'sticky_exempt'
                ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-400 dark:hover:bg-dark-500'
            ]"
            @click="rpmStrategy = 'sticky_exempt'"
          >
            <div class="text-center">
              <div>{{ t('admin.accounts.quotaControl.rpmLimit.strategyStickyExempt') }}</div>
              <div class="mt-0.5 text-[10px] opacity-70">
                {{ t('admin.accounts.quotaControl.rpmLimit.strategyStickyExemptHint') }}
              </div>
            </div>
          </button>
        </div>
      </div>
      <div v-if="rpmStrategy === 'tiered'">
        <label class="input-label" :for="fieldId('sticky-buffer')">
          {{ t('admin.accounts.quotaControl.rpmLimit.stickyBuffer') }}
        </label>
        <input
          :id="fieldId('sticky-buffer')"
          v-model.number="rpmStickyBuffer"
          type="number"
          min="1"
          step="1"
          class="input"
          :placeholder="t('admin.accounts.quotaControl.rpmLimit.stickyBufferPlaceholder')"
        />
        <p class="input-hint">{{ t('admin.accounts.quotaControl.rpmLimit.stickyBufferHint') }}</p>
      </div>
    </div>

    <!-- 动态降速：作用于账号自己的并发与 RPM -->
    <div class="mt-5 flex items-center justify-between gap-4 border-t border-gray-100 pt-4 dark:border-dark-700">
      <div>
        <label class="input-label mb-0">{{ t('admin.accounts.capacityFields.adaptiveThrottle') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.capacityFields.adaptiveThrottleHint') }}
        </p>
      </div>
      <Toggle v-model="adaptiveThrottle" data-testid="capacity-adaptive-toggle" />
    </div>

    <!-- 用户消息限速 -->
    <div class="mt-5 border-t border-gray-100 pt-4 dark:border-dark-700">
      <label class="input-label">{{ t('admin.accounts.quotaControl.rpmLimit.userMsgQueue') }}</label>
      <p class="mb-2 mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.quotaControl.rpmLimit.userMsgQueueHint') }}
      </p>
      <div class="flex space-x-2">
        <button
          v-for="option in umqModeOptions"
          :key="option.value"
          type="button"
          :class="[
            'rounded-md border px-3 py-1.5 text-sm transition-colors',
            userMsgQueueMode === option.value
              ? 'border-primary-600 bg-primary-600 text-white'
              : 'border-gray-300 bg-white text-gray-700 hover:bg-gray-50 dark:border-dark-500 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600'
          ]"
          @click="userMsgQueueMode = option.value"
        >
          {{ option.label }}
        </button>
      </div>
    </div>

    <!-- 养号：全局总开关关闭时置灰 -->
    <div class="mt-5 flex items-center justify-between gap-4 border-t border-gray-100 pt-4 dark:border-dark-700">
      <div>
        <label class="input-label mb-0">{{ t('admin.accounts.capacityFields.nurturing') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.capacityFields.nurturingHint') }}
        </p>
        <p
          v-if="!nurturingGlobalEnabled"
          data-testid="capacity-nurturing-global-off"
          class="mt-1 text-xs text-amber-600 dark:text-amber-400"
        >
          {{ t('admin.accounts.capacityFields.nurturingGlobalOff') }}
        </p>
      </div>
      <Toggle
        v-model="nurturing"
        data-testid="capacity-nurturing-toggle"
        :disabled="!nurturingGlobalEnabled"
      />
    </div>
  </div>
</template>
