import {
  departmentBucketLabel,
  flattenDepartmentNames,
  formatRate,
  managerBucketLabel,
} from '../ProjectPortfolioMetrics';
import type { Department } from '@/lib/services/department-service';

// PE-24 组合视图纯函数（比率展示/桶标签/部门映射）；口径见 docs/project-management/plan/p2-portfolio-metrics-design.md §6。

describe('formatRate', () => {
  it('null/undefined → 「—」（分母为 0 不显示 0%）', () => {
    expect(formatRate(null)).toBe('—');
    expect(formatRate(undefined)).toBe('—');
  });

  it('比率转 1 位小数百分比', () => {
    expect(formatRate(0)).toBe('0.0%');
    expect(formatRate(0.5)).toBe('50.0%');
    expect(formatRate(1 / 3)).toBe('33.3%');
    expect(formatRate(1)).toBe('100.0%');
  });
});

describe('managerBucketLabel', () => {
  it('优先后端富化姓名', () => {
    expect(managerBucketLabel({ key: '3', count: 2, name: '张三' })).toBe('张三');
  });

  it('未设置（key=0）与缺失姓名降级', () => {
    expect(managerBucketLabel({ key: '0', count: 1 })).toBe('未设置');
    expect(managerBucketLabel({ key: '9', count: 1 })).toBe('用户#9');
  });
});

describe('flattenDepartmentNames', () => {
  it('含子部门递归展开', () => {
    const tree = [
      {
        id: 1,
        name: '交付部',
        code: 'D1',
        children: [{ id: 2, name: '交付一组', code: 'D1-1' }],
      },
    ] as Department[];
    const names = flattenDepartmentNames(tree);
    expect(names.get(1)).toBe('交付部');
    expect(names.get(2)).toBe('交付一组');
    expect(names.size).toBe(2);
  });

  it('空树 → 空映射', () => {
    expect(flattenDepartmentNames([]).size).toBe(0);
  });
});

describe('departmentBucketLabel', () => {
  const names = new Map<number, string>([[9, '交付部']]);

  it('按部门树映射名称', () => {
    expect(departmentBucketLabel({ key: '9', count: 2 }, names)).toBe('交付部');
  });

  it('未设置与映射缺失降级', () => {
    expect(departmentBucketLabel({ key: '0', count: 1 }, names)).toBe('未设置');
    expect(departmentBucketLabel({ key: '7', count: 1 }, names)).toBe('部门#7');
  });
});