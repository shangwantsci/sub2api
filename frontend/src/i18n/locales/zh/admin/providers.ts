// 管理端供号商对账面板与供货商设置文案。
// Vue I18n 会把裸花括号当作占位符表达式，文案里不要放原始 JSON。
export default {
  providers: {
    title: '供号商对账',
    subtitle: '查看待结算金额、封账与结算历史',
    settlementTimezone: '结算时区',
    loadFailed: '加载供号商列表失败',
    empty: '暂无供号商',
    batchPartialFailure:
      '已结算 {settled} 个，{failed} 个失败：{detail}。失败的供号商本期金额未封账，请处理后重试。',
    voidReasonRequired: '请填写作废原因',

    totalProviders: '供号商总数',
    pendingProviders: '有待结算',
    totalPending: '待结算总额',
    batchSettle: '批量结算',
    batchSettleHint: '当前有 {count} 个供号商可结算',
    onlyPending: '只看有待结算金额的',

    colProvider: '供号商',
    colAccounts: '在线 / 总数',
    colPeriodStart: '本期起始',
    colRequests: '请求数',
    colTokens: 'Token 数',
    colPending: '待结算',
    colLastSettled: '上次结算',
    colActions: '操作',
    colAccount: '账号',
    colAmount: '金额',
    colPeriod: '结算周期',
    colSettledAt: '结算时间',
    colStatus: '状态',

    review: '核对并结算',
    history: '结算历史',
    viewAccounts: '查看账号',
    export: '导出',
    exportDetail: '导出明细',
    total: '合计',
    offlineTag: '已下线',

    detailTitle: '本期明细 - {provider}',
    detailPeriod: '结算周期',
    inconsistentWarning:
      '明细合计与总额不一致（总额 {total}，明细合计 {sum}），已暂停结算。请先排查取数口径，不要在两个数字之间猜测。',

    settle: '去结算',
    settleConfirmTitle: '确认结算',
    settleConfirmMessage:
      '将为 {provider} 生成一张结算单。结算后本期金额归零并从此刻重新累积；如有误可在结算历史中作废最近一期。',
    confirmSettle: '确认结算',
    notes: '备注',
    notesPlaceholder: '例如：7 月结算，已转账',

    batchSettleTitle: '批量结算',
    batchSettleMessage: '以下供号商将共用同一个结算周期终点，可逐个取消勾选。',
    selectedCount: '已选 {count} 个',

    historyTitle: '结算历史 - {provider}',
    noHistory: '暂无结算记录',
    status: {
      settled: '已结算',
      voided: '已作废',
    },

    void: '作废',
    voidTitle: '作废最近一期结算',
    voidMessage: '作废后该期 {amount} 将重新计入当前待结算金额。仅最近一期可作废。',
    voidReason: '作废原因',
  },

  providerSettings: {
    title: '供货商',
    description: '供号商站点开关、托管类型、速率档位与自定义档护栏',
    loadFailed: '加载供货商设置失败',
    saveFailed: '保存失败',
    saved: '已保存',
    unsavedChanges: '有未保存的修改',
    save: '保存设置',
    leaveConfirm: '有未保存的修改，确定离开吗？',

    sectionBasic: '基础',
    portalEnabled: '开启供号商站点',
    portalEnabledHint: '关闭后供号商无法登录、注册与上号。',
    defaultHostingType: '默认托管类型',
    defaultTier: '默认档位',
    settlementTimezone: '结算时区',
    settlementTimezoneHint: '影响按日明细的日期划分，不影响总额。',
    settlementCooldown: '结算冷却期（秒）',
    settlementCooldownHint:
      '用量是异步落库的，封账终点会从当前时刻往回退这段时间，让还没提交完的记录先落定。默认 600 秒。',
    cooldownRange: '冷却期需在 60 到 86400 秒之间',
    accountPriority: '账号调度优先级',
    accountPriorityHint:
      '供号商上号时写入的 priority。注意这是硬门槛不是权重：调度只会使用分组内数值最小的那批账号，数值大的一批完全拿不到流量。默认 1，与管理端新建账号的默认值一致。',
    priorityRange: '优先级需在 0 到 100 之间',
    priorityConflictTitle: '优先级冲突：以下分组内会有一批账号完全拿不到流量',
    priorityConflictItem: '{group}：组内自有账号的优先级为 {others}，与供号商的 {mine} 不一致',
    priorityConflictHint:
      '调度只保留分组内优先级数值最小的那批账号，其余不参与选择且不会有任何报错。请把两边调成同一个值，或把供号商账号放到独立分组。',

    sectionHostingTypes: '托管类型',
    hostingTypesHint:
      '勾选哪些分组开放给供号商，并填写对外显示名与效果描述。右侧只读列是该分组的真实策略，仅管理端可见。',
    colOpen: '开放',
    colGroup: '分组',
    colLabel: '对外显示名',
    colDescription: '效果描述',
    colSort: '排序',
    colContentReview: '内容审查',
    colSystemPrompt: '提示词注入',
    colRateMultiplier: '计费倍率',
    noAnthropicGroups: '还没有 Anthropic 分组，请先在分组管理中创建。',

    sectionTiers: '速率档位',
    tiersHint: '修改档位数值默认只对新上号的账号生效，存量账号需手动「应用到存量」。',
    colEnabled: '启用',
    colTier: '档位',
    colTierLabel: '显示名',
    colConcurrency: '并发',
    colMaxSessions: '会话数',
    colBaseRpm: '每分钟请求数',
    colWindowCost: '5 小时额度上限',
    windowCostZeroHint: '0 表示不限',
    applyToExisting: '应用到存量',
    applyConfirmTitle: '应用到存量账号',
    applyConfirmMessage:
      '该档位当前有 {count} 个账号，将把它们的并发、会话数、每分钟请求数与额度上限更新为最新值。人格等其它配置不受影响。',
    applyResult: '已更新 {updated} / {matched} 个账号',
    applyFailed: '应用失败',

    sectionCustomTier: '自定义档',
    customTierEnabled: '允许供号商使用自定义档',
    customTierHint: '供号商可自行设置速率参数，但不得超过下列上限。',
    capConcurrency: '并发上限',
    capMaxSessions: '会话数上限',
    capBaseRpm: '每分钟请求数上限',
    capWindowCost: '额度上限',
  },
}
