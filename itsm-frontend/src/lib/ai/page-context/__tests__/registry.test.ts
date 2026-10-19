/**
 * D0-03/I0-02 页面注册表契约测试。
 *
 * 门禁：
 * 1. 规模 ≥20（Top 20 页面可用）；
 * 2. 每个注册项必须命中前端路由表 ROUTE_PATHS（静态路径直查；动态模式校验静态前缀），
 *    防止注册表与路由漂移（僵尸声明）；
 * 3. pageKey 唯一；**I0-02：每页 ≥1 任务卡**（任务 1–2、问题 0–2、总数 ≤3）；动作 id 页内唯一；
 * 4. entrypoint 必须在后端已知入口枚举内；
 * 5. **D4 不变量**：任务卡/问题卡全部只读（零输入可执行不得触写）；
 * 6. 文案与提示词非空（避免空卡片）。
 */
import { ROUTE_PATHS } from '@/routes/route-paths';
import { ASK_AI_ENTRYPOINTS } from '@/lib/ai/ask-ai-scope';
import { PAGE_ASSISTANT_REGISTRY } from '../registry';
import { flattenPageActions } from '../types';

const staticPrefix = (pattern: string): string => {
  const idx = pattern.indexOf('/:');
  return idx === -1 ? pattern : pattern.slice(0, idx);
};

describe('页面助手注册表契约（D0-03 / I0-02）', () => {
  it('规模：Top 20 页面可用（≥20 项）', () => {
    expect(PAGE_ASSISTANT_REGISTRY.length).toBeGreaterThanOrEqual(20);
  });

  it('每个注册项都命中路由表（静态直查 / 动态校验静态前缀）', () => {
    for (const declaration of PAGE_ASSISTANT_REGISTRY) {
      const prefix = staticPrefix(declaration.pattern);
      const hit = ROUTE_PATHS.has(declaration.pattern) || ROUTE_PATHS.has(prefix);
      expect(hit).toBe(true);
    }
  });

  it('I0-02 分组：每页 ≥1 任务卡；任务 1–2、问题 0–2、总数 ≤3；id 页内唯一', () => {
    for (const declaration of PAGE_ASSISTANT_REGISTRY) {
      expect(declaration.tasks.length).toBeGreaterThanOrEqual(1);
      expect(declaration.tasks.length).toBeLessThanOrEqual(2);
      expect(declaration.questions.length).toBeLessThanOrEqual(2);

      const actions = flattenPageActions(declaration);
      expect(actions.length).toBeLessThanOrEqual(3);
      // 扁平化顺序 = 任务卡在前（服务端 rank / 建议区顺序一致）。
      expect(actions.slice(0, declaration.tasks.length).every(action => action.kind === 'task')).toBe(
        true
      );

      const actionIds = new Set<string>();
      for (const action of actions) {
        expect(actionIds.has(action.id)).toBe(false);
        actionIds.add(action.id);
      }
    }
  });

  it('pageKey 全局唯一', () => {
    const pageKeys = new Set<string>();
    for (const declaration of PAGE_ASSISTANT_REGISTRY) {
      expect(pageKeys.has(declaration.pageKey)).toBe(false);
      pageKeys.add(declaration.pageKey);
    }
  });

  it('entrypoint 必须在后端已知入口枚举内（缺省 = chat）', () => {
    for (const declaration of PAGE_ASSISTANT_REGISTRY) {
      if (declaration.entrypoint === undefined) continue;
      expect(
        (ASK_AI_ENTRYPOINTS as readonly string[]).includes(declaration.entrypoint)
      ).toBe(true);
    }
  });

  it('D4 不变量：任务卡/问题卡全部只读（不允许"点了就写"）', () => {
    for (const declaration of PAGE_ASSISTANT_REGISTRY) {
      for (const task of declaration.tasks) {
        expect(task.kind).toBe('task');
        expect(task.readOnly).toBe(true);
      }
      for (const question of declaration.questions) {
        expect(question.kind).toBe('question');
        expect(question.readOnly).toBe(true);
      }
    }
  });

  it('文案与提示词非空（避免空卡片）', () => {
    for (const declaration of PAGE_ASSISTANT_REGISTRY) {
      expect(declaration.title.trim().length).toBeGreaterThan(0);
      for (const action of flattenPageActions(declaration)) {
        expect(action.label.trim().length).toBeGreaterThan(0);
        expect(action.prompt.trim().length).toBeGreaterThan(0);
      }
    }
  });
});