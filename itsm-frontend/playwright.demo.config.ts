import { defineConfig, devices } from '@playwright/test';

/**
 * 独立 demo E2E 配置。
 *
 * 目标：直接针对已部署的演示栈（默认 238:8089）运行，不启动本地 Vite，
 * 避免现有 playwright.config.ts 的 webServer 把远端运行误导向 localhost:3000。
 *
 * 运行：
 *   PLAYWRIGHT_BASE_URL=http://172.18.3.238:8089 \
 *   npx playwright test -c playwright.demo.config.ts --workers=1
 */
const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://172.18.3.238:8089';
const useChromeChannel = process.env.PLAYWRIGHT_DEMO_CHANNEL !== 'chromium';

export default defineConfig({
  testDir: './tests/e2e/demo',
  outputDir: process.env.PLAYWRIGHT_DEMO_OUTPUT_DIR || '../.dev/playwright-demo-results',
  timeout: 60_000,
  expect: {
    timeout: 10_000,
  },
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [
    ['list'],
    ['html', { outputFolder: '../.dev/playwright-demo-report', open: 'never' }],
  ],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
    launchOptions: {
      args: ['--disable-crashpad', '--disable-breakpad', '--allow-insecure-localhost'],
    },
  },
  projects: [
    {
      name: 'demo-chrome',
      use: {
        browserName: 'chromium',
        ...(useChromeChannel ? { channel: 'chrome' as const } : {}),
        viewport: { width: 1440, height: 900 },
      },
    },
  ],
});