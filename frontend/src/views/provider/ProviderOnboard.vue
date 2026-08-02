<template>
  <ProviderLayout>
    <div class="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-dark-100">
          {{ t('provider.onboard.title') }}
        </h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('provider.onboard.subtitle') }}
        </p>
      </div>

      <div v-if="loadingOptions" class="py-12 text-center text-gray-500 dark:text-dark-400">
        {{ t('common.loading') }}
      </div>

      <form v-else class="space-y-6" @submit.prevent="handleSubmit">
        <!-- 基本信息 -->
        <section class="card space-y-4 p-5">
          <h2 class="font-semibold text-gray-900 dark:text-dark-100">
            {{ t('provider.onboard.sectionBasic') }}
          </h2>
          <div>
            <label class="input-label" for="onboard-name">{{ t('provider.onboard.name') }}</label>
            <input
              id="onboard-name"
              v-model.trim="form.name"
              type="text"
              :required="!isBatch"
              :disabled="isBatch"
              class="input disabled:cursor-not-allowed disabled:bg-gray-100 dark:disabled:bg-dark-800"
            />
            <p v-if="isBatch" class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('provider.onboard.batchAutoName') }}
            </p>
          </div>
          <div>
            <label class="input-label" for="onboard-notes">{{ t('provider.onboard.notes') }}</label>
            <input id="onboard-notes" v-model.trim="form.notes" type="text" class="input" />
          </div>
        </section>

        <!-- 托管类型：只显示对外文案，不暴露底层策略 -->
        <section class="card space-y-4 p-5">
          <h2 class="font-semibold text-gray-900 dark:text-dark-100">
            {{ t('provider.onboard.sectionHostingType') }}
          </h2>
          <div class="grid gap-3 sm:grid-cols-2">
            <label
              v-for="ht in options?.hosting_types || []"
              :key="ht.id"
              class="flex cursor-pointer gap-3 rounded-lg border p-4 transition-colors"
              :class="
                form.hostingTypeID === ht.id
                  ? 'border-indigo-500 bg-indigo-50 dark:border-indigo-400 dark:bg-indigo-500/10'
                  : 'border-gray-200 hover:border-gray-300 dark:border-dark-700'
              "
            >
              <input v-model="form.hostingTypeID" type="radio" :value="ht.id" class="mt-1" />
              <span>
                <span class="block font-medium text-gray-900 dark:text-dark-100">{{ ht.label }}</span>
                <span class="mt-1 block text-sm text-gray-500 dark:text-dark-400">
                  {{ ht.description }}
                </span>
              </span>
            </label>
          </div>
        </section>

        <!-- 速率档位 -->
        <section class="card space-y-4 p-5">
          <h2 class="font-semibold text-gray-900 dark:text-dark-100">
            {{ t('provider.onboard.sectionTier') }}
          </h2>
          <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <label
              v-for="tier in options?.tiers || []"
              :key="tier.tier"
              class="cursor-pointer rounded-lg border p-4 transition-colors"
              :class="
                form.tier === tier.tier
                  ? 'border-indigo-500 bg-indigo-50 dark:border-indigo-400 dark:bg-indigo-500/10'
                  : 'border-gray-200 hover:border-gray-300 dark:border-dark-700'
              "
            >
              <div class="flex items-center gap-2">
                <input v-model="form.tier" type="radio" :value="tier.tier" />
                <span class="font-medium text-gray-900 dark:text-dark-100">{{ tier.label }}</span>
              </div>
              <dl class="mt-2 space-y-0.5 text-xs text-gray-500 dark:text-dark-400">
                <div>{{ t('provider.onboard.concurrency') }}: {{ tier.concurrency }}</div>
                <div>{{ t('provider.onboard.maxSessions') }}: {{ tier.max_sessions }}</div>
                <div>{{ t('provider.onboard.baseRpm') }}: {{ tier.base_rpm }}</div>
                <div>
                  {{ t('provider.onboard.windowCostLimit') }}:
                  {{ tier.window_cost_limit > 0 ? `$${tier.window_cost_limit}` : t('provider.onboard.unlimited') }}
                </div>
              </dl>
            </label>

            <label
              v-if="options?.custom_tier.enabled"
              class="cursor-pointer rounded-lg border p-4 transition-colors"
              :class="
                form.tier === 'custom'
                  ? 'border-indigo-500 bg-indigo-50 dark:border-indigo-400 dark:bg-indigo-500/10'
                  : 'border-gray-200 hover:border-gray-300 dark:border-dark-700'
              "
            >
              <div class="flex items-center gap-2">
                <input v-model="form.tier" type="radio" value="custom" />
                <span class="font-medium text-gray-900 dark:text-dark-100">
                  {{ t('provider.onboard.customTier') }}
                </span>
              </div>
              <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">
                {{ t('provider.onboard.customTierHint') }}
              </p>
            </label>
          </div>

          <!-- 自定义档参数，显示各项上限 -->
          <div v-if="form.tier === 'custom' && options" class="grid gap-4 rounded-lg bg-gray-50 p-4 sm:grid-cols-2 dark:bg-dark-800">
            <div>
              <label class="input-label" for="custom-concurrency">
                {{ t('provider.onboard.concurrency') }}
                <span class="text-xs text-gray-400">
                  ({{ t('provider.onboard.max') }} {{ options.custom_tier.concurrency }})
                </span>
              </label>
              <input
                id="custom-concurrency"
                v-model.number="form.custom.concurrency"
                type="number"
                min="1"
                :max="options.custom_tier.concurrency"
                class="input"
              />
            </div>
            <div>
              <label class="input-label" for="custom-sessions">
                {{ t('provider.onboard.maxSessions') }}
                <span class="text-xs text-gray-400">
                  ({{ t('provider.onboard.max') }} {{ options.custom_tier.max_sessions }})
                </span>
              </label>
              <input
                id="custom-sessions"
                v-model.number="form.custom.max_sessions"
                type="number"
                min="1"
                :max="options.custom_tier.max_sessions"
                class="input"
              />
            </div>
            <div>
              <label class="input-label" for="custom-rpm">
                {{ t('provider.onboard.baseRpm') }}
                <span class="text-xs text-gray-400">
                  ({{ t('provider.onboard.max') }} {{ options.custom_tier.base_rpm }})
                </span>
              </label>
              <input
                id="custom-rpm"
                v-model.number="form.custom.base_rpm"
                type="number"
                min="1"
                :max="options.custom_tier.base_rpm"
                class="input"
              />
            </div>
            <div>
              <label class="input-label" for="custom-window">
                {{ t('provider.onboard.windowCostLimit') }}
                <span class="text-xs text-gray-400">
                  ({{ t('provider.onboard.max') }} {{ options.custom_tier.window_cost_limit }})
                </span>
              </label>
              <input
                id="custom-window"
                v-model.number="form.custom.window_cost_limit"
                type="number"
                min="1"
                :max="options.custom_tier.window_cost_limit"
                class="input"
              />
            </div>
          </div>
        </section>

        <!-- 出口 IP：平台分配或供号商自带 -->
        <section class="card space-y-4 p-5">
          <h2 class="font-semibold text-gray-900 dark:text-dark-100">
            {{ t('provider.onboard.sectionProxy') }}
          </h2>

          <div v-if="showProxyModeChoice" class="space-y-2">
            <span class="input-label">{{ t('provider.onboard.proxyMode') }}</span>
            <div class="flex flex-wrap gap-4">
              <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
                <input
                  v-model="form.proxyMode"
                  type="radio"
                  value="auto"
                  :disabled="!autoProxyAvailable"
                />
                <span>{{ t('provider.onboard.proxyModeAuto') }}</span>
              </label>
              <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
                <input v-model="form.proxyMode" type="radio" value="manual" />
                <span>{{ t('provider.onboard.proxyModeManual') }}</span>
              </label>
            </div>
          </div>

          <p v-if="proxyBlocked" class="text-sm text-red-600 dark:text-red-400">
            {{ t('provider.onboard.proxyBlocked') }}
          </p>
          <p
            v-else-if="autoProxyAllowed && !autoProxyAvailable"
            class="text-sm text-amber-600 dark:text-amber-400"
          >
            {{ t('provider.onboard.proxyAutoUnavailable') }}
          </p>

          <p
            v-if="form.proxyMode === 'auto' && autoProxyAvailable"
            class="text-sm text-gray-500 dark:text-dark-400"
          >
            {{ t('provider.onboard.proxyAutoHint') }}
          </p>

          <div v-if="form.proxyMode === 'manual' && manualProxyAllowed" class="space-y-2">
            <label class="input-label" for="proxy-url">{{ t('provider.onboard.proxyUrl') }}</label>
            <input
              id="proxy-url"
              v-model.trim="form.proxyUrl"
              type="text"
              required
              class="input font-mono text-xs"
              :placeholder="t('provider.onboard.proxyUrlPlaceholder')"
            />
            <p class="text-xs text-gray-500 dark:text-dark-400">
              {{ t('provider.onboard.proxyUrlHint') }}
            </p>
            <p v-if="parsedProxy" class="text-xs text-emerald-600 dark:text-emerald-400">
              {{ t('provider.onboard.proxyPreview', { address: parsedProxyAddress }) }}
              <span v-if="parsedProxyHasAuth">{{ t('provider.onboard.proxyPreviewAuth') }}</span>
            </p>
            <p v-else-if="form.proxyUrl" class="text-xs text-red-500 dark:text-red-400">
              {{ t('provider.onboard.proxyUrlInvalid') }}
            </p>
          </div>
        </section>

        <!-- 授权方式 -->
        <section class="card space-y-4 p-5">
          <h2 class="font-semibold text-gray-900 dark:text-dark-100">
            {{ t('provider.onboard.sectionAuth') }}
          </h2>

          <div>
            <label class="input-label">{{ t('provider.onboard.accountMethod') }}</label>
            <div class="flex gap-4">
              <label class="flex items-center gap-2">
                <input v-model="form.method" type="radio" value="oauth" />
                <span class="text-sm">{{ t('provider.onboard.methodOAuth') }}</span>
              </label>
              <label class="flex items-center gap-2">
                <input v-model="form.method" type="radio" value="setup-token" />
                <span class="text-sm">{{ t('provider.onboard.methodSetupToken') }}</span>
              </label>
            </div>
            <p class="mt-1.5 text-xs leading-relaxed text-gray-400">
              {{ t('provider.onboard.accountMethodHint') }}
            </p>
          </div>

          <div>
            <label class="input-label">{{ t('provider.onboard.authMode') }}</label>
            <div class="flex gap-4">
              <label class="flex items-center gap-2">
                <input v-model="authMode" type="radio" value="cookie" />
                <span class="text-sm">{{ t('provider.onboard.authModeCookie') }}</span>
              </label>
              <label class="flex items-center gap-2">
                <input v-model="authMode" type="radio" value="manual" />
                <span class="text-sm">{{ t('provider.onboard.authModeManual') }}</span>
              </label>
            </div>
            <p class="mt-1.5 text-xs leading-relaxed text-gray-400">
              {{ t('provider.onboard.authModeHint') }}
            </p>
          </div>

          <div v-if="authMode === 'cookie'">
            <label class="input-label" for="session-key">
              {{ t('provider.onboard.sessionKey') }}
            </label>
            <textarea
              id="session-key"
              v-model.trim="form.sessionKey"
              rows="6"
              class="input font-mono text-xs"
              :placeholder="t('provider.onboard.sessionKeyPlaceholder')"
            ></textarea>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('provider.onboard.sessionKeyHint') }}
            </p>
            <p v-if="isBatch" class="mt-1 text-xs text-indigo-600 dark:text-indigo-400">
              {{ t('provider.onboard.batchDetected', { count: sessionKeys.length }) }}
            </p>
          </div>

          <div v-else class="space-y-3">
            <button
              type="button"
              class="btn-secondary"
              :disabled="generatingURL"
              @click="handleGenerateURL"
            >
              {{ generatingURL ? t('common.loading') : t('provider.onboard.generateAuthURL') }}
            </button>
            <div v-if="authURL" class="space-y-2">
              <a
                :href="authURL"
                target="_blank"
                rel="noopener noreferrer"
                class="block break-all text-sm text-indigo-600 hover:underline dark:text-indigo-400"
              >
                {{ authURL }}
              </a>
              <div>
                <label class="input-label" for="auth-code">{{ t('provider.onboard.authCode') }}</label>
                <input id="auth-code" v-model.trim="form.code" type="text" class="input" />
              </div>
            </div>
          </div>
        </section>

        <!-- 批量进度。页面不能关：剩下的 key 由这个页面逐条发出去，走的是单账号接口。 -->
        <section v-if="batchItems.length" class="card space-y-3 p-5">
          <div class="flex items-center justify-between">
            <h2 class="font-semibold text-gray-900 dark:text-dark-100">
              {{ t('provider.onboard.batchProgress') }}
            </h2>
            <span class="text-sm text-gray-500 dark:text-dark-400">
              {{ batchDone }} / {{ batchItems.length }}
            </span>
          </div>

          <div class="h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
            <div
              class="h-full rounded-full bg-indigo-500 transition-all duration-300"
              :style="{ width: `${batchPercent}%` }"
            ></div>
          </div>

          <p class="text-sm text-gray-600 dark:text-dark-300">
            {{
              t('provider.onboard.batchSummary', {
                created: batchCreated,
                duplicate: batchDuplicate,
                failed: batchFailed,
              })
            }}
          </p>

          <ul class="max-h-64 space-y-1 overflow-y-auto">
            <li
              v-for="item in batchItems"
              :key="item.index"
              class="flex items-start justify-between gap-3 rounded px-2 py-1 text-sm odd:bg-gray-50 dark:odd:bg-dark-800/60"
            >
              <span class="shrink-0 font-mono text-xs text-gray-500 dark:text-dark-400">
                #{{ item.index }} {{ item.hint }}
              </span>
              <span class="text-right">
                <span :class="batchStatusClass(item.status)">
                  {{ t(`provider.onboard.batchStatus.${item.status}`) }}
                </span>
                <span v-if="item.name" class="ml-2 text-gray-700 dark:text-dark-200">
                  {{ item.name }}
                </span>
                <span v-if="item.message" class="block text-xs text-red-500 dark:text-red-400">
                  {{ item.message }}
                </span>
              </span>
            </li>
          </ul>

          <div v-if="batchFinished" class="flex flex-wrap gap-3">
            <button
              v-if="batchFailed > 0"
              type="button"
              class="btn-secondary"
              @click="retryFailed"
            >
              {{ t('provider.onboard.batchRetryFailed', { count: batchFailed }) }}
            </button>
            <RouterLink to="/provider/accounts" class="btn-primary">
              {{ t('provider.onboard.batchGoToAccounts') }}
            </RouterLink>
          </div>
        </section>

        <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
        <p v-if="successMessage" class="text-sm text-emerald-600 dark:text-emerald-400">
          {{ successMessage }}
        </p>

        <button type="submit" class="btn-primary w-full" :disabled="submitting || !proxyReady">
          {{
            submitting
              ? t('provider.onboard.submitting')
              : isBatch
                ? t('provider.onboard.submitBatch', { count: sessionKeys.length })
                : t('provider.onboard.submit')
          }}
        </button>
      </form>
    </div>
  </ProviderLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRouter } from 'vue-router'
