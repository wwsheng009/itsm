/**
 * IP-P0-8 / FE-A1-A2：会话作用域必须由服务端派生，禁止 tenants[0] 强制自动选择。
 */

jest.mock('@/lib/api/http-client', () => ({
  httpClient: { get: jest.fn() },
}));

jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: { getState: jest.fn() },
}));

type LoginArgs = [unknown, string, { id: number } | undefined];

async function runBootstrap(me: unknown, tenantsResponse: unknown) {
  const { httpClient } = await import('@/lib/api/http-client');
  (httpClient.get as jest.Mock).mockImplementation((url: string) =>
    url.includes('/auth/me') ? Promise.resolve(me) : Promise.resolve(tenantsResponse)
  );

  const store = await import('@/lib/store/auth-store');
  const login = jest.fn();
  const setCurrentTenant = jest.fn();
  (store.useAuthStore.getState as jest.Mock).mockReturnValue({
    login,
    setCurrentTenant,
    isAuthenticated: false,
  });

  const bootstrap = await import('../session-bootstrap');
  bootstrap.resetSessionBootstrap();
  const status = await bootstrap.bootstrapSession();
  return { status, login, setCurrentTenant };
}

describe('session bootstrap tenant scope', () => {
  beforeEach(() => {
    jest.resetModules();
    jest.clearAllMocks();
  });

  it('uses the server tenantId (JWT scope) instead of tenants[0]', async () => {
    const { status, login, setCurrentTenant } = await runBootstrap(
      { id: 1, tenantId: 7 },
      {
        tenants: [
          { id: 9, name: 'Other', code: 'other', type: 'msp_customer', status: 'active' },
          { id: 7, name: 'Home', code: 'home', type: 'msp_provider', status: 'active' },
        ],
      }
    );

    expect(status).toBe('authenticated');
    expect(setCurrentTenant).toHaveBeenCalledTimes(1);
    expect(setCurrentTenant.mock.calls[0][0]).toMatchObject({ id: 7, code: 'home' });
    const loginArgs = login.mock.calls[0] as LoginArgs;
    expect(loginArgs[2]).toMatchObject({ id: 7, code: 'home' });
  });

  it('does not auto-select any tenant when server scope is absent', async () => {
    const { setCurrentTenant, login } = await runBootstrap(
      { id: 1 },
      { tenants: [{ id: 9, name: 'Other', code: 'other' }] }
    );

    expect(setCurrentTenant).not.toHaveBeenCalled();
    const loginArgs = login.mock.calls[0] as LoginArgs;
    expect(loginArgs[2]).toBeUndefined();
  });
});
