import { computed, reactive, ref, type Ref } from 'vue'
import { adminAPI } from '@/api/admin'
import {
  matchPreset,
  presetToValues,
  readCapacityFromAccount,
  writeCapacityToExtra,
  type NumberInput,
  type RpmStrategy
} from '@/components/account/capacityBuilder'
import { useCapacityPresets } from '@/composables/useCapacityPresets'

/**
 * 账号弹窗里「容量与节流」卡的全部状态与动作（仿 useQuotaNotifyState）。
 *
 * 状态必须放在弹窗 setup 里的 composable，不能放进展示用的子组件：新建弹窗第一步是
 * v-if="step === 1"，进入授权步骤后整块会被销毁，而三条 Anthropic 授权路径都在第二步读取这些状态。
 *
 * 并发是账号列，与表单的 form.concurrency 共用同一个 ref（传入 toRef(form, 'concurrency')），
 * 这样新建弹窗里十几处 `concurrency: form.concurrency` 不用改。
 */

function initialValues() {
  return {
    maxSessions: 0 as NumberInput,
    sessionIdleTimeout: null as NumberInput,
    windowCostLimit: 0 as NumberInput,
    windowCostStickyReserve: null as NumberInput,
    baseRpm: 0 as NumberInput,
    rpmStrategy: 'tiered' as RpmStrategy,
    rpmStickyBuffer: null as NumberInput,
    userMsgQueueMode: '',
    adaptiveThrottle: false,
    nurturing: false
  }
}

export function useAccountCapacity(options: { concurrency: Ref<number> }) {
  const { presets, defaultKey, ready: presetsReady, failed: presetsFailed, ensureLoaded } =
    useCapacityPresets()

  const state = reactive({ concurrency: options.concurrency, ...initialValues() })

  /** 全局养号总开关（设置页 → 网关）。关闭时账号上的养号开关置灰。 */
  const nurturingGlobalEnabled = ref(false)

  /** 用户是否主动碰过容量输入或档位下拉。碰过之后就不再自动预选默认档位。 */
  const touched = ref(false)

  /** 当前数值反推出的档位 key；模板没加载时恒为 null，调用方必须结合 presetsReady 判断，不能直接当「自定义」。 */
  const matchedKey = computed(() =>
    presetsReady.value
      ? matchPreset(
          {
            concurrency: state.concurrency,
            maxSessions: state.maxSessions,
            baseRpm: state.baseRpm,
            windowCostLimit: state.windowCostLimit
          },
          presets.value
        )
      : null
  )

  async function loadNurturingGlobal(): Promise<void> {
    try {
      const settings = await adminAPI.settings.getSettings()
      nurturingGlobalEnabled.value = settings?.account_nurturing_enabled === true
    } catch {
      nurturingGlobalEnabled.value = false
    }
  }

  /** 加载档位模板与全局养号开关；两者都不会抛异常，失败时降级为「不显示档位 / 养号置灰」。 */
  async function ensureReady(): Promise<void> {
    await Promise.all([ensureLoaded(), loadNurturingGlobal()])
  }

  function copyPresetValues(key: string): boolean {
    const preset = presets.value.find((item) => item.key === key)
    if (!preset) return false
    // 只写 5 项（并发、会话数、RPM、5h 额度、动态降速），0 = 关闭；
    // 不动 RPM 策略、粘性缓冲、粘性预留、空闲超时，那些不属于档位。
    const values = presetToValues(preset)
    state.concurrency = values.concurrency
    state.maxSessions = values.maxSessions
    state.baseRpm = values.baseRpm
    state.windowCostLimit = values.windowCostLimit
    state.adaptiveThrottle = values.adaptiveThrottle
    return true
  }

  /** 用户在下拉里选了某个档位：复制写入，并记为「用户碰过」。 */
  function applyPreset(key: string): boolean {
    const applied = copyPresetValues(key)
    if (applied) touched.value = true
    return applied
  }

  /**
   * 只在用户没碰过时，按设置里的 default_key 预选。
   * 模板没加载（失败 / 还在请求）时保持原状，不发明第二套默认值。
   */
  function applyDefaultIfPristine(): boolean {
    if (touched.value || !presetsReady.value) return false
    return copyPresetValues(defaultKey.value)
  }

  /**
   * 容量卡不再适用（例如新建弹窗改选了其它平台）时，撤销默认预选：只在用户没碰过时生效，
   * 碰过则保持用户的选择。返回是否撤销了；撤销后共用的并发由调用方恢复成表单自己的默认值。
   */
  function releaseDefaultPreset(): boolean {
    if (touched.value) return false
    Object.assign(state, initialValues())
    return true
  }

  function markTouched(): void {
    touched.value = true
  }

  /** 编辑弹窗：从账号载入。并发读账号列，其余读 extra 里的显式值。 */
  function loadFromAccount(account: Parameters<typeof readCapacityFromAccount>[0]): void {
    const { concurrency, ...rest } = readCapacityFromAccount(account)
    state.concurrency = Number(concurrency) || 0
    Object.assign(state, rest)
    // 载入的是账号现有配置，不是「没碰过的初始状态」：任何情况下都不允许被默认档位覆盖
    touched.value = true
  }

  /** 把当前状态写进 extra（原地修改并返回）。新建与编辑共用，数值为 0 的限制会被删键。 */
  function writeToExtra(extra: Record<string, unknown>): Record<string, unknown> {
    return writeCapacityToExtra(extra, { ...state })
  }

  /** 恢复初始值。并发由表单自己重置（它与 form.concurrency 是同一个 ref）。 */
  function reset(): void {
    Object.assign(state, initialValues())
    touched.value = false
  }

  return {
    state,
    presets,
    presetsReady,
    presetsFailed,
    matchedKey,
    nurturingGlobalEnabled,
    ensureReady,
    applyPreset,
    applyDefaultIfPristine,
    releaseDefaultPreset,
    markTouched,
    loadFromAccount,
    writeToExtra,
    reset
  }
}
