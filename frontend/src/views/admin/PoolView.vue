<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.pool.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.pool.description') }}</p>
        </div>
        <div class="flex items-center gap-2">
          <span v-if="overview && !overview.runtime.enabled" class="badge badge-warning">{{ t('admin.pool.runtimeDisabled') }}</span>
          <span v-else-if="overview" class="badge badge-success">{{ t('admin.pool.owner') }}: {{ overview.runtime.owner_id || '—' }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="loading" @click="loadAll(true)">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
            {{ t('admin.pool.refresh') }}
          </button>
          <RouterLink to="/admin/accounts" class="btn btn-secondary btn-sm">{{ t('admin.pool.allAccounts') }}</RouterLink>
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2 rounded-xl border border-gray-200 bg-white px-3 py-2 text-xs dark:border-dark-700 dark:bg-dark-800">
        <span class="text-gray-500">{{ t('admin.pool.accountTypes') }}</span>
        <span class="badge badge-primary">{{ t('admin.pool.subscription') }}</span>
        <RouterLink to="/admin/accounts?type=apikey" class="badge badge-gray hover:underline">API Key / Bedrock</RouterLink>
        <RouterLink to="/admin/accounts?platform=openai" class="badge badge-gray hover:underline">OpenAI</RouterLink>
        <RouterLink to="/admin/accounts?platform=gemini" class="badge badge-gray hover:underline">Gemini</RouterLink>
        <RouterLink to="/admin/accounts?platform=antigravity" class="badge badge-gray hover:underline">Antigravity</RouterLink>
        <RouterLink to="/admin/accounts?platform=grok" class="badge badge-gray hover:underline">Grok</RouterLink>
        <RouterLink to="/admin/accounts" class="badge badge-gray hover:underline">{{ t('admin.pool.allAccounts') }}</RouterLink>
      </div>

      <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <div v-for="card in cards" :key="card.key" class="card p-4">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ card.label }}</p>
          <p class="mt-1 text-2xl font-bold text-gray-900 dark:text-white">{{ card.value }}</p>
          <p class="mt-1 text-xs" :class="card.tone">{{ card.hint }}</p>
        </div>
      </div>

      <div v-if="overview" class="grid grid-cols-2 gap-3 md:grid-cols-4">
        <div class="rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 text-xs dark:border-dark-700 dark:bg-dark-800">
          <span class="text-gray-500">{{ t('admin.pool.globalConcurrency') }}</span>
          <span class="ml-2 font-semibold text-gray-900 dark:text-white">{{ overview.runtime.pool.active_global }} / {{ overview.runtime.pool.global_limit || '∞' }}</span>
        </div>
        <div class="rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 text-xs dark:border-dark-700 dark:bg-dark-800">
          <span class="text-gray-500">{{ t('admin.pool.accountConcurrency') }}</span>
          <span class="ml-2 font-semibold text-gray-900 dark:text-white">{{ overview.runtime.pool.per_account_limit }}</span>
        </div>
        <div class="rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 text-xs dark:border-dark-700 dark:bg-dark-800">
          <span class="text-gray-500">{{ t('admin.pool.queue') }}</span>
          <span class="ml-2 font-semibold text-gray-900 dark:text-white">{{ overview.runtime.pool.waiting_accounts }} / {{ overview.runtime.pool.queue_limit || '∞' }}</span>
        </div>
        <div class="rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 text-xs dark:border-dark-700 dark:bg-dark-800">
          <span class="text-gray-500">{{ t('admin.pool.sessions') }}</span>
          <span class="ml-2 font-semibold text-gray-900 dark:text-white">{{ overview.runtime.sessions }}</span>
          <span v-if="overview.runtime.persistent_runs" class="ml-2 badge badge-success">persistent</span>
        </div>
      </div>

      <div class="flex gap-1 border-b border-gray-200 dark:border-dark-700">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          class="border-b-2 px-4 py-2 text-sm font-medium"
          :class="activeTab === tab.key ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400'"
          @click="activeTab = tab.key"
        >
          {{ tab.label }}
        </button>
      </div>

      <div v-if="loading && !overview" class="flex justify-center py-16"><LoadingSpinner /></div>

      <template v-else>
        <div v-if="activeTab === 'accounts'" class="card overflow-hidden">
          <div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-200 px-4 py-3 dark:border-dark-700">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.pool.accounts') }}</h2>
            <div class="flex items-center gap-2">
              <input v-model="accountSearch" class="input py-1 text-xs" :placeholder="t('admin.pool.searchAccounts')" />
              <select v-model="accountStatus" class="input py-1 text-xs">
                <option value="">{{ t('admin.pool.allStatus') }}</option>
                <option value="active">active</option>
                <option value="rate_limited">{{ t('admin.pool.rateLimited') }}</option>
                <option value="error">{{ t('admin.pool.error') }}</option>
              </select>
            </div>
          </div>
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
                <tr>
                  <th class="px-4 py-2">{{ t('admin.pool.accountName') }}</th>
                  <th class="px-4 py-2">{{ t('admin.pool.status') }}</th>
                  <th class="px-4 py-2">{{ t('admin.pool.type') }}</th>
                  <th class="px-4 py-2">{{ t('admin.pool.capacity') }}</th>
                  <th class="px-4 py-2">{{ t('admin.pool.usageWindow') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="account in filteredAccounts" :key="account.id" class="cursor-pointer border-t border-gray-100 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/40" @click="openAccount(account)">
                  <td class="px-4 py-3">
                    <div class="font-medium text-gray-900 dark:text-white">{{ account.name || `#${account.id}` }}</div>
                    <div class="text-xs text-gray-400">#{{ account.id }} · {{ account.priority ?? '—' }}</div>
                  </td>
                  <td class="px-4 py-3">
                    <span class="badge" :class="account.status === 'active' ? 'badge-success' : 'badge-gray'">{{ account.status }}</span>
                    <span v-if="account.schedulable === false" class="badge badge-warning ml-1">unschedulable</span>
                  </td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ account.type }}</td>
                  <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ account.current_concurrency ?? 0 }} / {{ account.concurrency ?? '∞' }}</td>
                  <td class="px-4 py-3 text-xs text-gray-500">
                    <div>5h: {{ fiveHourUsage(account) }}</div>
                    <div>7d: {{ sevenDayUsage(account) }}</div>
                  </td>
                </tr>
                <tr v-if="filteredAccounts.length === 0"><td colspan="5" class="px-4 py-10 text-center text-gray-500">{{ t('admin.pool.noAccounts') }}</td></tr>
              </tbody>
            </table>
          </div>
          <div v-if="accounts.length < accountsTotal" class="border-t border-gray-200 p-3 text-center dark:border-dark-700">
            <button class="btn btn-secondary btn-sm" :disabled="accountsLoading" @click="loadAccounts(false)">
              {{ t('admin.pool.loadMore') }}
            </button>
          </div>
        </div>

        <div v-else-if="activeTab === 'runs'" class="card overflow-hidden">
          <div class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.pool.runs') }}</h2>
          </div>
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead class="bg-gray-50 text-left text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
                <tr>
                  <th class="px-4 py-2">{{ t('admin.pool.session') }}</th>
                  <th class="px-4 py-2">{{ t('admin.pool.account') }}</th>
                  <th class="px-4 py-2">{{ t('admin.pool.state') }}</th>
                  <th class="px-4 py-2">{{ t('admin.pool.startedAt') }}</th>
                  <th class="px-4 py-2">{{ t('admin.pool.lastUsed') }}</th>
                  <th class="px-4 py-2"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="run in runs" :key="run.session" class="cursor-pointer border-t border-gray-100 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/40" @click="openRun(run)">
                  <td class="px-4 py-3 font-mono text-xs">{{ run.session }}</td>
                  <td class="px-4 py-3">{{ run.account_name || `#${run.account_id}` }}</td>
                  <td class="px-4 py-3"><span class="badge" :class="run.state === 'waiting_tool' ? 'badge-warning' : 'badge-success'">{{ run.state }}</span></td>
                  <td class="px-4 py-3 text-xs text-gray-500">{{ formatTime(run.started_at) }}</td>
                  <td class="px-4 py-3 text-xs text-gray-500">{{ formatTime(run.last_used_at) }}</td>
                  <td class="px-4 py-3 text-right">
                    <button class="btn btn-secondary btn-sm" :disabled="releasing === run.run_id" @click.stop="releaseRun(run)">
                      {{ t('admin.pool.release') }}
                    </button>
                  </td>
                </tr>
                <tr v-if="runs.length === 0"><td colspan="6" class="px-4 py-10 text-center text-gray-500">{{ t('admin.pool.noRuns') }}</td></tr>
              </tbody>
            </table>
          </div>
        </div>

        <div v-else-if="activeTab === 'groups'" class="card overflow-hidden">
          <div class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.pool.groups') }}</h2>
          </div>
          <div class="divide-y divide-gray-100 dark:divide-dark-700">
            <div v-for="group in groups" :key="group.id" class="flex items-center justify-between px-4 py-3">
              <div>
                <div class="text-sm font-medium text-gray-900 dark:text-white">{{ group.name }}</div>
                <div class="text-xs text-gray-500">{{ group.platform }} · {{ group.status }} · {{ group.account_count ?? 0 }} accounts</div>
              </div>
              <button class="btn btn-secondary btn-sm" @click="manageGroup(group)">{{ t('admin.pool.manageAccounts') }}</button>
            </div>
            <div v-if="groups.length === 0" class="px-4 py-10 text-center text-gray-500">{{ t('admin.pool.noGroups') }}</div>
          </div>
        </div>
        <div v-else class="card overflow-hidden">
          <div class="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-dark-700">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.pool.events') }}</h2>
            <select v-model="eventFilter" class="input py-1 text-xs">
              <option value="">{{ t('admin.pool.allEvents') }}</option>
              <option value="run_started">run_started</option>
              <option value="manual_release">manual_release</option>
              <option value="lease_lost">lease_lost</option>
              <option value="tool_timeout">tool_timeout</option>
            </select>
          </div>
          <div class="divide-y divide-gray-100 dark:divide-dark-700">
            <div v-for="event in filteredEvents" :key="event.time + event.type" class="flex flex-wrap items-center gap-2 px-4 py-2.5 text-xs">
              <span class="w-36 text-gray-500">{{ formatTime(event.time) }}</span>
              <span class="badge" :class="event.type === 'lease_lost' || event.type === 'tool_timeout' ? 'badge-warning' : 'badge-gray'">{{ event.type }}</span>
              <span v-if="event.session" class="font-mono text-gray-500">{{ event.session }}</span>
              <span class="text-gray-600 dark:text-gray-300">{{ event.message }}</span>
            </div>
            <div v-if="filteredEvents.length === 0" class="px-4 py-10 text-center text-gray-500">{{ t('admin.pool.noEvents') }}</div>
          </div>
        </div>
      </template>
    </div>

    <GroupAccountsModal :show="showGroupAccounts" :group="selectedGroup" @close="showGroupAccounts = false" @saved="loadAll" />
    <PoolAccountDrawer
      :show="showAccountDrawer"
      :account="selectedAccount"
      :active-run="selectedAccountRun"
      @close="showAccountDrawer = false"
    />
    <PoolRunDrawer
      :show="showRunDrawer"
      :run="selectedRun"
      @close="showRunDrawer = false"
      @release="(run) => { showRunDrawer = false; releaseRun(run) }"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import GroupAccountsModal from '@/components/admin/group/GroupAccountsModal.vue'
