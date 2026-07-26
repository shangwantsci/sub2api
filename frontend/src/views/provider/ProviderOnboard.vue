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
            <input id="onboard-name" v-model.trim="form.name" type="text" required class="input" />
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

        <!-- 代理：供号商自带，必填 -->
        <section class="card space-y-4 p-5">
          <h2 class="font-semibold text-gray-900 dark:text-dark-100">
            {{ t('provider.onboard.sectionProxy') }}
          </h2>
          <p class="text-sm text-gray-500 dark:text-dark-400">
            {{ t('provider.onboard.proxyHint') }}
          </p>
          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <label class="input-label" for="proxy-protocol">
                {{ t('provider.onboard.proxyProtocol') }}
              </label>
              <select id="proxy-protocol" v-model="form.proxy.protocol" class="input">
                <option value="http">http</option>
                <option value="https">https</option>
                <option value="socks5">socks5</option>
                <option value="socks5h">socks5h</option>
              </select>
            </div>
            <div>
              <label class="input-label" for="proxy-host">{{ t('provider.onboard.proxyHost') }}</label>
              <input id="proxy-host" v-model.trim="form.proxy.host" type="text" required class="input" />
            </div>
            <div>
              <label class="input-label" for="proxy-port">{{ t('provider.onboard.proxyPort') }}</label>
              <input
                id="proxy-port"
                v-model.number="form.proxy.port"
                type="number"
                min="1"
                max="65535"
                required
                class="input"
              />
            </div>
            <div>
              <label class="input-label" for="proxy-username">
                {{ t('provider.onboard.proxyUsername') }}
              </label>
              <input id="proxy-username" v-model.trim="form.proxy.username" type="text" class="input" />
            </div>
            <div>
              <label class="input-label" for="proxy-password">
                {{ t('provider.onboard.proxyPassword') }}
              </label>
              <input id="proxy-password" v-model="form.proxy.password" type="password" class="input" />
            </div>
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
          </div>

          <div v-if="authMode === 'cookie'">
            <label class="input-label" for="session-key">
              {{ t('provider.onboard.sessionKey') }}
            </label>
            <textarea
              id="session-key"
              v-model.trim="form.sessionKey"
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

        <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
        <p v-if="successMessage" class="text-sm text-emerald-600 dark:text-emerald-400">
          {{ successMessage }}
        </p>

        <button type="submit" class="btn-primary w-full" :disabled="submitting">
          {{ submitting ? t('provider.onboard.submitting') : t('provider.onboard.submit') }}
        </button>
      </form>
    </div>
  </ProviderLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import ProviderLayout from '@/components/layout/ProviderLayout.vue'
import { generateAuthURL, getOnboardOptions, onboard } from '@/api/provider'
import type { ProviderOnboardOptions } from '@/api/provider'

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
  proxy: { protocol: 'http', host: '', port: 8080, username: '', password: '' },
  custom: { concurrency: 1, max_sessions: 1, base_rpm: 10, window_cost_limit: 20 },
})

onMounted(async () => {
  try {
    const data = await getOnboardOptions()
    options.value = data
    form.hostingTypeID = data.default_hosting_type_id || data.hosting_types[0]?.id || 0
    form.tier = data.default_tier || data.tiers[0]?.tier || ''
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
  submitting.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await onboard({
      name: form.name,
      notes: form.notes || null,
      method: form.method,
      session_key: authMode.value === 'cookie' ? form.sessionKey : undefined,
      session_id: authMode.value === 'manual' ? form.sessionID : undefined,
      code: authMode.value === 'manual' ? form.code : undefined,
      proxy: {
        protocol: form.proxy.protocol,
        host: form.proxy.host,
        port: form.proxy.port,
        username: form.proxy.username || undefined,
        password: form.proxy.password || undefined,
      },
      hosting_type_id: form.hostingTypeID,
      tier: form.tier,
      custom_tier: form.tier === 'custom' ? { ...form.custom } : null,
    })
    successMessage.value = t('provider.onboard.success')
    router.push('/provider/accounts')
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || t('provider.onboard.failed')
  } finally {
    submitting.value = false
  }
}
</script>
