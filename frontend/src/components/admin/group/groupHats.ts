/**
 * 分组「帽子」的唯一定义（纯 TS，无 Vue / vue-i18n / pinia 依赖，仿同类的 components/account/capacityBuilder.ts）。
 *
 * 规则：
 * - 帽子是三个分组字段的「完整划分」：claude_code_only、fallback_group_id、
 *   claude_oauth_system_prompt_policy 的任何取值恰好落在 6 顶之一，没有「自定义」。
 *   选帽子 = 把字段写好（applyHat）；标签永远由字段值反推（deriveHat / describeHat），
 *   数据库不新增列，也不存帽子名。
 * - 这三个字段名的字面量只允许出现在本文件（与表单 / DTO 同名的 snake_case 属性除外：
 *   createForm、editForm、AdminGroup 里本来就叫这个名字）。新增分组策略字段，先更新这里的帽子定义。
 * - applyHat 永远写 null，不写 0。创建接口对降级分组传 0 会校验失败（只要不是 null 就去查分组是否存在）；
 *   更新接口才把 0 当「清除降级分组」，这个 null -> 0 的映射留在 GroupsView 的 handleUpdateGroup，不放在这里。
 * - CC 类帽子（cc_line / cc_fallback）不改动已存的注入策略：界面隐藏但保留，applyHat 原样带回（经归一）；
 *   只有开放类帽子才显式覆盖策略。cc_fallback 必须显式给出降级分组才生效，没给就不写任何字段。
 * - 注入策略与后端 NormalizeClaudeOAuthSystemPromptPolicy（backend/internal/service/group_anthropic_policy.go）
 *   同口径：缺省 / 未知值按 inherit。库里有 CHECK 约束、接口入参有 oneof 校验、输出也只会是 4 个规范值，
 *   所以这里只做精确匹配，不做大小写 / 空白处理。
 * - 降级分组只在 claude_code_only 为真、且请求不是 Claude Code 客户端时才被后端读取
 *   （backend/internal/service/gateway_scheduling.go）。claude_code_only 为假却还留着降级分组，
 *   是旧界面关开关不清降级分组造成的「残留」，describeHat 用 staleFallback 标出；界面只提示，不静默改写。
 * - 开放类帽子的注入策略只在 HAT_META 的 fixedPolicy 里定义一次：applyHat 正向写入、deriveHat 反推都读它，
 *   groupHats.spec.ts 再用字面量逐顶钉住。
 */
import type { ClaudeOAuthSystemPromptPolicy } from '@/types'

export type HatId =
  | 'cc_line'
  | 'cc_fallback'
  | 'open_follow'
  | 'open_full'
  | 'open_light'
  | 'open_keep_prompt'

/** 展示顺序：CC 类在前，开放类在后。 */
export const HAT_IDS: readonly HatId[] = [
  'cc_line',
  'cc_fallback',
  'open_follow',
  'open_full',
  'open_light',
  'open_keep_prompt'
]

/** 帽子管辖的三个分组字段（归一后的完整取值）。 */
export interface HatFields {
  claude_code_only: boolean
  fallback_group_id: number | null
  claude_oauth_system_prompt_policy: ClaudeOAuthSystemPromptPolicy
}

/** 宽松输入：列表行 / 表单对象 / 缺字段的 mock 都能直接传，缺省与 null 一律按「没设置」处理。 */
export interface HatInput {
  claude_code_only?: boolean | null
  fallback_group_id?: number | null
  claude_oauth_system_prompt_policy?: string | null
}

// ========== 归一 ==========

/** 只认 4 个合法值，其余（缺省 / null / 空串 / 未知字符串 / 非字符串）一律 inherit，与后端同口径。 */
export function normalizePolicy(raw: unknown): ClaudeOAuthSystemPromptPolicy {
  switch (raw) {
    case 'enabled':
    case 'identity_only':
    case 'disabled':
      return raw
    default:
      return 'inherit'
  }
}

