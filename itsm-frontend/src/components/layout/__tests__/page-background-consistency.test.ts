import fs from 'node:fs';
import path from 'node:path';

const root = process.cwd();
const read = (relativePath: string) => fs.readFileSync(path.join(root, relativePath), 'utf8');

describe('页面级背景一致性', () => {
  it('全局 page-chrome 使用页面底而不是内容 surface', () => {
    const globals = read('src/styles/globals.css');
    const rule = globals.match(/\.page-chrome\s*\{([^}]*)\}/)?.[1] ?? '';

    expect(rule).toMatch(/background-color:\s*var\(--color-background-primary\)/);
    expect(rule).toMatch(/border-color:\s*var\(--color-border-primary\)/);
    expect(rule).not.toMatch(/surface-primary/);
  });

  it('业务页面模板的页头和筛选栏使用 page-chrome', () => {
    const template = read('src/components/layout/BusinessPageTemplate.tsx');

    expect(template.match(/className="page-chrome border-b"/g)).toHaveLength(2);
    expect(template).not.toMatch(/bg-\[var\(--color-surface-primary\)\][^\n]*border-b/);
  });

  it('工单布局、工单页头和流程设计器工具栏使用 page-chrome', () => {
    const ticketsLayout = read('src/layouts/TicketsLayout.tsx');
    const ticketsPage = read('src/pages/(main)/tickets/index.tsx');
    const workflowToolbar = read('src/components/workflow/designer/WorkflowToolbar.tsx');

    expect(ticketsLayout).toContain('className="page-chrome border-b"');
    expect(ticketsLayout).not.toMatch(/className="[^"]*\bbg-white\b/);
    expect(ticketsPage.match(/className="page-chrome border-b"/g)?.length).toBeGreaterThanOrEqual(1);
    expect(workflowToolbar).toContain('page-chrome border-b');
    expect(workflowToolbar).not.toMatch(/className="[^"]*\bbg-white\b/);
  });

  it('个人中心等一级页面根容器使用页面底色', () => {
    const profile = read('src/pages/(main)/profile/index.tsx');

    expect(profile).toContain("bgSubtle: 'var(--color-page-background)'");
    expect(profile).not.toContain("bgSubtle: 'var(--color-background-secondary)'");
  });

  it('旧布局的页面级 chrome 也跟随页面底色，避免再次引入 surface', () => {
    const pageLayout = read('src/components/layout/PageLayout.tsx');
    const responsiveLayout = read('src/components/layout/ResponsiveLayout.tsx');

    expect(pageLayout.match(/backgroundColor:\s*'var\(--color-background-primary\)'/g)?.length).toBe(4);
    // ContentLayout 的 bordered 分支是内容表面，仍应保留 colorBgContainer。
    expect(pageLayout).toContain('backgroundColor: token.colorBgContainer');
    expect(responsiveLayout.match(/page-chrome/g)?.length).toBe(2);
  });
});