/**
 * 容量档位的数值护栏。
 *
 * 与后端 provider_settings.go 的护栏保持一致，两边都校验，前端只是提前反馈。
 * 设置页的「容量档位模板」卡与供货商设置面板的自定义档共用这一份，
 * 避免两处各抄一遍之后慢慢漂移。
 */
export const MAX_CONCURRENCY = 50
export const MAX_SESSIONS = 50
export const MAX_RPM = 500
export const MAX_WINDOW_COST = 1000
