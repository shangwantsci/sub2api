<template>
  <div class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-400" data-testid="group-claude-policy-card">
    <div>
      <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ t('admin.groups.anthropicPolicies.title') }}
      </h4>
      <p class="input-hint">{{ t('admin.groups.hatCard.hatHint') }}</p>
    </div>

    <!-- 帽子单选：每顶一个容器；降级分组的下拉放在 radio 按钮之外、同一个容器内（按钮里不能嵌交互元素） -->
    <div role="radiogroup" :aria-label="t('admin.groups.hatCard.hatTitle')" class="space-y-2">
      <div
        v-for="id in HAT_IDS"
        :key="id"
        :class="[
          'rounded-lg border p-3 transition-colors',
          id === hat
            ? 'border-primary-500 bg-primary-50 dark:border-primary-400 dark:bg-primary-900/20'
            : 'border-gray-200 dark:border-dark-500'
        ]"
      >
        <button
          type="button"
          role="radio"
          :aria-checked="id === hat ? 'true' : 'false'"
          :disabled="HAT_META[id].requiresFallback && fallbackDisabled"
          :data-testid="`hat-option-${id}`"
          class="flex w-full items-start gap-3 rounded-md text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 disabled:cursor-not-allowed disabled:opacity-60"
          @click="pick(id)"
        >
          <span :class="[HAT_META[id].badgeClass, 'shrink-0 whitespace-nowrap']">{{ t(HAT_META[id].shortKey) }}</span>
          <span class="min-w-0 flex-1">
            <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t(HAT_META[id].nameKey) }}</span>
            <span class="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">{{ t(HAT_META[id].purposeKey) }}</span>
          </span>
        </button>

        <!-- 降级池：下拉始终显示（不管是否选中），选了分组才生效 -->
        <div v-if="HAT_META[id].requiresFallback" class="mt-3">
          <label class="input-label">{{ t('admin.groups.claudeCode.fallbackGroup') }}</label>
          <Select
            data-testid="hat-fallback-select"
            :model-value="fallbackSelectValue"
            :options="fallbackSelectOptions"
            :placeholder="t('admin.groups.claudeCode.fallbackGroup')"
            :disabled="fallbackDisabled"
            @update:model-value="onFallbackSelect"
          />
          <p v-if="fallbackLoading" data-testid="hat-fallback-loading" class="input-hint">
            {{ t('common.loading') }}
          </p>
          <p
            v-else-if="fallbackOptions.length === 0 && !fallbackSelected"
            data-testid="hat-fallback-empty"
            class="input-hint text-amber-600 dark:text-amber-400"
          >
            {{ t('admin.groups.hatCard.fallbackNoCandidates') }}
          </p>
          <p v-else-if="!fallbackSelected" data-testid="hat-fallback-pick-hint" class="input-hint">
            {{ t('admin.groups.hatCard.fallbackPickHint') }}
          </p>
          <p
            v-if="fallbackIsDangling"
            data-testid="hat-dangling-fallback"
            class="input-hint text-red-600 dark:text-red-400"
          >
            {{ t('admin.groups.hatCard.danglingFallbackNotice') }}
          </p>
        </div>

        <!-- 选中的帽子展开：强度与限制、暴露面提示、降级流量的提示词策略 -->
        <div
          v-if="id === hat"
          class="mt-3 space-y-3 border-t border-primary-200 pt-3 dark:border-primary-800/50"
        >
          <p data-testid="hat-detail" class="text-xs leading-relaxed text-gray-600 dark:text-gray-300">
            {{ t(meta.detailKey) }}
          </p>
          <p
            v-if="meta.family === 'open'"
            data-testid="hat-exposure"
            class="text-xs text-amber-600 dark:text-amber-400"
          >
            {{ t('admin.groups.hatCard.exposureNotice') }}
          </p>
          <div v-if="meta.showsPolicySelect" class="flex items-center justify-between gap-4">
            <div>
              <label class="input-label mb-0" :for="fieldId('fallback-policy')">
                {{ t('admin.groups.hatCard.fallbackPolicyLabel') }}
              </label>
              <p class="input-hint">{{ t('admin.groups.hatCard.fallbackPolicyHint') }}</p>
            </div>
            <select
              :id="fieldId('fallback-policy')"
              v-model="systemPromptPolicy"
              data-testid="hat-fallback-policy-select"
              class="input w-48 flex-shrink-0 text-sm"
            >
              <option v-for="option in policyOptions" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </div>
        </div>
      </div>
    </div>

    <!-- 将写入：就是保存时实际提交的三个字段值 -->
    <p class="font-mono text-xs text-gray-600 dark:text-gray-300" data-testid="hat-will-write">
      <span class="font-bold">{{ t('admin.groups.hatCard.willWrite') }}</span>
      {{ t('admin.groups.hatCard.willWriteLine', willWriteParams) }}
    </p>

    <!-- 数据边界：只提示，不自动改写 -->
    <div
      v-if="description.staleFallback"
      data-testid="hat-stale-fallback"
      class="flex items-start justify-between gap-3 rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-400"
    >
      <span>{{ t('admin.groups.hatCard.staleFallbackNotice', { name: fallbackFieldName }) }}</span>
      <button
        type="button"
        data-testid="hat-stale-fallback-clear"
        class="flex-shrink-0 rounded-md border border-amber-300 px-2 py-1 font-medium hover:bg-amber-100 dark:border-amber-700 dark:hover:bg-amber-900/30"
        @click="clearStaleFallback"
      >
        {{ t('admin.groups.hatCard.clearStaleFallback') }}
      </button>
    </div>
    <div
      v-if="referrers.length > 0 && meta.family === 'cc'"
      data-testid="hat-referrers"
      class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-400"
    >
      {{ t('admin.groups.hatCard.referrersNotice', { names: referrerNames }) }}
    </div>

    <!-- 风控中心内容审计：与帽子无关 -->
    <div class="border-t border-gray-200 pt-4 dark:border-dark-500">
      <div class="flex items-center justify-between gap-4">
        <div>
          <label class="input-label mb-0" :for="fieldId('content-review')">
            {{ t('admin.groups.anthropicPolicies.contentReview') }}
            <span class="ml-1 text-xs font-normal text-gray-400 dark:text-gray-500">
              {{ t('admin.groups.hatCard.contentReviewNote') }}
            </span>
          </label>
          <p class="input-hint">{{ t('admin.groups.anthropicPolicies.contentReviewHint') }}</p>
        </div>
        <select
          :id="fieldId('content-review')"
          v-model="contentReviewPolicy"
          data-testid="content-review-select"
          class="input w-32 flex-shrink-0 text-sm"
        >
          <option v-for="option in contentReviewOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 「Claude 策略」卡（创建与编辑分组共用）：6 顶帽子的单选 + 降级分组 + 降级流量的提示词策略 + 内容审计。
 *
 * 完全派生：没有本地状态、没有 watch、没有 validate、没有 defineExpose。三个字段只通过 v-model 读写，
 * 当前帽子永远由 describeHat 从字段值反推，所以外部重置字段（弹窗重置、平台切换）时界面立刻跟着变；
 * 挂载时绝不写任何字段。
 * 选帽子 = pick：先用 applyHat 一次算出三个字段的新值，再一次性赋值（defineModel 在父级 v-model 下
 * 写后立刻读回的是旧值，所以不能「写一个、读一个」）。降级池必须显式选一个降级分组才生效；
 * 写进去的降级分组永远是 null 或正整数，绝不写 0（更新接口把 0 当「清除」的映射在 GroupsView 的 handleUpdateGroup）。
 * 数据边界（残留的降级分组、已停用或已删除的降级分组、被其它分组当作降级目标）只提示，不自动改写。
 * 「策略 -> 词条」的映射只在 groupHats.ts 的 POLICY_LABEL_KEYS，这里不再写一份。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import type { ClaudeOAuthSystemPromptPolicy, GroupPolicy } from '@/types'
