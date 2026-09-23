/**
 * Auth Service 完整测试套件
 */

import { AuthService } from '../auth-service';

// Mock Zustand store
const mockLogin = jest.fn();
const mockLogout = jest.fn();

jest.mock('@/lib/store/auth-store', () => ({
  useAuthStore: {
    getState: jest.fn(() => ({
      user: {
        id: 1,
        username: 'testuser',
        email: 'test@example.com',
        name: 'Test User',
        role: 'agent',
        tenantId: 1,
      },
      isAuthenticated: true,
      login: mockLogin,
      logout: mockLogout,
    })),
  },
}));

// Mock fetch
const mockFetch = jest.fn();
global.fetch = mockFetch;

// Mock document.cookie
const mockCookieStore: Record<string, string> = {};
Object.defineProperty(document, 'cookie', {
  get: jest.fn(() => {
    return Object.entries(mockCookieStore)
      .map(([key, value]) => `${key}=${value}`)
      .join('; ');
  }),
  set: jest.fn((cookie: string) => {
    const [key, value] = cookie.split('=');
    if (value.includes('max-age=0')) {
      delete mockCookieStore[key.trim()];
    } else {
      mockCookieStore[key.trim()] = value.split(';')[0];
    }
  }),
});

describe('AuthService', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    // Clear cookie store
    Object.keys(mockCookieStore).forEach(key => delete mockCookieStore[key]);
  });

  describe('Token Management', () => {
    describe('setTokens', () => {
      it('should set tokens without throwing', () => {
        expect(() => AuthService.setTokens('access-token', 'refresh-token')).not.toThrow();
      });
    });

    describe('getAccessToken', () => {
      it('should return null when no token exists', () => {
        expect(AuthService.getAccessToken()).toBeNull();
      });

      it('should not expose an access token even if a test cookie is present', () => {
        mockCookieStore['access_token'] = 'test-access-token';
        expect(AuthService.getAccessToken()).toBeNull();
      });
    });

    describe('getRefreshToken', () => {
      it('should return null when no token exists', () => {
        expect(AuthService.getRefreshToken()).toBeNull();
      });

      it('should not expose a refresh token even if a test cookie is present', () => {
        mockCookieStore['refresh_token'] = 'test-refresh-token';
        expect(AuthService.getRefreshToken()).toBeNull();
      });
    });

    describe('getToken', () => {
      it('should not expose a token through the backward-compatible helper', () => {
        mockCookieStore['access_token'] = 'test-token';
        expect(AuthService.getToken()).toBeNull();
      });
    });
  });

  describe('Authentication Status', () => {
    describe('getCurrentUser', () => {
      it('should return current user from store', () => {
        const user = AuthService.getCurrentUser();
        expect(user).toBeDefined();
        expect(user?.id).toBe(1);
        expect(user?.username).toBe('testuser');
      });
    });

    describe('isAuthenticated', () => {
      it('should return true when store has authenticated status', () => {
        const result = AuthService.isAuthenticated();
        expect(result).toBe(true);
      });

      it('should return true when access token exists in cookie', () => {
        const userState = require('@/lib/store/auth-store').useAuthStore.getState();
        userState.isAuthenticated = false;
        mockCookieStore['access_token'] = 'valid-token';

        const result = AuthService.isAuthenticated();
        expect(result).toBe(true);

        // Reset
        userState.isAuthenticated = true;
      });
    });
  });

  describe('Login Functionality', () => {
    describe('login', () => {
      it('should login successfully with valid credentials', async () => {
        mockFetch.mockResolvedValueOnce({
          ok: true,
          json: () =>
            Promise.resolve({
              code: 0,
              message: 'success',
              data: {
                accessToken: 'new-access-token',
                refreshToken: 'new-refresh-token',
                user: {
                  id: 1,
                  username: 'testuser',
                  email: 'test@example.com',
                  name: 'Test User',
                  role: 'agent',
                  tenantId: 1,
                },
                tenant: {
                  id: 1,
                  name: 'Test Tenant',
                  code: 'test',
                  type: 'standard',
                  status: 'active',
                },
              },
            }),
        });

        // 登录成功后 AuthService 会再确认一次会话（GET /api/v1/auth/me），
        // 以确保 middleware 真正认可这次登录，而不是静默跳回 /login。
        mockFetch.mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ code: 0, message: 'success', data: { id: 1 } }),
        });

        const result = await AuthService.login('testuser', 'password123', 'test', true);

        expect(result).toBe(true);
        expect(mockLogin).toHaveBeenCalled();
        expect(mockFetch).toHaveBeenCalledWith(
          expect.stringContaining('/api/v1/auth/login'),
          expect.objectContaining({
            method: 'POST',
            body: expect.stringContaining('testuser'),
          })
        );
      });

      it('should return false on login failure', async () => {
        mockFetch.mockResolvedValueOnce({
          ok: false,
          json: () => Promise.resolve({ code: 1001, message: 'Invalid credentials' }),
        });

        const result = await AuthService.login('wronguser', 'wrongpass');

        expect(result).toBe(false);
      });

      // 回归：登录限流返回的是 HTTP 403 + {message, data.retryAfterSeconds}
      // （middleware/rate_limiter.go → common.FailWithData(ForbiddenCode)）。
      // 早先只解析 2xx 响应体，导致限流时前端既拿不到原因、也拿不到倒计时秒数，
      // 用户看到的是内部串 "HTTP error! status: 403"。
      it('should surface backend message and retryAfterSeconds from a non-2xx rate-limit response', async () => {
        mockFetch.mockResolvedValueOnce({
          ok: false,
          status: 403,
          json: () =>
            Promise.resolve({
              code: 2003,
              message: '登录请求过于频繁，请稍后再试',
              data: { retryAfterSeconds: 42 },
            }),
        });

        await expect(AuthService.login('admin', 'wrongpass')).rejects.toMatchObject({
          message: '登录请求过于频繁，请稍后再试',
          retryAfterSeconds: 42,
        });
      });

      it('should handle network errors gracefully', async () => {
        mockFetch.mockRejectedValueOnce(new Error('Network error'));

        const result = await AuthService.login('testuser', 'password');

        expect(result).toBe(false);
      });
    });
  });

  describe('Third Party Login', () => {
    it('should handle third party login successfully', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ token: 'test-token', user: { id: 1 } }),
      });

      await expect(AuthService.thirdPartyLogin('google', 'code123')).resolves.not.toThrow();
    });

    it('should throw error on failed third party login', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
      });

      await expect(AuthService.thirdPartyLogin('google', 'invalid')).rejects.toThrow('登录失败');
    });
  });

  describe('Registration', () => {
    it('should register user successfully', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'success',
            data: { id: 2, username: 'newuser', email: 'new@example.com' },
          }),
      });

      const result = await AuthService.register({
        username: 'newuser',
        email: 'new@example.com',
        password: 'password123',
        fullName: 'New User',
        phone: '1234567890',
        company: 'Test Company',
        role: 'user',
      });

      expect(result).toBe(true);
    });

    it('should return false on registration failure', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Registration failed'));

      const result = await AuthService.register({
        username: 'newuser',
        email: 'new@example.com',
        password: 'password123',
        fullName: 'New User',
      });

      expect(result).toBe(false);
    });
  });

  describe('Password Reset', () => {
    describe('forgotPassword', () => {
      it('should send password reset email successfully', async () => {
        mockFetch.mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ code: 0, message: 'Reset email sent' }),
        });

        const result = await AuthService.forgotPassword('test@example.com', 'test');

        expect(result).toBe(true);
      });

      it('should return false on forgot password failure', async () => {
        mockFetch.mockRejectedValueOnce(new Error('Failed to send email'));

        const result = await AuthService.forgotPassword('test@example.com');

        expect(result).toBe(false);
      });
    });

    describe('resetPassword', () => {
      it('should reset password successfully', async () => {
        mockFetch.mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ code: 0, message: 'Password reset' }),
        });

        const result = await AuthService.resetPassword({
          token: 'reset-token',
          email: 'test@example.com',
          password: 'newpassword123',
          passwordConfirm: 'newpassword123',
        });

        expect(result).toBe(true);
      });

      it('should return false on reset password failure', async () => {
        mockFetch.mockRejectedValueOnce(new Error('Invalid token'));

        const result = await AuthService.resetPassword({
          token: 'invalid-token',
          email: 'test@example.com',
          password: 'newpass',
          passwordConfirm: 'newpass',
        });

        expect(result).toBe(false);
      });
    });

    describe('validateResetToken', () => {
      it('should validate reset token successfully', async () => {
        mockFetch.mockResolvedValueOnce({
          ok: true,
          json: () =>
            Promise.resolve({
              code: 0,
              message: 'success',
              data: { valid: true, email: 'test@example.com' },
            }),
        });

        const result = await AuthService.validateResetToken('valid-token', 'test@example.com');

        expect(result).toBe(true);
      });

      it('should return false for invalid token', async () => {
        mockFetch.mockRejectedValueOnce(new Error('Invalid token'));

        const result = await AuthService.validateResetToken('invalid-token', 'test@example.com');

        expect(result).toBe(false);
      });
    });
  });

  describe('Token Refresh', () => {
    it('should refresh token successfully', async () => {
      mockCookieStore['refresh_token'] = 'valid-refresh-token';
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'success',
            data: {
              accessToken: 'new-access-token',
              refreshToken: 'new-refresh-token',
            },
          }),
      });

      const result = await AuthService.refreshToken();

      expect(result).toBe(true);
    });

    it('should return false when no refresh token exists', async () => {
      const result = await AuthService.refreshToken();

      expect(result).toBe(false);
    });

    it('should return false on refresh failure', async () => {
      mockCookieStore['refresh_token'] = 'expired-token';
      mockFetch.mockRejectedValueOnce(new Error('Token expired'));

      const result = await AuthService.refreshToken();

      expect(result).toBe(false);
      expect(mockLogout).toHaveBeenCalled();
    });
  });

  describe('Logout', () => {
    it('should logout successfully', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 0, message: 'Logged out' }),
      });

      expect(() => AuthService.logout()).not.toThrow();
      expect(mockLogout).toHaveBeenCalled();
    });

    it('should handle logout network error gracefully', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Network error'));

      expect(() => AuthService.logout()).not.toThrow();
      expect(mockLogout).toHaveBeenCalled();
    });
  });

  describe('Clear Tokens', () => {
    it('should clear tokens and logout', () => {
      expect(() => AuthService.clearTokens()).not.toThrow();
      expect(mockLogout).toHaveBeenCalled();
    });
  });

  describe('Cookie Helper', () => {
    it('getCookie should return null in non-browser environment', () => {
      // Since we're in test environment, document is mocked
      const token = AuthService['getCookie']('nonexistent');
      expect(token).toBeNull();
    });

    it('getCookie should return cookie value when exists', () => {
      mockCookieStore['test_cookie'] = 'test-value';
      const value = AuthService['getCookie']('test_cookie');
      expect(value).toBe('test-value');
    });
  });
});
