<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <!-- 顶栏。刻意不复用 AppSidebar：那份菜单包含管理端与消费端入口，
         供号商站点必须是完全独立的导航，避免误露不该看到的功能。 -->
    <header
      class="sticky top-0 z-30 border-b border-gray-200 bg-white/90 backdrop-blur dark:border-dark-700 dark:bg-dark-900/90"
    >
      <div class="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
        <div class="flex items-center gap-8">
          <RouterLink to="/provider/dashboard" class="flex items-center gap-2">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-8 w-8 object-contain" />
            <span class="text-lg font-semibold text-slate-800 dark:text-dark-100">
              {{ t('provider.portalName') }}
            </span>
          </RouterLink>

          <nav class="hidden items-center gap-1 md:flex">
            <RouterLink
              v-for="item in navItems"
              :key="item.path"
              :to="item.path"
              class="rounded-lg px-3 py-2 text-sm font-medium transition-colors"
              :class="
                isActive(item.path)
                  ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-500/10 dark:text-indigo-300'
                  : 'text-gray-600 hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-800'
              "
            >
              {{ item.label }}
            </RouterLink>
          </nav>
        </div>

        <div class="flex items-center gap-3">
          <span class="hidden text-sm text-gray-500 dark:text-dark-400 sm:inline">
            {{ authStore.user?.email }}
          </span>
          <button
            type="button"
            class="rounded-lg px-3 py-2 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-800"
            @click="handleLogout"
          >
            {{ t('provider.logout') }}
          </button>
        </div>
      </div>

      <!-- 移动端导航 -->
      <nav class="flex items-center gap-1 overflow-x-auto border-t border-gray-200 px-4 py-2 dark:border-dark-700 md:hidden">
        <RouterLink
          v-for="item in navItems"
          :key="item.path"
          :to="item.path"
          class="whitespace-nowrap rounded-lg px-3 py-1.5 text-sm font-medium"
          :class="
            isActive(item.path)
              ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-500/10 dark:text-indigo-300'
              : 'text-gray-600 dark:text-dark-300'
          "
        >
          {{ item.label }}
        </RouterLink>
      </nav>
    </header>

    <main class="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      <slot />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { useAppStore, useAuthStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()

const siteLogo = computed(() =>
  sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true })
)

const navItems = computed(() => [
  { path: '/provider/dashboard', label: t('provider.nav.dashboard') },
  { path: '/provider/onboard', label: t('provider.nav.onboard') },
  { path: '/provider/accounts', label: t('provider.nav.accounts') },
  { path: '/provider/billing', label: t('provider.nav.billing') },
])

function isActive(path: string): boolean {
  return route.path === path || route.path.startsWith(`${path}/`)
}

async function handleLogout() {
  await authStore.logout()
  router.push('/provider/login')
}
</script>