import {
  HAT_IDS,
  HAT_META,
  POLICY_LABEL_KEYS,
  applyHat,
  describeHat,
  hasFallback,
  type HatId
} from './groupHats'

const props = withDefaults(
  defineProps<{
    /** 已过滤好的降级候选：anthropic、active、非 CC-only、非自身 */
    fallbackOptions: { id: number; name: string }[]
    /** 候选还在加载：降级池禁用，不显示「没有可用」 */
    fallbackLoading?: boolean
    /** 残留 / 悬空的降级分组与「将写入」行取名用；查不到显示 #id */
    resolveGroupName?: (id: number) => string | undefined
    /** 编辑时把本分组当作降级目标的其它分组 */
    referrers?: { id: number; name: string }[]
    /** 创建与编辑两个弹窗并存时，用来避免 id 重复 */
    idPrefix?: string
  }>(),
  { fallbackLoading: false, referrers: () => [], idPrefix: 'claude-policy' }
)

const claudeCodeOnly = defineModel<boolean>('claudeCodeOnly', { default: false })
const fallbackGroupId = defineModel<number | null>('fallbackGroupId', { default: null })
const systemPromptPolicy = defineModel<ClaudeOAuthSystemPromptPolicy>('systemPromptPolicy', { default: 'inherit' })
const contentReviewPolicy = defineModel<GroupPolicy>('contentReviewPolicy', { default: 'inherit' })

