import { apiClient } from './client'

export type ClaudePoolStatusKind = 'fresh' | 'stale' | 'unavailable' | 'disabled'

export interface ClaudePoolModelStatus {
  name: string
  input_price_usd: number
  output_price_usd: number
  load_percent: number
  idle_percent: number
  coefficient: number
}

export interface ClaudePoolPricingRule {
  idle_range: string
  coefficient: number
  label: string
}

export interface ClaudePoolStatus {
  status: ClaudePoolStatusKind
  source_url: string
  updated_at?: string
  age_seconds: number
  stale: boolean
  coefficient: number
  load_percent: number
  idle_percent: number
  selected_model?: string
  state_label: string
  models: ClaudePoolModelStatus[]
  pricing_rules: ClaudePoolPricingRule[]
}

export const claudePoolAPI = {
  getStatus() {
    return apiClient.get<ClaudePoolStatus>('/claude-pool/status')
  },
}
