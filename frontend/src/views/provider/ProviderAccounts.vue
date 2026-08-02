<template>
  <ProviderLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-dark-100">
            {{ t('provider.accounts.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
            {{ t('provider.accounts.periodSince') }} {{ formatDateTime(periodStart) }}
          </p>
        </div>
        <RouterLink to="/provider/onboard" class="btn-primary">
          {{ t('provider.accounts.addAccount') }}
        </RouterLink>
      </div>

      <div v-if="loading" class="py-12 text-center text-gray-500 dark:text-dark-400">
        {{ t('common.loading') }}
      </div>

      <div v-else-if="accounts.length === 0" class="card py-12 text-center">
        <p class="text-gray-500 dark:text-dark-400">{{ t('provider.accounts.empty') }}</p>
      </div>

      <div v-else class="card overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
          <thead class="bg-gray-50 dark:bg-dark-800">
            <tr>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colName') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colStatus') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colHostingType') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colTier') }}
              </th>
              <th class="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colWindow') }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colRequests') }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colCost') }}
              </th>
              <th class="px-4 py-3 text-right text-xs font-medium uppercase text-gray-500 dark:text-dark-400">
                {{ t('provider.accounts.colActions') }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
            <tr v-for="account in accounts" :key="account.id">
              <td class="px-4 py-3">
                <div class="font-medium text-gray-900 dark:text-dark-100">{{ account.name }}</div>
                <!-- 邮箱是供号商对照「手上哪些号已经上了」的唯一标识，名称可以重复也可以乱填。 -->
                <div v-if="account.email" class="font-mono text-xs text-gray-500 dark:text-dark-400">
                  {{ account.email }}
                </div>
                <div v-if="account.notes" class="text-xs text-gray-400">{{ account.notes }}</div>
              </td>
              <td class="px-4 py-3">
                <span
                  class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium"
                  :class="statusClass(account.status)"
                >
                  {{ t(`provider.accounts.status.${account.status}`) }}
                </span>
              </td>
              <td class="px-4 py-3 text-sm text-gray-600 dark:text-dark-300">
                {{ account.hosting_type_label || '-' }}
              </td>
              <td class="px-4 py-3 text-sm text-gray-600 dark:text-dark-300">
                {{ account.tier_label || '-' }}
              </td>
              <td class="px-4 py-3 text-sm">
                <div v-if="usageState[account.id]?.loading" class="space-y-1.5">
                  <div class="h-3 w-28 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
                  <div class="h-3 w-28 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
                </div>
                <div v-else-if="usageWindows(account.id).length" class="space-y-1">
                  <UsageProgressBar
                    v-for="w in usageWindows(account.id)"
                    :key="w.label"
                    :label="w.label"
                    :utilization="w.window.utilization"
                    :resets-at="w.window.resets_at"
                    :color="w.color"
                  />
                  <button
                    type="button"
                    class="text-[10px] text-indigo-600 hover:underline disabled:opacity-50 dark:text-indigo-400"
                    :disabled="usageState[account.id]?.refreshing"
                    @click="refreshUsage(account)"
                  >
                    {{
                      usageState[account.id]?.refreshing
                        ? t('common.loading')
                        : t('provider.accounts.usageRefresh')
                    }}
                  </button>
                </div>
                <div v-else class="space-y-1">
                  <span class="text-gray-400">-</span>
                  <button
                    type="button"
                    class="block text-[10px] text-indigo-600 hover:underline disabled:opacity-50 dark:text-indigo-400"
                    :disabled="usageState[account.id]?.refreshing"
                    @click="refreshUsage(account)"
                  >
                    {{
                      usageState[account.id]?.refreshing
                        ? t('common.loading')
                        : t('provider.accounts.usageQuery')
                    }}
                  </button>
                </div>
              </td>
              <td class="px-4 py-3 text-right text-sm text-gray-600 dark:text-dark-300">
                {{ formatCount(account.period_requests) }}
              </td>
              <td class="px-4 py-3 text-right text-sm font-medium text-gray-900 dark:text-dark-100">
                {{ formatUSD(account.period_cost) }}
              </td>
              <td class="px-4 py-3 text-right">
                <div class="flex flex-wrap justify-end gap-x-3 gap-y-1">
                  <button
                    type="button"
                    class="text-sm text-indigo-600 hover:underline dark:text-indigo-400"
                    @click="openEdit(account)"
                  >
                    {{ t('provider.accounts.edit') }}
                  </button>
                  <button
                    type="button"
                    class="text-sm text-indigo-600 hover:underline dark:text-indigo-400"
                    @click="openReauth(account)"
                  >
                    {{ t('provider.accounts.reauth') }}
                  </button>
                  <button
                    v-if="account.status === 'active'"
                    type="button"
                    class="text-sm text-amber-600 hover:underline dark:text-amber-400"
                    @click="handlePause(account)"
                  >
                    {{ t('provider.accounts.pause') }}
                  </button>
                  <button
                    v-else-if="account.status === 'paused'"
                    type="button"
                    class="text-sm text-emerald-600 hover:underline dark:text-emerald-400"
                    @click="handleResume(account)"
                  >
                    {{ t('provider.accounts.resume') }}
                  </button>
                  <button
                    type="button"
                    class="text-sm text-red-600 hover:underline dark:text-red-400"
                    @click="askOffline(account)"
                  >
                    {{ t('provider.accounts.offline') }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />

      <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
    </div>

    <!-- 编辑：名称、备注、档位。托管类型与出口 IP 不在这里改，两者都会牵动调度。 -->
    <BaseDialog
      :show="editOpen"
      :title="t('provider.accounts.editTitle')"
      width="normal"
      @close="closeEdit"
    >
      <div class="space-y-4">
        <div>
          <label class="input-label" for="edit-name">{{ t('provider.onboard.name') }}</label>
          <input id="edit-name" v-model.trim="editForm.name" type="text" class="input" />
        </div>
        <div>
          <label class="input-label" for="edit-notes">{{ t('provider.onboard.notes') }}</label>
          <input id="edit-notes" v-model.trim="editForm.notes" type="text" class="input" />
        </div>

        <div>
          <span class="input-label">{{ t('provider.onboard.sectionTier') }}</span>
          <p v-if="loadingOptions" class="text-sm text-gray-500 dark:text-dark-400">
            {{ t('common.loading') }}
          </p>
          <div v-else class="grid gap-2 sm:grid-cols-2">
            <label
              v-for="tier in options?.tiers || []"
              :key="tier.tier"
              class="cursor-pointer rounded-lg border p-3 transition-colors"
              :class="
                editForm.tier === tier.tier
                  ? 'border-indigo-500 bg-indigo-50 dark:border-indigo-400 dark:bg-indigo-500/10'
                  : 'border-gray-200 hover:border-gray-300 dark:border-dark-700'
              "
            >
              <div class="flex items-center gap-2">
                <input v-model="editForm.tier" type="radio" :value="tier.tier" />
                <span class="text-sm font-medium text-gray-900 dark:text-dark-100">
                  {{ tier.label }}
                </span>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                {{ t('provider.onboard.windowCostLimit') }}:
                {{
                  tier.window_cost_limit > 0
                    ? `$${tier.window_cost_limit}`
                    : t('provider.onboard.unlimited')
                }}
              </p>
            </label>

          </div>

          <!--
            这里刻意不给自定义档：自定义档的并发/会话数/RPM 不下发到供号商侧，
            弹窗只能预填成默认值，供号商一保存就把自己原本填的参数静默重置了。
            要换成自定义档得回上号流程，或者让管理员调。
          -->
          <p
            v-if="editTarget?.tier === 'custom'"
            class="mt-2 text-xs text-amber-600 dark:text-amber-400"
          >
            {{ t('provider.accounts.customTierLocked') }}
          </p>
        </div>

        <p v-if="dialogError" class="text-sm text-red-600 dark:text-red-400">{{ dialogError }}</p>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary" @click="closeEdit">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn-primary" :disabled="saving" @click="handleSaveEdit">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </template>
    </BaseDialog>

    <!-- 重新授权：号掉了换一份凭据，沿用原有的出口 IP 与档位。 -->
    <BaseDialog
      :show="reauthOpen"
      :title="t('provider.accounts.reauthTitle')"
      width="normal"
      @close="closeReauth"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-500 dark:text-dark-400">
          {{ t('provider.accounts.reauthHint', { name: reauthTarget?.name ?? '' }) }}
        </p>

        <div>
          <span class="input-label">{{ t('provider.onboard.authMode') }}</span>
          <div class="flex gap-4">
            <label class="flex items-center gap-2">
              <input v-model="reauthMode" type="radio" value="cookie" />
              <span class="text-sm">{{ t('provider.onboard.authModeCookie') }}</span>
            </label>
            <label class="flex items-center gap-2">
              <input v-model="reauthMode" type="radio" value="manual" />
              <span class="text-sm">{{ t('provider.onboard.authModeManual') }}</span>
            </label>
          </div>
        </div>

        <div v-if="reauthMode === 'cookie'">
          <label class="input-label" for="reauth-key">{{ t('provider.onboard.sessionKey') }}</label>
          <textarea
            id="reauth-key"
            v-model.trim="reauthForm.sessionKey"
            rows="3"
            class="input font-mono text-xs"
            :placeholder="t('provider.onboard.sessionKeyPlaceholder')"
          ></textarea>
        </div>

        <div v-else class="space-y-3">
          <button
            type="button"
            class="btn-secondary"
            :disabled="generatingURL"
            @click="handleGenerateReauthURL"
          >
            {{ generatingURL ? t('common.loading') : t('provider.onboard.generateAuthURL') }}
          </button>
          <div v-if="reauthAuthURL" class="space-y-2">
            <a
              :href="reauthAuthURL"
              target="_blank"
              rel="noopener noreferrer"
              class="block break-all text-sm text-indigo-600 hover:underline dark:text-indigo-400"
            >
              {{ reauthAuthURL }}
            </a>
            <div>
              <label class="input-label" for="reauth-code">
                {{ t('provider.onboard.authCode') }}
              </label>
              <input id="reauth-code" v-model.trim="reauthForm.code" type="text" class="input" />
            </div>
          </div>
        </div>

        <p v-if="dialogError" class="text-sm text-red-600 dark:text-red-400">{{ dialogError }}</p>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary" @click="closeReauth">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn-primary" :disabled="saving" @click="handleReauth">
          {{ saving ? t('provider.onboard.submitting') : t('provider.accounts.reauthSubmit') }}
        </button>
      </template>
    </BaseDialog>

    <!-- 下线确认。必须说清楚金额不会因下线而减少，否则供号商会以为下线等于放弃结算。 -->
    <ConfirmDialog
      :show="offlineDialogOpen"
      :title="t('provider.accounts.offlineConfirmTitle')"
      :message="offlineConfirmMessage"
      :confirm-text="t('provider.accounts.offline')"
      danger
      @confirm="handleOffline"
      @cancel="offlineDialogOpen = false"
    />
  </ProviderLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import ProviderLayout from '@/components/layout/ProviderLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'
import {
  generateReauthURL,
  getAccountUsage,
  getCurrentPeriodInfo,
  getOnboardOptions,
  listAccounts,
  offlineAccount,
  pauseAccount,
  reauthAccount,
  resumeAccount,
  updateAccount,
} from '@/api/provider'
import type { ProviderAccount, ProviderAccountUsage, ProviderOnboardOptions } from '@/api/provider'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatCount, formatUSD } from '@/utils/providerMoney'

const { t } = useI18n()

const loading = ref(true)
const accounts = ref<ProviderAccount[]>([])
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const periodStart = ref('')
const errorMessage = ref('')
const offlineDialogOpen = ref(false)
const offlineTarget = ref<ProviderAccount | null>(null)

// 弹窗共用的保存态与错误位：同一时刻只会开一个弹窗。
const saving = ref(false)
const dialogError = ref('')

const editOpen = ref(false)
const editTarget = ref<ProviderAccount | null>(null)
const editForm = reactive({ name: '', notes: '', tier: '' })

// 档位选项只有编辑时才需要，页面加载时不去拉。
const options = ref<ProviderOnboardOptions | null>(null)
const loadingOptions = ref(false)

const reauthOpen = ref(false)
const reauthTarget = ref<ProviderAccount | null>(null)
const reauthMode = ref<'cookie' | 'manual'>('cookie')
const reauthAuthURL = ref('')
const generatingURL = ref(false)
const reauthForm = reactive({ sessionKey: '', sessionID: '', code: '' })

const offlineConfirmMessage = computed(() =>
  t('provider.accounts.offlineConfirmMessage', { name: offlineTarget.value?.name ?? '' })
)

function statusClass(status: string): string {
  switch (status) {
    case 'active':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    case 'paused':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
    case 'error':
      return 'bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300'
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
  }
}

function formatDateTime(value?: string | null): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

/**
 * 每个账号的额度用量，按 id 存。
 *
 * 不塞进账号列表接口：这份数据来自 Anthropic 的窗口采样，和账号本身的属性是两回事，
 * 而且主动查询要走外部 API，逐账号按需拉才控制得住。
 */
const usageState = reactive<
  Record<number, { loading: boolean; refreshing: boolean; data: ProviderAccountUsage | null }>
>({})

/** 展示顺序与配色跟管理端账号列表保持一致，免得两边对同一个号看出不同结论。 */
const USAGE_WINDOWS = [
  { key: 'five_hour', label: '5h', color: 'indigo' },
  { key: 'seven_day', label: '7d', color: 'emerald' },
  { key: 'seven_day_sonnet', label: '7d S', color: 'purple' },
  { key: 'seven_day_fable', label: '7d F', color: 'amber' },
] as const

function usageWindows(accountID: number) {
  const data = usageState[accountID]?.data
  if (!data) return []
  return USAGE_WINDOWS.flatMap((w) => {
    const window = data[w.key]
    // 上游没给这个窗口就整条不渲染，凭空补一个 0% 会被读成「这个号没用过」。
    return window ? [{ label: w.label, color: w.color, window }] : []
  })
}

/**
 * 拉当前页所有账号的被动用量。
 *
 * passive 不打外部 API，但仍是逐账号一次请求，所以限并发 —— 一页 100 条时
 * 一次性发出去只会让浏览器自己排队。
 */
async function loadUsage(list: ProviderAccount[]) {
  const queue = [...list]
  const workers = Array.from({ length: Math.min(4, queue.length) }, async () => {
    for (;;) {
      const account = queue.shift()
      if (!account) return
      usageState[account.id] = { loading: true, refreshing: false, data: null }
      try {
        usageState[account.id].data = await getAccountUsage(account.id)
      } catch {
        // 用量拿不到不该影响整张表：这一格留白就行，别把整页打成错误状态。
        usageState[account.id].data = null
      } finally {
        usageState[account.id].loading = false
      }
    }
  })
  await Promise.all(workers)
}

/** 主动查一次上游。被动采样可能是很久以前那次请求留下的。 */
async function refreshUsage(account: ProviderAccount) {
  const state = usageState[account.id] ?? { loading: false, refreshing: false, data: null }
  usageState[account.id] = { ...state, refreshing: true }
  try {
    usageState[account.id].data = await getAccountUsage(account.id, 'active')
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || t('provider.accounts.usageFailed')
  } finally {
    usageState[account.id].refreshing = false
  }
}

async function load() {
  loading.value = true
  try {
    const [data, info] = await Promise.all([
      listAccounts(pagination.page, pagination.page_size),
      getCurrentPeriodInfo(),
    ])
    accounts.value = data.items
    pagination.total = data.total
    periodStart.value = info.period_start
    // 不 await：表格先渲染出来，额度那一栏自己转圈补上。
    void loadUsage(data.items)
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || t('provider.accounts.loadFailed')
  } finally {
    loading.value = false
  }
}

function handlePageChange(page: number) {
  pagination.page = page
  void load()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  void load()
}

async function handlePause(account: ProviderAccount) {
  try {
    await pauseAccount(account.id)
    await load()
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || ''
  }
}

async function handleResume(account: ProviderAccount) {
  try {
    await resumeAccount(account.id)
    await load()
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || ''
  }
}

function askOffline(account: ProviderAccount) {
  offlineTarget.value = account
  offlineDialogOpen.value = true
}

async function handleOffline() {
  if (!offlineTarget.value) return
  try {
    await offlineAccount(offlineTarget.value.id)
    await load()
    // 下线的可能是本页最后一条，此时当前页变空。往前退一页重拉，
    // 否则用户会盯着一个空列表以为账号全没了。
    if (accounts.value.length === 0 && pagination.page > 1) {
      pagination.page -= 1
      await load()
    }
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || ''
  } finally {
    offlineTarget.value = null
    offlineDialogOpen.value = false
  }
}

async function openEdit(account: ProviderAccount) {
  editTarget.value = account
  editForm.name = account.name
  editForm.notes = account.notes || ''
  editForm.tier = account.tier
  dialogError.value = ''
  editOpen.value = true

  if (options.value) return
  loadingOptions.value = true
  try {
    options.value = await getOnboardOptions()
  } catch (error) {
    dialogError.value = (error as { message?: string })?.message || t('provider.onboard.loadFailed')
  } finally {
    loadingOptions.value = false
  }
}

function closeEdit() {
  editOpen.value = false
  editTarget.value = null
  dialogError.value = ''
}

async function handleSaveEdit() {
  if (!editTarget.value) return
  const target = editTarget.value
  const name = editForm.name.trim()
  if (!name) {
    dialogError.value = t('provider.accounts.nameRequired')
    return
  }
  saving.value = true
  dialogError.value = ''
  try {
    await updateAccount(target.id, {
      name,
      // 空串是「清空备注」，与不传不同 —— 这里恰好就是要这个语义。
      notes: editForm.notes,
      // 档位没变就不发：换档会重写 extra 并重新入队调度快照，没必要白跑一趟。
      // 编辑只在固定档之间切换，所以永远不带 custom_tier。
      tier: editForm.tier !== target.tier ? editForm.tier : undefined,
    })
    closeEdit()
    await load()
  } catch (error) {
    dialogError.value = (error as { message?: string })?.message || t('provider.accounts.editFailed')
  } finally {
    saving.value = false
  }
}

function openReauth(account: ProviderAccount) {
  reauthTarget.value = account
  reauthMode.value = 'cookie'
  reauthAuthURL.value = ''
  reauthForm.sessionKey = ''
  reauthForm.sessionID = ''
  reauthForm.code = ''
  dialogError.value = ''
  reauthOpen.value = true
}

function closeReauth() {
  reauthOpen.value = false
  reauthTarget.value = null
  dialogError.value = ''
}

async function handleGenerateReauthURL() {
  if (!reauthTarget.value) return
  generatingURL.value = true
  dialogError.value = ''
  try {
    // 不传类型：链接必须与账号原有类型一致，而这个类型不下发到供号商侧，
    // 由后端按账号自己决定生成哪种链接。
    const result = await generateReauthURL(reauthTarget.value.id)
    reauthAuthURL.value = result.auth_url
    reauthForm.sessionID = result.session_id
  } catch (error) {
    dialogError.value =
      (error as { message?: string })?.message || t('provider.onboard.generateFailed')
  } finally {
    generatingURL.value = false
  }
}

async function handleReauth() {
  if (!reauthTarget.value) return
  saving.value = true
  dialogError.value = ''
  try {
    await reauthAccount(reauthTarget.value.id, {
      session_key: reauthMode.value === 'cookie' ? reauthForm.sessionKey : undefined,
      session_id: reauthMode.value === 'manual' ? reauthForm.sessionID : undefined,
      code: reauthMode.value === 'manual' ? reauthForm.code : undefined,
    })
    closeReauth()
    await load()
  } catch (error) {
    dialogError.value =
      (error as { message?: string })?.message || t('provider.accounts.reauthFailed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
