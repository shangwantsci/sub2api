import { describe, expect, it } from 'vitest'

import {
  PERSONA_TIMEZONE_PRESETS,
  buildPersonaSelectOptions,
  defaultPersonaLocaleForTimezone,
} from '@/constants/persona'

const t = (key: string, params?: Record<string, unknown>) =>
  params?.value ? `${key}:${String(params.value)}` : key

describe('persona presets', () => {
  it('offers auto, UTC+8 and common US timezone choices', () => {
    const values = PERSONA_TIMEZONE_PRESETS.map((option) => option.value)
    expect(values).toEqual(expect.arrayContaining([
      '',
      'Asia/Shanghai',
      'Asia/Singapore',
      'America/New_York',
      'America/Los_Angeles',
    ]))
  })

  it('links common timezone choices to locale defaults', () => {
    expect(defaultPersonaLocaleForTimezone('Asia/Shanghai')).toBe('zh-CN')
    expect(defaultPersonaLocaleForTimezone('Asia/Singapore')).toBe('en-SG')
    expect(defaultPersonaLocaleForTimezone('America/Chicago')).toBe('en-US')
    expect(defaultPersonaLocaleForTimezone('')).toBe('')
  })

  it('keeps an existing legacy custom value selectable without enabling free-form input', () => {
    const options = buildPersonaSelectOptions(PERSONA_TIMEZONE_PRESETS, 'Europe/London', t)
    expect(options.at(-1)).toEqual({
      value: 'Europe/London',
      label: 'admin.accounts.persona.customOption:Europe/London',
    })
  })
})
