<template>
  <div class="card" data-testid="capacity-presets-card">
    <div
      class="flex flex-col gap-3 border-b border-gray-100 px-6 py-4 dark:border-dark-700 lg:flex-row lg:items-start lg:justify-between"
    >
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
          {{ t("admin.settings.capacityPresets.title") }}
        </h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.capacityPresets.description") }}
        </p>
        <p class="mt-1 text-xs text-amber-600 dark:text-amber-400">
          {{ t("admin.settings.capacityPresets.onlyAffectsFuture") }}
        </p>
        <p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
          {{ t("admin.settings.capacityPresets.savedSeparately") }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          data-testid="capacity-presets-add"
          :disabled="loading || saving"
          @click="addRow"
        >
          {{ t("admin.settings.capacityPresets.addRow") }}
        </button>
        <button
          type="button"
          class="btn btn-primary btn-sm"
          data-testid="capacity-presets-save"
          :disabled="loading || saving || loadFailed"
          @click="save"
        >
          {{ saving ? t("common.saving") : t("common.save") }}
        </button>
      </div>
    </div>

    <div class="space-y-4 p-6">
      <div
        v-if="loading"
        class="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400"
      >
        <span class="h-4 w-4 animate-spin rounded-full border-b-2 border-primary-600"></span>
        {{ t("common.loading") }}
      </div>

      <div
        v-else-if="loadFailed"
        class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
        data-testid="capacity-presets-load-failed"
      >
        {{ t("admin.settings.capacityPresets.loadFailed") }}
        <button
          type="button"
          class="ml-2 underline"
          data-testid="capacity-presets-retry"
          @click="load"
        >
          {{ t("common.retry") }}
        </button>
      </div>

      <template v-else>
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t("admin.settings.capacityPresets.colDefault") }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t("admin.settings.capacityPresets.colKey") }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t("admin.settings.capacityPresets.colLabel") }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t("admin.settings.capacityPresets.colConcurrency") }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t("admin.settings.capacityPresets.colMaxSessions") }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t("admin.settings.capacityPresets.colBaseRpm") }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t("admin.settings.capacityPresets.colWindowCost") }}
                </th>
                <th class="px-3 py-2 text-left text-xs uppercase text-gray-500">
                  {{ t("admin.settings.capacityPresets.colAdaptive") }}
                </th>
                <th class="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <tr v-for="(row, index) in rows" :key="row.uid" data-testid="capacity-presets-row">
                <td class="px-3 py-2">
                  <input
                    v-model="defaultUid"
                    type="radio"
                    name="capacity-presets-default"
                    data-field="default"
                    :value="row.uid"
                    :aria-label="t('admin.settings.capacityPresets.colDefault')"
                    @keydown.enter.prevent
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.trim="row.key"
                    type="text"
                    data-field="key"
                    class="input w-24 py-1 text-sm"
                    :class="invalidClass(index, 'key')"
                    :readonly="!row.isNew"
                    :title="row.isNew ? '' : t('admin.settings.capacityPresets.keyReadonlyHint')"
                    @keydown.enter.prevent
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.trim="row.label"
                    type="text"
                    data-field="label"
                    class="input w-28 py-1 text-sm"
                    :class="invalidClass(index, 'label')"
                    @keydown.enter.prevent
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="row.concurrency"
                    type="number"
                    data-field="concurrency"
                    min="1"
                    :max="MAX_CONCURRENCY"
                    class="input w-20 py-1 text-sm"
                    :class="invalidClass(index, 'concurrency')"
                    @keydown.enter.prevent
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="row.max_sessions"
                    type="number"
                    data-field="max_sessions"
                    min="0"
                    :max="MAX_SESSIONS"
                    class="input w-20 py-1 text-sm"
                    :class="invalidClass(index, 'max_sessions')"
                    @keydown.enter.prevent
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="row.base_rpm"
                    type="number"
                    data-field="base_rpm"
                    min="0"
                    :max="MAX_RPM"
                    class="input w-20 py-1 text-sm"
                    :class="invalidClass(index, 'base_rpm')"
                    @keydown.enter.prevent
                  />
                </td>
                <td class="px-3 py-2">
                  <input
                    v-model.number="row.window_cost_limit"
                    type="number"
                    data-field="window_cost_limit"
                    min="0"
                    :max="MAX_WINDOW_COST"
                    class="input w-24 py-1 text-sm"
                    :class="invalidClass(index, 'window_cost_limit')"
                    @keydown.enter.prevent
                  />
                </td>
                <td class="px-3 py-2">
                  <Toggle v-model="row.adaptive_throttle" data-field="adaptive_throttle" />
                </td>
                <td class="px-3 py-2 text-right">
                  <button
                    type="button"
                    class="whitespace-nowrap text-sm text-red-600 hover:underline dark:text-red-400"
                    data-testid="capacity-presets-delete"
                    :disabled="rows.length <= 1"
                    @click="removeRow(index)"
                  >
                    {{ t("common.delete") }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="text-xs text-gray-400">{{ t("admin.settings.capacityPresets.zeroHint") }}</p>

        <ul
          v-if="errors.length > 0"
          class="list-inside list-disc space-y-1 text-sm text-red-600 dark:text-red-400"
          data-testid="capacity-presets-errors"
        >
          <li v-for="message in errors" :key="message">{{ message }}</li>
        </ul>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 容量档位模板：新建 / 批量账号时「选档位」就是把这里的数值复制进账号。
 *
 * 只管模板本身（名称、数值、动态降速、默认档位）；「哪些模板对供货商开放」归供货商设置面板，
 * 两处各写各的列，避免互相回滚。key 创建后只读，数值元组（并发、会话数、RPM、5h 额度）不允许重复，
 * 否则按数值反推档位名时会有歧义。
 *
 * 设置页整体是一个 <form>：这里所有输入都拦住回车，所有按钮都是 type="button"，
 * 避免敲回车把整个设置页提交掉。
 */
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { adminAPI } from "@/api";
import type { CapacityPreset, CapacityPresetsSettings } from "@/api/admin/settings";
import Toggle from "@/components/common/Toggle.vue";
import { useCapacityPresets } from "@/composables/useCapacityPresets";
import {
  MAX_CONCURRENCY,
  MAX_RPM,
  MAX_SESSIONS,
  MAX_WINDOW_COST,
} from "@/constants/capacity";
import { useAppStore } from "@/stores";
import { extractApiErrorMessage } from "@/utils/apiError";

/** 与后端保留字一致：key 不能叫 custom（「自定义」是界面上的派生显示，不是模板）。 */
const RESERVED_KEY = "custom";

interface EditableRow extends CapacityPreset {
  /** 仅用于 v-for 的稳定 key，不提交 */
  uid: number;
  /** 本次编辑中新增、尚未保存过：只有这种行的 key 可以改 */
  isNew: boolean;
}

const { t } = useI18n();
const appStore = useAppStore();
const { setPresets } = useCapacityPresets();

const loading = ref(true);
const loadFailed = ref(false);
const saving = ref(false);
const rows = ref<EditableRow[]>([]);
/** 默认档位按行 id 记，而不是按 key：新增行的 key 还在编辑时，默认指向不会跟丢。 */
const defaultUid = ref<number | null>(null);
const errors = ref<string[]>([]);
const invalidCells = ref<Set<string>>(new Set());

let nextUid = 1;

function toRow(preset: CapacityPreset, isNew: boolean): EditableRow {
  return { ...preset, uid: nextUid++, isNew };
}

function applyLoaded(data: CapacityPresetsSettings) {
  rows.value = data.presets.map((preset) => toRow(preset, false));
  defaultUid.value =
    (rows.value.find((row) => row.key === data.default_key) ?? rows.value[0])?.uid ?? null;
  errors.value = [];
  invalidCells.value = new Set();
}

async function load() {
  loading.value = true;
  loadFailed.value = false;
  try {
    const data = await adminAPI.settings.getCapacityPresets();
    if (!data || !Array.isArray(data.presets)) throw new Error("invalid payload");
    applyLoaded(data);
  } catch {
    loadFailed.value = true;
  } finally {
    loading.value = false;
  }
}

function addRow() {
  rows.value.push(
    toRow(
      {
        key: "",
        label: "",
        concurrency: 1,
        max_sessions: 0,
        base_rpm: 0,
        window_cost_limit: 0,
        adaptive_throttle: true,
      },
      true,
    ),
  );
}

function removeRow(index: number) {
  if (rows.value.length <= 1) return;
  const [removed] = rows.value.splice(index, 1);
  // 删掉的是默认档位：默认改指第一个剩余模板，免得保存时报「默认档位不存在」
  if (removed.uid === defaultUid.value) {
    defaultUid.value = rows.value[0]?.uid ?? null;
  }
  errors.value = [];
  invalidCells.value = new Set();
}

function invalidClass(index: number, field: string) {
  return invalidCells.value.has(`${index}:${field}`) ? "border-red-500" : "";
}

const isInt = (value: unknown): value is number =>
  typeof value === "number" && Number.isInteger(value);

/** 前端字段级校验，后端会再校验一遍，两边都不可省。 */
function validate(): boolean {
  const messages: string[] = [];
  const invalid = new Set<string>();
  const flag = (index: number, field: string, message: string) => {
    invalid.add(`${index}:${field}`);
    messages.push(t("admin.settings.capacityPresets.rowError", { row: index + 1, message }));
  };

  const seenKeys = new Map<string, number>();
  const seenTuples = new Map<string, number>();

  rows.value.forEach((row, index) => {
    const key = row.key.trim();
    if (!key) {
      flag(index, "key", t("admin.settings.capacityPresets.errKeyRequired"));
    } else if (key.toLowerCase() === RESERVED_KEY) {
      flag(index, "key", t("admin.settings.capacityPresets.errKeyReserved"));
    } else if (seenKeys.has(key)) {
      flag(index, "key", t("admin.settings.capacityPresets.errKeyDuplicate"));
    } else {
      seenKeys.set(key, index);
    }

    if (!row.label.trim()) {
      flag(index, "label", t("admin.settings.capacityPresets.errLabelRequired"));
    }
    if (!isInt(row.concurrency) || row.concurrency < 1 || row.concurrency > MAX_CONCURRENCY) {
      flag(index, "concurrency", t("admin.settings.capacityPresets.errConcurrency", { max: MAX_CONCURRENCY }));
    }
    if (!isInt(row.max_sessions) || row.max_sessions < 0 || row.max_sessions > MAX_SESSIONS) {
      flag(index, "max_sessions", t("admin.settings.capacityPresets.errMaxSessions", { max: MAX_SESSIONS }));
    }
    if (!isInt(row.base_rpm) || row.base_rpm < 0 || row.base_rpm > MAX_RPM) {
      flag(index, "base_rpm", t("admin.settings.capacityPresets.errBaseRpm", { max: MAX_RPM }));
    }
    if (
      typeof row.window_cost_limit !== "number" ||
      !Number.isFinite(row.window_cost_limit) ||
      row.window_cost_limit < 0 ||
      row.window_cost_limit > MAX_WINDOW_COST
    ) {
      flag(index, "window_cost_limit", t("admin.settings.capacityPresets.errWindowCost", { max: MAX_WINDOW_COST }));
    }

    // 4 个数值组成的元组必须唯一：账号上不存档位名，靠数值反推，重复就分不清是哪个
    const tuple = [row.concurrency, row.max_sessions, row.base_rpm, row.window_cost_limit].join("/");
    const firstSeen = seenTuples.get(tuple);
    if (firstSeen === undefined) {
      seenTuples.set(tuple, index);
    } else {
      flag(index, "concurrency", t("admin.settings.capacityPresets.errTupleDuplicate", { other: firstSeen + 1 }));
      invalid.add(`${firstSeen}:concurrency`);
    }
  });

  if (!rows.value.some((row) => row.uid === defaultUid.value)) {
    messages.push(t("admin.settings.capacityPresets.errDefaultMissing"));
  }

  errors.value = messages;
  invalidCells.value = invalid;
  return messages.length === 0;
}

async function save() {
  if (!validate()) return;
  const defaultRow = rows.value.find((row) => row.uid === defaultUid.value);
  if (!defaultRow) return;
  saving.value = true;
  try {
    const saved = await adminAPI.settings.updateCapacityPresets({
      presets: rows.value.map(
        ({ key, label, concurrency, max_sessions, base_rpm, window_cost_limit, adaptive_throttle }) => ({
          key: key.trim(),
          label: label.trim(),
          concurrency,
          max_sessions,
          base_rpm,
          window_cost_limit,
          adaptive_throttle,
        }),
      ),
      default_key: defaultRow.key.trim(),
    });
    // 以服务端返回为准重建表格（新增行从此 key 只读），并让账号弹窗等其它处自动刷新
    applyLoaded(saved);
    setPresets(saved);
    appStore.showSuccess(t("admin.settings.capacityPresets.saved"));
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t("admin.settings.capacityPresets.saveFailed")));
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>