import PoolAccountDrawer from '@/components/admin/pool/PoolAccountDrawer.vue'
import PoolRunDrawer from '@/components/admin/pool/PoolRunDrawer.vue'
import { adminAPI } from '@/api/admin'
import type { Account, AdminGroup } from '@/types'
import type { PoolEvent, PoolOverview, PoolRun, PoolUsage } from '@/api/admin/pool'

const { t } = useI18n()
const loading = ref(false)
const overview = ref<PoolOverview | null>(null)
const accounts = ref<Account[]>([])
const runs = ref<PoolRun[]>([])
const groups = ref<AdminGroup[]>([])
const events = ref<PoolEvent[]>([])
const eventFilter = ref('')
const accountSearch = ref('')
const accountStatus = ref('')
const accountsLoading = ref(false)
const accountsPage = ref(1)
const accountsTotal = ref(0)
const usageByAccount = ref<Record<string, PoolUsage>>({})
let usageKey = ''
let searchTimer: ReturnType<typeof setTimeout> | undefined
const activeTab = ref<'accounts' | 'runs' | 'groups' | 'events'>('accounts')
const showGroupAccounts = ref(false)
const selectedGroup = ref<AdminGroup | null>(null)
const showAccountDrawer = ref(false)
const selectedAccount = ref<Account | null>(null)
const releasing = ref('')
const showRunDrawer = ref(false)
const selectedRun = ref<PoolRun | null>(null)
let timer: ReturnType<typeof setInterval> | undefined

