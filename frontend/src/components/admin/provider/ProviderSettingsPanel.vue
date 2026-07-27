<template>
  <div class="space-y-6">
    <div v-if="loading" class="py-12 text-center text-gray-500 dark:text-dark-400">
      {{ t('common.loading') }}
    </div>

    <template v-else-if="settings">
      <!-- ========== 基础 ========== -->
      <section class="space-y-4 rounded-lg border border-gray-200 p-5 dark:border-dark-700">
        <h3 class="font-semibold text-gray-900 dark:text-dark-100">
          {{ t('admin.providerSettings.sectionBasic') }}
        </h3>

        <label class="flex items-start gap-3">
          <input v-model="settings.portal_enabled" type="checkbox" class="mt-1" />
          <span>
            <span class="block text-sm font-medium text-gray-900 dark:text-dark-100">
              {{ t('admin.providerSettings.portalEnabled') }}
            </span>
            <span class="block text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.providerSettings.portalEnabledHint') }}
            </span>
          </span>
        </label>

        <div class="grid gap-4 sm:grid-cols-3">
          <div>
            <label class="input-label" for="ps-default-hosting">
              {{ t('admin.providerSettings.defaultHostingType') }}
            </label>
            <select id="ps-default-hosting" v-model.number="settings.default_group_id" class="input">
              <option :value="0">-</option>
              <option v-for="ht in enabledHostingTypes" :key="ht.group_id" :value="ht.group_id">
                {{ ht.label || groupName(ht.group_id) }}
              </option>
            </select>
            <p v-if="errors.default_group_id" class="mt-1 text-xs text-red-600">
              {{ errors.default_group_id }}
            </p>
          </div>

          <div>
            <label class="input-label" for="ps-default-tier">
              {{ t('admin.providerSettings.defaultTier') }}
            </label>
            <select id="ps-default-tier" v-model="settings.default_tier" class="input">
              <option v-for="tier in enabledTiers" :key="tier.tier" :value="tier.tier">
                {{ tier.label }}
              </option>
            </select>
            <p v-if="errors.default_tier" class="mt-1 text-xs text-red-600">
              {{ errors.default_tier }}
            </p>
          </div>

          <div>
            <label class="input-label" for="ps-timezone">
              {{ t('admin.providerSettings.settlementTimezone') }}
            </label>
            <input id="ps-timezone" v-model.trim="settings.settlement_timezone" type="text" class="input" />
            <p class="mt-1 text-xs text-gray-400">
              {{ t('admin.providerSettings.settlementTimezoneHint') }}
            </p>
          </div>

          <div>
            <label class="input-label" for="ps-cooldown">
              {{ t('admin.providerSettings.settlementCooldown') }}
            </label>
            <input
              id="ps-cooldown"
              v-model.number="settings.settlement_cooldown_seconds"
              type="number"
              min="60"
              max="86400"
              class="input"
            />
            <p class="mt-1 text-xs text-gray-400">
              {{ t('admin.providerSettings.settlementCooldownHint') }}
            </p>
            <p v-if="errors.settlement_cooldown_seconds" class="mt-1 text-xs text-red-500">
              {{ errors.settlement_cooldown_seconds }}
            </p>
          </div>

          <!--
            调度优先级不再手工配置：上号时自动取目标托管分组内现有账号的最小 priority。
            priority 在调度里是硬门槛而非权重，只有分组内数值最小的那批账号会被选中，
            手工填一个和分组不一致的值会让其中一边被静默饿死。这里只展示实际会用到的值。
          -->
          <div>
            <span class="input-label">{{ t('admin.providerSettings.accountPriority') }}</span>
            <div
              class="mt-1 rounded-md border border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-800"
            >
              <p v-if="resolvedPriorities.length === 0" class="text-sm text-gray-500 dark:text-dark-400">
                {{ t('admin.providerSettings.priorityNoGroups') }}
              </p>
              <ul v-else class="space-y-1">
                <li
                  v-for="p in resolvedPriorities"
                  :key="p.groupId"
                  class="flex items-center justify-between text-sm"
                >
                  <span class="text-gray-700 dark:text-dark-200">{{ p.groupName }}</span>
                  <span class="font-medium text-gray-900 dark:text-dark-100">
                    {{ p.priority }}
                    <span v-if="p.fallback" class="ml-1 text-xs font-normal text-gray-400">
                      {{ t('admin.providerSettings.priorityFallbackTag') }}
                    </span>
                  </span>
                </li>
              </ul>
            </div>
            <p class="mt-1 text-xs text-gray-400">
              {{ t('admin.providerSettings.accountPriorityHint') }}
            </p>
          </div>
        </div>
      </section>

      <!-- ========== 托管类型 ========== -->
      <section class="space-y-4 rounded-lg border border-gray-200 p-5 dark:border-dark-700">
        <div>
          <h3 class="font-semibold text-gray-900 dark:text-dark-100">
            {{ t('admin.providerSettings.sectionHostingTypes') }}
          </h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.providerSettings.hostingTypesHint') }}
          </p>
        </div>

        <p v-if="availableGroups.length === 0" class="text-sm text-gray-500 dark:text-dark-400">
          {{ t('admin.providerSettings.noAnthropicGroups') }}
        </p>

        <div v-else class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colOpen') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colGroup') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colLabel') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colDescription') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colSort') }}
                </th>
                <!-- 以下三列只读，是该分组的真实策略。填对外文案时对照着看，
                     供号商侧永远看不到这些。 -->
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-400">
                  {{ t('admin.providerSettings.colContentReview') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-400">
                  {{ t('admin.providerSettings.colSystemPrompt') }}
                </th>
                <th class="px-3 py-2 text-right text-xs uppercase text-gray-400">
                  {{ t('admin.providerSettings.colRateMultiplier') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr v-for="group in availableGroups" :key="group.id">
                <td class="px-3 py-2">
                  <input
                    type="checkbox"
                    :checked="hostingEntry(group.id).enabled"
                    @change="toggleHosting(group.id, ($event.target as HTMLInputElement).checked)"
                  />
                </td>
                <td class="px-3 py-2 text-gray-900 dark:text-dark-100">{{ group.name }}</td>
                <td class="px-3 py-2">
                  <input
                    v-model.trim="hostingEntry(group.id).label"
                    type="text"
                    class="input py-1 text-sm"
                    :class="errors[`hosting_label_${group.id}`] ? 'border-red-500' : ''"
                  />
                  <p v-if="errors[`hosting_label_${group.id}`]" class="mt-1 text-xs text-red-600">
                    {{ errors[`hosting_label_${group.id}`] }}
                  </p>
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.trim="hostingEntry(group.id).description"
                    type="text"
                    class="input py-1 text-sm"
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="hostingEntry(group.id).sort"
                    type="number"
                    class="input w-20 py-1 text-sm"
                  />
                </td>
                <td class="px-3 py-2 text-xs text-gray-400">{{ group.content_review_policy }}</td>
                <td class="px-3 py-2 text-xs text-gray-400">
                  {{ group.claude_oauth_system_prompt_policy }}
                </td>
                <td class="px-3 py-2 text-right text-xs text-gray-400">
                  {{ group.rate_multiplier }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- ========== 速率档位 ========== -->
      <section class="space-y-4 rounded-lg border border-gray-200 p-5 dark:border-dark-700">
        <div>
          <h3 class="font-semibold text-gray-900 dark:text-dark-100">
            {{ t('admin.providerSettings.sectionTiers') }}
          </h3>
          <p
            class="mt-2 rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-500/10 dark:text-amber-300"
          >
            {{ t('admin.providerSettings.tiersHint') }}
          </p>
        </div>

        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colEnabled') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colTier') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colTierLabel') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colConcurrency') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colMaxSessions') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colBaseRpm') }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colWindowCost') }}
                </th>
                <th class="px-3 py-2 text-right text-xs uppercase text-gray-500"></th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr v-for="tier in settings.capacity_tiers" :key="tier.tier">
                <td class="px-3 py-2"><input v-model="tier.enabled" type="checkbox" /></td>
                <td class="px-3 py-2 font-medium text-gray-900 dark:text-dark-100">{{ tier.tier }}</td>
                <td class="px-3 py-2">
                  <input v-model.trim="tier.label" type="text" class="input w-24 py-1 text-sm" />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="tier.concurrency"
                    type="number"
                    min="1"
                    :max="MAX_CONCURRENCY"
                    class="input w-20 py-1 text-sm"
                    :class="errors[`tier_concurrency_${tier.tier}`] ? 'border-red-500' : ''"
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="tier.max_sessions"
                    type="number"
                    min="0"
                    :max="MAX_SESSIONS"
                    class="input w-20 py-1 text-sm"
                    :class="errors[`tier_sessions_${tier.tier}`] ? 'border-red-500' : ''"
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="tier.base_rpm"
                    type="number"
                    min="0"
                    :max="MAX_RPM"
                    class="input w-20 py-1 text-sm"
                    :class="errors[`tier_rpm_${tier.tier}`] ? 'border-red-500' : ''"
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="tier.window_cost_limit"
                    type="number"
                    min="0"
                    :max="MAX_WINDOW_COST"
                    class="input w-24 py-1 text-sm"
                    :class="errors[`tier_window_${tier.tier}`] ? 'border-red-500' : ''"
                  />
                </td>
                <td class="px-3 py-2 text-right">
                  <button
                    type="button"
                    class="whitespace-nowrap text-sm text-indigo-600 hover:underline dark:text-indigo-400"
                    @click="askApplyToExisting(tier.tier)"
                  >
                    {{ t('admin.providerSettings.applyToExisting') }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="text-xs text-gray-400">{{ t('admin.providerSettings.windowCostZeroHint') }}</p>
        <p v-if="errors.tiers" class="text-sm text-red-600">{{ errors.tiers }}</p>
      </section>

      <!-- ========== 自定义档 ========== -->
      <section class="space-y-4 rounded-lg border border-gray-200 p-5 dark:border-dark-700">
        <h3 class="font-semibold text-gray-900 dark:text-dark-100">
          {{ t('admin.providerSettings.sectionCustomTier') }}
        </h3>
        <label class="flex items-start gap-3">
          <input v-model="settings.custom_tier_enabled" type="checkbox" class="mt-1" />
          <span>
            <span class="block text-sm font-medium text-gray-900 dark:text-dark-100">
              {{ t('admin.providerSettings.customTierEnabled') }}
            </span>
            <span class="block text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.providerSettings.customTierHint') }}
            </span>
          </span>
        </label>

        <div v-if="settings.custom_tier_enabled" class="grid gap-4 sm:grid-cols-4">
          <div>
            <label class="input-label" for="ps-cap-conc">
              {{ t('admin.providerSettings.capConcurrency') }}
            </label>
            <input
              id="ps-cap-conc"
              v-model.number="settings.custom_tier_caps.concurrency"
              type="number"
              min="1"
              :max="MAX_CONCURRENCY"
              class="input"
            />
          </div>
          <div>
            <label class="input-label" for="ps-cap-sessions">
              {{ t('admin.providerSettings.capMaxSessions') }}
            </label>
            <input
              id="ps-cap-sessions"
              v-model.number="settings.custom_tier_caps.max_sessions"
              type="number"
              min="1"
              :max="MAX_SESSIONS"
              class="input"
            />
          </div>
          <div>
            <label class="input-label" for="ps-cap-rpm">
              {{ t('admin.providerSettings.capBaseRpm') }}
            </label>
            <input
              id="ps-cap-rpm"
              v-model.number="settings.custom_tier_caps.base_rpm"
              type="number"
              min="0"
              :max="MAX_RPM"
              class="input"
            />
          </div>
          <div>
            <label class="input-label" for="ps-cap-window">
              {{ t('admin.providerSettings.capWindowCost') }}
            </label>
            <input
              id="ps-cap-window"
              v-model.number="settings.custom_tier_caps.window_cost_limit"
              type="number"
              min="0"
              :max="MAX_WINDOW_COST"
              class="input"
            />
          </div>
        </div>
        <p v-if="errors.custom_tier" class="text-sm text-red-600">{{ errors.custom_tier }}</p>
      </section>

      <!-- 底部保存栏 -->
      <div
        class="sticky bottom-0 flex items-center justify-between gap-4 border-t border-gray-200 bg-white/95 py-4 backdrop-blur dark:border-dark-700 dark:bg-dark-900/95"
      >
        <div class="text-sm">
          <span v-if="dirty" class="font-medium text-amber-600 dark:text-amber-400">
            {{ t('admin.providerSettings.unsavedChanges') }}
          </span>
          <span v-else-if="savedMessage" class="text-emerald-600 dark:text-emerald-400">
            {{ savedMessage }}
          </span>
          <span v-if="errorMessage" class="text-red-600 dark:text-red-400">{{ errorMessage }}</span>
        </div>
        <button type="button" class="btn-primary" :disabled="!dirty || saving" @click="handleSave">
          {{ saving ? t('common.loading') : t('admin.providerSettings.save') }}
        </button>
      </div>
    </template>

    <!-- 应用到存量：先预览影响面再执行 -->
    <BaseDialog
      :show="applyDialogOpen"
      :title="t('admin.providerSettings.applyConfirmTitle')"
      width="narrow"
      @close="applyDialogOpen = false"
    >
      <p class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('admin.providerSettings.applyConfirmMessage', { count: applyAffectedCount }) }}
      </p>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn-secondary" @click="applyDialogOpen = false">
            {{ t('common.cancel') }}
          </button>
          <button type="button" class="btn-primary" :disabled="applying" @click="handleApplyToExisting">
            {{ applying ? t('common.loading') : t('admin.providerSettings.applyToExisting') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave } from 'vue-router'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api'
import type {
  ProviderGroupPolicyView,
  ProviderHostingTypeSetting,
  ProviderSettings,
} from '@/api/admin/providers'

const { t } = useI18n()

// 与后端 provider_settings.go 的护栏保持一致。两边都校验，前端只是提前反馈。
const MAX_CONCURRENCY = 50
const MAX_SESSIONS = 50
const MAX_RPM = 500
const MAX_WINDOW_COST = 1000

const loading = ref(true)
const saving = ref(false)
const applying = ref(false)
const settings = ref<ProviderSettings | null>(null)
const availableGroups = ref<ProviderGroupPolicyView[]>([])
const errors = ref<Record<string, string>>({})
const errorMessage = ref('')
const savedMessage = ref('')

const applyDialogOpen = ref(false)
const applyAffectedCount = ref(0)
const applyTier = ref('')

// 用序列化快照判断脏状态，避免为每个字段单独挂 watcher。
const savedSnapshot = ref('')
const dirty = computed(() => {
  if (!settings.value) return false
  return JSON.stringify(settings.value) !== savedSnapshot.value
})

const enabledHostingTypes = computed(
  () => settings.value?.hosting_types.filter((h) => h.enabled) ?? []
)

/**
 * 展示每个已开放托管分组上号时实际会用的 priority。
 *
 * 上号时自动取该分组现有账号的最小 priority，与分组内正在跑的账号对齐；
 * 分组还是空的就回落到 account_priority（种子值 1）。
 *
 * 之所以不让人手填：priority 在调度里是硬门槛而不是权重，
 * filterByMinPriority 只保留分组内数值最小的那批账号，其余一个请求都拿不到。
 * 填一个和分组不一致的值，数值大的一方会被永久饿死且完全没有报错——
 * 供号商只会看到「账号正常但用量恒为 0」。
 */
const resolvedPriorities = computed(() => {
  const s = settings.value
  if (!s) return []
  const out: { groupId: number; groupName: string; priority: number; fallback: boolean }[] = []

  for (const h of s.hosting_types) {
    if (!h.enabled) continue
    const group = availableGroups.value.find((g) => g.id === h.group_id)
    if (!group) continue
    const existing = group.existing_priorities ?? []
    if (existing.length > 0) {
      out.push({
        groupId: group.id,
        groupName: group.name,
        priority: Math.min(...existing),
        fallback: false,
      })
    } else {
      out.push({
        groupId: group.id,
        groupName: group.name,
        priority: s.account_priority,
        fallback: true,
      })
    }
  }
  return out
})
const enabledTiers = computed(() => settings.value?.capacity_tiers.filter((t2) => t2.enabled) ?? [])

function groupName(id: number): string {
  return availableGroups.value.find((g) => g.id === id)?.name ?? String(id)
}

/**
 * 取或建某分组的托管类型配置行。
 *
 * 表格行来源是分组列表，设置里可能还没有对应条目，这里按需补一条空的，
 * 这样 v-model 能直接绑定。
 */
function hostingEntry(groupID: number): ProviderHostingTypeSetting {
  if (!settings.value) {
    return { group_id: groupID, label: '', description: '', enabled: false, sort: 0 }
  }
  let entry = settings.value.hosting_types.find((h) => h.group_id === groupID)
  if (!entry) {
    entry = { group_id: groupID, label: '', description: '', enabled: false, sort: 0 }
    settings.value.hosting_types.push(entry)
  }
  return entry
}

function toggleHosting(groupID: number, enabled: boolean) {
  const entry = hostingEntry(groupID)
  entry.enabled = enabled
  if (enabled && !entry.label) {
    entry.label = groupName(groupID)
  }
}

/** 前端字段级校验。后端 SaveProviderSettings 会再校验一遍，两边都不可省。 */
function validate(): boolean {
  const next: Record<string, string> = {}
  const s = settings.value
  if (!s) return false

  for (const h of s.hosting_types) {
    if (h.enabled && !h.label.trim()) {
      next[`hosting_label_${h.group_id}`] = t('common.required')
    }
  }

  // 与后端 provider_settings.go 的区间保持一致。
  if (s.settlement_cooldown_seconds < 60 || s.settlement_cooldown_seconds > 86400) {
    next.settlement_cooldown_seconds = t('admin.providerSettings.cooldownRange')
  }

  let anyTierEnabled = false
  for (const tier of s.capacity_tiers) {
    if (tier.enabled) anyTierEnabled = true
    // 并发下限是 1：后端并发服务把 0 当作「不限并发」，填 0 的效果与直觉相反。
    if (tier.concurrency < 1 || tier.concurrency > MAX_CONCURRENCY) {
      next[`tier_concurrency_${tier.tier}`] = 'x'
    }
    if (tier.max_sessions < 0 || tier.max_sessions > MAX_SESSIONS) {
      next[`tier_sessions_${tier.tier}`] = 'x'
    }
    if (tier.base_rpm < 0 || tier.base_rpm > MAX_RPM) {
      next[`tier_rpm_${tier.tier}`] = 'x'
    }
    if (tier.window_cost_limit < 0 || tier.window_cost_limit > MAX_WINDOW_COST) {
      next[`tier_window_${tier.tier}`] = 'x'
    }
  }
  if (!anyTierEnabled) {
    next.tiers = t('admin.providerSettings.tiersHint')
  }
  if (!s.capacity_tiers.some((tier) => tier.enabled && tier.tier === s.default_tier)) {
    next.default_tier = t('common.required')
  }
  // 站点开着时才强制要求托管类型齐备，关闭状态允许慢慢配。
  if (s.portal_enabled) {
    if (!s.hosting_types.some((h) => h.enabled && h.group_id === s.default_group_id)) {
      next.default_group_id = t('common.required')
    }
  }

  errors.value = next
  return Object.keys(next).length === 0
}

async function load() {
  loading.value = true
  try {
    const data = await adminAPI.providers.getSettings()
    availableGroups.value = data.available_groups ?? []
    const { available_groups: _ignored, ...rest } = data
    settings.value = rest
    savedSnapshot.value = JSON.stringify(rest)
  } catch (error) {
    errorMessage.value =
      (error as { message?: string })?.message || t('admin.providerSettings.loadFailed')
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  if (!settings.value) return
  errorMessage.value = ''
  savedMessage.value = ''
  if (!validate()) return

  saving.value = true
  try {
    await adminAPI.providers.updateSettings(settings.value)
    savedSnapshot.value = JSON.stringify(settings.value)
    savedMessage.value = t('admin.providerSettings.saved')
  } catch (error) {
    errorMessage.value =
      (error as { message?: string })?.message || t('admin.providerSettings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function askApplyToExisting(tier: string) {
  applyTier.value = tier
  applyAffectedCount.value = 0
  try {
    const result = await adminAPI.providers.getTierAffectedCount(tier)
    applyAffectedCount.value = result.affected
  } catch {
    applyAffectedCount.value = 0
  }
  applyDialogOpen.value = true
}

async function handleApplyToExisting() {
  applying.value = true
  try {
    const result = await adminAPI.providers.applyTierToExisting(applyTier.value)
    savedMessage.value = t('admin.providerSettings.applyResult', {
      updated: result.updated,
      matched: result.matched,
    })
    applyDialogOpen.value = false
  } catch (error) {
    errorMessage.value =
      (error as { message?: string })?.message || t('admin.providerSettings.applyFailed')
  } finally {
    applying.value = false
  }
}

// 有改动时提示后再离开，避免辛苦填的档位与文案被一次误导航清空。
function beforeUnloadHandler(event: BeforeUnloadEvent) {
  if (dirty.value) {
    event.preventDefault()
    event.returnValue = ''
  }
}

onBeforeRouteLeave(() => {
  if (!dirty.value) return true
  return window.confirm(t('admin.providerSettings.leaveConfirm'))
})

watch(dirty, (value) => {
  if (value) savedMessage.value = ''
})

onMounted(() => {
  load()
  window.addEventListener('beforeunload', beforeUnloadHandler)
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', beforeUnloadHandler)
})
</script>
