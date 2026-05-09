import { apiClient } from './client'

export type ClaudePoolStatusKind = 'fresh' | 'stale' | 'unavailable' | 'disabled'

export interface ClaudePoolModelStatus {
  name: string
  load_percent: number
  idle_percent: number
  coefficient: number
}

export interface ClaudePoolStatus {
  status: ClaudePoolStatusKind
  updated_at?: string
  age_seconds: number
  stale: boolean
  coefficient: number
  load_percent: number
  idle_percent: number
  selected_model?: string
  state_label: string
  models: ClaudePoolModelStatus[]
}

export const claudePoolAPI = {
  getStatus() {
    return apiClient.get<ClaudePoolStatus>('/claude-pool/status')
  },
}
