import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

// t 回显「key::参数 JSON」：断言里能直接看到组件传了哪个 key、带了哪些参数
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => (params ? `${key}::${JSON.stringify(params)}` : key)
    })
  }
})

import GroupHatBadge from '../GroupHatBadge.vue'

type GroupRow = Record<string, unknown>

function group(
  claudeCodeOnly: boolean,
  fallbackGroupId: number | null,
  policy: string | null | undefined,
  platform = 'anthropic'
): GroupRow {
  return {
    platform,
    claude_code_only: claudeCodeOnly,
    fallback_group_id: fallbackGroupId,
    claude_oauth_system_prompt_policy: policy
  }
}

function mountBadge(row: GroupRow, resolveGroupName?: (id: number) => string | undefined) {
  return mount(GroupHatBadge, { props: { group: row as never, resolveGroupName } })
}

type Mounted = ReturnType<typeof mountBadge>

const badgeOf = (wrapper: Mounted) => wrapper.get('[data-testid="group-hat-badge"]')
const titleOf = (wrapper: Mounted) => badgeOf(wrapper).attributes('title')

// 6 顶帽子：字段组合、期望的徽章 class 与短名 key、data-hat，全部是字面量
const HATS: Array<[string, GroupRow, string, string]> = [
  ['cc_line', group(true, null, 'inherit'), 'badge badge-success', 'admin.groups.hats.cc_line.short'],
  ['cc_fallback', group(true, 7, 'inherit'), 'badge badge-primary', 'admin.groups.hats.cc_fallback.short'],
  ['open_follow', group(false, null, 'inherit'), 'badge badge-gray', 'admin.groups.hats.open_follow.short'],
  ['open_full', group(false, null, 'enabled'), 'badge badge-purple', 'admin.groups.hats.open_full.short'],
  ['open_light', group(false, null, 'identity_only'), 'badge badge-purple', 'admin.groups.hats.open_light.short'],
  ['open_keep_prompt', group(false, null, 'disabled'), 'badge badge-warning', 'admin.groups.hats.open_keep_prompt.short']
]

describe('GroupHatBadge：anthropic 分组的徽章', () => {
  it.each(HATS)('%s：徽章 class、短名 key、data-hat', (hat, row, badgeClass, shortKey) => {
    const wrapper = mountBadge(row)
    const badge = badgeOf(wrapper)
    expect(badge.attributes('data-hat')).toBe(hat)
    expect(badge.attributes('class')).toBe(`${badgeClass} whitespace-nowrap`)
    expect(badge.text()).toBe(shortKey)
    expect(wrapper.find('[data-testid="group-hat-none"]').exists()).toBe(false)
  })

  it('缺字段的行（只有 platform）按开放·跟随，title 只有名称与用途两行', () => {
    const wrapper = mountBadge({ platform: 'anthropic' })
    expect(badgeOf(wrapper).attributes('data-hat')).toBe('open_follow')
    expect(badgeOf(wrapper).attributes('class')).toBe('badge badge-gray whitespace-nowrap')
    expect(titleOf(wrapper)).toBe(['admin.groups.hats.open_follow.name', 'admin.groups.hats.open_follow.purpose'].join('\n'))
  })

  it('字段为 null 或未知策略时同样按开放·跟随', () => {
    const wrapper = mountBadge({
      platform: 'anthropic',
      claude_code_only: null,
      fallback_group_id: null,
      claude_oauth_system_prompt_policy: 'bogus'
    })
    expect(badgeOf(wrapper).attributes('data-hat')).toBe('open_follow')
  })
})

describe('GroupHatBadge：非 anthropic 分组', () => {
  it.each(['openai', 'gemini', 'antigravity', 'grok'])('平台 %s：显示「—」，没有徽章，没有 title', (platform) => {
    // 即使数据里带着 CC 限制与降级分组，非 anthropic 也不能显示成帽子
    const wrapper = mountBadge(group(true, 7, 'disabled', platform))
    const none = wrapper.get('[data-testid="group-hat-none"]')
    expect(none.text()).toBe('—')
    expect(none.attributes('title')).toBeUndefined()
    expect(wrapper.find('[data-testid="group-hat-badge"]').exists()).toBe(false)
  })
})

