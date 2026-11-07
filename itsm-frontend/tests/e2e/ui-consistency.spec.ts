import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test.describe('公共 UI 一致性', () => {
  test('登录页使用独立交互元素并提供可见键盘焦点', async ({ page }) => {
    await page.goto('/login');

    await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible();
    await expect(page.locator('a button')).toHaveCount(0);

    const accessibility = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21aa'])
      .analyze();
    expect(accessibility.violations).toEqual([]);

    const loginButton = page.getByRole('button', { name: '登录', exact: true });
    await loginButton.focus();
    await expect(loginButton).toHaveCSS('outline-style', 'solid');
    const outlineWidth = await loginButton.evaluate(element =>
      Number.parseFloat(getComputedStyle(element).outlineWidth)
    );
    expect(outlineWidth).toBeGreaterThanOrEqual(2);
  });

  test('360px 窄屏下登录表单无横向溢出且主操作完整可见', async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 800 });
    await page.goto('/login');

    const loginButton = page.getByRole('button', { name: '登录', exact: true });
    await expect(loginButton).toBeVisible();

    const viewport = await page.evaluate(() => ({
      contentWidth: document.documentElement.scrollWidth,
      width: window.innerWidth,
    }));
    expect(viewport.contentWidth).toBeLessThanOrEqual(viewport.width);

    const buttonBox = await loginButton.boundingBox();
    expect(buttonBox).not.toBeNull();
    expect(buttonBox!.x).toBeGreaterThanOrEqual(0);
    expect(buttonBox!.x + buttonBox!.width).toBeLessThanOrEqual(360);
  });
});
