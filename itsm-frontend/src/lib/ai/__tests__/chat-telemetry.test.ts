/**
 * D0-05 聊天埋点映射器单测：顺序、去重、幂等、隐私（只报长度）与失败静默。
 */
import {
  createChatTelemetry,
  type ChatTelemetryTrack,
} from '@/lib/ai/chat-telemetry';
import { hashTelemetryText, type AssistantEventName } from '@/lib/ai/telemetry';

function createRecorder() {
  const calls: Array<{ name: AssistantEventName; input: Record<string, unknown> }> = [];
  const track: ChatTelemetryTrack = (name, input) => {
    calls.push({ name, input: (input ?? {}) as Record<string, unknown> });
  };
  return { calls, track };
}

describe('createChatTelemetry', () => {
  it('完整回合：发送 → 首 token（去重）→ 工具 → 结束', () => {
    const { calls, track } = createRecorder();
    let clock = 1_000;
    const telemetry = createChatTelemetry(
      { pageKey: 'ai.chat', route: '/ai/chat', entrypoint: 'ticket_detail', botId: 3 },
      track,
      () => clock
    );

    telemetry.onSend('hello world!', 88);
    expect(calls).toHaveLength(1);
    expect(calls[0].name).toBe('message_sent');
    expect(calls[0].input).toMatchObject({
      pageKey: 'ai.chat',
      route: '/ai/chat',
      entrypoint: 'ticket_detail',
      conversationId: 88,
      queryHash: hashTelemetryText('hello world!'),
      extra: { queryLength: 12, botId: 3 },
    });

    clock += 250;
    telemetry.onFirstToken();
    telemetry.onFirstToken(); // 重复调用只计一次
    expect(calls.filter(c => c.name === 'first_token')).toHaveLength(1);
    expect(calls[1].input.latencyMs).toBe(250);

    telemetry.onToolEvent({ tool: 'list_tickets', status: 'done', durationMs: 40 });
    expect(calls[2].name).toBe('tool_call');
    expect(calls[2].input).toMatchObject({
      toolName: 'list_tickets',
      reasonCode: 'done',
      latencyMs: 40,
    });

    clock += 100;
    telemetry.onSources(2);
    telemetry.onDone({
      conversationId: 99,
      provider: 'doubao',
      providerSource: 'tenant',
      model: 'doubao-pro',
    });
    expect(calls[3].name).toBe('stream_end');
    expect(calls[3].input).toMatchObject({
      conversationId: 99,
      latencyMs: 350,
      providerSource: 'tenant',
      model: 'doubao-pro',
      sourcesCount: 2,
      hasSources: true,
      extra: { provider: 'doubao' },
    });

    // 结束后重复 onDone / onFirstToken 不再产生事件
    telemetry.onDone();
    telemetry.onFirstToken();
    expect(calls).toHaveLength(4);
  });

  it('未发送时忽略一切事件（不产生噪声）', () => {
    const { calls, track } = createRecorder();
    const telemetry = createChatTelemetry({ pageKey: 'ai.chat' }, track, () => 0);

    telemetry.onFirstToken();
    telemetry.onToolEvent({ tool: 'x', status: 'started' });
    telemetry.onDone();
    telemetry.onError('boom');
    expect(calls).toHaveLength(0);
  });

  it('失败路径：error_shown（详情透传由 SDK 脱敏），且随后 onDone 幂等不重复', () => {
    const { calls, track } = createRecorder();
    let clock = 5_000;
    const telemetry = createChatTelemetry({ pageKey: 'ai.chat' }, track, () => clock);

    telemetry.onSend('test');
    clock += 80;
    telemetry.onError('provider timeout');
    expect(calls).toHaveLength(2);
    expect(calls[1].name).toBe('error_shown');
    expect(calls[1].input).toMatchObject({ latencyMs: 80, extra: { detail: 'provider timeout' } });

    telemetry.onDone({ conversationId: 1 });
    expect(calls).toHaveLength(2);
  });

  it('确认单映射：shown / accepted / rejected', () => {
    const { calls, track } = createRecorder();
    const telemetry = createChatTelemetry({ pageKey: 'ai.chat' }, track, () => 0);

    telemetry.onConfirmation('shown');
    telemetry.onConfirmation('accepted');
    telemetry.onConfirmation('rejected');
    expect(calls.map(c => c.name)).toEqual([
      'confirmation_shown',
      'confirmation_accepted',
      'confirmation_rejected',
    ]);
  });

  it('track 抛错不冒泡（埋点失败静默）', () => {
    const failing: ChatTelemetryTrack = () => {
      throw new Error('sink down');
    };
    const telemetry = createChatTelemetry({ pageKey: 'ai.chat' }, failing, () => 0);
    expect(() => telemetry.onSend('abc')).not.toThrow();
    expect(() => telemetry.onDone()).not.toThrow();
  });

  it('会话 id 回退：onSend 未传时使用上下文会话 id', () => {
    const { calls, track } = createRecorder();
    const telemetry = createChatTelemetry({ pageKey: 'ai.chat', conversationId: 7 }, track, () => 0);
    telemetry.onSend('x');
    expect(calls[0].input.conversationId).toBe(7);
  });

  it('stream_end 携带回合 token 估算与口径标记（O2-02/§13.4）', () => {
    const { calls, track } = createRecorder();
    const telemetry = createChatTelemetry({ pageKey: 'ai.chat' }, track, () => 0);

    telemetry.onSend('abcdef');
    telemetry.onDone({ conversationId: 5, tokenIn: 120, tokenOut: 48 });

    const streamEnd = calls.find(call => call.name === 'stream_end');
    expect(streamEnd).toBeDefined();
    expect(streamEnd?.input).toMatchObject({
      tokenIn: 120,
      tokenOut: 48,
      extra: { tokenEstimated: true },
    });
  });

  it('provider 实测（tokenEstimated=false）原样落口径标记（B4-02）', () => {
    const { calls, track } = createRecorder();
    const telemetry = createChatTelemetry({ pageKey: 'ai.chat' }, track, () => 0);

    telemetry.onSend('abcdef');
    telemetry.onDone({ conversationId: 5, tokenIn: 33, tokenOut: 7, tokenEstimated: false });

    const streamEnd = calls.find(call => call.name === 'stream_end');
    expect(streamEnd?.input).toMatchObject({
      tokenIn: 33,
      tokenOut: 7,
      extra: { tokenEstimated: false },
    });
  });

  it('无 usage 时 stream_end 不落 token 字段（旧后端零变化）', () => {
    const { calls, track } = createRecorder();
    const telemetry = createChatTelemetry({ pageKey: 'ai.chat' }, track, () => 0);

    telemetry.onSend('abcdef');
    telemetry.onDone({ conversationId: 5 });

    const streamEnd = calls.find(call => call.name === 'stream_end');
    expect(streamEnd?.input.tokenIn).toBeUndefined();
    expect(streamEnd?.input.tokenOut).toBeUndefined();
    expect(streamEnd?.input.sourcesCount).toBe(0);
    expect(streamEnd?.input.hasSources).toBe(false);
    expect(streamEnd?.input.extra).toEqual({});
  });

  it('无 sources 事件时 stream_end 记 0（无引用回答进入质量层分母）', () => {
    const { calls, track } = createRecorder();
    const telemetry = createChatTelemetry({ pageKey: 'ai.chat' }, track, () => 0);

    telemetry.onSend('无引用问题');
    telemetry.onDone({ conversationId: 5 });

    const streamEnd = calls.find(call => call.name === 'stream_end');
    expect(streamEnd?.input).toMatchObject({
      sourcesCount: 0,
      hasSources: false,
      queryHash: hashTelemetryText('无引用问题'),
    });
  });
});