import ProviderLayout from '@/components/layout/ProviderLayout.vue'
import { generateAuthURL, getOnboardOptions, onboard } from '@/api/provider'
import type { ProviderOnboardOptions, ProviderOnboardPayload, ProviderProxyMode } from '@/api/provider'
import { describeParsedProxy, parseProxyUrl } from '@/utils/proxyUrl'
import { resolveProviderProxyModeState } from '@/utils/providerProxyMode'

const { t } = useI18n()
const router = useRouter()

const loadingOptions = ref(true)
const submitting = ref(false)
const generatingURL = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const options = ref<ProviderOnboardOptions | null>(null)
const authURL = ref('')
const authMode = ref<'cookie' | 'manual'>('cookie')

const form = reactive({
  name: '',
  notes: '',
  method: 'oauth' as 'oauth' | 'setup-token',
  sessionKey: '',
  sessionID: '',
  code: '',
  hostingTypeID: 0,
  tier: '',
  proxyMode: 'auto' as ProviderProxyMode,
  proxyUrl: '',
  custom: { concurrency: 1, max_sessions: 1, base_rpm: 10, window_cost_limit: 20 },
})

const proxyModeState = computed(() =>
  resolveProviderProxyModeState(
    options.value?.proxy_mode_policy,
    options.value?.auto_proxy_available ?? false
  )
)
const autoProxyAllowed = computed(() => proxyModeState.value.autoAllowed)
const manualProxyAllowed = computed(() => proxyModeState.value.manualAllowed)
const autoProxyAvailable = computed(() => proxyModeState.value.autoAvailable)
const showProxyModeChoice = computed(() => proxyModeState.value.showChoice)

