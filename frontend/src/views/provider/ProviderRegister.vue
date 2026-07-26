<template>
  <ProviderAuthLayout>
    <form class="space-y-5" @submit.prevent="handleSubmit">
      <h2 class="text-xl font-semibold text-gray-900 dark:text-dark-100">
        {{ t('provider.auth.registerTitle') }}
      </h2>
      <p class="text-sm text-gray-500 dark:text-dark-400">
        {{ t('provider.auth.registerHint') }}
      </p>

      <div>
        <label class="input-label" for="provider-reg-invite">
          {{ t('provider.auth.inviteCode') }}
        </label>
        <input
          id="provider-reg-invite"
          v-model.trim="form.inviteCode"
          type="text"
          required
          class="input"
          :placeholder="t('provider.auth.inviteCodePlaceholder')"
        />
      </div>

      <div>
        <label class="input-label" for="provider-reg-email">{{ t('provider.auth.email') }}</label>
        <input
          id="provider-reg-email"
          v-model.trim="form.email"
          type="email"
          autocomplete="email"
          required
          class="input"
        />
      </div>

      <div>
        <label class="input-label" for="provider-reg-password">
          {{ t('provider.auth.password') }}
        </label>
        <input
          id="provider-reg-password"
          v-model="form.password"
          type="password"
          autocomplete="new-password"
          required
          minlength="6"
          class="input"
        />
      </div>

      <div v-if="emailVerifyEnabled">
        <label class="input-label" for="provider-reg-code">
          {{ t('provider.auth.verifyCode') }}
        </label>
        <div class="flex gap-2">
          <input id="provider-reg-code" v-model.trim="form.verifyCode" type="text" class="input flex-1" />
          <button
            type="button"
            class="btn-secondary whitespace-nowrap"
            :disabled="sendingCode || countdown > 0 || !form.email"
            @click="handleSendCode"
          >
            {{ countdown > 0 ? `${countdown}s` : t('provider.auth.sendCode') }}
          </button>
        </div>
      </div>

      <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>

      <button type="submit" class="btn-primary w-full" :disabled="loading">
        {{ loading ? t('provider.auth.registering') : t('provider.auth.register') }}
      </button>
    </form>

    <template #footer>
      <RouterLink to="/provider/login" class="text-indigo-600 hover:underline dark:text-indigo-400">
        {{ t('provider.auth.haveAccount') }}
      </RouterLink>
    </template>
  </ProviderAuthLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter, RouterLink } from 'vue-router'
import ProviderAuthLayout from '@/components/layout/ProviderAuthLayout.vue'
import { register as registerProvider, sendVerifyCode } from '@/api/provider'
import { useAppStore, useAuthStore } from '@/stores'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()

const form = reactive({ email: '', password: '', verifyCode: '', inviteCode: '' })
const loading = ref(false)
const sendingCode = ref(false)
const countdown = ref(0)
const errorMessage = ref('')

const emailVerifyEnabled = computed(
  () => appStore.cachedPublicSettings?.email_verify_enabled === true
)

onMounted(() => {
  appStore.fetchPublicSettings()
})

async function handleSendCode() {
  sendingCode.value = true
  errorMessage.value = ''
  try {
    await sendVerifyCode(form.email)
    countdown.value = 60
    const timer = setInterval(() => {
      countdown.value -= 1
      if (countdown.value <= 0) clearInterval(timer)
    }, 1000)
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || t('provider.auth.sendCodeFailed')
  } finally {
    sendingCode.value = false
  }
}

async function handleSubmit() {
  loading.value = true
  errorMessage.value = ''
  try {
    await registerProvider({
      email: form.email,
      password: form.password,
      verify_code: form.verifyCode || undefined,
      invite_code: form.inviteCode,
    })
    // 注册接口返回的 token 结构与普通登录不同，直接用凭据登录一次，
    // 让 auth store 走统一的会话建立路径（含 refresh token 与自动续期）。
    await authStore.login({ email: form.email, password: form.password })
    router.push('/provider/dashboard')
  } catch (error) {
    errorMessage.value =
      (error as { message?: string })?.message || t('provider.auth.registerFailed')
  } finally {
    loading.value = false
  }
}
</script>
