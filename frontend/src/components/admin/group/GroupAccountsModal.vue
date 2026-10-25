<template>
  <div v-if="show" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
    <div class="flex max-h-[85vh] w-full max-w-2xl flex-col rounded-2xl bg-white shadow-xl dark:bg-dark-800">
      <div class="flex items-center justify-between border-b px-5 py-4 dark:border-dark-700">
        <div>
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.groups.groupAccountsTitle') }}</h3>
          <p class="mt-0.5 text-xs text-gray-500">{{ group?.name }} · {{ t('admin.groups.groupAccountsHint') }}</p>
        </div>
        <button class="text-gray-400 hover:text-gray-600" @click="$emit('close')">✕</button>
      </div>
      <div class="border-b px-5 py-3 dark:border-dark-700">
        <input v-model="search" class="input w-full" :placeholder="t('admin.groups.groupAccountsSearch')" />
      </div>
      <div class="min-h-0 flex-1 overflow-auto px-5 py-3">
        <p v-if="error" class="mb-2 rounded bg-red-50 px-3 py-2 text-xs text-red-600">{{ error }}</p>
        <p v-if="loading" class="py-8 text-center text-sm text-gray-500">…</p>
        <p v-else-if="accounts.length === 0" class="py-8 text-center text-sm text-gray-500">{{ t('admin.groups.groupAccountsEmpty') }}</p>
        <label v-for="account in accounts" :key="account.id" class="flex cursor-pointer items-center gap-3 rounded-lg px-2 py-2 hover:bg-gray-50 dark:hover:bg-dark-700">
          <input type="checkbox" :checked="selected.has(account.id)" @change="toggle(account.id)" />
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm text-gray-900 dark:text-white">{{ account.name || ('#' + account.id) }}</span>
            <span class="block truncate text-xs text-gray-500">{{ account.platform }} · {{ account.type }} · {{ account.status }}</span>
          </span>
        </label>
        <button v-if="accounts.length < total" class="btn btn-secondary mt-2 w-full" :disabled="loadingMore" @click="loadMore">
          {{ t('admin.groups.groupAccountsLoadMore') }}
        </button>
      </div>
      <div class="flex justify-end gap-2 border-t px-5 py-4 dark:border-dark-700">
        <button class="btn btn-secondary" @click="$emit('close')">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" data-testid="save-accounts" :disabled="saving" @click="save">{{ t('admin.groups.groupAccountsSave') }}</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Account, AdminGroup } from '@/types'

const props = defineProps<{ show: boolean; group: AdminGroup | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()

const loading = ref(false)
const loadingMore = ref(false)
const saving = ref(false)
const error = ref("")
const search = ref('')
const accounts = ref<Account[]>([])
const selected = ref(new Set<number>())
const page = ref(1)
const total = ref(0)

let searchTimer: ReturnType<typeof setTimeout> | undefined

async function loadPage(reset: boolean) {
  if (!props.group) return
  const nextPage = reset ? 1 : page.value + 1
  if (reset) loading.value = true
  else loadingMore.value = true
  try {
    const result = await adminAPI.accounts.list(nextPage, 100, {
      platform: props.group.platform,
      search: search.value.trim() || undefined,
    })
    if (reset) accounts.value = result.items
    else accounts.value = [...accounts.value, ...result.items]
    page.value = nextPage
    total.value = result.total
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

async function load() {
  if (!props.show || !props.group) return
  loading.value = true
  try {
    const bound = await adminAPI.groups.getGroupAccounts(props.group.id)
    selected.value = new Set(bound.map((a) => a.id))
    await loadPage(true)
    if (bound.length > 0) {
      const byID = new Map(accounts.value.map((a) => [a.id, a]))
      for (const a of bound) {
        if (!byID.has(a.id)) byID.set(a.id, a)
      }
      accounts.value = [...byID.values()]
    }
  } finally {
    loading.value = false
  }
}

function loadMore() {
  if (loadingMore.value || accounts.value.length >= total.value) return
  void loadPage(false)
}

function scheduleSearch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    page.value = 1
    void loadPage(true)
  }, 300)
}

function toggle(id: number) {
  const next = new Set(selected.value)
  next.has(id) ? next.delete(id) : next.add(id)
  selected.value = next
}

async function save() {
  if (!props.group) return
  saving.value = true
  error.value = ""
  try {
    await adminAPI.groups.setGroupAccounts(props.group.id, [...selected.value])
    emit('saved')
    emit('close')
  } catch (e: any) {
    error.value = e?.response?.data?.detail || e?.message || 'save failed'
  } finally {
    saving.value = false
  }
}

watch(() => [props.show, props.group?.id], load, { immediate: true })
watch(search, scheduleSearch)
watch(() => props.show, (show) => { if (!show && searchTimer) clearTimeout(searchTimer) })
</script>
