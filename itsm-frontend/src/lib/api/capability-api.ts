import { httpClient } from './http-client';

export type CapabilityMaturity = 'ga' | 'pilot' | 'disabled';

export interface ProductCapabilityState {
  key: string;
  maturity: CapabilityMaturity;
  buildAvailable: boolean;
  deploymentReady: boolean;
  tenantReady: boolean;
  allowedActions: string[];
  dependencies: string[];
  degradedReason?: string;
  lastHealthCheckAt?: string;
  acceptanceVersion: string;
}

export interface CapabilityResponse {
  items: ProductCapabilityState[];
  acceptanceVersion: string;
  /** 部署模式（后端 DEPLOYMENT_MODE：private|saas|saas_msp；未装配时为空） */
  deploymentMode?: string;
  /** /api/v1/msp/* 路由族是否开放（仅 saas_msp 为 true） */
  mspRoutesEnabled?: boolean;
}

export const getCapabilities = (): Promise<CapabilityResponse> =>
  httpClient.get<CapabilityResponse>('/api/v1/capabilities');

export const capabilityAllows = (
  capabilities: ProductCapabilityState[],
  key: string,
  action = 'read'
): boolean => {
  const capability = capabilities.find(item => item.key === key);
  return Boolean(
    capability &&
      capability.maturity !== 'disabled' &&
      capability.buildAvailable &&
      capability.deploymentReady &&
      capability.tenantReady &&
      capability.allowedActions.includes(action)
  );
};
