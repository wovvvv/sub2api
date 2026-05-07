import type {
  CodexRegistrationLivenessStatus,
  CodexRegistrationWorkflowState
} from '@/types'

export type AccountRegistrationTabKey = 'cpa-import' | 'account-detection' | 'tasks'

export interface CodexFilterOption<T> {
  value: T | 'all'
  label: string
}

export type CodexLivenessFilterValue = CodexRegistrationLivenessStatus | 'all'
export type CodexWorkflowFilterValue = CodexRegistrationWorkflowState | 'all'

export interface CodexImportResultSummary {
  imported: number
  skipped: number
  items: Array<{
    id: number
    label: string
    reason: string
  }>
}