describe('GroupHatBadge：title（原生提示，多行用 \\n 连接）', () => {
  it('cc_fallback：依次是名称、用途、降级到、降级流量的提示词策略；name 来自 resolveGroupName', () => {
    const resolve = vi.fn((id: number) => (id === 7 ? '备用池' : undefined))
    const wrapper = mountBadge(group(true, 7, 'enabled'), resolve)
    expect(titleOf(wrapper)).toBe(
      [
        'admin.groups.hats.cc_fallback.name',
        'admin.groups.hats.cc_fallback.purpose',
        'admin.groups.hatCard.tipFallbackTo::{"name":"备用池"}',
        'admin.groups.hatCard.tipFallbackPolicy::{"policy":"admin.groups.anthropicPolicies.enabled"}'
      ].join('\n')
    )
    expect(resolve).toHaveBeenCalledWith(7)
  })

  it('resolveGroupName 查不到（返回 undefined）时 name 显示 #id', () => {
    const wrapper = mountBadge(group(true, 7, 'enabled'), () => undefined)
    expect(titleOf(wrapper)).toContain('admin.groups.hatCard.tipFallbackTo::{"name":"#7"}')
  })

  it('没有传 resolveGroupName 也不报错，name 显示 #id', () => {
    const wrapper = mountBadge(group(true, 12, 'enabled'))
    expect(titleOf(wrapper)).toContain('admin.groups.hatCard.tipFallbackTo::{"name":"#12"}')
  })

  it.each([
    ['inherit', 'admin.groups.anthropicPolicies.inherit'],
    ['enabled', 'admin.groups.anthropicPolicies.enabled'],
    ['identity_only', 'admin.groups.anthropicPolicies.identityOnly'],
    ['disabled', 'admin.groups.anthropicPolicies.disabled'],
    // 缺省 / 未知值与后端一致，按 inherit
    [undefined, 'admin.groups.anthropicPolicies.inherit'],
    ['bogus', 'admin.groups.anthropicPolicies.inherit']
  ])('cc_fallback 的策略为 %s：title 里翻成 %s', (policy, labelKey) => {
    const wrapper = mountBadge(group(true, 7, policy))
    expect(titleOf(wrapper)).toContain(`admin.groups.hatCard.tipFallbackPolicy::{"policy":"${labelKey}"}`)
  })

  it('cc 为假但残留降级分组：title 多一行 tipStaleFallback，名称与用途仍是开放帽的，徽章不变成降级池', () => {
    const wrapper = mountBadge(group(false, 3, 'enabled'), (id) => (id === 3 ? '旧降级组' : undefined))
    expect(badgeOf(wrapper).attributes('data-hat')).toBe('open_full')
    expect(titleOf(wrapper)).toBe(
      [
        'admin.groups.hats.open_full.name',
        'admin.groups.hats.open_full.purpose',
        'admin.groups.hatCard.tipStaleFallback::{"name":"旧降级组"}'
      ].join('\n')
    )
  })

  it('残留的降级分组查不到名字（或没传 resolveGroupName）时同样显示 #id', () => {
    expect(titleOf(mountBadge(group(false, 3, 'enabled'), () => undefined))).toContain(
      'admin.groups.hatCard.tipStaleFallback::{"name":"#3"}'
    )
    expect(titleOf(mountBadge(group(false, 3, 'enabled')))).toContain(
      'admin.groups.hatCard.tipStaleFallback::{"name":"#3"}'
    )
  })

  it.each([
    ['cc 为假且没有降级分组', group(false, null, 'enabled')],
    ['cc 为假且降级分组为 0（不算残留）', group(false, 0, 'enabled')],
    ['cc 为真且有降级分组（那是降级池，不是残留）', group(true, 3, 'enabled')]
  ])('%s：没有 tipStaleFallback', (_name, row) => {
    expect(titleOf(mountBadge(row))).not.toContain('tipStaleFallback')
  })

  it('CC 专线（降级池以外的 CC 帽）：title 只有名称与用途两行，没有 tipFallbackTo / tipFallbackPolicy', () => {
    const wrapper = mountBadge(group(true, null, 'enabled'))
    expect(titleOf(wrapper)).toBe(['admin.groups.hats.cc_line.name', 'admin.groups.hats.cc_line.purpose'].join('\n'))
    expect(titleOf(wrapper)).not.toContain('tipFallbackTo')
    expect(titleOf(wrapper)).not.toContain('tipFallbackPolicy')
  })

  it.each([
    ['open_follow', group(false, null, 'inherit')],
    ['open_full', group(false, null, 'enabled')],
    ['open_light', group(false, null, 'identity_only')],
    ['open_keep_prompt', group(false, null, 'disabled')]
  ])('开放帽 %s：title 只有名称与用途两行', (hat, row) => {
    expect(titleOf(mountBadge(row))).toBe([`admin.groups.hats.${hat}.name`, `admin.groups.hats.${hat}.purpose`].join('\n'))
  })

  it.each(HATS)('%s：title 不含 detail 词条（detail 太长，留给编辑卡片）', (_hat, row) => {
    const title = titleOf(mountBadge(row, () => '某分组'))
    expect(title).not.toContain('.detail')
  })
})

describe('GroupHatBadge：响应式', () => {
  it('group 变化后徽章与 title 跟着变；平台换成非 anthropic 后变成「—」', async () => {
    const wrapper = mountBadge(group(false, null, 'enabled'))
    expect(badgeOf(wrapper).attributes('data-hat')).toBe('open_full')

    await wrapper.setProps({ group: group(true, null, 'enabled') as never })
    expect(badgeOf(wrapper).attributes('data-hat')).toBe('cc_line')
    expect(badgeOf(wrapper).attributes('class')).toBe('badge badge-success whitespace-nowrap')
    expect(titleOf(wrapper)).toContain('admin.groups.hats.cc_line.name')

    await wrapper.setProps({ group: group(true, null, 'enabled', 'openai') as never })
    expect(wrapper.find('[data-testid="group-hat-badge"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="group-hat-none"]').text()).toBe('—')
  })
})