/** 降级分组 id 是否有效：只有有限的正数才算「有降级分组」；0 / 负数 / NaN / 字符串 / null 都不算。 */
export function hasFallback(id: unknown): id is number {
  return typeof id === 'number' && Number.isFinite(id) && id > 0
}

/** 三字段归一：claude_code_only 只认严格 true；降级分组无效一律成 null（不是 0，也不是 undefined）。 */
function normalizeFields(input: HatInput | null | undefined): HatFields {
  const fallback = input?.fallback_group_id
  return {
    claude_code_only: input?.claude_code_only === true,
    fallback_group_id: hasFallback(fallback) ? fallback : null,
    claude_oauth_system_prompt_policy: normalizePolicy(input?.claude_oauth_system_prompt_policy)
  }
}

// ========== 帽子清单 ==========

export interface HatMeta {
  id: HatId
  /** cc = 只允许 Claude Code 客户端；open = 不限制客户端。 */
  family: 'cc' | 'open'
  nameKey: string
  shortKey: string
  purposeKey: string
  detailKey: string
  /** 徽章 class，必须写成完整字面量：Tailwind 只输出源码里出现过的完整类名，不能拼接。 */
  badgeClass: string
  /** 必须显式选一个降级分组才生效（applyHat 没拿到有效 id 就不写任何字段）。 */
  requiresFallback: boolean
  /** 卡片上是否显示「降级流量的提示词策略」下拉（降级流量的注入策略取本分组的策略字段）。 */
  showsPolicySelect: boolean
  /** 开放类帽子写入的固定注入策略；CC 类为 null，表示不改动已存的策略。 */
  fixedPolicy: ClaudeOAuthSystemPromptPolicy | null
}

export const HAT_META: Record<HatId, HatMeta> = {
  cc_line: {
    id: 'cc_line',
    family: 'cc',
    nameKey: 'admin.groups.hats.cc_line.name',
    shortKey: 'admin.groups.hats.cc_line.short',
    purposeKey: 'admin.groups.hats.cc_line.purpose',
    detailKey: 'admin.groups.hats.cc_line.detail',
    badgeClass: 'badge badge-success',
    requiresFallback: false,
    showsPolicySelect: false,
    fixedPolicy: null
  },
  cc_fallback: {
    id: 'cc_fallback',
    family: 'cc',
    nameKey: 'admin.groups.hats.cc_fallback.name',
    shortKey: 'admin.groups.hats.cc_fallback.short',
    purposeKey: 'admin.groups.hats.cc_fallback.purpose',
    detailKey: 'admin.groups.hats.cc_fallback.detail',
    badgeClass: 'badge badge-primary',
    requiresFallback: true,
    showsPolicySelect: true,
    fixedPolicy: null
  },
  open_follow: {
    id: 'open_follow',
    family: 'open',
    nameKey: 'admin.groups.hats.open_follow.name',
    shortKey: 'admin.groups.hats.open_follow.short',
    purposeKey: 'admin.groups.hats.open_follow.purpose',
    detailKey: 'admin.groups.hats.open_follow.detail',
    badgeClass: 'badge badge-gray',
    requiresFallback: false,
    showsPolicySelect: false,
    fixedPolicy: 'inherit'
  },
  open_full: {
    id: 'open_full',
    family: 'open',
    nameKey: 'admin.groups.hats.open_full.name',
    shortKey: 'admin.groups.hats.open_full.short',
    purposeKey: 'admin.groups.hats.open_full.purpose',
    detailKey: 'admin.groups.hats.open_full.detail',
    badgeClass: 'badge badge-purple',
    requiresFallback: false,
    showsPolicySelect: false,
    fixedPolicy: 'enabled'
  },
  open_light: {
    id: 'open_light',
    family: 'open',
    nameKey: 'admin.groups.hats.open_light.name',
    shortKey: 'admin.groups.hats.open_light.short',
    purposeKey: 'admin.groups.hats.open_light.purpose',
    detailKey: 'admin.groups.hats.open_light.detail',
    badgeClass: 'badge badge-purple',
    requiresFallback: false,
    showsPolicySelect: false,
    fixedPolicy: 'identity_only'
  },
  open_keep_prompt: {
    id: 'open_keep_prompt',
    family: 'open',
    nameKey: 'admin.groups.hats.open_keep_prompt.name',
    shortKey: 'admin.groups.hats.open_keep_prompt.short',
    purposeKey: 'admin.groups.hats.open_keep_prompt.purpose',
    detailKey: 'admin.groups.hats.open_keep_prompt.detail',
    badgeClass: 'badge badge-warning',
    requiresFallback: false,
    showsPolicySelect: false,
    fixedPolicy: 'disabled'
  }
}

