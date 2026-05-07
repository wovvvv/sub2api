import { apiClient } from '../client'
import type {
  CodexRegistrationClearResponse,
  CodexRegistrationImportRequest,
  CodexRegistrationImportResponse,
  CodexRegistrationListFilters,
  CodexRegistrationListResponse,
  CodexRegistrationScanRequest,
  CodexRegistrationScanResponse,
  CodexRegistrationScanTaskResponse,
  CodexRegistrationSelectResponse,
  CodexRegistrationUnselectResponse
} from '@/types'

export async function scan(payload?: CodexRegistrationScanRequest): Promise<CodexRegistrationScanResponse> {
  const { data } = await apiClient.post<CodexRegistrationScanResponse>('/admin/account-registration/codex/scan', payload ?? {})
  return data
}

export async function getScanTask(taskId: string): Promise<CodexRegistrationScanTaskResponse> {
  const { data } = await apiClient.get<CodexRegistrationScanTaskResponse>(`/admin/account-registration/codex/scan/${encodeURIComponent(taskId)}`)
  return data
}

export async function list(filters?: CodexRegistrationListFilters): Promise<CodexRegistrationListResponse> {
  const { data } = await apiClient.get<CodexRegistrationListResponse>('/admin/account-registration/codex/candidates', {
    params: filters
  })
  return data
}

export async function stage(candidateIds: number[]): Promise<CodexRegistrationSelectResponse> {
  const { data } = await apiClient.post<CodexRegistrationSelectResponse>(
    '/admin/account-registration/codex/candidates/select',
    {
      candidate_ids: candidateIds
    }
  )
  return data
}

export async function unstage(candidateIds: number[]): Promise<CodexRegistrationUnselectResponse> {
  const { data } = await apiClient.post<CodexRegistrationUnselectResponse>(
    '/admin/account-registration/codex/candidates/unselect',
    {
      candidate_ids: candidateIds
    }
  )
  return data
}

export async function clear(): Promise<CodexRegistrationClearResponse> {
  const { data } = await apiClient.delete<CodexRegistrationClearResponse>('/admin/account-registration/codex/candidates')
  return data
}

export async function importCandidates(
  payload: CodexRegistrationImportRequest,
  options?: { timeout?: number }
): Promise<CodexRegistrationImportResponse> {
  const { data } = await apiClient.post<CodexRegistrationImportResponse>('/admin/account-registration/codex/import', payload, {
    timeout: options?.timeout
  })
  return data
}

export const codexRegistrationAPI = {
  scan,
  getScanTask,
  list,
  stage,
  unstage,
  clear,
  importCandidates
}

export default codexRegistrationAPI