// 平台没有可分配的出口，又不允许自带——这时任何提交都必然失败，
// 与其让人填完一整张表再吃报错，不如提前说清楚。
const proxyBlocked = computed(() => proxyModeState.value.blocked)

// 预览按后端的落库规则来：allowSchemeless 对齐 ParseProviderProxyURL 支持的三种写法，
// upgradeSocks5 对齐网关拨号时对 socks5 的升级，否则预览显示的协议和实际存的不一致。
const parsedProxy = computed(() =>
  form.proxyMode === 'manual'
    ? parseProxyUrl(form.proxyUrl, { allowSchemeless: true, upgradeSocks5: true })
    : null
)
const parsedProxyAddress = computed(() =>
  parsedProxy.value ? describeParsedProxy(parsedProxy.value) : ''
)
const parsedProxyHasAuth = computed(() => Boolean(parsedProxy.value?.username))

const proxyReady = computed(() =>
  form.proxyMode === 'auto' ? autoProxyAvailable.value : manualProxyAllowed.value && !!parsedProxy.value
)

/** 批量上号中每一条的状态。duplicate 不是失败：这个号之前就上过，本次没有重复建。 */
type BatchItemStatus = 'pending' | 'running' | 'created' | 'duplicate' | 'failed'

interface BatchItem {
  index: number
  /** 原始 key，只用于失败重试，不渲染到界面上。 */
  key: string
  /** 脱敏后的显示串。 */
  hint: string
  status: BatchItemStatus
  /** 落库的账号名，通常就是该号的邮箱。 */
  name: string
  message: string
}

