import { globalSearch } from '../global-search-api';
import { httpClient } from '../http-client';

jest.mock('../http-client', () => ({
  httpClient: {
    get: jest.fn(),
    post: jest.fn(),
  },
}));

const mockGet = httpClient.get as jest.Mock;

describe('globalSearch', () => {
  beforeEach(() => { jest.clearAllMocks(); });

  it('should search with keyword', async () => {
    mockGet.mockResolvedValue({ results: [{ id: 1, type: 'ticket', title: 'Test' }], total: 1 });
    const result = await globalSearch('test');
    expect(mockGet).toHaveBeenCalledWith('/api/v1/global-search', { q: 'test' });
    expect(result.results).toHaveLength(1);
    expect(result.total).toBe(1);
  });

  it('should handle empty results', async () => {
    mockGet.mockResolvedValue({ results: [], total: 0 });
    const result = await globalSearch('nonexistent');
    expect(result.results).toEqual([]);
    expect(result.total).toBe(0);
  });

  it('should pass through project/project_task results (PE-16)', async () => {
    mockGet.mockResolvedValue({
      results: [
        { id: 7, type: 'project', title: '迁移改造项目', code: 'PRJ-MIG-1' },
        { id: 8, type: 'project_task', title: '迁移任务一', projectId: 7, projectName: '迁移改造项目' },
      ],
      total: 2,
    });
    const result = await globalSearch('迁移');
    expect(result.total).toBe(2);
    expect(result.results[0]).toMatchObject({ type: 'project', code: 'PRJ-MIG-1' });
    expect(result.results[1]).toMatchObject({ type: 'project_task', projectId: 7, projectName: '迁移改造项目' });
  });

  it('should propagate errors', async () => {
    mockGet.mockRejectedValue(new Error('Search failed'));
    await expect(globalSearch('test')).rejects.toThrow('Search failed');
  });
});
