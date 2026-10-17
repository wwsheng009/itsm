import { buildHeatmapRows, formatHours, heatmapCellColor } from '../ProjectResourceHeatmap';
import type { HeatmapMemberWeek, HeatmapWeek } from '@/lib/services/project-service';

// PE-23 资源热图纯函数（转置/配色/格式化）；口径见 docs/project-management/plan/p2-resource-heatmap-design.md §5。

function mkMember(userId: number, name: string, planned: number, capacity: number): HeatmapMemberWeek {
  return {
    userId,
    name,
    username: `u${userId}`,
    projectRole: 'member',
    memberStatus: 'active',
    capacityHours: capacity,
    plannedHours: planned,
    remainingHours: planned / 2,
    plannedRatio: planned / capacity,
    remainingRatio: planned / capacity / 2,
  };
}

function mkWeek(weekStart: string, members: HeatmapMemberWeek[]): HeatmapWeek {
  return { weekStart, weekEnd: weekStart, members };
}

describe('heatmapCellColor', () => {
  it('按负荷率分档（0 灰、≤50% 绿、≤80% 黄、≤100% 橙、>100% 红）', () => {
    expect(heatmapCellColor(0)).toBe('#fafafa');
    expect(heatmapCellColor(0.3)).toBe('#d9f7be');
    expect(heatmapCellColor(0.5)).toBe('#d9f7be');
    expect(heatmapCellColor(0.8)).toBe('#fff1b8');
    expect(heatmapCellColor(1)).toBe('#ffe7ba');
    expect(heatmapCellColor(1.01)).toBe('#ffccc7');
  });

  it('非法值降级为无负荷色', () => {
    expect(heatmapCellColor(Number.NaN)).toBe('#fafafa');
    expect(heatmapCellColor(-1)).toBe('#fafafa');
  });
});

describe('formatHours', () => {
  it('最多 1 位小数且整数不带 .0', () => {
    expect(formatHours(14)).toBe('14');
    expect(formatHours(2.85)).toBe('2.9');
    expect(formatHours(0)).toBe('0');
    expect(formatHours(Number.POSITIVE_INFINITY)).toBe('0');
  });
});

describe('buildHeatmapRows', () => {
  it('周×成员转置为成员行（跨周对齐）', () => {
    const weeks = [
      mkWeek('2026-10-19', [mkMember(1, '张三', 14, 20), mkMember(2, '李四', 7, 40)]),
      mkWeek('2026-10-26', [mkMember(1, '张三', 7, 20), mkMember(2, '李四', 0, 40)]),
    ];

    const rows = buildHeatmapRows(weeks);

    expect(rows).toHaveLength(2);
    expect(rows[0].name).toBe('张三');
    expect(rows[0].cells[0]?.plannedHours).toBe(14);
    expect(rows[0].cells[1]?.plannedHours).toBe(7);
    expect(rows[1].username).toBe('u2');
    expect(rows[1].cells[1]?.plannedHours).toBe(0);
  });

  it('仅在后一周出现的成员补齐为空单元格', () => {
    const weeks = [mkWeek('2026-10-19', [mkMember(1, '张三', 8, 20)]), mkWeek('2026-10-26', [mkMember(9, '王五', 4, 40)])];

    const rows = buildHeatmapRows(weeks);

    expect(rows).toHaveLength(2);
    const extra = rows.find((r) => r.userId === 9);
    expect(extra).toBeDefined();
    expect(extra?.cells[0]).toBeUndefined();
    expect(extra?.cells[1]?.plannedHours).toBe(4);
    expect(rows[0].cells[1]).toBeUndefined();
  });

  it('无名成员降级为 用户#id；空周返回空行', () => {
    const unnamed: HeatmapMemberWeek = { ...mkMember(7, '', 1, 40), username: '' };
    const rows = buildHeatmapRows([mkWeek('2026-10-19', [unnamed])]);
    expect(rows[0].name).toBe('用户#7');
    expect(buildHeatmapRows([])).toEqual([]);
  });
});