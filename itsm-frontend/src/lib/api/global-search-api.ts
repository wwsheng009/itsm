import { httpClient } from './http-client';

export interface SearchResult {
  id: number;
  type: 'ticket' | 'incident' | 'problem' | 'change' | 'knowledge' | 'project' | 'project_task';
  title: string;
  description?: string;
  status?: string;
  ticketNumber?: string;
  /** 项目编号（type=project/project_task 时返回；PE-16）。 */
  code?: string;
  /** 所属项目 ID（type=project_task 时返回，供跳转；PE-16）。 */
  projectId?: number;
  /** 所属项目名（type=project_task 时返回；PE-16）。 */
  projectName?: string;
}

export interface GlobalSearchResponse {
  results: SearchResult[];
  total: number;
}

export async function globalSearch(query: string): Promise<GlobalSearchResponse> {
  return httpClient.get<GlobalSearchResponse>('/api/v1/global-search', { q: query });
}