const batchItems = ref<BatchItem[]>([])

/**
 * 一行一个 session key。
 *
 * 供号商手上的 key 本来就是一行一条，逐条填一遍表单不现实 —— 之前只能整段当成
 * 一个 key 提交，必然换票失败。
 */
const sessionKeys = computed(() =>
  form.sessionKey
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
)

// 授权链接方式一个授权码只能换一个账号，天然不支持批量。
const isBatch = computed(() => authMode.value === 'cookie' && sessionKeys.value.length > 1)

const batchDone = computed(
  () => batchItems.value.filter((i) => i.status !== 'pending' && i.status !== 'running').length
)
const batchCreated = computed(() => batchItems.value.filter((i) => i.status === 'created').length)
const batchDuplicate = computed(() => batchItems.value.filter((i) => i.status === 'duplicate').length)
const batchFailed = computed(() => batchItems.value.filter((i) => i.status === 'failed').length)
const batchPercent = computed(() =>
  batchItems.value.length === 0 ? 0 : Math.round((batchDone.value / batchItems.value.length) * 100)
)
const batchFinished = computed(() => batchItems.value.length > 0 && !submitting.value)

/**
 * 只显示头尾各几位。
 *
 * 整串贴在界面上，一次截图或录屏就把凭据带出去了 —— 而供号商本来就是在
 * 「一次粘贴几十条」的场景下用这个页面。
 */