const tabs = computed(() => [
  { key: 'accounts' as const, label: t('admin.pool.accounts') },
  { key: 'runs' as const, label: `${t('admin.pool.runs')} (${runs.value.length})` },
  { key: 'groups' as const, label: t('admin.pool.groups') },
  { key: 'events' as const, label: t('admin.pool.events') },
])

const filteredEvents = computed(() =>
  eventFilter.value ? events.value.filter((e) => e.type === eventFilter.value) : events.value,
)

const subscriptionAccounts = computed(() =>
  accounts.value.filter((a) => a.platform === 'anthropic' && (a.type === 'oauth' || a.type === 'setup-token')),
)

const filteredAccounts = computed(() => {
  return subscriptionAccounts.value.filter((a) => {
    if (accountStatus.value === 'rate_limited' && !a.rate_limit_reset_at) return false
    if (accountStatus.value === 'error' && !a.error_message) return false
    if (accountStatus.value === 'active' && a.status !== 'active') return false
    return true
  })
})

const cards = computed(() => {
  const a = overview.value?.accounts
  const r = overview.value?.runtime
  return [
    { key: 'total', label: t('admin.pool.total'), value: a?.total ?? '—', hint: `${a?.active ?? 0} active`, tone: 'text-gray-500' },
    { key: 'schedulable', label: t('admin.pool.schedulable'), value: a?.schedulable ?? '—', hint: `${a?.error ?? 0} ${t('admin.pool.error')}`, tone: 'text-green-600' },
    { key: 'limited', label: t('admin.pool.rateLimited'), value: a?.rate_limited ?? '—', hint: `${a?.total ? Math.round(((a.rate_limited || 0) / a.total) * 100) : 0}%`, tone: 'text-amber-600' },
    { key: 'runs', label: t('admin.pool.activeRuns'), value: r?.run_count ?? '—', hint: `${r?.waiting_tools ?? 0} ${t('admin.pool.waitingTools')}`, tone: 'text-blue-600' },
  ]
})

