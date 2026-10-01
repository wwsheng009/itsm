import type { Tenant } from '@/lib/api/api-config';
import { API_BASE_URL } from '@/lib/api/api-config';
import { useAuthStore } from '@/lib/store/auth-store';

export class AuthService {
  /**
   * 第三方登录
   */
  static async thirdPartyLogin(provider: string, code: string, state?: string | null): Promise<void> {
    // 例外：第三方 OAuth 回调路径（/api/auth/:provider/callback）不同于 SSO callback，无法走 AuthAPI
    // eslint-disable-next-line no-restricted-syntax
    const response = await fetch(`/api/auth/${provider}/callback`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ code, state }),
    });

    if (!response.ok) {
      throw new Error('登录失败');
    }

    const data = await response.json();
    // 不将令牌/用户信息写入 localStorage（避免 XSS 窃取）。
    // 令牌由后端 httpOnly cookie 管理；前端仅写入 auth-token 标记位供 middleware 路由守卫使用。
    if (typeof window !== 'undefined' && data.user) {
      const secure = location.protocol === 'https:' ? '; Secure' : '';
      // 仅写入标记位（非真值 token），供 middleware 路由守卫判断登录态
      // 真值 token 由后端 httpOnly cookie 管理，JS 不可读，防 XSS 窃取
      document.cookie = `auth-token=1; path=/; SameSite=Lax${secure}`;
    }
    if (data.user) {
      const { login } = useAuthStore.getState();
      const u = data.user as any;
      login(
        {
          id: Number(u?.id || 0),
          username: String(u?.username || ''),
          role: String(u?.role || 'end_user'),
          email: String(u?.email || ''),
          name: String(u?.name || u?.fullName || ''),
          tenantId: u?.tenantId ? Number(u.tenantId) : undefined,
          department: u?.department,
          permissions: u?.permissions,
        },
        String(data.token || 'authenticated'),
        {
          id: Number(u?.tenantId || 1),
          name: '默认租户',
          code: 'default',
          type: 'standard' as any,
          status: 'active' as any,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        } as Tenant
      );
    }
  }

  // Only non-sensitive UI marker cookies may be inspected in the browser.
  // Authentication tokens are intentionally not read through this helper.
  private static getCookie(name: string): string | null {
    if (typeof document === 'undefined') return null;
    for (const cookie of document.cookie.split(';')) {
      const [cookieName, cookieValue] = cookie.trim().split('=');
      if (cookieName === name) return decodeURIComponent(cookieValue || '');
    }
    return null;
  }

  // 设置tokens（空实现，保留向后兼容）
  // 安全：access_token 由后端 httpOnly cookie 管理，前端不存储 token 真值
  // middleware 路由守卫依赖 auth-token 标记位 cookie（非真值）
  static setTokens(accessToken: string, refreshToken: string) {
    void accessToken;
    void refreshToken;
  }

  // 获取access token
  static getAccessToken(): string | null {
	return null;
  }

  // Backward-compatible helpers used by some UI providers
  static getToken(): string | null {
    return this.getAccessToken();
  }

  static getCurrentUser() {
    const { user } = useAuthStore.getState();
    return user;
  }

  // 获取refresh token
  static getRefreshToken(): string | null {
	return null;
  }

  // 检查是否已认证
  static isAuthenticated(): boolean {
    const { isAuthenticated } = useAuthStore.getState();
    if (isAuthenticated) return true;
	return false;
  }

  // 直接使用fetch进行HTTP请求，避免循环依赖
  private static async makeRequest<T>(endpoint: string, options: RequestInit): Promise<T> {
    const url = `${API_BASE_URL}${endpoint}`;

    // eslint-disable-next-line no-restricted-syntax
    const response = await fetch(url, {
      headers: {
        'Content-Type': 'application/json',
        ...options.headers,
      },
      credentials: options.credentials || 'include', // 默认包含cookies
      ...options,
    });

    if (!response.ok) {
      throw await this.errorFromFailedResponse(response);
    }

    const responseData = (await response.json()) as {
      code: number;
      message: string;
      data: T | { retryAfterSeconds?: number } | null;
    };

    // 检查响应码
    if (responseData.code !== 0) {
      // P0-2（2026-09-06 UAT 修复）：登录限流响应 data.retryAfterSeconds 一并带出，
      // 调用方（LoginForm）据此展示倒计时。
      const data = (responseData.data ?? null) as { retryAfterSeconds?: number } | null;
      const err = new Error(responseData.message || '请求失败');
      if (data && typeof data.retryAfterSeconds === 'number') {
        (err as Error & { retryAfterSeconds?: number }).retryAfterSeconds = data.retryAfterSeconds;
      }
      throw err;
    }

    return responseData.data as T;
  }

  /**
   * 把非 2xx 响应转成带可展示原因的 Error。
   *
   * 后端的失败响应体同样是标准 {code, message, data}（见 common/response.go 的 Fail），
   * 只是 HTTP 状态码非 2xx；fetch 不会自动解析，若直接抛
   * "HTTP error! status: 401"，登录页就会把这条内部串当成失败原因展示给用户。
   *
   * 同时把登录限流响应里的 data.retryAfterSeconds 一并带出：
   * 限流走 ForbiddenCode → HTTP 403（middleware/rate_limiter.go），
   * 只解析 2xx 分支会永远拿不到它，按钮倒计时形同虚设。
   */
  private static async errorFromFailedResponse(response: Response): Promise<Error> {
    let message = '';
    let retryAfterSeconds: number | undefined;

    try {
      const body = (await response.json()) as {
        message?: unknown;
        data?: { retryAfterSeconds?: unknown } | null;
      };
      if (typeof body?.message === 'string') {
        message = body.message.trim();
      }
      const retryAfter = body?.data?.retryAfterSeconds;
      if (typeof retryAfter === 'number' && retryAfter > 0) {
        retryAfterSeconds = retryAfter;
      }
    } catch {
      // 响应体不是 JSON（网关/代理错误页等），退回下面的状态码描述
    }

    const err = new Error(message || `HTTP error! status: ${response.status}`);
    if (retryAfterSeconds !== undefined) {
      (err as Error & { retryAfterSeconds?: number }).retryAfterSeconds = retryAfterSeconds;
    }
    return err;
  }

  /**
   * 以服务端为准确认会话已生效。
   *
   * 登录接口 200 只代表服务端下发了 Set-Cookie；真正决定能否进入受保护路由的是
   * middleware 对 httpOnly access_token cookie 的校验（前端 JS 读不到它）。
   * 因此先用一次真实请求确认会话可用，避免出现
   * 「接口成功、store 已登录，但 router.push 被 middleware 静默 307 回 /login」的分裂状态。
   */
  private static async confirmSession(): Promise<boolean> {
    try {
      await this.makeRequest<unknown>('/api/v1/auth/me', { method: 'GET' });
      return true;
    } catch {
      return false;
    }
  }

  // 刷新token
  static async refreshToken(): Promise<boolean> {
    try {
      await this.makeRequest<Record<string, never>>('/api/v1/auth/refresh', {
        method: 'POST',
        credentials: 'include', // Include httpOnly cookies
		body: '{}',
      });
      return true;
    } catch (error) {
      console.error('Token refresh failed:', error);
      this.clearTokens();
      return false;
    }
  }

  // 清除所有tokens
  static clearTokens() {
    const { logout } = useAuthStore.getState();
    logout();
  }

  // 登出方法
  static logout() {
    const { logout } = useAuthStore.getState();
    try {
      // 例外：登出是 fire-and-forget，不阻塞 UI 跳转
      // eslint-disable-next-line no-restricted-syntax
      fetch(`${API_BASE_URL}/api/v1/auth/logout`, {
        method: 'POST',
        credentials: 'include',
      }).catch(() => {});
    } finally {
      // 清除 auth-token cookie（middleware 路由守卫使用）
      const secure = location.protocol === 'https:' ? '; Secure' : '';
      document.cookie = `auth-token=; path=/; max-age=0; SameSite=Lax${secure}`;
      logout();
    }
  }

  // 修改login方法
  static async login(
    username: string,
    password: string,
    tenantCode?: string,
    rememberMe?: boolean
  ): Promise<boolean> {
    try {
      const data = await this.makeRequest<{
        accessToken: string;
        refreshToken: string;
        user: unknown;
        tenant?: unknown;
      }>('/api/v1/auth/login', {
        method: 'POST',
        body: JSON.stringify({
          username,
          password,
          tenantCode: tenantCode,
        }),
      });

      // Token 仅通过 httpOnly cookie 管理（由后端设置）
      // 前端仅设置 auth-token cookie 供 middleware 路由守卫使用
      // 先确认服务端确实接受了这次会话，再写入前端登录态。
      // 否则一旦 cookie 未被浏览器保存，router.push 会被 middleware 静默 307 回 /login，
      // 用户看到的就是「接口已成功响应，但页面没有更新」。
      if (!(await this.confirmSession())) {
        const fatal = new Error('登录成功但会话未生效（浏览器可能未保存 Cookie），请重试');
        (fatal as Error & { fatal?: boolean }).fatal = true;
        throw fatal;
      }

      // 标记位与 store 都是前端登录态信号，必须一起写入；
      // 不再以 data.user 是否存在为条件（后端只保证返回 user 字段，
      // 一旦缺失，标记位漏写会让 AuthGuard 误判为未登录）。
      if (typeof window !== 'undefined') {
        const cookieMaxAge = rememberMe ? `; max-age=${7 * 24 * 60 * 60}` : '';
        const secure = location.protocol === 'https:' ? '; Secure' : '';
        // 仅写入 auth-token 标记位供 middleware 路由守卫使用，不写真值 token
		document.cookie = `auth-token=1; path=/; SameSite=Lax${cookieMaxAge}${secure}`;
      }

      // 使用store管理登录状态
      const { login } = useAuthStore.getState();
      const u = data.user as any;
      const t = data.tenant as any;
      login(
        {
          id: Number(u?.id || 0),
          username: String(u?.username || username),
          role: String(u?.role || 'end_user'),
          email: String(u?.email || ''),
          name: String(u?.name || u?.fullName || ''),
          tenantId: u?.tenantId
            ? Number(u.tenantId)
            : u?.tenantId
              ? Number(u.tenantId)
              : undefined,
          department: u?.department,
          permissions: u?.permissions,
          // IP-P1-5：首登强制改密标志（登录下发；改密成功后清除）
          mustChangePassword: Boolean(u?.mustChangePassword),
          createdAt: u?.createdAt || u?.createdAt,
          updatedAt: u?.updatedAt || u?.updatedAt,
        },
		'authenticated',
        {
          id: Number(t?.id || u?.tenantId || 1),
          name: String(t?.name || '默认租户'),
          code: String(t?.code || tenantCode || 'default'),
          type: (t?.type || 'standard') as any,
          status: (t?.status || 'active') as any,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        } as Tenant
      );

      return true;
    } catch (error) {
      // P0-2（2026-09-06 UAT 修复）：限流响应 data.retryAfterSeconds 由 makeRequest
      // 附加到 Error 上，LoginForm 据此展示按钮倒计时。仅对限流错误 rethrow，
      // 其他登录失败（凭证错误、网络错误）按调用方契约返回 false。
      const e = error as Error & { retryAfterSeconds?: number; fatal?: boolean };
      // 会话确认失败是致命错误：必须把原因抛给登录页展示，
      // 而不是静默返回 false（静默会让页面停在原地、没有任何反馈）。
      if (e.fatal) {
        throw e;
      }
      if (typeof e.retryAfterSeconds === 'number' && e.retryAfterSeconds > 0) {
        throw e;
      }
      return false;
    }
  }

  // 注册
  static async register(params: {
    username: string;
    email: string;
    password: string;
    fullName: string;
    phone?: string;
    company?: string;
    role?: string;
  }): Promise<boolean> {
    try {
      await this.makeRequest<{ id: number; username: string; email: string; message: string }>(
        '/api/v1/auth/register',
        {
          method: 'POST',
          body: JSON.stringify({
            username: params.username,
            email: params.email,
            password: params.password,
            fullName: params.fullName,
            phone: params.phone,
            company: params.company,
            role: params.role,
          }),
        }
      );

      return true;
    } catch (error) {
      console.error('Registration failed:', error);
      return false;
    }
  }

  // 发送密码重置邮件
  static async forgotPassword(email: string, tenantCode?: string): Promise<boolean> {
    try {
      await this.makeRequest<{ message: string }>('/api/v1/auth/forgot-password', {
        method: 'POST',
        body: JSON.stringify({
          email,
          tenantCode: tenantCode,
        }),
      });

      return true;
    } catch (error) {
      console.error('Forgot password request failed:', error);
      return false;
    }
  }

  // 重置密码
  static async resetPassword(params: {
    token: string;
    email: string;
    password: string;
    passwordConfirm: string;
  }): Promise<boolean> {
    try {
      await this.makeRequest<{ message: string }>('/api/v1/auth/reset-password', {
        method: 'POST',
        body: JSON.stringify({
          token: params.token,
          email: params.email,
          password: params.password,
          passwordConfirm: params.passwordConfirm,
        }),
      });

      return true;
    } catch (error) {
      console.error('Reset password failed:', error);
      return false;
    }
  }

  // 验证重置令牌
  static async validateResetToken(token: string, email: string): Promise<boolean> {
    try {
      const result = await this.makeRequest<{ valid: boolean; email: string }>(
        '/api/v1/auth/validate-reset-token',
        {
          method: 'POST',
          body: JSON.stringify({
            token,
            email,
          }),
        }
      );

      return result.valid;
    } catch (error) {
      console.error('Validate reset token failed:', error);
      return false;
    }
  }

  /**
   * IP-P1-5 自助改密（认证后）：旧密码校验 + 服务端密码策略；成功后后端清除
   * must_change_password 标志。
   */
  static async changePassword(params: { oldPassword: string; newPassword: string }): Promise<void> {
    await this.makeRequest<{ mustChangePassword: boolean }>('/api/v1/auth/change-password', {
      method: 'POST',
      body: JSON.stringify(params),
    });
  }

  /**
   * IP-P1-4c 邀请落地页回显（公开，GET）：最小字段 + 邮箱脱敏。
   * 失效/撤销/过期由后端错误体给出可展示原因（makeRequest 抛 Error）。
   */
  static async inspectInvitation(token: string): Promise<InvitationLandingInfo> {
    return this.makeRequest<InvitationLandingInfo>(
      `/api/v1/auth/invitations/${encodeURIComponent(token)}`,
      { method: 'GET' }
    );
  }

  /**
   * IP-P1-4c 接受邀请（公开，POST）：设置密码并完成建号/绑定 + membership。
   */
  static async acceptInvitation(params: {
    token: string;
    password: string;
    name?: string;
  }): Promise<{ userId: number; username: string }> {
    return this.makeRequest<{ userId: number; username: string }>(
      `/api/v1/auth/invitations/${encodeURIComponent(params.token)}/accept`,
      {
        method: 'POST',
        body: JSON.stringify({ password: params.password, name: params.name }),
      }
    );
  }
}

/** 邀请落地页回显契约（service.InvitationInfo）。 */
export interface InvitationLandingInfo {
  status: 'pending' | 'accepted' | 'revoked' | 'expired' | string;
  emailMasked: string;
  tenantName: string;
  roleCode: string;
  mspRole?: string;
  expiresAt: string;
  hasTargetUser: boolean;
}

export default AuthService;
