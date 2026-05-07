import { apiClient } from '../client'
import type {
  OpenAIOAuthDetectionProbeRequest,
  OpenAIOAuthDetectionProbeResponse
} from '@/types'

export async function probe(
  payload: OpenAIOAuthDetectionProbeRequest
): Promise<OpenAIOAuthDetectionProbeResponse> {
  const { data } = await apiClient.post<OpenAIOAuthDetectionProbeResponse>(
    '/admin/account-registration/openai-oauth-detection/probe',
    payload
  )
  return data
}

const openaiOAuthDetectionAPI = {
  probe
}

export default openaiOAuthDetectionAPI
