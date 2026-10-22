import { computed, ref } from 'vue'
import { adminAPI } from '@/api/admin'
import type { CapacityPreset, CapacityPresetsSettings } from '@/api/admin/settings'
import { normalizePreset } from '@/components/account/capacityBuilder'

/**
 * 容量档位模板的共享缓存（模块级单例）。
 *
 * 新建 / 编辑 / 批量编辑弹窗、账号列表的容量列、设置页的模板卡都读同一份，
 * 设置页保存后调 setPresets() 就能让其它地方自动刷新。
 *
 * 为什么不用 pinia：编辑 / 批量弹窗的现有 spec 没装 pinia，且它们 mock 的 @/api/admin 里
 * 没有 getCapacityPresets。所以整个加载过程必须 try/catch 降级：加载失败时模板为空、
 * 状态是 failed，调用方据此「不显示档位标签」，而不是把没加载误判成「自定义」。
 */

type LoadStatus = 'idle' | 'loading' | 'ready' | 'failed'

const presets = ref<CapacityPreset[]>([])
const defaultKey = ref('')
const status = ref<LoadStatus>('idle')
let inflight: Promise<void> | null = null

function isValidPayload(data: CapacityPresetsSettings | null | undefined): data is CapacityPresetsSettings {
  return !!data && Array.isArray(data.presets)
}

/** 用服务端返回（或刚保存成功）的内容替换缓存。 */
export function setPresets(data: CapacityPresetsSettings): void {
  presets.value = data.presets.map(normalizePreset)
  defaultKey.value = typeof data.default_key === 'string' ? data.default_key : ''
  status.value = 'ready'
}

async function load(): Promise<void> {
  try {
    const data = await adminAPI.settings.getCapacityPresets()
    if (!isValidPayload(data)) throw new Error('invalid capacity presets payload')
    setPresets(data)
  } catch {
    status.value = 'failed'
  }
}

/** 确保已加载一次；并发调用共用同一个请求，失败后下次调用会重试。永远不抛异常。 */
export function ensureLoaded(): Promise<void> {
  if (status.value === 'ready') return Promise.resolve()
  if (inflight) return inflight
  status.value = 'loading'
  // 必须在赋值之后才清空：load() 同步抛错（例如 mock 里没有这个方法）时也要保证能被清掉
  inflight = load().finally(() => {
    inflight = null
  })
  return inflight
}

/** 仅供测试：清空单例，避免用例之间互相污染。 */
export function resetCapacityPresets(): void {
  presets.value = []
  defaultKey.value = ''
  status.value = 'idle'
  inflight = null
}

export function useCapacityPresets() {
  return {
    presets,
    defaultKey,
    status,
    /** 已加载且至少有一个模板：只有这时才能显示档位标签，否则「自定义」会误导。 */
    ready: computed(() => status.value === 'ready' && presets.value.length > 0),
    failed: computed(() => status.value === 'failed'),
    ensureLoaded,
    setPresets
  }
}