function maskSessionKey(key: string): string {
  if (key.length <= 12) {
    return `${key.slice(0, 4)}…`
  }
  return `${key.slice(0, 8)}…${key.slice(-4)}`
}

function batchStatusClass(status: BatchItemStatus): string {
  switch (status) {
    case 'created':
      return 'text-emerald-600 dark:text-emerald-400'
    case 'duplicate':
      return 'text-amber-600 dark:text-amber-400'
    case 'failed':
      return 'text-red-600 dark:text-red-400'
    case 'running':
      return 'text-indigo-600 dark:text-indigo-400'
    default:
      return 'text-gray-400'
  }
}

/** 除凭据以外的公共字段，批量时每条共用。 */
function basePayload(): ProviderOnboardPayload {
  return {
    notes: form.notes || null,
    method: form.method,
    proxy_mode: form.proxyMode,
    // 发原串而不是前端解析结果：解析规则以后端为准，两边一旦漂移，
    // 前端预览错了顶多显示不准，落库的才是对的。
    proxy_url: form.proxyMode === 'manual' ? form.proxyUrl : undefined,
    hosting_type_id: form.hostingTypeID,
    tier: form.tier,
    custom_tier: form.tier === 'custom' ? { ...form.custom } : null,
  }
}

onMounted(async () => {
  try {
    const data = await getOnboardOptions()
    options.value = data
    form.hostingTypeID = data.default_hosting_type_id || data.hosting_types[0]?.id || 0
    form.tier = data.default_tier || data.tiers[0]?.tier || ''
    // 初始来源由策略决定，不能只看库存：只开放平台出口时即便池子空了也停在 auto，
    // 否则页面会一边说「无法上号」一边把自带代理的输入框露出来。
    form.proxyMode = proxyModeState.value.initialMode
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || t('provider.onboard.loadFailed')
  } finally {
    loadingOptions.value = false
  }
})