/**
 * 注入策略 -> 词条 key（完整字面量，不拼接：identity_only 对应 camelCase 的 identityOnly）。
 * 列表徽章的 title 与编辑卡片的策略下拉都读这一份，不要在别处再写一遍「策略 -> 词条」的映射；
 * 键的顺序就是下拉里的顺序。
 */
export const POLICY_LABEL_KEYS: Record<ClaudeOAuthSystemPromptPolicy, string> = {
  inherit: 'admin.groups.anthropicPolicies.inherit',
  enabled: 'admin.groups.anthropicPolicies.enabled',
  identity_only: 'admin.groups.anthropicPolicies.identityOnly',
  disabled: 'admin.groups.anthropicPolicies.disabled'
}

// ========== 反推 / 套用 / 描述 ==========

/**
 * 字段 -> 帽子。claude_code_only 只有严格 true 才算真；为真时看有没有降级分组，
 * 为假时看注入策略（降级分组此时不被后端读取，所以与它无关）。
 */
export function deriveHat(input: HatInput | null | undefined): HatId {
  if (input?.claude_code_only === true) {
    return hasFallback(input.fallback_group_id) ? 'cc_fallback' : 'cc_line'
  }
  const policy = normalizePolicy(input?.claude_oauth_system_prompt_policy)
  // 开放类四顶与四个策略一一对应；?? 只为满足类型，inherit 本来就对应 open_follow
  return HAT_IDS.find((id) => HAT_META[id].fixedPolicy === policy) ?? 'open_follow'
}

/**
 * 帽子 -> 三个字段的新值（纯函数，不修改入参；当前值先经归一）。
 * cc_fallback 只认第三个参数：当前残留的 fallback_group_id 不算选择，没给有效 id 就原样返回当前值。
 * fixedPolicy 为 null 的帽子保留当前策略。
 */
export function applyHat(
  hat: HatId,
  current: HatInput | null | undefined,
  fallbackGroupId?: number | null
): HatFields {
  const now = normalizeFields(current)
  const policy = HAT_META[hat].fixedPolicy ?? now.claude_oauth_system_prompt_policy
  switch (hat) {
    case 'cc_line':
      return { claude_code_only: true, fallback_group_id: null, claude_oauth_system_prompt_policy: policy }
    case 'cc_fallback':
      return hasFallback(fallbackGroupId)
        ? { claude_code_only: true, fallback_group_id: fallbackGroupId, claude_oauth_system_prompt_policy: policy }
        : now
    case 'open_follow':
    case 'open_full':
    case 'open_light':
    case 'open_keep_prompt':
      return { claude_code_only: false, fallback_group_id: null, claude_oauth_system_prompt_policy: policy }
  }
}

export interface HatDescription {
  hat: HatId
  /** 当前三个字段的归一值，供界面「将写入」一行与列表 title 使用。 */
  fields: HatFields
  /** claude_code_only 不为真却留着降级分组：旧界面关开关不清降级分组造成的残留，后端不会读它。 */
  staleFallback: boolean
}

export function describeHat(input: HatInput | null | undefined): HatDescription {
  const fields = normalizeFields(input)
  return {
    hat: deriveHat(input),
    fields,
    staleFallback: !fields.claude_code_only && hasFallback(fields.fallback_group_id)
  }
}
