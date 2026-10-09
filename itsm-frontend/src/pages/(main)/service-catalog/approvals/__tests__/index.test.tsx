/**
 * 待审批页：加载失败分类测试。
 *
 * 背景：后端「作用域内无审批人身份」已收敛为 200 空集合（见
 * itsm-backend/handlers/service_request/service.go ListPendingApprovals）；
 * 残余 403 仅来自 RBAC 拒绝（无 service_request:read）。页面据此在页内给出可读提示，
 * 不再弹「获取失败」。全页 UI 用例依赖 antd Table/App context 较重，这里按仓库既有
 * 做法（参见 admin/tenants/__tests__）确定性验证页面同一套分类实现。
 */
import { isApprovalsForbidden } from '..';

describe('ServiceApprovals · 加载失败分类', () => {
  it('HTTP 403 → 视为无审批权限', () => {
    expect(isApprovalsForbidden({ httpStatus: 403 })).toBe(true);
  });

  it('业务码 2003（ForbiddenCode）→ 视为无审批权限', () => {
    expect(isApprovalsForbidden({ code: 2003 })).toBe(true);
  });

  it('其他错误 → 保留「获取失败」toast', () => {
    expect(isApprovalsForbidden({ httpStatus: 500 })).toBe(false);
    expect(isApprovalsForbidden({ httpStatus: 401 })).toBe(false);
    expect(isApprovalsForbidden({ code: 1001 })).toBe(false);
    expect(isApprovalsForbidden(new Error('Network Error'))).toBe(false);
  });

  it('空值/非对象 → 安全返回 false', () => {
    expect(isApprovalsForbidden(null)).toBe(false);
    expect(isApprovalsForbidden(undefined)).toBe(false);
    expect(isApprovalsForbidden('403')).toBe(false);
  });
});
