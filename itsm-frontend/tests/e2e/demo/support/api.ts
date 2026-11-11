import type { APIRequestContext, APIResponse } from 'playwright';
import { expect } from '@playwright/test';
import type { DemoSession } from '../fixtures/demo-auth';

export interface ApiEnvelope<T = unknown> {
  code: number;
  message: string;
  data: T;
}

export interface ApiCallOptions {
  headers?: Record<string, string>;
  params?: Record<string, string | number | boolean | undefined>;
  failOnError?: boolean;
}

export function authHeaders(
  session: DemoSession,
  extra: Record<string, string> = {},
): Record<string, string> {
  return {
    Authorization: `Bearer ${session.token}`,
    ...extra,
  };
}

export function tenantHeaders(customerTenantId: number): Record<string, string> {
  return { 'X-Customer-Tenant-ID': String(customerTenantId) };
}

function withParams(path: string, params?: ApiCallOptions['params']): string {
  if (!params) return path;
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.append(key, String(value));
  }
  const suffix = query.toString();
  if (!suffix) return path;
  return `${path}${path.includes('?') ? '&' : '?'}${suffix}`;
}

export async function callRaw(
  api: APIRequestContext,
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  options: ApiCallOptions & { data?: unknown } = {},
): Promise<APIResponse> {
  return api.fetch(withParams(path, options.params), {
    method,
    headers: options.headers,
    data: options.data,
  });
}

export async function unwrap<T = unknown>(
  response: APIResponse,
  expectedStatus = 200,
): Promise<T> {
  expect(response.status()).toBe(expectedStatus);
  const text = await response.text();
  let body: ApiEnvelope<T>;
  try {
    body = JSON.parse(text) as ApiEnvelope<T>;
  } catch {
    throw new Error(`expected JSON envelope, got HTTP ${response.status()}: ${text.slice(0, 300)}`);
  }
  expect(body.code, `API envelope code: ${JSON.stringify(body).slice(0, 500)}`).toBe(0);
  return body.data;
}

export async function apiGet<T = unknown>(
  session: DemoSession,
  path: string,
  options: ApiCallOptions = {},
): Promise<T> {
  const response = await callRaw(session.api, 'GET', path, {
    ...options,
    headers: authHeaders(session, options.headers),
  });
  return unwrap<T>(response);
}

export async function apiPost<T = unknown>(
  session: DemoSession,
  path: string,
  data?: unknown,
  options: ApiCallOptions = {},
): Promise<T> {
  const response = await callRaw(session.api, 'POST', path, {
    ...options,
    headers: authHeaders(session, options.headers),
    data,
  });
  return unwrap<T>(response);
}

export async function apiPut<T = unknown>(
  session: DemoSession,
  path: string,
  data?: unknown,
  options: ApiCallOptions = {},
): Promise<T> {
  const response = await callRaw(session.api, 'PUT', path, {
    ...options,
    headers: authHeaders(session, options.headers),
    data,
  });
  return unwrap<T>(response);
}

export async function apiPatch<T = unknown>(
  session: DemoSession,
  path: string,
  data?: unknown,
  options: ApiCallOptions = {},
): Promise<T> {
  const response = await callRaw(session.api, 'PATCH', path, {
    ...options,
    headers: authHeaders(session, options.headers),
    data,
  });
  return unwrap<T>(response);
}

export async function apiDelete<T = unknown>(
  session: DemoSession,
  path: string,
  options: ApiCallOptions & { data?: unknown } = {},
): Promise<T> {
  const response = await callRaw(session.api, 'DELETE', path, {
    ...options,
    headers: authHeaders(session, options.headers),
    data: options.data,
  });
  return unwrap<T>(response);
}

/** 负向断言：状态码必须命中 allowed，且响应体不得含 forbiddenText。 */
export async function expectRejected(
  response: APIResponse,
  allowedStatuses: number[],
  options: { forbiddenText?: string } = {},
): Promise<void> {
  expect(allowedStatuses, `unexpected success HTTP ${response.status()}`).toContain(response.status());
  if (options.forbiddenText) {
    const text = await response.text();
    expect(text).not.toContain(options.forbiddenText);
  }
}

export function runId(prefix = 'E2E'): string {
  const now = new Date();
  const pad = (n: number, len = 2) => String(n).padStart(len, '0');
  const stamp = [
    now.getFullYear(),
    pad(now.getMonth() + 1),
    pad(now.getDate()),
    pad(now.getHours()),
    pad(now.getMinutes()),
    pad(now.getSeconds()),
  ].join('');
  const random = Math.random().toString(36).slice(2, 6);
  return `${prefix}-${stamp}-${random}`;
}

export function title(run: string, suffix: string): string {
  return `[${run}] ${suffix}`;
}