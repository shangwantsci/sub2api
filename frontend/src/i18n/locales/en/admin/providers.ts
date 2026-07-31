// Admin provider reconciliation panel and provider settings copy.
// Vue I18n parses bare braces as placeholder expressions, so never put raw JSON here.
export default {
  providers: {
    title: 'Provider settlement',
    subtitle: 'Review pending amounts, close periods, and browse settlement history',
    settlementTimezone: 'Settlement timezone',
    loadFailed: 'Failed to load providers',
    empty: 'No providers yet',

    tabSettlement: 'Settlement',
    tabManage: 'Providers',
    colRegisteredAt: 'Registered',
    colAccountsDetail: 'Accounts',
    accountsBreakdown: '{active} live / {paused} paused',
    userStatus: {
      active: 'Active',
      disabled: 'Disabled',
      banned: 'Banned',
    },
    resetPassword: 'Reset password',
    resetPasswordTitle: 'Reset provider password',
    resetPasswordMessage:
      'Set a new password for {provider}. Providers cannot change their own password, so you will need to pass it on to them.',
    newPassword: 'New password',
    newPasswordHint: 'At least 6 characters. Takes effect immediately and signs the provider out.',
    resetPasswordDone: 'Password reset. Remember to pass it on to the provider.',
    generate: 'Generate',
    disable: 'Disable',
    enable: 'Enable',
    batchPartialFailure:
      'Settled {settled}, {failed} failed: {detail}. The failed providers were not closed for this period; resolve the issue and retry.',
    voidReasonRequired: 'A reason is required to void a settlement',

    totalProviders: 'Providers',
    pendingProviders: 'With pending',
    totalPending: 'Total pending',
    batchSettle: 'Batch settle',
    batchSettleHint: '{count} providers can be settled',
    onlyPending: 'Only show providers with a pending amount',

    colProvider: 'Provider',
    colAccounts: 'Active / total',
    colPeriodStart: 'Period start',
    colRequests: 'Requests',
    colTokens: 'Tokens',
    colPending: 'Pending',
    colLastSettled: 'Last settled',
    colActions: 'Actions',
    colAccount: 'Account',
    colAmount: 'Amount',
    colPeriod: 'Period',
    colSettledAt: 'Settled at',
    colStatus: 'Status',

    review: 'Review and settle',
    history: 'History',
    viewAccounts: 'View accounts',
    export: 'Export',
    exportDetail: 'Export details',
    total: 'Total',
    offlineTag: 'Offline',

    detailTitle: 'Current period - {provider}',
    detailPeriod: 'Period',
    inconsistentWarning:
      'The per-account rows do not add up to the reported total (total {total}, rows {sum}). Settlement is blocked. Investigate the data source rather than guessing between two numbers.',

    settle: 'Settle',
    settleConfirmTitle: 'Confirm settlement',
    settleConfirmMessage:
      'A settlement record will be created for {provider}. The pending amount resets to zero and starts accruing from now; the most recent settlement can be voided if this was a mistake.',
    confirmSettle: 'Confirm',
    notes: 'Notes',
    notesPlaceholder: 'e.g. July settlement, paid by transfer',

    batchSettleTitle: 'Batch settle',
    batchSettleMessage:
      'These providers will share the same period end. Uncheck anyone you want to skip.',
    selectedCount: '{count} selected',

    historyTitle: 'Settlement history - {provider}',
    noHistory: 'No settlements yet',
    status: {
      settled: 'Settled',
      voided: 'Voided',
    },

    void: 'Void',
    voidTitle: 'Void the most recent settlement',
    voidMessage:
      'Voiding returns {amount} to the pending balance. Only the most recent settlement can be voided.',
    voidReason: 'Reason',
  },

  providerSettings: {
    title: 'Providers',
    description: 'Portal switch, hosting types, capacity tiers, and custom tier caps',
    loadFailed: 'Failed to load provider settings',
    saveFailed: 'Save failed',
    saved: 'Saved',
    unsavedChanges: 'You have unsaved changes',
    save: 'Save settings',
    leaveConfirm: 'You have unsaved changes. Leave anyway?',

    sectionBasic: 'Basics',
    portalEnabled: 'Enable the provider portal',
    portalEnabledHint: 'When off, providers cannot sign in, sign up, or add accounts.',
    defaultHostingType: 'Default hosting type',
    defaultTier: 'Default tier',
    settlementTimezone: 'Settlement timezone',
    settlementTimezoneHint: 'Affects how daily rows are bucketed; totals are unaffected.',
    settlementCooldown: 'Settlement cooldown (seconds)',
    settlementCooldownHint:
      'Usage rows are written asynchronously. Closing a period stops this far short of the current moment so in-flight writes can land first. Defaults to 600 seconds.',
    cooldownRange: 'Cooldown must be between 60 and 86400 seconds',
    proxyModePolicy: 'Exit IP sources offered to providers',
    proxyModePolicyBoth: 'Both',
    proxyModePolicyAutoOnly: 'Platform IPs only',
    proxyModePolicyManualOnly: 'Provider-supplied only',
    proxyModePolicyHint:
      'Controls which options the onboarding page offers; the backend enforces the same rule. When platform IPs run short, switch to provider-supplied only so providers stop hitting "no IP available".',
    autoProxyMaxAccounts: 'Max accounts per platform IP',
    autoProxyMaxAccountsHint:
      'Automatic assignment always picks the least loaded IP, and an IP at this cap drops out of the pool; once every open IP is capped, providers can no longer use a platform IP. Stacking many accounts behind one exit IP makes them look related, so keep this small. Defaults to 2.',
    autoProxyMaxAccountsRange: 'Enter a number between 1 and {max}',
    accountPriority: 'Account scheduling priority (automatic)',
    accountPriorityHint:
      'Onboarding takes the lowest priority already present in the chosen hosting group, so new accounts line up with whatever is currently serving traffic there. Nothing to configure. Priority is a hard gate rather than a weight: only the lowest value in a group is ever scheduled, so a mismatch would leave one side with no traffic at all.',
    priorityNoGroups: 'No hosting types enabled yet',
    priorityFallbackTag: '(group empty, using default)',

    sectionHostingTypes: 'Hosting types',
    hostingTypesHint:
      'Pick which groups providers may choose and write the public label and description. The read-only columns show what the group actually does and are admin-only.',
    colOpen: 'Open',
    colGroup: 'Group',
    colLabel: 'Public label',
    colDescription: 'Description',
    colSort: 'Order',
    colContentReview: 'Content review',
    colSystemPrompt: 'Prompt injection',
    colRateMultiplier: 'Rate multiplier',
    noAnthropicGroups: 'No Anthropic groups yet. Create one in group management first.',

    sectionTiers: 'Capacity tiers',
    tiersHint:
      'Changing tier values only affects newly onboarded accounts by default; use "Apply to existing" to backfill.',
    colEnabled: 'Enabled',
    colTier: 'Tier',
    colTierLabel: 'Label',
    colConcurrency: 'Concurrency',
    colMaxSessions: 'Sessions',
    colBaseRpm: 'Requests per minute',
    colWindowCost: '5-hour window cap',
    windowCostZeroHint: '0 means unlimited',
    applyToExisting: 'Apply to existing',
    applyConfirmTitle: 'Apply to existing accounts',
    applyConfirmMessage:
      'This tier currently has {count} accounts. Their concurrency, sessions, requests per minute, and window cap will be updated. Persona and other settings are left untouched.',
    applyResult: 'Updated {updated} of {matched} accounts',
    applyFailed: 'Failed to apply',

    sectionCustomTier: 'Custom tier',
    customTierEnabled: 'Allow providers to use a custom tier',
    customTierHint: 'Providers may set their own limits but never above these caps.',
    capConcurrency: 'Concurrency cap',
    capMaxSessions: 'Sessions cap',
    capBaseRpm: 'Requests per minute cap',
    capWindowCost: 'Window cap',
  },
}
