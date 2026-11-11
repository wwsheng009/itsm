/**
 * 238 演示环境账号矩阵（与 scripts/demo/seed_demo_dataset.py / demo-environment.md 对齐）。
 *
 * 口令：
 * - 平台管理员：DEMO_ADMIN_PASSWORD，默认 Demo@2026!Admin
 * - 租户账号：DEMO_USER_PASSWORD，默认 Demo@2026!
 *
 * 注意：这些是演示环境公开口令，不得用于真实环境。
 */
export type DemoRoleKey =
  | 'admin'
  | 'mspManager'
  | 'mspAgent1'
  | 'mspAgent2'
  | 'mfgAdmin'
  | 'mfgAgent'
  | 'mfgLead'
  | 'mfgUser'
  | 'mfgItAdmin'
  | 'mfgSecAdmin'
  | 'medAdmin'
  | 'medAgent'
  | 'medLead'
  | 'medUser'
  | 'medItAdmin'
  | 'medSecAdmin'
  | 'eduAdmin'
  | 'eduAgent'
  | 'eduLead'
  | 'eduUser'
  | 'eduItAdmin'
  | 'eduSecAdmin';

export interface DemoAccount {
  key: DemoRoleKey;
  username: string;
  password: string;
  expectedRole: string;
  expectedMspRole?: string;
  tenantCode: string;
  label: string;
}

const adminPassword = process.env.DEMO_ADMIN_PASSWORD || 'Demo@2026!Admin';
const userPassword = process.env.DEMO_USER_PASSWORD || 'Demo@2026!';

export const DEMO_ACCOUNTS: Record<DemoRoleKey, DemoAccount> = {
  admin: {
    key: 'admin',
    username: process.env.E2E_ADMIN_USERNAME || 'admin',
    password: adminPassword,
    expectedRole: 'super_admin',
    tenantCode: 'default',
    label: '平台管理员',
  },
  mspManager: {
    key: 'mspManager',
    username: 'demo.msp.manager',
    password: userPassword,
    expectedRole: 'msp_manager',
    expectedMspRole: 'provider_admin',
    tenantCode: 'demo-provider',
    label: '服务商经理',
  },
  mspAgent1: {
    key: 'mspAgent1',
    username: 'demo.msp.agent1',
    password: userPassword,
    expectedRole: 'msp_tech',
    expectedMspRole: 'provider_agent',
    tenantCode: 'demo-provider',
    label: '服务商一线工程师',
  },
  mspAgent2: {
    key: 'mspAgent2',
    username: 'demo.msp.agent2',
    password: userPassword,
    expectedRole: 'msp_tech',
    expectedMspRole: 'provider_agent',
    tenantCode: 'demo-provider',
    label: '服务商二线工程师',
  },
  mfgAdmin: {
    key: 'mfgAdmin',
    username: 'demo.mfg.admin',
    password: userPassword,
    expectedRole: 'admin',
    tenantCode: 'demo-mfg',
    label: '制造-管理员',
  },
  mfgAgent: {
    key: 'mfgAgent',
    username: 'demo.mfg.agent',
    password: userPassword,
    expectedRole: 'agent',
    tenantCode: 'demo-mfg',
    label: '制造-服务台',
  },
  mfgLead: {
    key: 'mfgLead',
    username: 'demo.mfg.lead',
    password: userPassword,
    expectedRole: 'manager',
    tenantCode: 'demo-mfg',
    label: '制造-部门经理',
  },
  mfgUser: {
    key: 'mfgUser',
    username: 'demo.mfg.user',
    password: userPassword,
    expectedRole: 'end_user',
    tenantCode: 'demo-mfg',
    label: '制造-终端用户',
  },
  mfgItAdmin: {
    key: 'mfgItAdmin',
    username: 'demo.mfg.itadmin',
    password: userPassword,
    expectedRole: 'it_admin',
    tenantCode: 'demo-mfg',
    label: '制造-IT 主管',
  },
  mfgSecAdmin: {
    key: 'mfgSecAdmin',
    username: 'demo.mfg.secadmin',
    password: userPassword,
    expectedRole: 'security_admin',
    tenantCode: 'demo-mfg',
    label: '制造-安全管理员',
  },
  medAdmin: {
    key: 'medAdmin',
    username: 'demo.med.admin',
    password: userPassword,
    expectedRole: 'admin',
    tenantCode: 'demo-med',
    label: '医疗-管理员',
  },
  medAgent: {
    key: 'medAgent',
    username: 'demo.med.agent',
    password: userPassword,
    expectedRole: 'agent',
    tenantCode: 'demo-med',
    label: '医疗-服务台',
  },
  medLead: {
    key: 'medLead',
    username: 'demo.med.lead',
    password: userPassword,
    expectedRole: 'manager',
    tenantCode: 'demo-med',
    label: '医疗-科室主任',
  },
  medUser: {
    key: 'medUser',
    username: 'demo.med.user',
    password: userPassword,
    expectedRole: 'end_user',
    tenantCode: 'demo-med',
    label: '医疗-医生',
  },
  medItAdmin: {
    key: 'medItAdmin',
    username: 'demo.med.itadmin',
    password: userPassword,
    expectedRole: 'it_admin',
    tenantCode: 'demo-med',
    label: '医疗-IT 主管',
  },
  medSecAdmin: {
    key: 'medSecAdmin',
    username: 'demo.med.secadmin',
    password: userPassword,
    expectedRole: 'security_admin',
    tenantCode: 'demo-med',
    label: '医疗-安全管理员',
  },
  eduAdmin: {
    key: 'eduAdmin',
    username: 'demo.edu.admin',
    password: userPassword,
    expectedRole: 'admin',
    tenantCode: 'demo-edu',
    label: '教育-管理员',
  },
  eduAgent: {
    key: 'eduAgent',
    username: 'demo.edu.agent',
    password: userPassword,
    expectedRole: 'agent',
    tenantCode: 'demo-edu',
    label: '教育-服务台',
  },
  eduLead: {
    key: 'eduLead',
    username: 'demo.edu.lead',
    password: userPassword,
    expectedRole: 'manager',
    tenantCode: 'demo-edu',
    label: '教育-教务主任',
  },
  eduUser: {
    key: 'eduUser',
    username: 'demo.edu.user',
    password: userPassword,
    expectedRole: 'end_user',
    tenantCode: 'demo-edu',
    label: '教育-教师',
  },
  eduItAdmin: {
    key: 'eduItAdmin',
    username: 'demo.edu.itadmin',
    password: userPassword,
    expectedRole: 'it_admin',
    tenantCode: 'demo-edu',
    label: '教育-IT 主管',
  },
  eduSecAdmin: {
    key: 'eduSecAdmin',
    username: 'demo.edu.secadmin',
    password: userPassword,
    expectedRole: 'security_admin',
    tenantCode: 'demo-edu',
    label: '教育-安全管理员',
  },
};

export function account(key: DemoRoleKey): DemoAccount {
  return DEMO_ACCOUNTS[key];
}

/** 高频使用的代表账号组合。 */
export const MSP_ACCOUNTS = ['mspManager', 'mspAgent1', 'mspAgent2'] as const;
export const CUSTOMER_ROLE_KEYS = ['admin', 'agent', 'lead', 'user', 'itadmin', 'secadmin'] as const;