import {
  BOT_RISKS,
  BOT_RISK_RANK,
  buildImpactPreview,
  describeBotError,
  grantRiskExceeds,
  isBotFeatureDisabled,
  isBotPermissionDenied,
  parseEntrypoints,
  serializeEntrypoints,
} from '../bot-api';

/**
 * Bot 管理 API 纯函数测试（B2-03）。
 *
 * 覆盖：entrypoints JSON 解析的容错（损坏/非数组/混合类型）、序列化去重、
 * 影响面预览（受众展开 + 三类告警）、授权风险上限比较（合法/越界/未知值不误伤）、
 * 开关关闭（404 无 errorCode）与权限不足（403）的判定边界。
 */
describe('bot-api 纯函数', () => {
  describe('parseEntrypoints', () => {
    it('解析合法数组字符串并过滤空串', () => {
      expect(parseEntrypoints('["chat","ticket",""]')).toEqual(['chat', 'ticket']);
    });

    it('损坏/非数组/空值一律返回空数组（不抛错）', () => {
      expect(parseEntrypoints('not-json')).toEqual([]);
      expect(parseEntrypoints('{"a":1}')).toEqual([]);
      expect(parseEntrypoints('')).toEqual([]);
      expect(parseEntrypoints(undefined)).toEqual([]);
      expect(parseEntrypoints(null)).toEqual([]);
      expect(parseEntrypoints(42)).toEqual([]);
    });

    it('接受已经是数组的输入（视图层双通道）', () => {
      expect(parseEntrypoints(['chat', '  ', 'ticket'])).toEqual(['chat', 'ticket']);
    });
  });

  describe('serializeEntrypoints', () => {
    it('trim + 去重 + 丢弃空项，输出稳定 JSON', () => {
      expect(serializeEntrypoints(['chat', ' chat ', '', 'ticket'])).toBe('["chat","ticket"]');
      expect(serializeEntrypoints([])).toBe('[]');
    });
  });

  describe('buildImpactPreview', () => {
    it('audience=all → 内部 + 终端用户；draft + 零入口 + 零授权 → 三类告警全命中', () => {
      const preview = buildImpactPreview(
        { audience: 'all', entrypoints: [], status: 'draft' },
        0
      );
      expect(preview.audiences).toEqual(['internal', 'end_user']);
      expect(preview.entrypoints).toEqual([]);
      expect(preview.warnings).toEqual(['no_entrypoints', 'draft', 'no_grants']);
    });

    it('audience=end_user / internal 各自只列对应受众；ga + 有入口 + 有授权 → 无告警', () => {
      expect(buildImpactPreview({ audience: 'end_user', entrypoints: ['chat'], status: 'ga' }, 1)).toEqual({
        audiences: ['end_user'],
        entrypoints: ['chat'],
        warnings: [],
      });
      expect(buildImpactPreview({ audience: 'internal', entrypoints: ['chat '], status: 'pilot' }, 2)).toEqual({
        audiences: ['internal'],
        entrypoints: ['chat'],
        warnings: [],
      });
    });

    it('空 audience 归 internal（与后端 VisibleForRole 口径一致）', () => {
      expect(buildImpactPreview({ audience: '', entrypoints: ['chat'], status: 'ga' }, 1).audiences).toEqual([
        'internal',
      ]);
    });
  });

  describe('grantRiskExceeds', () => {
    it('越界判定：act_high > act_medium > act_low > plan > read', () => {
      expect(grantRiskExceeds('read', 'read')).toBe(false);
      expect(grantRiskExceeds('read', 'plan')).toBe(true);
      expect(grantRiskExceeds('act_low', 'act_low')).toBe(false);
      expect(grantRiskExceeds('act_low', 'act_medium')).toBe(true);
      expect(grantRiskExceeds('act_medium', 'act_high')).toBe(true);
      expect(grantRiskExceeds('act_high', 'read')).toBe(false);
    });

    it('未知取值不误伤（交后端校验，前端不阻塞）', () => {
      expect(grantRiskExceeds('unknown', 'read')).toBe(false);
      expect(grantRiskExceeds('read', 'unknown')).toBe(false);
    });

    it('风险序与后端 riskRank 对齐（完整枚举单调）', () => {
      const ranks = BOT_RISKS.map(risk => BOT_RISK_RANK[risk]);
      expect(ranks).toEqual([...ranks].sort((a, b) => a - b));
    });
  });

  describe('错误判定', () => {
    it('404 且无 errorCode → 功能未启用；403 → 权限不足（不误判为未启用）', () => {
      expect(isBotFeatureDisabled({ httpStatus: 404 })).toBe(true);
      expect(isBotFeatureDisabled({ httpStatus: 404, errorCode: 'not_found' })).toBe(false);
      expect(isBotFeatureDisabled({ httpStatus: 403 })).toBe(false);
      expect(isBotPermissionDenied({ httpStatus: 403 })).toBe(true);
      expect(isBotPermissionDenied({ httpStatus: 404 })).toBe(false);
      expect(isBotFeatureDisabled(undefined)).toBe(false);
      expect(isBotPermissionDenied(null)).toBe(false);
    });

    it('describeBotError 优先暴露后端 message（校验信息含字段原因）', () => {
      expect(describeBotError(new Error('bot: 参数非法: risk_limit 风险级别非法'))).toBe(
        'bot: 参数非法: risk_limit 风险级别非法'
      );
      expect(describeBotError({}, '兜底')).toBe('兜底');
    });
  });
});
