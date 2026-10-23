<template>
  <span v-if="group.platform !== 'anthropic'" class="text-xs text-gray-400" data-testid="group-hat-none">—</span>
  <span
    v-else
    :class="[meta.badgeClass, 'whitespace-nowrap']"
    :title="titleText"
    data-testid="group-hat-badge"
    :data-hat="hat"
  >{{ t(meta.shortKey) }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { HAT_META, POLICY_LABEL_KEYS, describeHat } from './groupHats'

// 列表行 / 表单对象 / 缺字段的 mock 都能直接传；帽子的反推与字段判断全部交给 describeHat
interface HatGroupLike {
  platform: string
  claude_code_only?: boolean | null
  fallback_group_id?: number | null
  claude_oauth_system_prompt_policy?: string | null
}

const props = defineProps<{
  group: HatGroupLike
  /** 按 id 取分组名；查不到（不在当前页、已删除）时标题里显示 #id */
  resolveGroupName?: (id: number) => string | undefined
}>()

const { t } = useI18n()

const description = computed(() => describeHat(props.group))
const hat = computed(() => description.value.hat)
const meta = computed(() => HAT_META[hat.value])

// 降级分组的显示名：查得到用分组名，查不到显示 #id；只在标题需要它时才会被读取
const fallbackName = computed(() => {
  const id = description.value.fields.fallback_group_id
  if (id === null) return ''
  return props.resolveGroupName?.(id) ?? `#${id}`
})

// 用原生 title：表格容器 overflow:auto 会裁掉绝对定位的悬浮气泡。detail 太长，留给编辑卡片
const titleText = computed(() => {
  const lines = [t(meta.value.nameKey), t(meta.value.purposeKey)]
  if (meta.value.requiresFallback) {
    lines.push(t('admin.groups.hatCard.tipFallbackTo', { name: fallbackName.value }))
  }
  if (meta.value.showsPolicySelect) {
    // 用已有的 anthropicPolicies 词条把策略值翻成可读文字
    lines.push(
      t('admin.groups.hatCard.tipFallbackPolicy', {
        policy: t(POLICY_LABEL_KEYS[description.value.fields.claude_oauth_system_prompt_policy])
      })
    )
  }
  if (description.value.staleFallback) {
    lines.push(t('admin.groups.hatCard.tipStaleFallback', { name: fallbackName.value }))
  }
  return lines.join('\n')
})
</script>
