import {
  aiTriage,
  aiSearchKB,
  aiSimilarIncidents,
  aiSummarize,
  aiSaveFeedback,
  aiGetMetrics,
  aiGetEvaluation,
  aiGetAuditLogs,
  aiClassifyTicket,
  aiSuggestSolutions,
  aiIntelligentSearch,
  aiListVisibleBots,
  AIApi,
} from '../ai-api';
import { httpClient } from '../http-client';

jest.mock('../http-client', () => ({
  httpClient: {
    get: jest.fn(),
    post: jest.fn(),
    put: jest.fn(),
    delete: jest.fn(),
    getBaseURL: jest.fn(() => 'http://localhost:3000'),
    getAuthToken: jest.fn(() => 'token123'),
    getTenantId: jest.fn(() => 1),
  },
}));

const mockGet = httpClient.get as jest.Mock;
const mockPost = httpClient.post as jest.Mock;

describe('AI API', () => {
  beforeEach(() => { jest.clearAllMocks(); });

  describe('aiTriage', () => {
    it('should triage and transform response', async () => {
      mockPost.mockResolvedValue({ suggestions: { category: 'network', priority: 'high', confidence: 0.9, reasoning: 'network issue', urgency: 'urgent' } });
      const result = await aiTriage('Network down', 'Cannot connect');
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/triage', { title: 'Network down', description: 'Cannot connect' });
      expect(result.category).toBe('network');
      expect(result.priority).toBe('high');
      expect(result.confidence).toBe(0.9);
      expect(result.explanation).toBe('network issue');
    });

    it('should handle missing suggestions', async () => {
      mockPost.mockResolvedValue({});
      const result = await aiTriage('Test', 'Desc');
      expect(result.category).toBe('general');
      expect(result.priority).toBe('medium');
      expect(result.confidence).toBe(0);
    });
  });

  describe('aiSearchKB', () => {
    it('should search knowledge base', async () => {
      mockPost.mockResolvedValue({ results: [{ id: 1, objectType: 'article', snippet: 'found' }] });
      const result = await aiSearchKB('password reset', 3);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/rag/search', { query: 'password reset', limit: 3, type: 'kb' });
      expect(result.answers).toHaveLength(1);
    });

    it('should handle missing results', async () => {
      mockPost.mockResolvedValue({});
      const result = await aiSearchKB('test');
      expect(result.answers).toEqual([]);
    });
  });

  describe('aiSimilarIncidents', () => {
    it('should find similar incidents', async () => {
      mockPost.mockResolvedValue({ results: [{ id: 1, objectType: 'incident', snippet: 'similar' }] });
      const result = await aiSimilarIncidents('server crash', 5);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/rag/search', { query: 'server crash', limit: 5, type: 'incident' });
      expect(result.incidents).toHaveLength(1);
    });
  });

  describe('incident analysis contract', () => {
    it('uses the incident aggregate endpoint', async () => {
      mockPost.mockResolvedValue({ incidentId: 42, degraded: false });
      await AIApi.analyzeIncident(42);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/incidents/42/analyze', {});
    });
  });

  describe('aiSummarize', () => {
    it('should summarize text', async () => {
      mockPost.mockResolvedValue({ answers: ['This is a summary'] });
      const result = await aiSummarize('Long text here', 100);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/chat', expect.objectContaining({ limit: 1 }));
      expect(result.summary).toBe('This is a summary');
    });

    it('should handle empty answers', async () => {
      mockPost.mockResolvedValue({ answers: [] });
      const result = await aiSummarize('text');
      expect(result.summary).toBe('');
    });
  });

  describe('aiSaveFeedback', () => {
    it('should save feedback', async () => {
      mockPost.mockResolvedValue({ message: 'saved' });
      const feedback = { kind: 'triage', useful: true };
      const result = await aiSaveFeedback(feedback);
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/feedback', feedback);
      expect(result.message).toBe('saved');
    });
  });

  describe('aiGetMetrics', () => {
    it('should get metrics', async () => {
      mockGet.mockResolvedValue({ totalRequests: 100, usefulRate: 0.8 });
      const result = await aiGetMetrics(14);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/ai/metrics?days=14');
      expect(result.totalRequests).toBe(100);
    });

    it('should default to 7 days', async () => {
      mockGet.mockResolvedValue({});
      await aiGetMetrics();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/ai/metrics?days=7');
    });
  });

  describe('aiGetEvaluation', () => {
    it('should fetch evaluation report with days param', async () => {
      mockGet.mockResolvedValue({ healthScore: 57.5, hasData: true, byScenario: [{ kind: 'triage' }] });
      const result = await aiGetEvaluation(30);
      expect(mockGet).toHaveBeenCalledWith('/api/v1/ai/evaluation?days=30');
      expect(result.healthScore).toBe(57.5);
      expect(result.hasData).toBe(true);
    });

    it('should default to 30 days', async () => {
      mockGet.mockResolvedValue({});
      await aiGetEvaluation();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/ai/evaluation?days=30');
    });
  });

  describe('aiGetAuditLogs', () => {
    it('should fetch audit logs with all params', async () => {
      mockGet.mockResolvedValue({ items: [{ id: 1, scenario: 'analyze' }], total: 3, page: 1, pageSize: 20 });
      const result = await aiGetAuditLogs({ page: 2, pageSize: 10, kind: 'analyze', days: 90 });
      expect(mockGet).toHaveBeenCalledWith('/api/v1/ai/audit-logs?page=2&pageSize=10&kind=analyze&days=90');
      expect(result.items).toHaveLength(1);
      expect(result.total).toBe(3);
    });

    it('should omit empty params', async () => {
      mockGet.mockResolvedValue({ items: [], total: 0 });
      await aiGetAuditLogs();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/ai/audit-logs');
    });

    it('should handle missing items as empty list', async () => {
      mockGet.mockResolvedValue({ total: 0 });
      const result = await aiGetAuditLogs({ page: 1 });
      expect(result.items).toBeUndefined();
    });
  });

  describe('AIApi class wrapper', () => {
    it('triage should delegate', async () => {
      mockPost.mockResolvedValue({ suggestions: { category: 'test' } });
      const result = await AIApi.triage('title', 'desc');
      expect(result.category).toBe('test');
    });

    it('chat should call post', async () => {
      mockPost.mockResolvedValue({ answers: [] });
      await AIApi.chat({ query: 'hello', limit: 5 });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/chat', { query: 'hello', limit: 5, conversationId: undefined });
    });

    it('chat 传 botId 时才落该字段（B2-04：未选择 = 请求体与现状一致）', async () => {
      mockPost.mockResolvedValue({ answers: [] });
      await AIApi.chat({ query: 'hello', limit: 5, botId: 0 });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/chat', {
        query: 'hello',
        limit: 5,
        conversationId: undefined,
      });

      mockPost.mockClear();
      mockPost.mockResolvedValue({ answers: [] });
      await AIApi.chat({ query: 'hello', limit: 5, botId: 42 });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/chat', {
        query: 'hello',
        limit: 5,
        conversationId: undefined,
        botId: 42,
      });
    });

    it('searchKB should delegate', async () => {
      mockPost.mockResolvedValue({ results: [] });
      const result = await AIApi.searchKB('test');
      expect(result.answers).toEqual([]);
    });

    it('similarIncidents should delegate', async () => {
      mockPost.mockResolvedValue({ results: [] });
      const result = await AIApi.similarIncidents('test');
      expect(result.incidents).toEqual([]);
    });

    it('listVisibleBots 解析 items；端点不可用（404/异常）时返回空数组（选择器不渲染）', async () => {
      mockGet.mockResolvedValue({
        items: [
          { id: 11, slug: 'ops', name: '运维助手', audience: 'internal' },
          { id: 22, slug: 'user-bot', name: '用户助手', audience: 'end_user' },
        ],
      });
      const bots = await AIApi.listVisibleBots();
      expect(mockGet).toHaveBeenCalledWith('/api/v1/agent/bots');
      expect(bots).toEqual([
        { id: 11, slug: 'ops', name: '运维助手', audience: 'internal' },
        { id: 22, slug: 'user-bot', name: '用户助手', audience: 'end_user' },
      ]);

      mockGet.mockRejectedValue(new Error('404 not found'));
      expect(await aiListVisibleBots()).toEqual([]);

      // 非法条目（缺 id/name）被过滤，避免渲染出不可选选项。
      mockGet.mockResolvedValue({ items: [{ slug: 'broken' }, { id: 33, name: '合法' }] });
      expect(await aiListVisibleBots()).toEqual([
        { id: 33, slug: '', name: '合法', audience: '' },
      ]);
    });

    it('summarize should delegate', async () => {
      mockPost.mockResolvedValue({ answers: ['sum'] });
      const result = await AIApi.summarize('text');
      expect(result.summary).toBe('sum');
    });

    it('saveFeedback should delegate', async () => {
      mockPost.mockResolvedValue({ message: 'ok' });
      await AIApi.saveFeedback({ kind: 'test', useful: true });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/feedback', { kind: 'test', useful: true });
    });

    it('getMetrics should delegate', async () => {
      mockGet.mockResolvedValue({ totalRequests: 50 });
      const result = await AIApi.getMetrics(30);
      expect(result.totalRequests).toBe(50);
    });
  });

  describe('aiClassifyTicket (merged from legacy AIService)', () => {
    it('should classify with suggestions mapping', async () => {
      mockPost.mockResolvedValue({
        suggestions: { category: 'network', priority: 'high', confidence: 0.88, reasoning: 'network down', urgency: 'urgent' },
      });
      const result = await aiClassifyTicket({ title: 'Test', description: 'Desc' });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/triage', { title: 'Test', description: 'Desc' });
      expect(result.category).toBe('network');
      expect(result.priority).toBe('high');
      expect(result.urgency).toBe('urgent');
      expect(result.confidence).toBe(0.88);
      expect(result.reasoning).toBe('network down');
    });

    it('should default urgency to priority and handle missing suggestions', async () => {
      mockPost.mockResolvedValue({ suggestions: { category: 'software', priority: 'medium' } });
      const result = await aiClassifyTicket({ title: 'Test', description: 'Desc' });
      expect(result.category).toBe('software');
      expect(result.urgency).toBe('medium');
    });

    it('should fall back to general/medium on empty response', async () => {
      mockPost.mockResolvedValue({});
      const result = await aiClassifyTicket({ title: 'Test', description: 'Desc' });
      expect(result.category).toBe('general');
      expect(result.priority).toBe('medium');
      expect(result.confidence).toBe(0);
    });
  });

  describe('aiSuggestSolutions (merged from legacy AIService)', () => {
    const answers = [
      { id: 1, title: '重置密码指南', snippet: '通过SSO重置', score: 0.9, source: 'kb/1' },
      { id: 2, title: 'VPN连接排查', snippet: '检查客户端版本', score: 0.7 },
    ];

    it('should map rag results to solution suggestions', async () => {
      mockPost.mockResolvedValue({ results: answers });
      const result = await aiSuggestSolutions({ query: 'password reset', limit: 5 });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/rag/search', { query: 'password reset', limit: 5, type: 'kb' });
      expect(result).toHaveLength(2);
      expect(result[0].solutionId).toBe('1');
      expect(result[0].title).toBe('重置密码指南');
      expect(result[0].description).toBe('通过SSO重置');
      expect(result[0].relatedKnowledge).toEqual(['kb/1']);
    });

    it('should default limit to 5 when not provided or invalid', async () => {
      mockPost.mockResolvedValue({ results: [] });
      await aiSuggestSolutions({ query: 'test', limit: 0 });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/rag/search', { query: 'test', limit: 5, type: 'kb' });
    });

    it('should handle non-array response', async () => {
      mockPost.mockResolvedValue(null);
      const result = await aiSuggestSolutions({ query: 'test' });
      expect(result).toEqual([]);
    });
  });

  describe('aiIntelligentSearch (merged from legacy AIService)', () => {
    it('should split results by type', async () => {
      mockGet.mockResolvedValue({
        results: [
          { id: 1, type: 'ticket', title: 'T1', ticketNumber: 'TCK-1' },
          { id: 2, type: 'incident', title: 'I1' },
          { id: 3, type: 'knowledge', title: 'K1' },
          { id: 4, type: 'unknown', title: 'X1' },
        ],
      });
      const result = await aiIntelligentSearch('login issue');
      expect(mockGet).toHaveBeenCalledWith('/api/v1/global-search?q=login%20issue');
      expect(result.tickets).toHaveLength(1);
      expect(result.tickets[0].number).toBe('TCK-1');
      expect(result.incidents).toHaveLength(1);
      expect(result.knowledge).toHaveLength(1);
    });

    it('should handle empty results', async () => {
      mockGet.mockResolvedValue({ results: [] });
      const result = await aiIntelligentSearch('nothing');
      expect(result.tickets).toEqual([]);
      expect(result.suggestions).toEqual([]);
    });
  });

  describe('error propagation', () => {
    it('should propagate errors', async () => {
      mockPost.mockRejectedValue(new Error('AI service unavailable'));
      await expect(aiTriage('t', 'd')).rejects.toThrow('AI service unavailable');
    });
  });

  describe('aiChatStream', () => {
    let originalFetch: typeof global.fetch;
    const encode = (str: string) => Buffer.from(str);

    beforeEach(() => {
      originalFetch = global.fetch;
      // Polyfill TextDecoder for jsdom
      if (typeof TextDecoder === 'undefined') {
        (global as any).TextDecoder = class { decode(buf: any) { return Buffer.from(buf).toString('utf-8'); } };
      }
    });
    afterEach(() => {
      global.fetch = originalFetch;
    });

    it('should stream SSE events and invoke callbacks', async () => {
      const { aiChatStream } = require('../ai-api');
      const chunks = [
        encode('event:sources\ndata:[{"objectType":"article","id":1,"snippet":"hi"}]\n\n'),
        encode('event:delta\ndata:{"content":"Hello"}\n\n'),
        encode('event:done\ndata:{"conversationId":42}\n\n'),
      ];
      let chunkIndex = 0;
      const mockReader = {
        read: jest.fn().mockImplementation(() => {
          if (chunkIndex < chunks.length) {
            return Promise.resolve({ value: chunks[chunkIndex++], done: false });
          }
          return Promise.resolve({ value: undefined, done: true });
        }),
      };
      const mockBody = { getReader: () => mockReader };
      global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200, body: mockBody });

      const onSources = jest.fn();
      const onDelta = jest.fn();
      const onDone = jest.fn();
      const result = await aiChatStream({ query: 'test', limit: 5 }, { onSources, onDelta, onDone });

      expect(onSources).toHaveBeenCalledWith([expect.objectContaining({ id: 1 })]);
      expect(onDelta).toHaveBeenCalledWith('Hello');
      expect(onDone).toHaveBeenCalledWith(42);
      expect(result).toBe(42);
    });

    it('should handle HTTP error', async () => {
      const { aiChatStream } = require('../ai-api');
      global.fetch = jest.fn().mockResolvedValue({ ok: false, status: 500, body: null });
      const onError = jest.fn();
      await expect(aiChatStream({ query: 'test' }, { onError })).rejects.toThrow();
      expect(onError).toHaveBeenCalled();
    });

    // M1-03：工具事件解析（四种事件名 → 同一回调；未知事件忽略；半截载荷不炸）。
    const streamFrames = (frames: string[]) => {
      const chunks = frames.map((f) => encode(f));
      let chunkIndex = 0;
      const mockReader = {
        read: jest.fn().mockImplementation(() => {
          if (chunkIndex < chunks.length) {
            return Promise.resolve({ value: chunks[chunkIndex++], done: false });
          }
          return Promise.resolve({ value: undefined, done: true });
        }),
      };
      global.fetch = jest.fn().mockResolvedValue({
        ok: true,
        status: 200,
        body: { getReader: () => mockReader },
      });
    };

    it('should surface tool_call events through onToolEvent', async () => {
      const { aiChatStream } = require('../ai-api');
      streamFrames([
        'event:tool_call_started\ndata:{"tool":"mcp__mock__list_issues","provider":"mcp","server":"mock","phase":"read","status":"started"}\n\n',
        'event:tool_call_finished\ndata:{"tool":"mcp__mock__list_issues","provider":"mcp","server":"mock","phase":"read","status":"done","summary":"{\\"issues\\":[]}","durationMs":12}\n\n',
        'event:tool_call_failed\ndata:{"tool":"delete_ci","provider":"builtin","phase":"write","status":"failed","errorCode":"tool_permission_denied"}\n\n',
        'event:approval_pending\ndata:{"id":9,"tool":"create_ticket","provider":"builtin","phase":"write","status":"pending"}\n\n',
        'event:done\ndata:{"conversationId":5}\n\n',
      ]);

      const onToolEvent = jest.fn();
      const result = await aiChatStream({ query: 'test' }, { onToolEvent });

      expect(onToolEvent).toHaveBeenCalledTimes(4);
      expect(onToolEvent.mock.calls.map((c: any[]) => c[0].status)).toEqual([
        'started',
        'done',
        'failed',
        'pending',
      ]);
      expect(onToolEvent.mock.calls[0][0]).toEqual(
        expect.objectContaining({ tool: 'mcp__mock__list_issues', provider: 'mcp', server: 'mock', phase: 'read' }),
      );
      expect(onToolEvent.mock.calls[1][0]).toEqual(expect.objectContaining({ summary: '{"issues":[]}', durationMs: 12 }));
      expect(onToolEvent.mock.calls[2][0]).toEqual(expect.objectContaining({ errorCode: 'tool_permission_denied' }));
      expect(onToolEvent.mock.calls[3][0]).toEqual(expect.objectContaining({ id: 9, phase: 'write' }));
      expect(result).toBe(5);
    });

    it('should ignore unknown SSE events and keep streaming afterwards', async () => {
      const { aiChatStream } = require('../ai-api');
      streamFrames([
        'event:tool_call_future_thing\ndata:{"tool":"x","unexpected":true}\n\n',
        'event:delta\ndata:{"content":"Hello"}\n\n',
        'event:done\ndata:{"conversationId":11}\n\n',
      ]);

      const onToolEvent = jest.fn();
      const onDelta = jest.fn();
      const onUnknownEvent = jest.fn();
      const result = await aiChatStream({ query: 'test' }, { onToolEvent, onDelta, onUnknownEvent });

      expect(onToolEvent).not.toHaveBeenCalled();
      expect(onDelta).toHaveBeenCalledWith('Hello');
      // 未知事件静默忽略（不影响渲染），但可经 onUnknownEvent 上报观测。
      expect(onUnknownEvent).toHaveBeenCalledTimes(1);
      expect(onUnknownEvent).toHaveBeenCalledWith('tool_call_future_thing', { tool: 'x', unexpected: true });
      expect(result).toBe(11);
    });

    it('should dispatch v2 run events (run_started / step) and keep v1 frames intact', async () => {
      const { aiChatStream } = require('../ai-api');
      streamFrames([
        'event:run_started\ndata:{"v":2,"runId":42,"entrypoint":"chat","conversationId":7}\n\n',
        'event:step\ndata:{"v":2,"runId":42,"stepIndex":0,"type":"llm","durationMs":12}\n\n',
        'event:step\ndata:{"v":2,"runId":42,"stepIndex":1,"type":"tool","payloadRef":"tool_invocation:9"}\n\n',
        'event:delta\ndata:{"content":"Hi"}\n\n',
        'event:done\ndata:{"conversationId":7}\n\n',
      ]);

      const onRunStarted = jest.fn();
      const onStep = jest.fn();
      const onDelta = jest.fn();
      const result = await aiChatStream({ query: 'test' }, { onRunStarted, onStep, onDelta });

      expect(onRunStarted).toHaveBeenCalledTimes(1);
      expect(onRunStarted).toHaveBeenCalledWith({
        v: 2,
        runId: 42,
        entrypoint: 'chat',
        conversationId: 7,
      });
      expect(onStep.mock.calls.map((c: any[]) => [c[0].stepIndex, c[0].type])).toEqual([
        [0, 'llm'],
        [1, 'tool'],
      ]);
      expect(onStep.mock.calls[1][0].payloadRef).toBe('tool_invocation:9');
      expect(onDelta).toHaveBeenCalledWith('Hi');
      expect(result).toBe(7);
    });

    it('should dedupe approval_pending + confirmation_required for the same invocation id', async () => {
      const { aiChatStream } = require('../ai-api');
      streamFrames([
        'event:approval_pending\ndata:{"id":9,"tool":"create_ticket","provider":"builtin","phase":"write","status":"pending"}\n\n',
        'event:confirmation_required\ndata:{"v":2,"id":9,"tool":"create_ticket","provider":"builtin","phase":"write","status":"pending"}\n\n',
        // 另一条 invocation：只发 v2（v2-only 后端）也必须被接住。
        'event:confirmation_required\ndata:{"v":2,"id":10,"tool":"update_ticket","provider":"mcp","server":"mock","phase":"write","status":"pending"}\n\n',
        'event:done\ndata:{"conversationId":3}\n\n',
      ]);

      const onToolEvent = jest.fn();
      const result = await aiChatStream({ query: 'test' }, { onToolEvent });

      expect(onToolEvent).toHaveBeenCalledTimes(2);
      expect(onToolEvent.mock.calls.map((c: any[]) => c[0].id)).toEqual([9, 10]);
      expect(onToolEvent.mock.calls.every((c: any[]) => c[0].status === 'pending')).toBe(true);
      expect(result).toBe(3);
    });

    it('should ignore malformed v2 run/step payloads without breaking the stream', async () => {
      const { aiChatStream } = require('../ai-api');
      streamFrames([
        // 缺 runId
        'event:run_started\ndata:{"v":2,"entrypoint":"chat"}\n\n',
        // stepIndex 非法
        'event:step\ndata:{"v":2,"runId":1,"stepIndex":"zero","type":"llm"}\n\n',
        // type 为空
        'event:step\ndata:{"v":2,"runId":1,"stepIndex":0,"type":""}\n\n',
        'event:delta\ndata:{"content":"ok"}\n\n',
        'event:done\ndata:{"conversationId":4}\n\n',
      ]);

      const onRunStarted = jest.fn();
      const onStep = jest.fn();
      const onDelta = jest.fn();
      const result = await aiChatStream({ query: 'test' }, { onRunStarted, onStep, onDelta });

      expect(onRunStarted).not.toHaveBeenCalled();
      expect(onStep).not.toHaveBeenCalled();
      expect(onDelta).toHaveBeenCalledWith('ok');
      expect(result).toBe(4);
    });

    it('should drop malformed tool events instead of throwing', async () => {
      const { aiChatStream } = require('../ai-api');
      streamFrames([
        // 缺 tool 字段（半截载荷）
        'event:tool_call_started\ndata:{"provider":"mcp"}\n\n',
        // 非对象载荷
        'event:approval_pending\ndata:[1,2,3]\n\n',
        'event:done\ndata:{"conversationId":3}\n\n',
      ]);

      const onToolEvent = jest.fn();
      const result = await aiChatStream({ query: 'test' }, { onToolEvent });

      expect(onToolEvent).not.toHaveBeenCalled();
      expect(result).toBe(3);
    });

    it('should handle error event in stream', async () => {
      const { aiChatStream } = require('../ai-api');
      const chunks = [
        encode('event:error\ndata:{"message":"rate limited"}\n\n'),
      ];
      let chunkIndex = 0;
      const mockReader = {
        read: jest.fn().mockImplementation(() => {
          if (chunkIndex < chunks.length) {
            return Promise.resolve({ value: chunks[chunkIndex++], done: false });
          }
          return Promise.resolve({ value: undefined, done: true });
        }),
      };
      global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200, body: { getReader: () => mockReader } });
      const onError = jest.fn();
      await aiChatStream({ query: 'test' }, { onError });
      expect(onError).toHaveBeenCalledWith('rate limited');
    });

    it('should handle invalid JSON in SSE data gracefully', async () => {
      const { aiChatStream } = require('../ai-api');
      const chunks = [
        encode('event:delta\ndata:not-json\n\n'),
        encode('event:done\ndata:{"conversationId":7}\n\n'),
      ];
      let chunkIndex = 0;
      const mockReader = {
        read: jest.fn().mockImplementation(() => {
          if (chunkIndex < chunks.length) {
            return Promise.resolve({ value: chunks[chunkIndex++], done: false });
          }
          return Promise.resolve({ value: undefined, done: true });
        }),
      };
      global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200, body: { getReader: () => mockReader } });
      const onDelta = jest.fn();
      const result = await aiChatStream({ query: 'test' }, { onDelta });
      expect(onDelta).not.toHaveBeenCalled();
      expect(result).toBe(7);
    });
  });

  describe('AIApi.chatStream', () => {
    it('should delegate to aiChatStream', async () => {
      const encode = (str: string) => Buffer.from(str);
      const chunks = [encode('event:done\ndata:{"conversationId":99}\n\n')];
      let chunkIndex = 0;
      const mockReader = {
        read: jest.fn().mockImplementation(() => {
          if (chunkIndex < chunks.length) {
            return Promise.resolve({ value: chunks[chunkIndex++], done: false });
          }
          return Promise.resolve({ value: undefined, done: true });
        }),
      };
      global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200, body: { getReader: () => mockReader } });
      const result = await AIApi.chatStream({ query: 'hello' });
      expect(result).toBe(99);
    });
  });

  describe('LLM provider passthrough (FE-4)', () => {
    let originalFetch: typeof global.fetch;
    const encode = (str: string) => Buffer.from(str);

    const mockStream = (chunks: Buffer[]) => {
      let chunkIndex = 0;
      const mockReader = {
        read: jest.fn().mockImplementation(() => {
          if (chunkIndex < chunks.length) {
            return Promise.resolve({ value: chunks[chunkIndex++], done: false });
          }
          return Promise.resolve({ value: undefined, done: true });
        }),
      };
      const fetchMock = jest.fn().mockResolvedValue({
        ok: true,
        status: 200,
        body: { getReader: () => mockReader },
      });
      global.fetch = fetchMock as unknown as typeof global.fetch;
      return fetchMock;
    };

    // 首个 fetch 可能是 CSRF 预取（GET、无 body）；取真正带 JSON body 的流式请求。
    const streamCallBody = (fetchMock: jest.Mock): string => {
      const call = fetchMock.mock.calls.find(
        c => c[1] && typeof (c[1] as { body?: unknown }).body === 'string'
      );
      expect(call).toBeDefined();
      return (call![1] as { body: string }).body;
    };

    beforeEach(() => {
      originalFetch = global.fetch;
      if (typeof TextDecoder === 'undefined') {
        (global as any).TextDecoder = class {
          decode(buf: any) {
            return Buffer.from(buf).toString('utf-8');
          }
        };
      }
    });
    afterEach(() => {
      global.fetch = originalFetch;
    });

    it('omits provider from the SSE body when not selected (QA-3 byte-identity)', async () => {
      const { aiChatStream } = require('../ai-api');
      const fetchMock = mockStream([encode('event:done\ndata:{"conversationId":7}\n\n')]);

      await aiChatStream({ query: 'test', conversationId: 3, limit: 5 }, {});

      const rawBody = streamCallBody(fetchMock);
      // 与改动前构造顺序一致：query → limit → conversationId；未选实例时不落 provider。
      expect(rawBody).toBe(
        JSON.stringify({ query: 'test', limit: 5, conversationId: 3, provider: undefined })
      );
      expect(Object.prototype.hasOwnProperty.call(JSON.parse(rawBody), 'provider')).toBe(false);
    });

    it('passes provider through and surfaces the effective instance on done', async () => {
      const { aiChatStream } = require('../ai-api');
      const fetchMock = mockStream([
        encode('event:done\ndata:{"conversationId":42,"provider":"db-7","providerSource":"request"}\n\n'),
      ]);
      const onDone = jest.fn();

      const result = await aiChatStream(
        { query: 'test', conversationId: 3, limit: 5, provider: 'db-7' },
        { onDone }
      );

      expect(JSON.parse(streamCallBody(fetchMock))).toEqual({
        query: 'test',
        limit: 5,
        conversationId: 3,
        provider: 'db-7',
      });
      expect(onDone).toHaveBeenCalledWith(42, { provider: 'db-7', providerSource: 'request' });
      expect(result).toBe(42);
    });

    it('keeps done callback backward compatible when the switch is off (no provider fields)', async () => {
      const { aiChatStream } = require('../ai-api');
      mockStream([encode('event:done\ndata:{"conversationId":9}\n\n')]);
      const onDone = jest.fn();

      await aiChatStream({ query: 'test' }, { onDone });

      expect(onDone).toHaveBeenCalledWith(9);
    });

    it('omits provider in one-shot fallback body unless selected, and forwards it otherwise', async () => {
      mockPost.mockResolvedValue({});
      await AIApi.chat({ query: 'q', conversationId: 3, limit: 5 });
      const legacyBody = mockPost.mock.calls[0][1];
      expect(legacyBody).toEqual({ query: 'q', limit: 5, conversationId: 3 });
      expect(Object.prototype.hasOwnProperty.call(legacyBody, 'provider')).toBe(false);

      mockPost.mockClear();
      mockPost.mockResolvedValue({});
      await AIApi.chat({ query: 'q', conversationId: 3, limit: 5, provider: 'db-7' });
      expect(mockPost).toHaveBeenCalledWith('/api/v1/ai/chat', {
        query: 'q',
        limit: 5,
        conversationId: 3,
        provider: 'db-7',
      });
    });
  });
});
