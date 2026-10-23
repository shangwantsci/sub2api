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
              <option v-for="preset in openedPresets" :key="preset.key" :value="preset.key">
                {{ preset.label }}
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

          <div>
            <label class="input-label" for="ps-proxy-policy">
              {{ t('admin.providerSettings.proxyModePolicy') }}
            </label>
            <select id="ps-proxy-policy" v-model="settings.proxy_mode_policy" class="input">
              <option value="both">{{ t('admin.providerSettings.proxyModePolicyBoth') }}</option>
              <option value="auto_only">
                {{ t('admin.providerSettings.proxyModePolicyAutoOnly') }}
              </option>
              <option value="manual_only">
                {{ t('admin.providerSettings.proxyModePolicyManualOnly') }}
              </option>
            </select>
            <p class="mt-1 text-xs text-gray-400">
              {{ t('admin.providerSettings.proxyModePolicyHint') }}
            </p>
          </div>

          <div>
            <label class="input-label" for="ps-auto-proxy-cap">
              {{ t('admin.providerSettings.autoProxyMaxAccounts') }}
            </label>
            <input
              id="ps-auto-proxy-cap"
              v-model.number="settings.auto_proxy_max_accounts"
              type="number"
              min="1"
              :max="MAX_AUTO_PROXY_ACCOUNTS"
              class="input"
            />
            <p class="mt-1 text-xs text-gray-400">
              {{ t('admin.providerSettings.autoProxyMaxAccountsHint') }}
            </p>
            <p v-if="errors.auto_proxy_max_accounts" class="mt-1 text-xs text-red-500">
              {{ errors.auto_proxy_max_accounts }}
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
      <!--
        档位数值来自「设置 → 常规」的容量档位模板，这里只读，只管「哪些档位对供货商开放」。
        两处各写各的列：数值归模板接口，开放范围归本面板（enabled_tier_keys），谁都不会回滚谁。
      -->
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

        <p
          v-if="!presetsReady"
          class="text-sm text-gray-500 dark:text-dark-400"
          data-testid="provider-tiers-unavailable"
        >
          {{ presetsFailed ? t('admin.providerSettings.tiersPresetsUnavailable') : t('common.loading') }}
        </p>

        <div v-else class="overflow-x-auto">
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
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t('admin.providerSettings.colAdaptive') }}
                </th>
                <th class="px-3 py-2 text-right text-xs uppercase text-gray-500"></th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr v-for="preset in presets" :key="preset.key" data-testid="provider-tier-row">
                <td class="px-3 py-2">
                  <input
                    type="checkbox"
                    data-testid="provider-tier-open"
                    :checked="isTierOpen(preset.key)"
                    @change="toggleTierOpen(preset.key, ($event.target as HTMLInputElement).checked)"
                  />
                </td>
                <td class="px-3 py-2 font-medium text-gray-900 dark:text-dark-100">{{ preset.key }}</td>
                <td class="px-3 py-2 text-gray-700 dark:text-dark-200">{{ preset.label }}</td>
                <td class="px-3 py-2 text-gray-700 dark:text-dark-200">{{ preset.concurrency }}</td>
                <td class="px-3 py-2 text-gray-700 dark:text-dark-200">{{ preset.max_sessions }}</td>
                <td class="px-3 py-2 text-gray-700 dark:text-dark-200">{{ preset.base_rpm }}</td>
                <td class="px-3 py-2 text-gray-700 dark:text-dark-200">{{ preset.window_cost_limit }}</td>
                <td class="px-3 py-2 text-gray-700 dark:text-dark-200">
                  {{ preset.adaptive_throttle ? t('admin.providerSettings.adaptiveOn') : t('admin.providerSettings.adaptiveOff') }}
                </td>
                <td class="px-3 py-2 text-right">
                  <button
                    type="button"
                    class="whitespace-nowrap text-sm text-indigo-600 hover:underline disabled:cursor-not-allowed disabled:no-underline disabled:opacity-50 dark:text-indigo-400"
                    data-testid="provider-tier-apply"
                    :disabled="applyBusy || !savedOpenTierKeys.has(preset.key)"
                    :title="savedOpenTierKeys.has(preset.key) ? undefined : t('admin.providerSettings.applyClosedTierHint')"
                    @click="askApplyToExisting(preset.key)"
                  >
                    {{
                      applyPreviewLoading && applyTier === preset.key
                        ? t('common.loading')
                        : t('admin.providerSettings.applyToExisting')
                    }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="presetsReady" class="text-xs text-gray-400">{{ t('admin.providerSettings.windowCostZeroHint') }}</p>
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
          <!-- 「应用到存量」的结果单独显示：有未保存的修改时 savedMessage 会被上面的提示顶掉，
               而这条结果管理员必须看得到。 -->
          <span
            v-if="applyNotice"
            class="ml-3 text-emerald-600 dark:text-emerald-400"
            data-testid="apply-notice"
          >
            {{ applyNotice }}
          </span>
          <span v-if="errorMessage" class="text-red-600 dark:text-red-400">{{ errorMessage }}</span>
        </div>
        <button type="button" class="btn-primary" :disabled="!dirty || saving" @click="handleSave">
          {{ saving ? t('common.loading') : t('admin.providerSettings.save') }}
        </button>
      </div>
    </template>

    <!--
      应用到存量：先预览真实改动数，管理员确认后才写账号。
      确认时带上预览的 will_change，后端重算不一致会返回 409（预览之后数据变了），此时自动重新预览。
    -->
    <BaseDialog
      :show="applyDialogOpen"
      :title="t('admin.providerSettings.applyConfirmTitle', { tier: applyTierText })"
      width="normal"
      @close="closeApplyDialog"
    >
      <div class="space-y-4">
        <p
          v-if="applyConflict"
          class="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-700 dark:bg-amber-500/10 dark:text-amber-300"
          role="alert"
          data-testid="apply-conflict"
        >
          {{ t('admin.providerSettings.applyConflict') }}
        </p>

        <p
          v-if="applyPreviewLoading"
          class="text-sm text-gray-500 dark:text-dark-400"
          data-testid="apply-preview-loading"
        >
          {{ t('admin.providerSettings.applyPreviewLoading') }}
        </p>

        <template v-else-if="applyPreview">
          <p
            v-if="applyPreview.matched === 0"
            class="text-sm text-gray-600 dark:text-dark-300"
            data-testid="apply-no-match"
          >
            {{ t('admin.providerSettings.applyNoMatched') }}
          </p>
          <p
            v-else-if="applyPreview.will_change === 0"
            class="text-sm text-gray-600 dark:text-dark-300"
            data-testid="apply-no-change"
          >
            {{ t('admin.providerSettings.applyNoChange', { matched: applyPreview.matched }) }}
          </p>
          <template v-else>
            <p class="text-sm font-medium text-gray-900 dark:text-dark-100" data-testid="apply-summary">
              {{
                t('admin.providerSettings.applyConfirmMessage', {
                  willChange: applyPreview.will_change,
                  matched: applyPreview.matched
                })
              }}
            </p>
            <p
              class="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm font-medium text-amber-800 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-200"
              role="alert"
              data-testid="apply-warning"
            >
              {{ t('admin.providerSettings.applyOverwriteWarning') }}
            </p>
            <ul
              class="max-h-64 divide-y divide-gray-200 overflow-y-auto rounded-md border border-gray-200 dark:divide-dark-700 dark:border-dark-700"
              data-testid="apply-samples"
            >
              <li
                v-for="sample in applyPreview.samples"
                :key="sample.account_id"
                class="px-3 py-2"
                data-testid="apply-sample"
              >
                <div class="text-sm font-medium text-gray-900 dark:text-dark-100">
                  {{ sample.name }}
                  <span class="text-xs font-normal text-gray-400">#{{ sample.account_id }}</span>
                </div>
                <ul class="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-600 dark:text-dark-300">
                  <li v-for="change in sample.changes" :key="change.field" data-testid="apply-change">
                    {{ describeChange(change) }}
                  </li>
                </ul>
              </li>
            </ul>
            <p
              v-if="applyPreview.will_change > applyPreview.samples.length"
              class="text-xs text-gray-500 dark:text-dark-400"
              data-testid="apply-samples-more"
            >
              {{
                t('admin.providerSettings.applySamplesMore', {
                  shown: applyPreview.samples.length,
                  rest: applyPreview.will_change - applyPreview.samples.length
                })
              }}
            </p>
          </template>
        </template>

        <p
          v-if="applyError"
          class="text-sm text-red-600 dark:text-red-400"
          role="alert"
          data-testid="apply-error"
        >
          {{ applyError }}
        </p>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button
            type="button"
            class="btn btn-secondary"
            data-testid="apply-cancel"
            :disabled="applying"
            @click="closeApplyDialog"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="btn btn-danger"
            data-testid="apply-confirm"
            :disabled="!canConfirmApply"
            @click="handleApplyToExisting"
          >
            {{
              applying
                ? t('common.loading')
                : canConfirmApply
                  ? t('admin.providerSettings.applyConfirmButton', { count: applyPreview?.will_change ?? 0 })
                  : t('common.confirm')
            }}
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
import { useCapacityPresets } from '@/composables/useCapacityPresets'
import { MAX_CONCURRENCY, MAX_RPM, MAX_SESSIONS, MAX_WINDOW_COST } from '@/constants/capacity'
import type {
  ProviderGroupPolicyView,
  ProviderHostingTypeSetting,
  ProviderSettings,
  ProviderTierApplyPreview,
  ProviderTierChange,
} from '@/api/admin/providers'

const { t } = useI18n()

// 与后端 provider_settings.go 的护栏保持一致。两边都校验，前端只是提前反馈。
// 与后端 providerMaxAutoProxyMaxAccounts 一致。容量类的上限（并发 / 会话 / RPM / 5h 额度）
// 与设置页的档位模板卡共用 @/constants/capacity。
const MAX_AUTO_PROXY_ACCOUNTS = 50

// 容量档位模板（共享缓存）：数值只读，本面板只决定哪些模板对供货商开放
const {
  presets,
  ready: presetsReady,
  failed: presetsFailed,
  ensureLoaded: ensurePresetsLoaded
} = useCapacityPresets()

const loading = ref(true)
const saving = ref(false)
const applying = ref(false)
const settings = ref<ProviderSettings | null>(null)
const availableGroups = ref<ProviderGroupPolicyView[]>([])
const errors = ref<Record<string, string>>({})
const errorMessage = ref('')
const savedMessage = ref('')

// 「应用到存量」：applyPreviewLoading 是预览请求在途（含 409 之后的重新预览），applying 是应用请求在途
const applyDialogOpen = ref(false)
const applyTier = ref('')
const applyPreview = ref<ProviderTierApplyPreview | null>(null)
const applyPreviewLoading = ref(false)
const applyConflict = ref(false)
const applyError = ref('')
const applyNotice = ref('')

// 用序列化快照判断脏状态，避免为每个字段单独挂 watcher。
const savedSnapshot = ref('')
const dirty = computed(() => {
  if (!settings.value) return false
  return JSON.stringify(settings.value) !== savedSnapshot.value
})

/**
 * 已保存的、对供货商开放的档位。「应用到存量」只对这些档位有意义：后端按已保存的设置判断开放范围，
 * 对没开放的档位返回 400 INVALID_TIER；所以刚勾选但还没保存的档位也算没开放，保存后才可点。
 */
const savedOpenTierKeys = computed(() => {
  try {
    const saved = JSON.parse(savedSnapshot.value) as { enabled_tier_keys?: string[] }
    return new Set(saved.enabled_tier_keys ?? [])
  } catch {
    return new Set<string>()
  }
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

const isTierOpen = (key: string) => settings.value?.enabled_tier_keys.includes(key) ?? false

/** 对供货商开放的模板，按模板表的顺序；默认档位下拉只列这些。 */
const openedPresets = computed(() => presets.value.filter((preset) => isTierOpen(preset.key)))

/** 勾选 / 取消勾选某个模板是否对供货商开放，并保持与模板表一致的顺序，避免来回勾选后无端变「有未保存的修改」。 */
function toggleTierOpen(key: string, open: boolean) {
  const s = settings.value
  if (!s) return
  const wanted = new Set(s.enabled_tier_keys)
  if (open) wanted.add(key)
  else wanted.delete(key)
  const known = presets.value.map((preset) => preset.key)
  s.enabled_tier_keys = [
    ...known.filter((k) => wanted.has(k)),
    ...s.enabled_tier_keys.filter((k) => !known.includes(k) && wanted.has(k))
  ]
}

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

  if (s.auto_proxy_max_accounts < 1 || s.auto_proxy_max_accounts > MAX_AUTO_PROXY_ACCOUNTS) {
    next.auto_proxy_max_accounts = t('admin.providerSettings.autoProxyMaxAccountsRange', {
      max: MAX_AUTO_PROXY_ACCOUNTS
    })
  }

  // 档位数值不在这里编辑（归容量档位模板），这里只校验「开放哪些」：至少开放一个，
  // 默认档位必须是已开放且存在于模板表里的。模板没加载出来时没法判断，交给后端校验。
  if (presetsReady.value) {
    const open = openedPresets.value
    if (open.length === 0) {
      next.tiers = t('admin.providerSettings.tiersHint')
    }
    if (!open.some((preset) => preset.key === s.default_tier)) {
      next.default_tier = t('common.required')
    }
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
    const [data] = await Promise.all([adminAPI.providers.getSettings(), ensurePresetsLoaded()])
    availableGroups.value = data.available_groups ?? []
    const { available_groups: _ignored, ...rest } = data
    rest.enabled_tier_keys = rest.enabled_tier_keys ?? []
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

// ========== 应用到存量：先预览，再确认 ==========
//
// 「应用到存量」会用模板数值覆盖账号上已手工调整过的并发 / 会话数 / RPM / 5h 额度 / 动态降速，
// 所以不能一点就写：先拉预览（只读）展示真实改动数与样例，管理员确认后才调应用接口，
// 并把预览的 will_change 作为 expected_changed 带上。预览到确认之间账号数据又变了，
// 后端重算不一致会返回 409 且不写任何账号，这里自动重新预览，让管理员对新的数字再决定一次。

/** 预览或应用请求在途：此时所有「应用到存量」入口都禁用，防止重复点击。 */
const applyBusy = computed(() => applyPreviewLoading.value || applying.value)

/** 只有拿到预览、确实有账号会变、且没有请求在途时才允许确认。 */
const canConfirmApply = computed(
  () => !applyBusy.value && (applyPreview.value?.will_change ?? 0) > 0
)

/** 对话框标题里的档位：模板 key，查得到显示名就带上。 */
const applyTierText = computed(() => {
  const label = presets.value.find((preset) => preset.key === applyTier.value)?.label
  return label ? `${applyTier.value} (${label})` : applyTier.value
})

function changeFieldLabel(field: string): string {
  switch (field) {
    case 'concurrency':
      return t('admin.providerSettings.applyField.concurrency')
    case 'max_sessions':
      return t('admin.providerSettings.applyField.max_sessions')
    case 'base_rpm':
      return t('admin.providerSettings.applyField.base_rpm')
    case 'window_cost_limit':
      return t('admin.providerSettings.applyField.window_cost_limit')
    case 'adaptive_throttle':
      return t('admin.providerSettings.applyField.adaptive_throttle')
    case 'load_factor':
      return t('admin.providerSettings.applyField.load_factor')
    default:
      // 后端以后新增了字段而前端还没跟上：宁可显示字段名，也不要显示空白
      return field
  }
}

/**
 * 把预览里的数值写成人话：布尔显示开 / 关；额度加美元符号；
 * 0 表示不限（并发除外，与档位下拉 presetOptionLabel 的写法一致）。
 */
function formatChangeValue(field: string, value: number | boolean): string {
  if (typeof value === 'boolean') {
    return t(value ? 'admin.providerSettings.adaptiveOn' : 'admin.providerSettings.adaptiveOff')
  }
  if (value > 0) return field === 'window_cost_limit' ? `$${value}` : String(value)
  return field === 'concurrency' ? String(value) : t('admin.accounts.capacityFields.unlimited')
}

function describeChange(change: ProviderTierChange): string {
  return `${changeFieldLabel(change.field)} ${formatChangeValue(change.field, change.from)} → ${formatChangeValue(change.field, change.to)}`
}

/** 拉取预览并写入 applyPreview。失败返回错误文案，成功返回空串。 */
async function fetchApplyPreview(tier: string): Promise<string> {
  applyPreviewLoading.value = true
  // 旧预览在新预览回来之前不能留着：否则管理员可能对着过期的数字点确认
  applyPreview.value = null
  try {
    applyPreview.value = await adminAPI.providers.getTierApplyPreview(tier)
    return ''
  } catch (error) {
    return (error as { message?: string })?.message || t('admin.providerSettings.applyPreviewFailed')
  } finally {
    applyPreviewLoading.value = false
  }
}

async function askApplyToExisting(tier: string) {
  if (applyBusy.value) return
  applyTier.value = tier
  applyConflict.value = false
  applyError.value = ''
  applyNotice.value = ''
  errorMessage.value = ''

  const failure = await fetchApplyPreview(tier)
  if (failure) {
    // 拿不到预览就没有东西可确认：不弹框，错误放进底部原有的错误行
    errorMessage.value = failure
    return
  }
  applyDialogOpen.value = true
}

/** 应用请求在途时不允许关闭，否则结果提示会落在一个已经看不到的对话框上。 */
function closeApplyDialog() {
  if (applying.value) return
  applyDialogOpen.value = false
}

async function handleApplyToExisting() {
  const preview = applyPreview.value
  if (!preview || preview.will_change <= 0 || applyBusy.value) return

  applying.value = true
  applyError.value = ''
  applyConflict.value = false
  let conflicted = false
  try {
    const result = await adminAPI.providers.applyTierToExisting(applyTier.value, preview.will_change)
    applyNotice.value = t('admin.providerSettings.applyResult', {
      updated: result.updated,
      matched: result.matched,
    })
    applyDialogOpen.value = false
  } catch (error) {
    // 409 不是普通失败：说明预览之后账号数据变了，后端没有写任何账号，需要重新预览再确认
    if ((error as { status?: number })?.status === 409) {
      conflicted = true
    } else {
      applyError.value = (error as { message?: string })?.message || t('admin.providerSettings.applyFailed')
    }
  } finally {
    applying.value = false
  }

  if (conflicted) {
    applyConflict.value = true
    applyError.value = await fetchApplyPreview(applyTier.value)
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
