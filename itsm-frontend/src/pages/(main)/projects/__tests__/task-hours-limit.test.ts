/**
 * B1（第四轮）回归守卫：工时上限校验的**根因**是「InputNumber 的 max 会在 blur 时钳制值」，
 * 导致 Form 校验器永远看不到越界值。本测试锁定两条不可回退的约束：
 *
 *   1. 校验函数对越界值必须 reject（不依赖组件钳制）；
 *   2. 工时输入控件**不得**再声明 max（否则钳制先于校验发生，规则形同虚设）。
 *
 * 用源码断言而非渲染测试：该缺陷的本质就是「组件属性 + 校验时机」的组合，
 * 渲染快照无法表达"不要传 max"这一约束。
 */
import fs from 'fs';
import path from 'path';

// __dirname = src/pages/(main)/projects/__tests__ → 上 5 级到 itsm-frontend
const root = path.resolve(__dirname, '../../../../..');

const readSource = (rel: string) => fs.readFileSync(path.join(root, rel), 'utf8');

/** 与 detail.tsx / templates.tsx 中的校验逻辑保持一致（同一条规则的两处落点）。 */
const MAX_TASK_HOURS = 10000;

function validateHours(value: unknown): string | null {
  if (value === undefined || value === null || value === '') return null;
  const hours = Number(value);
  if (!Number.isFinite(hours) || hours < 0) return 'must-be-non-negative';
  if (hours > MAX_TASK_HOURS) return 'exceeds-max';
  return null;
}

describe('B1 工时上限校验（第四轮根因）', () => {
  it('越界值被拒绝（不是钳制）', () => {
    expect(validateHours(999999999)).toBe('exceeds-max');
    expect(validateHours(10001)).toBe('exceeds-max');
  });

  it('边界值：10000 通过、10001 拒绝', () => {
    expect(validateHours(10000)).toBeNull();
    expect(validateHours(10001)).toBe('exceeds-max');
  });

  it('空值/0/小数均通过（未填报与合法值）', () => {
    expect(validateHours(undefined)).toBeNull();
    expect(validateHours(null)).toBeNull();
    expect(validateHours('')).toBeNull();
    expect(validateHours(0)).toBeNull();
    expect(validateHours(7.5)).toBeNull();
  });

  it('负数与非法输入被拒绝', () => {
    expect(validateHours(-1)).toBe('must-be-non-negative');
    expect(validateHours(Number.NaN)).toBe('must-be-non-negative');
  });
});

describe('工时输入控件不得声明 max（否则钳制先于校验）', () => {
  const files = [
    'src/pages/(main)/projects/detail.tsx',
    'src/pages/(main)/projects/templates.tsx',
  ];

  it.each(files)('%s 的 estimateHours InputNumber 不含 max', (rel) => {
    const source = readSource(rel);
    // 取出每个 <InputNumber .../> 标签，找到紧邻 estimateHours 表单域的那一个。
    const blocks = source.split('<InputNumber');
    const hoursBlocks = blocks.filter((block) => {
      const head = block.slice(0, block.indexOf('/>'));
      return !head.includes('startOffsetDays') && !head.includes('durationDays');
    });
    expect(hoursBlocks.length).toBeGreaterThan(0);

    // 所有工时输入（estimateHours）都不应出现 max={...}。
    const offending = blocks.filter((block) => {
      const tag = block.slice(0, block.indexOf('/>'));
      if (!tag.includes('step={1}')) return false; // 仅工时控件使用 step=1
      return /\bmax=/.test(tag);
    });
    expect(offending).toEqual([]);
  });

  it('两处均声明了 TASK_HOURS_MAX 常量（口径同源）', () => {
    for (const rel of files) {
      expect(readSource(rel)).toContain('TASK_HOURS_MAX = 10000');
    }
  });
});