async function handleGenerateURL() {
  generatingURL.value = true
  errorMessage.value = ''
  try {
    const result = await generateAuthURL(form.method)
    authURL.value = result.auth_url
    form.sessionID = result.session_id
  } catch (error) {
    errorMessage.value =
      (error as { message?: string })?.message || t('provider.onboard.generateFailed')
  } finally {
    generatingURL.value = false
  }
}

async function handleSubmit() {
  if (isBatch.value) {
    await submitBatch(sessionKeys.value)
    return
  }
  await submitSingle()
}

async function submitSingle() {
  submitting.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const result = await onboard({
      ...basePayload(),
      name: form.name || undefined,
      session_key: authMode.value === 'cookie' ? sessionKeys.value[0] : undefined,
      session_id: authMode.value === 'manual' ? form.sessionID : undefined,
      code: authMode.value === 'manual' ? form.code : undefined,
    })
    if (result.duplicate) {
      // 不跳转：这个号本来就在列表里，直接跳走的话供号商只会以为自己又上了一个，
      // 得让他看见「这条是重复的」。
      successMessage.value = t('provider.onboard.duplicate', { name: result.account.name })
      return
    }
    successMessage.value = t('provider.onboard.success')
    router.push('/provider/accounts')
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || t('provider.onboard.failed')
  } finally {
    submitting.value = false
  }
}

/**
 * 逐条串行提交。
 *
 * 串行不是为了省事：auto 代理模式下平台按「当前绑定最少」选出口，只有等上一条
 * 落库、绑定数 +1，下一条才会挑到别的 IP；并发发出去的话整批号会全挤在同一个出口上。
 * 换票打的又是 claude.ai，同 IP 高频请求本身也容易被风控。
 */
async function submitBatch(keys: string[]) {
  submitting.value = true
  errorMessage.value = ''
  successMessage.value = ''
  batchItems.value = keys.map((key, i) => ({
    index: i + 1,
    key,
    hint: maskSessionKey(key),
    status: 'pending' as BatchItemStatus,
    name: '',
    message: '',
  }))
  for (const item of batchItems.value) {
    await runBatchItem(item)
  }
  submitting.value = false
}

async function runBatchItem(item: BatchItem) {
  item.status = 'running'
  item.message = ''
  try {
    const result = await onboard({ ...basePayload(), session_key: item.key })
    item.status = result.duplicate ? 'duplicate' : 'created'
    item.name = result.account.name || result.account.email || ''
  } catch (error) {
    item.status = 'failed'
    item.message = (error as { message?: string })?.message || t('provider.onboard.failed')
  }
}

/** 只重跑失败项。成功与重复的不再动，避免把已经建好的号又走一遍换票。 */
async function retryFailed() {
  submitting.value = true
  for (const item of batchItems.value) {
    if (item.status === 'failed') {
      await runBatchItem(item)
    }
  }
  submitting.value = false
}
</script>
