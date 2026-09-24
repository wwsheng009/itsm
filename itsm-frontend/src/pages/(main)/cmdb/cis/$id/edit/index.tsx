import { useNavigate, useParams } from 'react-router';

import React, { useCallback, useEffect, useMemo, useRef } from 'react';
import { App, Card, Form } from 'antd';

import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';
import { CIEditorForm } from '@/components/cmdb/CIEditorForm';
import { useUnsavedChangesGuard } from '@/components/cmdb/useUnsavedChangesGuard';
import type { CIFormValues, SchemaField } from '@/components/cmdb/ci-editor-shared';
import {
  compactRecord,
  extractCloudDataList,
  normalizeSchemaFields,
  resolveEffectiveTypeSchemaFields,
} from '@/components/cmdb/ci-editor-shared';
import { ManagementNotice, ManagementPageHeader } from '@/components/ui/ManagementPageHeader';
import {
  useCIQuery,
  useCITypesQuery,
  useCloudResourcesQuery,
  useCloudServicesQuery,
  useUpdateCIMutation,
} from '@/lib/hooks/useCMDB';
import type { CIType, CloudResource, CloudService, ConfigurationItem } from '@/types/biz/cmdb';
import { AttachmentApi, cmdbCiAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import { htmlToPlainText, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import { useInlineImageUnbind } from '@/lib/rich-text/useInlineImageUnbind';

const DESCRIPTION_MAX_LENGTH = 20000;

const EditCIPage: React.FC = () => {
  const navigate = useNavigate();
  const { id } = useParams() as { id: string };
  const { message } = App.useApp();
  const [form] = Form.useForm<CIFormValues>();

  // React Query：CI 类型 / 云资源 / 云服务 / 当前 CI（缓存 + 自动重试）
  const typesQuery = useCITypesQuery();
  const cloudResourcesQuery = useCloudResourcesQuery();
  const cloudServicesQuery = useCloudServicesQuery();
  const ciQuery = useCIQuery(id);

  const types: CIType[] = (typesQuery.data as unknown as CIType[]) ?? [];
  const cloudResources: CloudResource[] =
    extractCloudDataList<CloudResource>(cloudResourcesQuery.data) ?? [];
  const cloudServices: CloudService[] =
    extractCloudDataList<CloudService>(cloudServicesQuery.data) ?? [];
  const ci = (ciQuery.data as unknown as ConfigurationItem) ?? null;

  const typesLoading = typesQuery.isLoading;
  const cloudLoading = cloudResourcesQuery.isLoading || cloudServicesQuery.isLoading;
  const loading = ciQuery.isLoading;

  // 错误提示
  useEffect(() => {
    if (typesQuery.isError) message.error('加载资产类型失败');
  }, [typesQuery.isError, message]);
  useEffect(() => {
    if (cloudResourcesQuery.isError && cloudServicesQuery.isError)
      message.error('加载云资源数据失败');
  }, [cloudResourcesQuery.isError, cloudServicesQuery.isError, message]);
  useEffect(() => {
    if (ciQuery.isError) message.error('加载配置项失败');
  }, [ciQuery.isError, message]);

  const schemaFieldsStateRef = useRef<SchemaField[]>([]);
  const typeSchemaFieldsStateRef = useRef<SchemaField[]>([]);
  const [schemaFields, setSchemaFields] = React.useState<SchemaField[]>([]);
  const [typeSchemaFields, setTypeSchemaFields] = React.useState<SchemaField[]>([]);
  // 维持原 ref 行为以便引用一致
  schemaFieldsStateRef.current = schemaFields;
  typeSchemaFieldsStateRef.current = typeSchemaFields;

  const { markDirty, clearDirty, handleCancel } = useUnsavedChangesGuard(navigate);

  const richTextEnabled = isRichTextEnabled();
  const ciId = Number(id);
  // 编辑态正文内嵌图片解绑：加载时记基线，保存成功后 diff 出被移除的图片（失败仅告警）
  const { captureInlineImageBaseline, unbindRemovedInlineImages } = useInlineImageUnbind();

  // 编辑态已持有 ciId：图片直接上传附件，编辑器内回填的就是正式地址
  const handleEditorImageUpload = useCallback(
    async (file: File): Promise<UploadedImage> => {
      if (!Number.isFinite(ciId) || ciId <= 0) {
        throw new Error('配置项 ID 无效，无法上传图片');
      }
      const uploaded = await AttachmentApi.upload(file, {
        bizType: 'cmdb_ci',
        bizId: ciId,
        usage: 'inline_image',
      });
      return {
        url: uploaded.previewUrl || cmdbCiAttachmentPreviewUrl(ciId, uploaded.id),
        id: uploaded.id,
        name: uploaded.fileName || file.name,
      };
    },
    [ciId]
  );

  const cloudServiceMap = useMemo(
    () => new Map(cloudServices.map(service => [service.id, service])),
    [cloudServices]
  );

  const initializedRef = useRef(false);

  const omitSchemaFieldValues = (
    attributes: Record<string, unknown> | undefined,
    fields: SchemaField[]
  ) => {
    if (!attributes) return undefined;
    const schemaKeys = new Set(fields.map(field => field.key));
    const entries = Object.entries(attributes).filter(([key]) => !schemaKeys.has(key));
    return entries.length > 0 ? Object.fromEntries(entries) : undefined;
  };

  useEffect(() => {
    if (!ci || typesLoading || initializedRef.current) return;
    const ciTypeId = ci.ciTypeId ?? 0;
    const initialTypeSchemaFields = resolveEffectiveTypeSchemaFields(types, ciTypeId);
    const attributeRecord =
      ci.attributes && typeof ci.attributes === 'object'
        ? (ci.attributes as Record<string, unknown>)
        : undefined;
    const remainingAttributes = omitSchemaFieldValues(attributeRecord, initialTypeSchemaFields);

    const initialValues: Partial<CIFormValues> = {
      name: ci.name,
      ciTypeId: ciTypeId,
      status: ci.status,
      description: ci.description,
      serialNumber: ci.serialNumber,
      model: ci.model,
      vendor: ci.vendor,
      location: ci.location,
      assetTag: ci.assetTag,
      assignedTo: ci.assignedTo,
      ownedBy: ci.ownedBy,
      environment: ci.environment,
      criticality: ci.criticality,
      discoverySource: ci.discoverySource,
      source: ci.source,
      cloudProvider: ci.cloudProvider,
      cloudAccountId: ci.cloudAccountId ? String(ci.cloudAccountId) : undefined,
      cloudRegion: ci.cloudRegion,
      cloudZone: ci.cloudZone,
      cloudResourceId: ci.cloudResourceId,
      cloudResourceType: ci.cloudResourceType,
      cloudSyncStatus: ci.cloudSyncStatus,
      cloudResourceRefId: ci.cloudResourceRefId,
      cloudMetadata: ci.cloudMetadata as Record<string, {} | undefined> | undefined,
      customAttributes: attributeRecord as Record<string, {} | undefined> | undefined,
    };
    if (remainingAttributes) {
      initialValues.attributes = JSON.stringify(remainingAttributes, null, 2);
    }
    form.setFieldsValue(initialValues);
    setTypeSchemaFields(initialTypeSchemaFields);
    captureInlineImageBaseline(ci.description);

    initializedRef.current = true;
  }, [ci, form, types, typesLoading, captureInlineImageBaseline]);

  useEffect(() => {
    const cloudResourceRefId = ci?.cloudResourceRefId ?? ci?.cloudResourceRefId;
    if (!cloudResourceRefId || !cloudResources.length || !cloudServices.length) return;
    const resource = cloudResources.find(item => item.id === cloudResourceRefId);
    const service = resource ? cloudServiceMap.get(resource.serviceId) : undefined;
    setSchemaFields(normalizeSchemaFields(service?.attributeSchema));
  }, [ci, cloudResources, cloudServices, cloudServiceMap]);

  const handleCloudResourceChange = (value?: number) => {
    if (!value) {
      setSchemaFields([]);
      return;
    }
    const resource = cloudResources.find(item => item.id === value);
    const service = resource ? cloudServiceMap.get(resource.serviceId) : undefined;
    setSchemaFields(normalizeSchemaFields(service?.attributeSchema));
    if (!resource) return;
    form.setFieldsValue({
      cloudResourceId: resource.resourceId,
      cloudRegion: resource.region,
      cloudZone: resource.zone,
      cloudAccountId: String(resource.cloudAccountId),
      cloudProvider: service?.provider,
      cloudResourceType: service?.resourceTypeCode,
    });
  };

  const handleCITypeChange = (value?: number) => {
    setTypeSchemaFields(resolveEffectiveTypeSchemaFields(types, value));
    form.setFieldValue('customAttributes', undefined);
  };

  const updateMutation = useUpdateCIMutation();

  const handleSubmit = async (values: CIFormValues) => {
    const rawDescription = typeof values.description === 'string' ? values.description : '';
    // 富文本按纯文本口径校验长度，与后端 DTO 上限 20000 对齐
    if (
      richTextEnabled &&
      htmlToPlainText(rawDescription, Number.MAX_SAFE_INTEGER).length > DESCRIPTION_MAX_LENGTH
    ) {
      message.warning('配置项描述最多 20000 字，请精简后再提交');
      return;
    }

    let attributes: Record<string, unknown> | undefined;
    if (values.attributes) {
      try {
        attributes =
          typeof values.attributes === 'string'
            ? JSON.parse(values.attributes)
            : values.attributes;
      } catch {
        message.error('扩展属性需要是有效的 JSON');
        return;
      }
    }
    const customAttributes = compactRecord(
      values.customAttributes as Record<string, unknown> | undefined
    );
    attributes = {
      ...(attributes || {}),
      ...(customAttributes || {}),
    };
    if (Object.keys(attributes).length === 0) {
      attributes = undefined;
    }
    try {
      await updateMutation.mutateAsync({
        id,
        data: {
          name: values.name,
          ciTypeId: values.ciTypeId,
          status: values.status,
          description: values.description,
          attributes,
          serialNumber: values.serialNumber,
          model: values.model,
          vendor: values.vendor,
          location: values.location,
          assetTag: values.assetTag,
          assignedTo: values.assignedTo,
          ownedBy: values.ownedBy,
          environment: values.environment,
          criticality: values.criticality,
          discoverySource: values.discoverySource,
          source: values.source,
          cloudProvider: values.cloudProvider,
          cloudAccountId: values.cloudAccountId,
          cloudRegion: values.cloudRegion,
          cloudZone: values.cloudZone,
          cloudResourceId: values.cloudResourceId,
          cloudResourceType: values.cloudResourceType,
          cloudSyncStatus: values.cloudSyncStatus,
          cloudResourceRefId: values.cloudResourceRefId,
          cloudMetadata: values.cloudMetadata,
        },
      });
      // mutation onSuccess 已展示 '配置项已更新'
      // 编辑器内被删除的图片：调用附件解绑接口（域内别名路由沿用 ci:delete，
      // 幂等、失败不阻断保存结果）。
      if (richTextEnabled) {
        await unbindRemovedInlineImages({ bizType: 'cmdb_ci', bizId: ciId }, rawDescription);
      }
      clearDirty();
      navigate(`/cmdb/cis/${id}`);
    } catch (error) {
      if (error instanceof Error) {
        message.error(error.message || '更新配置项失败');
      } else {
        message.error('更新配置项失败');
      }
    }
  };

  return (
    <div className='space-y-6'>
      <ManagementPageHeader
        title='编辑配置项'
        description='修改这个配置项的基础信息、云资源关联和扩展属性。'
        notice={
          <ManagementNotice
            message='编辑时会保留原有云资源映射'
            description='如果更换云资源，动态属性字段会重新加载，请确认扩展属性是否仍然适用。'
          />
        }
      />

      <Card className='rounded-xl shadow-sm' loading={loading}>
        <CIEditorForm
          form={form}
          types={types}
          typesLoading={typesLoading}
          cloudResources={cloudResources}
          cloudServices={cloudServices}
          cloudLoading={cloudLoading}
          schemaFields={schemaFields}
          typeSchemaFields={typeSchemaFields}
          saving={updateMutation.isPending}
          submitText='保存修改'
          onSubmit={handleSubmit}
          onCancel={handleCancel}
          onCITypeChange={handleCITypeChange}
          onCloudResourceChange={handleCloudResourceChange}
          onDescriptionImageUpload={handleEditorImageUpload}
          onValuesChange={() => {
            if (initializedRef.current) markDirty();
          }}
        />
      </Card>
    </div>
  );
};

export default EditCIPage;
