import fs from 'node:fs';
import path from 'node:path';
import { colors, contrastChecker, darkColors } from '@/lib/design-system/colors';
import { generateCSSVariables, getAntdTheme } from '@/lib/design-system/theme';

const root = process.cwd();
const read = (relativePath: string) => fs.readFileSync(path.join(root, relativePath), 'utf8');
const stripComments = (source: string) => source.replace(/\/\*[\s\S]*?\*\//g, '');
const ruleBody = (source: string, selector: string): string | undefined => {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return stripComments(source).match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1];
};
const contrast = (foreground: string, background: string) =>
  contrastChecker.getContrastRatio(foreground, background);

describe('Sidebar 主题一致性', () => {
  it.each([
    ['亮色', false, colors],
    ['暗色', true, darkColors],
  ] as const)('%s菜单的父子项状态共用令牌且文字可读', (_name, isDark, palette) => {
    const menu = getAntdTheme(isDark).components?.Menu;

    expect(menu).toMatchObject({
      itemColor: palette.functional.text.secondary,
      itemHoverColor: palette.primary[700],
      itemSelectedColor: palette.primary[700],
      subMenuItemSelectedColor: palette.primary[700],
      itemBg: palette.functional.background.primary,
      subMenuItemBg: palette.functional.background.primary,
      itemHoverBg: palette.functional.background.tertiary,
      itemSelectedBg: palette.primary[50],
      darkItemColor: darkColors.functional.text.secondary,
      darkItemHoverColor: darkColors.primary[700],
      darkItemSelectedColor: darkColors.primary[700],
      darkItemBg: darkColors.functional.background.primary,
      darkItemHoverBg: darkColors.functional.background.tertiary,
      darkItemSelectedBg: darkColors.primary[50],
    });

    expect(contrast(palette.functional.text.secondary, palette.functional.background.primary)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(palette.primary[700], palette.functional.background.tertiary)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(palette.primary[700], palette.primary[50])).toBeGreaterThanOrEqual(4.5);
  });

  it('暗色折叠弹层使用暗色文字、背景和选中令牌', () => {
    const menu = getAntdTheme(true).components?.Menu;

    expect(menu).toMatchObject({
      darkPopupBg: darkColors.functional.background.elevated,
      darkSubMenuItemBg: darkColors.functional.background.primary,
    });
    expect(
      contrast(darkColors.functional.text.secondary, darkColors.functional.background.elevated),
    ).toBeGreaterThanOrEqual(4.5);
  });

  it.each([false, true])('辅助文字变量在 isDark=%s 时满足普通文本对比度', isDark => {
    const variables = generateCSSVariables(isDark);

    expect(
      contrast(variables['--color-text-secondary'], variables['--color-background-primary']),
    ).toBeGreaterThanOrEqual(4.5);
  });

  it('Sider 与两处 MenuItems 均跟随同一个运行时主题', () => {
    const sidebar = read('src/components/layout/sidebar/Sidebar.tsx');
    const menuItems = read('src/components/layout/sidebar/MenuItems.tsx');

    expect(sidebar).toContain('const { isDark } = useTheme();');
    expect(sidebar).toContain("const menuTheme = isDark ? 'dark' : 'light';");
    expect(sidebar.match(/theme=\{menuTheme\}/g)).toHaveLength(3);
    expect(menuItems).toContain('theme={theme}');
    expect(menuItems).not.toMatch(/theme=["'{]light/);
  });

  it('侧边栏辅助文字和背景使用主题变量', () => {
    const css = read('src/components/layout/sidebar/Sidebar.module.css');

    expect(ruleBody(css, '.sider')).toMatch(/background:\s*var\(--color-background-primary/);
    expect(ruleBody(css, '.logoSubtext')).toMatch(/color:\s*var\(--color-text-secondary\)/);
    expect(ruleBody(css, '.userRole')).toMatch(/color:\s*var\(--color-text-secondary\)/);
    expect(css).not.toContain('.customMenu :global(.ant-menu-item:hover)');
    expect(ruleBody(css, '.customMenu :global(.ant-menu-item-selected)')).not.toMatch(
      /(?:background|color)\s*:/,
    );
  });

  it('全局样式不再用固定渐变或颜色覆盖菜单令牌', () => {
    const overrides = read('src/styles/antd-layout-overrides.css');
    const globals = read('src/styles/globals.css');

    expect(overrides).not.toContain('.ant-layout-sider-light');
    expect(overrides).not.toContain('.ant-layout-sider-dark');
    expect(ruleBody(overrides, '.ant-menu-item-selected')).not.toMatch(/(?:background|color)\s*:/);
    expect(ruleBody(overrides, '.ant-menu-item-selected .ant-menu-title-content')).not.toMatch(
      /(?:background|color)\s*:/,
    );
    expect(globals).not.toMatch(/\.custom-menu \.ant-menu-item:hover\s*\{/);
    expect(globals).not.toMatch(/\.custom-menu \.ant-menu-item-selected\s*\{/);
  });

  it('菜单与 Sider 滚动条跟随主题变量', () => {
    const sidebar = read('src/components/layout/sidebar/Sidebar.module.css');
    const globals = read('src/styles/globals.css');

    expect(ruleBody(sidebar, '.mainMenu')).toMatch(
      /scrollbar-color:\s*var\(--color-border-secondary\) transparent/,
    );
    expect(ruleBody(sidebar, '.mainMenu::-webkit-scrollbar-thumb')).toMatch(
      /background-color:\s*var\(--color-border-secondary\)/,
    );
    expect(ruleBody(globals, '.ant-layout-sider::-webkit-scrollbar-thumb')).toMatch(
      /background:\s*var\(--color-border-secondary\)/,
    );
  });
});
