import { httpClient } from './http-client';

// 邀请管理侧 API（IP-P1-4c）：创建 / 列表 / 撤销。
//
// 接受侧（公开回显 + 设密接受）在 `lib/services/auth-service.ts`（`/api/v1/auth/invitations/*`）；
// 本文件仅覆盖认证后的管理面（`/api/v1/users/invitations*`，后端 user:write）。

export type InvitationStatus = 'pending' | 'accepted' | 'revoked' | 'expired';

export interface InvitationListItem {
  id: number;
  tenantId: number;
  email: string;
  roleId: number;
  roleCode: string;
  roleName?: string;
  mspRole?: string;
  status: InvitationStatus;
  invitedBy: number;
  inviterName?: string;
  targetUserId?: number;
  expiresAt: string;
  createdAt: string;
  acceptedAt?: string;
  revokedAt?: string;
}

export interface InvitationListParams {
  /** 目标租户；省略时后端取当前会话租户 */
  tenantId?: number;
  status?: InvitationStatus | '';
  limit?: number;
  offset?: number;
}

export interface InvitationListResponse {
  invitations: InvitationListItem[];
  total: number;
  limit: number;
  offset: number;
}

export interface CreateInvitationPayload {
  /** 目标租户；省略时后端取当前会话租户 */
  tenantId?: number;
  email: string;
  roleId: number;
  /** 服务方角色白名单（provider_admin/provider_agent），仅平台/MSP/provider 本租户通道允许 */
  mspRole?: string;
  /** 可选：绑定已存在账号（users.id） */
  targetUserId?: number;
}

export interface CreateInvitationResponse {
  id: number;
  email: string;
  status: InvitationStatus;
  expiresAt: string;
  /** SMTP 未配置时（emailSent=false）为唯一投递通道：复制给被邀请人 */
  inviteUrl: string;
  emailSent: boolean;
}

export interface RevokeInvitationResponse {
  id: number;
  status: InvitationStatus;
  revokedAt?: string;
}

export class InvitationAPI {
  /** 邀请列表（认证 + user:write）：支持 status 过滤与 limit/offset 分页。 */
  static async listInvitations(params: InvitationListParams = {}): Promise<InvitationListResponse> {
    const query: Record<string, string | number> = {};
    if (params.tenantId) query.tenantId = params.tenantId;
    if (params.status) query.status = params.status;
    if (params.limit) query.limit = params.limit;
    if (params.offset) query.offset = params.offset;
    return httpClient.get<InvitationListResponse>('/api/v1/users/invitations', query);
  }

  /** 创建邀请：返回 inviteUrl（SMTP 未配置时线下传递）。 */
  static async createInvitation(payload: CreateInvitationPayload): Promise<CreateInvitationResponse> {
    return httpClient.post<CreateInvitationResponse>('/api/v1/users/invitations', payload);
  }

  /** 撤销邀请（仅 pending 生效）。 */
  static async revokeInvitation(id: number): Promise<RevokeInvitationResponse> {
    return httpClient.post<RevokeInvitationResponse>(`/api/v1/users/invitations/${id}/revoke`, {});
  }
}

export default InvitationAPI;
