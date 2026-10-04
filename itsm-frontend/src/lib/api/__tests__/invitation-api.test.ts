/**
 * InvitationAPI 契约测试（IP-P1-4c 管理侧）。
 *
 * 锁住三类调用的 URL / 载荷形态，防止后端契约（§4.0-C）被无意改断：
 * - GET  /api/v1/users/invitations（可选 tenantId/status/limit/offset）
 * - POST /api/v1/users/invitations
 * - POST /api/v1/users/invitations/:id/revoke
 */
import { InvitationAPI } from '@/lib/api/invitation-api';
import { httpClient } from '@/lib/api/http-client';

jest.mock('@/lib/api/http-client', () => ({
  httpClient: {
    get: jest.fn(),
    post: jest.fn(),
  },
}));

const mockGet = httpClient.get as jest.Mock;
const mockPost = httpClient.post as jest.Mock;

describe('InvitationAPI', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('listInvitations', () => {
    it('透传全部查询参数', async () => {
      const expected = { invitations: [], total: 0, limit: 10, offset: 20 };
      mockGet.mockResolvedValue(expected);

      const res = await InvitationAPI.listInvitations({
        tenantId: 7,
        status: 'pending',
        limit: 10,
        offset: 20,
      });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/users/invitations', {
        tenantId: 7,
        status: 'pending',
        limit: 10,
        offset: 20,
      });
      expect(res).toEqual(expected);
    });

    it('省略空参数（默认当前租户/全状态/默认分页）', async () => {
      mockGet.mockResolvedValue({ invitations: [], total: 0, limit: 50, offset: 0 });

      await InvitationAPI.listInvitations({ status: '' });

      expect(mockGet).toHaveBeenCalledWith('/api/v1/users/invitations', {});
    });
  });

  describe('createInvitation', () => {
    it('POST 载荷含 tenantId/email/roleId/mspRole', async () => {
      const payload = { tenantId: 3, email: 'new@example.com', roleId: 5, mspRole: 'provider_agent' };
      const expected = {
        id: 12,
        email: 'new@example.com',
        status: 'pending',
        expiresAt: '2026-10-06T10:00:00Z',
        inviteUrl: 'http://localhost:5173/invite/tok',
        emailSent: false,
      };
      mockPost.mockResolvedValue(expected);

      const res = await InvitationAPI.createInvitation(payload);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/users/invitations', payload);
      expect(res).toEqual(expected);
    });
  });

  describe('revokeInvitation', () => {
    it('POST /:id/revoke 且携带空对象体', async () => {
      mockPost.mockResolvedValue({ id: 9, status: 'revoked' });

      const res = await InvitationAPI.revokeInvitation(9);

      expect(mockPost).toHaveBeenCalledWith('/api/v1/users/invitations/9/revoke', {});
      expect(res.status).toBe('revoked');
    });
  });
});
