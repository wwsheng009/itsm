import { expect, type Page, type Response } from '@playwright/test';

export interface PageIssueCollector {
  consoleErrors: string[];
  failedRequests: string[];
  expectedFailures: string[];
  responseHistory: Map<string, number[]>;
  stop: () => void;
}

/**
 * 收集页面 console error 与 API 失败请求。
 *
 * 说明：演示环境已知噪音（antd 弃用告警、缺失静态资源等）应放进 ignoredPatterns；
 * 非预期错误必须在测试末尾断言为空。
 */
export function collectPageIssues(
  page: Page,
  ignoredPatterns: RegExp[] = [],
): PageIssueCollector {
  const consoleErrors: string[] = [];
  const failedRequests: string[] = [];
  const expectedFailures: string[] = [];
  const responseHistory = new Map<string, number[]>();

  const isIgnored = (text: string) => ignoredPatterns.some(pattern => pattern.test(text));

  const onConsole = (msg: { type: () => string; text: () => string }) => {
    if (msg.type() !== 'error') return;
    const text = msg.text();
    if (!isIgnored(text)) consoleErrors.push(text);
  };
  const onResponse = (response: Response) => {
    const url = response.url();
    if (!url.includes('/api/')) return;
    const requestKey = `${response.request().method()} ${url}`;
    const history = responseHistory.get(requestKey) ?? [];
    history.push(response.status());
    responseHistory.set(requestKey, history);
    if (response.status() < 400) return;
    const line = `${response.status()} ${url}`;
    if (isIgnored(line)) expectedFailures.push(line);
    else failedRequests.push(line);
  };

  page.on('console', onConsole);
  page.on('response', onResponse);

  return {
    consoleErrors,
    failedRequests,
    expectedFailures,
    responseHistory,
    stop: () => {
      page.off('console', onConsole);
      page.off('response', onResponse);
    },
  };
}

export async function expectCleanPage(
  page: Page,
  collector: PageIssueCollector,
  options: { allowFailedRequests?: RegExp[]; allowConsoleErrors?: RegExp[] } = {},
): Promise<void> {
  await page.waitForTimeout(300);
  const allow = options.allowFailedRequests ?? [];
  const consoleAllow = options.allowConsoleErrors ?? [];
  const retriedRequestKeys = new Set(
    [...collector.responseHistory.entries()]
      .filter(([, statuses]) =>
        statuses.some(
          (status, index) => status >= 400 && statuses.slice(index + 1).some(next => next < 400),
        ),
      )
      .map(([key]) => key),
  );
  const isRetriedFailure = (line: string) => {
    const url = line.slice(line.indexOf(' ') + 1);
    return [...retriedRequestKeys].some(key => key.endsWith(` ${url}`));
  };
  const unexpected = collector.failedRequests.filter(
    line => !allow.some(re => re.test(line)) && !isRetriedFailure(line),
  );
  let retriedFailureBudget = collector.failedRequests.filter(isRetriedFailure).length;
  const unexpectedConsole = collector.consoleErrors.filter(text => {
    if (consoleAllow.some(re => re.test(text))) return false;
    if (retriedFailureBudget > 0 && /status of 403/i.test(text)) {
      retriedFailureBudget -= 1;
      return false;
    }
    return true;
  });
  expect(unexpectedConsole, `console errors: ${JSON.stringify(unexpectedConsole)}`).toEqual([]);
  expect(unexpected, `unexpected API failures: ${JSON.stringify(unexpected)}`).toEqual([]);
}

/** 关键页面不应出现裸 i18n key（只扫描常见词表前缀，避免误伤正文）。 */
export async function expectNoRawI18nKeys(page: Page): Promise<void> {
  const body = (await page.locator('body').innerText()).slice(0, 200_000);
  const bad = body.match(
    /\b(?:detailTabs|ticketDetail|common|changes|incidents|problemInvestigation|serviceCatalog|msp)\.[a-zA-Z][a-zA-Z0-9_.-]{1,80}/g,
  );
  expect(bad ?? [], `raw i18n keys found: ${JSON.stringify(bad)}`).toEqual([]);
}

/** 关键页面不应裸透角色 code。 */
export async function expectNoRawRoleCodes(page: Page): Promise<void> {
  const body = (await page.locator('body').innerText()).slice(0, 200_000);
  const forbidden = [
    'super_admin',
    'tenant_admin',
    'customer_user',
    'provider_agent',
    'msp_specialist',
    'msp_manager',
    'msp_tech',
    'end_user',
    'it_admin',
    'security_admin',
  ].filter(code => body.includes(code));
  expect(forbidden, `raw role codes found: ${JSON.stringify(forbidden)}`).toEqual([]);
}

/** 页面不应展示 BPMN 技术标识。 */
export async function expectNoRawBpmnIds(page: Page): Promise<void> {
  const body = (await page.locator('body').innerText()).slice(0, 200_000);
  const forbidden = ['user_task', 'userTask', 'exclusiveGateway', 'serviceTask', 'startEvent', 'endEvent'].filter(
    id => body.includes(id),
  );
  expect(forbidden, `raw BPMN ids found: ${JSON.stringify(forbidden)}`).toEqual([]);
}

export async function expectNoHorizontalScroll(page: Page): Promise<void> {
  const result = await page.evaluate(() => {
    const doc = document.documentElement;
    const body = document.body;
    return {
      scrollWidth: Math.max(doc?.scrollWidth ?? 0, body?.scrollWidth ?? 0),
      clientWidth: window.innerWidth,
    };
  });
  expect(
    result.scrollWidth,
    `horizontal overflow: scrollWidth=${result.scrollWidth}, clientWidth=${result.clientWidth}`,
  ).toBeLessThanOrEqual(result.clientWidth + 2);
}

export async function gotoAndReady(page: Page, path: string): Promise<void> {
  await page.goto(path, { waitUntil: 'domcontentloaded' });
  await page.waitForLoadState('networkidle').catch(() => undefined);
}