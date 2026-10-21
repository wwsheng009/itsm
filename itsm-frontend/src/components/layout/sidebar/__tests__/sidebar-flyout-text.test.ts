import fs from 'node:fs';
import path from 'node:path';

/**
 * 收起态侧边栏 flyout（hover 弹层）文字可见性回归。
 *
 * 缺陷背景：有图标的子项在收起态弹层里只剩图标、没有文字；无图标子项正常。
 * 根因是弹层内 `<li>` 为 display:block 的 inline 排版，而 Sidebar 的 label 是
 * 块级 flex 容器，行内盒中夹块级元素 → 强制换行 → 文本被挤出 40px 行高，
 * 再被 li 的 overflow:hidden 裁掉。
 *
 * 这里钉住三件事，防止将来「顺手删掉」或改回 block：
 *  1. 弹层 li 必须 flex 排成一行；
 *  2. title-content 必须是可伸缩 flex 项，且恢复 inline-collapsed 的隐藏样式；
 *  3. Sidebar 只能有一份 CSS Module（历史上两份重复，改动只生效一半）。
 */
describe('Sidebar 收起态 flyout 文字可见性', () => {
  const root = process.cwd();
  const overridesPath = path.join(root, 'src/styles/antd-layout-overrides.css');
  const css = fs.readFileSync(overridesPath, 'utf8');

  /** 去掉注释，避免注释里的说明文字造成误判 */
  const stripComments = (source: string) => source.replace(/\/\*[\s\S]*?\*\//g, '');

  const ruleBody = (source: string, selector: string): string | undefined => {
    const cleaned = stripComments(source);
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const match = cleaned.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
    return match?.[1];
  };

  it('弹层内菜单项改为 flex 单行布局（有图标时文字不被换行挤出）', () => {
    const body = ruleBody(css, '.ant-menu-submenu-popup .ant-menu-item,\n.ant-menu-submenu-popup .ant-menu-submenu-title');
    // 选择器可能被格式化为单行，回退用逐条检查
    const itemFlex =
      body ??
      ruleBody(css, '.ant-menu-submenu-popup .ant-menu-item') ??
      '';
    expect(itemFlex).toMatch(/display:\s*flex/);
    expect(itemFlex).toMatch(/align-items:\s*center/);
  });

  it('弹层内 title-content 恢复可见并作为可伸缩 flex 项', () => {
    const body = ruleBody(css, '.ant-menu-submenu-popup .ant-menu-title-content');
    expect(body).toBeDefined();
    expect(body).toMatch(/opacity:\s*1/);
    expect(body).toMatch(/visibility:\s*visible/);
    expect(body).toMatch(/max-width:\s*none/);
    expect(body).toMatch(/overflow:\s*visible/);
    expect(body).toMatch(/flex:\s*auto/);
    expect(body).toMatch(/min-width:\s*0/);
  });

  it('Sidebar 仅保留一份 CSS Module（避免改动只生效一半）', () => {
    const duplicates = [
      path.join(root, 'src/components/layout/Sidebar.module.css'),
      path.join(root, 'src/components/layout/sidebar/Sidebar.module.css'),
    ].filter(file => fs.existsSync(file));

    expect(duplicates).toHaveLength(1);
    expect(duplicates[0].replace(/\\/g, '/')).toContain('src/components/layout/sidebar/Sidebar.module.css');
  });

  it('Sidebar 与 MenuItems 引用同一份 CSS Module', () => {
    const read = (rel: string) => fs.readFileSync(path.join(root, rel), 'utf8');
    const sidebarSrc = read('src/components/layout/sidebar/Sidebar.tsx');
    const menuItemsSrc = read('src/components/layout/sidebar/MenuItems.tsx');

    expect(sidebarSrc).toContain("import styles from './Sidebar.module.css'");
    expect(menuItemsSrc).toContain("import styles from './Sidebar.module.css'");
    expect(menuItemsSrc).not.toContain("'../Sidebar.module.css'");
  });
});
