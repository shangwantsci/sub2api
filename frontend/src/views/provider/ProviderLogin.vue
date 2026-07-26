<template>
  <ProviderAuthLayout>
    <form class="space-y-5" @submit.prevent="handleSubmit">
      <h2 class="text-xl font-semibold text-gray-900 dark:text-dark-100">
        {{ t('provider.auth.loginTitle') }}
      </h2>

      <div>
        <label class="input-label" for="provider-login-email">{{ t('provider.auth.email') }}</label>
        <input
          id="provider-login-email"
          v-model.trim="form.email"
          type="email"
          autocomplete="email"
          required
          class="input"
        />
      </div>

      <div>
        <label class="input-label" for="provider-login-password">
          {{ t('provider.auth.password') }}
        </label>
        <input
          id="provider-login-password"
          v-model="form.password"
          type="password"
          autocomplete="current-password"
          required
          class="input"
        />
      </div>

      <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>

      <button type="submit" class="btn-primary w-full" :disabled="loading">
        {{ loading ? t('provider.auth.loggingIn') : t('provider.auth.login') }}
      </button>
    </form>

    <template #footer>
      <RouterLink to="/provider/register" class="text-indigo-600 hover:underline dark:text-indigo-400">
        {{ t('provider.auth.needAccount') }}
      </RouterLink>
    </template>
  </ProviderAuthLayout>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter, RouterLink } from 'vue-router'
import ProviderAuthLayout from '@/components/layout/ProviderAuthLayout.vue'
import { useAuthStore } from '@/stores'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()

const form = reactive({ email: '', password: '' })
const loading = ref(false)
const errorMessage = ref('')

async function handleSubmit() {
  loading.value = true
  errorMessage.value = ''
  try {
    await authStore.login({ email: form.email, password: form.password })
    if (!authStore.isProvider) {
      // 非供号商登录了供号商入口：清掉会话并明确提示，而不是把人放进一个空面板。
      await authStore.logout()
      errorMessage.value = t('provider.auth.notAProvider')
      return
    }
    router.push('/provider/dashboard')
  } catch (error) {
    errorMessage.value = (error as { message?: string })?.message || t('provider.auth.loginFailed')
  } finally {
    loading.value = false
  }
}
</script>
