
import React from 'react';
import { Card, Row, Col, Input, Select, Button, Tooltip } from 'antd';
import { RefreshCw, PlusCircle } from 'lucide-react';
import { useI18n } from '@/lib/i18n';
import type { CIType, CloudService } from '@/types/biz/cmdb';

const { Search: SearchInput } = Input;

interface ServiceCatalogFiltersProps {
  onSearch: (value: string) => void;
  onCategoryFilterChange: (value: string) => void;
  onCITypeFilterChange: (value: number) => void;
  onCloudServiceFilterChange: (value: number) => void;
  ciTypes: CIType[];
  cloudServices: CloudService[];
  optionsLoading?: boolean;
  /**
   * 是否渲染 CMDB 依赖筛选项（CI 类型 / 云服务）。
   * 这两项数据来自 /api/v1/cmdb/*（需 cmdb:read）；无权限的用户（如 end_user）
   * 不请求也不渲染，避免「空下拉」与误导性的加载失败提示。默认 true 保持兼容。
   */
  canReadCmdb?: boolean;
  onCreateService?: () => void;
  onRefresh: () => void;
}

export const ServiceCatalogFilters: React.FC<ServiceCatalogFiltersProps> = ({
  onSearch,
  onCategoryFilterChange,
  onCITypeFilterChange,
  onCloudServiceFilterChange,
  ciTypes,
  cloudServices,
  optionsLoading,
  canReadCmdb = true,
  onCreateService,
  onRefresh,
}) => {
  const { t } = useI18n();
  return (
    <Card style={{ marginBottom: 24 }}>
      <Row gutter={[12, 12]} align="middle">
        <Col xs={24} sm={12} md={6}>
          <SearchInput
            placeholder={t('serviceCatalog.searchPlaceholder')}
            allowClear
            onSearch={onSearch}
            size="large"
            enterButton
            aria-label="搜索服务目录"
          />
        </Col>
        <Col xs={24} sm={12} md={4}>
          <Select
            placeholder={t('serviceCatalog.categoryFilter')}
            size="large"
            allowClear
            onChange={onCategoryFilterChange}
            style={{ width: '100%' }}
            options={[
              { value: '云计算', label: t('serviceCatalog.cloudResources') },
              { value: '支持', label: t('serviceCatalog.accountPermissions') },
              { value: '安全', label: t('serviceCatalog.securityServices') },
            ]}
          />
        </Col>
        {canReadCmdb && (
          <>
            <Col xs={24} sm={12} md={3}>
              <Select
                placeholder="CI类型"
                size="large"
                allowClear
                loading={optionsLoading}
                onChange={onCITypeFilterChange}
                style={{ width: '100%' }}
                options={ciTypes.map(type => ({ value: type.id, label: type.name }))}
              />
            </Col>
            <Col xs={24} sm={12} md={4}>
              <Select
                placeholder="云服务"
                size="large"
                allowClear
                loading={optionsLoading}
                showSearch
                optionFilterProp="label"
                onChange={onCloudServiceFilterChange}
                style={{ width: '100%' }}
                options={cloudServices.map(service => ({
                  value: service.id,
                  label: `${service.serviceName} (${service.resourceTypeName})`,
                }))}
              />
            </Col>
          </>
        )}
        {/* 刷新为只读操作：恒显示，不随创建权限隐藏（权限门禁只管写操作）。 */}
        <Col xs={24} sm={12} md={2}>
          <Tooltip title="刷新列表">
            <Button
              icon={<RefreshCw size={20} />}
              onClick={onRefresh}
              aria-label="刷新服务目录"
              size="large"
              style={{ width: '100%' }}
            >
              {t('serviceCatalog.refresh')}
            </Button>
          </Tooltip>
        </Col>
        {/* 创建服务需 service_catalog:write：无权限不渲染，避免「可见但点击无反应」的死按钮
            （与页面空态「请联系服务目录管理员」及仓库内 canManageXxx && <Button/> 惯例同口径）。 */}
        {onCreateService && (
          <Col xs={24} sm={12} md={2}>
            <Button
              type="primary"
              icon={<PlusCircle size={20} />}
              size="large"
              style={{ width: '100%' }}
              onClick={onCreateService}
            >
              {t('serviceCatalog.newService')}
            </Button>
          </Col>
        )}
      </Row>
    </Card>
  );
};
