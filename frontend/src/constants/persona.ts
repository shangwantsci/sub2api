export interface PersonaPresetDefinition {
  value: string
  labelKey: string
}

export interface PersonaSelectOption {
  value: string
  label: string
  [key: string]: unknown
}

export const PERSONA_TIMEZONE_PRESETS: PersonaPresetDefinition[] = [
  { value: '', labelKey: 'admin.accounts.persona.timezoneOptions.auto' },
  { value: 'Asia/Shanghai', labelKey: 'admin.accounts.persona.timezoneOptions.china' },
  { value: 'Asia/Singapore', labelKey: 'admin.accounts.persona.timezoneOptions.singapore' },
  { value: 'Asia/Tokyo', labelKey: 'admin.accounts.persona.timezoneOptions.japan' },
  { value: 'America/New_York', labelKey: 'admin.accounts.persona.timezoneOptions.usEastern' },
  { value: 'America/Chicago', labelKey: 'admin.accounts.persona.timezoneOptions.usCentral' },
  { value: 'America/Denver', labelKey: 'admin.accounts.persona.timezoneOptions.usMountain' },
  { value: 'America/Los_Angeles', labelKey: 'admin.accounts.persona.timezoneOptions.usPacific' },
]

export const PERSONA_LOCALE_PRESETS: PersonaPresetDefinition[] = [
  { value: '', labelKey: 'admin.accounts.persona.localeOptions.auto' },
  { value: 'zh-CN', labelKey: 'admin.accounts.persona.localeOptions.zhCN' },
  { value: 'en-US', labelKey: 'admin.accounts.persona.localeOptions.enUS' },
  { value: 'en-SG', labelKey: 'admin.accounts.persona.localeOptions.enSG' },
  { value: 'ja-JP', labelKey: 'admin.accounts.persona.localeOptions.jaJP' },
]

const timezoneLocaleDefaults: Record<string, string> = {
  'Asia/Shanghai': 'zh-CN',
  'Asia/Singapore': 'en-SG',
  'Asia/Tokyo': 'ja-JP',
  'America/New_York': 'en-US',
  'America/Chicago': 'en-US',
  'America/Denver': 'en-US',
  'America/Los_Angeles': 'en-US',
}

export function defaultPersonaLocaleForTimezone(timezone: string): string {
  return timezoneLocaleDefaults[timezone] || ''
}

export function buildPersonaSelectOptions(
  presets: PersonaPresetDefinition[],
  currentValue: string,
  translate: (key: string, params?: Record<string, unknown>) => string,
): PersonaSelectOption[] {
  const options = presets.map((preset) => ({
    value: preset.value,
    label: translate(preset.labelKey),
  }))
  const current = currentValue.trim()
  if (current && !options.some((option) => option.value === current)) {
    options.push({
      value: current,
      label: translate('admin.accounts.persona.customOption', { value: current }),
    })
  }
  return options
}