async function loadAccounts(reset = true) {
  accountsLoading.value = true
  try {
    const nextPage = reset ? 1 : accountsPage.value + 1
    const page = await adminAPI.pool.getAccounts({
      page: nextPage,
      page_size: 100,
      search: accountSearch.value.trim() || undefined,
    })
    accounts.value = reset ? page.items : [...accounts.value, ...page.items]
    accountsPage.value = nextPage
    accountsTotal.value = page.total
    if (reset) void loadUsage(true)
  } finally {
    accountsLoading.value = false
  }
}

function scheduleAccountSearch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => void loadAccounts(), 300)
}

async function loadAll(forceUsage = false) {
  loading.value = true
  try {
    const [ov, runResp, groupPage, eventResp] = await Promise.all([
      adminAPI.pool.getOverview(),
      adminAPI.pool.getRuns(),
      adminAPI.groups.list(1, 100, { platform: 'anthropic' }),
      adminAPI.pool.getEvents(),
      loadAccounts(true),
    ])
    overview.value = ov
    runs.value = runResp.items
    groups.value = groupPage.items
    events.value = eventResp.items
    await loadUsage(forceUsage)
  } finally {
    loading.value = false
  }
}

async function loadUsage(force: boolean) {
  const ids = subscriptionAccounts.value.slice(0, 50).map((a) => a.id)
  const key = ids.join(',')
  if (!force && key === usageKey) return
  usageKey = key
  if (ids.length === 0) {
    usageByAccount.value = {}
    return
  }
  try {
    const res = await adminAPI.pool.getUsageBatch(ids)
    usageByAccount.value = res.usage
  } catch {
    // 保留上一份 usage，不阻塞账号表
  }
}

const selectedAccountRun = computed(() => runs.value.find((r) => r.account_id === selectedAccount.value?.id) ?? null)

function openRun(run: PoolRun) {
  selectedRun.value = run
  showRunDrawer.value = true
}

function openAccount(account: Account) {
  selectedAccount.value = account
  showAccountDrawer.value = true
}

async function releaseRun(run: PoolRun) {
  if (!window.confirm(t('admin.pool.releaseConfirm', { session: run.session }))) return
  releasing.value = run.run_id
  try {
    await adminAPI.pool.releaseRun(run.run_id)
    await loadAll()
  } finally {
    releasing.value = ''
  }
}

function manageGroup(group: AdminGroup) {
  selectedGroup.value = group
  showGroupAccounts.value = true
}

function fiveHourUsage(account: Account) {
  const value = usageByAccount.value[String(account.id)]?.five_hour?.utilization
  return typeof value === 'number' ? `${Math.round(value)}%` : '—'
}

function sevenDayUsage(account: Account) {
  const value = usageByAccount.value[String(account.id)]?.seven_day?.utilization
  if (typeof value === 'number') return `${Math.round(value)}%`
  const raw = account.extra?.passive_usage_7d_utilization
  const fallback = typeof raw === 'number' ? raw : Number(raw)
  return Number.isFinite(fallback) ? `${Math.round(fallback * 100)}%` : '—'
}

function formatTime(value: string | null | undefined) {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

watch(accountSearch, scheduleAccountSearch)

onMounted(() => {
  void loadAll()
  timer = setInterval(() => void loadAll(), 20000)
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  if (searchTimer) clearTimeout(searchTimer)
})
</script>