const { t } = useI18n()

const fieldId = (name: string) => `${props.idPrefix}-${name}`

const current = computed(() => ({
  claude_code_only: claudeCodeOnly.value,
  fallback_group_id: fallbackGroupId.value,
  claude_oauth_system_prompt_policy: systemPromptPolicy.value
}))
const description = computed(() => describeHat(current.value))
const hat = computed(() => description.value.hat)
const meta = computed(() => HAT_META[hat.value])

/** 当前选中的帽子是否用降级分组（目前只有 cc_fallback）。 */
const fallbackSelected = computed(() => meta.value.requiresFallback)

// 降级分组的显示名：先查候选（全量列表），再用调用方给的 resolveGroupName（残留 / 悬空的分组不在候选里），都查不到显示 #id
function groupName(id: number): string {
  return props.fallbackOptions.find((option) => option.id === id)?.name ?? props.resolveGroupName?.(id) ?? `#${id}`
}
const fallbackFieldName = computed(() => {
  const id = description.value.fields.fallback_group_id
  return id === null ? null : groupName(id)
})

// 悬空：当前是降级池，但降级分组不在候选里（已停用或已删除）；候选还在加载时无法判断
const fallbackIsDangling = computed(() => {
  const id = fallbackGroupId.value
  return fallbackSelected.value && id !== null && !props.fallbackLoading && !props.fallbackOptions.some((option) => option.id === id)
})

// 下拉的值：开放帽下表单里可能留着残留的降级分组，不能当成选择显示
const fallbackSelectValue = computed<number | null>(() => (fallbackSelected.value ? fallbackGroupId.value : null))

// 「不降级」只在当前已是降级池时才有；开放帽下让占位符显示
const fallbackSelectOptions = computed(() => {
  const options: Array<{ value: number | null; label: string }> = []
  if (fallbackSelected.value) {
    options.push({ value: null, label: t('admin.groups.claudeCode.noFallback') })
  }
  for (const option of props.fallbackOptions) {
    options.push({ value: option.id, label: option.name })
  }
  const danglingId = fallbackGroupId.value
  if (fallbackIsDangling.value && danglingId !== null) {
    options.push({ value: danglingId, label: t('admin.groups.hatCard.danglingFallbackLabel', { id: danglingId }) })
  }
  return options
})

const fallbackDisabled = computed(
  () => props.fallbackLoading || (props.fallbackOptions.length === 0 && !fallbackSelected.value)
)

// 键的顺序就是下拉里的顺序
const policyOptions = computed(() =>
  (Object.keys(POLICY_LABEL_KEYS) as ClaudeOAuthSystemPromptPolicy[]).map((value) => ({
    value,
    label: t(POLICY_LABEL_KEYS[value])
  }))
)

const CONTENT_REVIEW_POLICIES: GroupPolicy[] = ['inherit', 'enabled', 'disabled']
const contentReviewOptions = computed(() =>
  CONTENT_REVIEW_POLICIES.map((value) => ({ value, label: t(POLICY_LABEL_KEYS[value]) }))
)

const willWriteParams = computed(() => ({
  cc: String(description.value.fields.claude_code_only),
  fallback: fallbackFieldName.value ?? t('admin.groups.hatCard.valueNone'),
  policy: description.value.fields.claude_oauth_system_prompt_policy
}))

// 名字用 ', ' 连接：这里分不出语言，统一一种分隔符
const referrerNames = computed(() => props.referrers.map((group) => group.name).join(', '))

function pick(id: HatId, fallbackId?: number | null) {
  // 降级池必须显式给出降级分组才生效，没给就不写任何字段：点降级池卡片本身（不带 id）也走这里，
  // 此时连归一后的当前值都不写回，避免把库里的怪值悄悄改掉
  if (HAT_META[id].requiresFallback && !hasFallback(fallbackId)) return
  const next = applyHat(id, current.value, fallbackId)
  claudeCodeOnly.value = next.claude_code_only
  fallbackGroupId.value = next.fallback_group_id
  systemPromptPolicy.value = next.claude_oauth_system_prompt_policy
}

function onFallbackSelect(value: unknown) {
  const id = hasFallback(value) ? value : null
  if (id !== null) {
    pick('cc_fallback', id)
  } else if (fallbackSelected.value) {
    // 选了「不降级」：落到 CC 专线，降级分组写 null
    pick('cc_line')
  }
}

// 只把残留的降级分组清成 null，不动别的字段
function clearStaleFallback() {
  fallbackGroupId.value = null
}
</script>
