<template>
  <div v-if="contentReviewPolicy !== undefined" class="border-t pt-4">
    <label class="input-label">{{ t('admin.groups.anthropicPolicies.contentReview') }}</label>
    <Select v-model="contentReviewPolicy" :options="contentReviewOptions" />
    <p class="input-hint">{{ t('admin.groups.anthropicPolicies.contentReviewHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import type { GroupPolicy } from '@/types'

const contentReviewPolicy = defineModel<GroupPolicy>('contentReviewPolicy', { default: 'inherit' })
const { t } = useI18n()

const CONTENT_REVIEW_POLICIES: GroupPolicy[] = ['inherit', 'enabled', 'disabled']
const contentReviewOptions = computed(() =>
  CONTENT_REVIEW_POLICIES.map((value) => ({ value, label: t(`admin.groups.anthropicPolicies.${value}`) })),
)
</script>
