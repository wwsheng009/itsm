import { useNavigate, useSearchParams } from 'react-router';

import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { App, Card, Form, Spin } from 'antd';

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
  useCITypesQuery,
  useCreateCIMutation,
  useCloudResourcesQuery,
  useCloudServicesQuery,
} from '@/lib/hooks/useCMDB';
import type { CIType, CloudResource, CloudService } from '@/types/biz/cmdb';
import { useI18n } from '@/lib/i18n';
import { AttachmentApi, cmdbCiAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import { CMDBApi } from '@/lib/api/cmdb-api';
import { htmlToPlainText, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import {
  STAGED_ID_PREFIX,
  extractStagedImageIds,
  replaceStagedImages,
  stripStagedImages,
  type StagedImageReplacement,
} from '@/lib/rich-text/staged-images';

const DESCRIPTION_MAX_LENGTH = 20000;
const DESCRIPTION_UPLOAD_KEY = 'cmdb-ci-description-images';

const CreateCIPage: React.FC = () => {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { message } = App.useApp();
  const [form] = Form.useForm<CIFormValues>();

  // React Query：CI 类型、云资源、云服务（10 分钟缓存，自动重试/竞态）
  const typesQuery = useCITypesQuery();
  const cloudResourcesQuery = useCloudResourcesQuery();
  const cloudServicesQuery = useCloudServicesQuery();

  const types: CIType[] = (typesQuery.data as unknown as CIType[]) ?? [];
  const cloudResources: CloudResource[] =
    extractCloudDataList<CloudResource>(cloudResourcesQuery.data) ?? [];
  const cloudServices: CloudService[] =
    extractCloudDataList<CloudService>(cloudServicesQuery.data) ?? [];

  const typesLoading = typesQuery.isLoading;
  const cloudLoading = cloudResourcesQuery.isLoading || cloudServicesQuery.isLoading;

  const [schemaFields, setSchemaFields] = useState<SchemaField[]>([]);
  const [typeSchemaFields, setTypeSchemaFields] = useState<SchemaField[]>([]);
  const { markDirty, clearDirty, handleCancel } = useUnsavedChangesGuard(navigate);

  const cloudServiceMap = useMemo(
    () => new Map(cloudServices.map(service => [service.id, service])),
    [cloudServices]
  );

  useEffect(() => {
    if (!cloudResources.length || !cloudServices.length) return;
    const resourceRefId = searchParams.get('cloudResourceRefId');
    if (!resourceRefId) return;
    const parsed = Number(resourceRefId);
    if (Number.isNaN(parsed)) return;
    form.setFieldsValue({ cloudResourceRefId: parsed });
    const resource = cloudResources.find(item => item.id === parsed);
    if (!resource) return;
    const service = cloudServiceMap.get(resource.serviceId);
    setSchemaFields(normalizeSchemaFields(service?.attributeSchema));
    form.setFieldsValue({
      cloudResourceId: resource.resourceId,
      cloudRegion: resource.region,
      cloudZone: resource.zone,
      cloudAccountId: String(resource.cloudAccountId),
      cloudProvider: service?.provider,
      cloudResourceType: service?.resourceTypeCode,
    });
  }, [cloudResources, cloudServices, cloudServiceMap, form, searchParams]);

  // 单一失败提示：两个查询都 failed 才弹，避免云资源/服务其中一个暂时不可用时打扰
  useEffect(() => {
    if (
      cloudResourcesQuery.isError &&
      cloudServicesQuery.isError
    ) {
      message.error(t('cmdb.loadCloudResourcesFailed'));
    }
  }, [cloudResourcesQuery.isError, cloudServicesQuery.isError, message, t]);

  useEffect(() => {
    if (typesQuery.isError) {
      message.error(t('cmdb.loadCITypesFailed'));
    }
  }, [typesQuery.isError, message, t]);

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

  // React Query mutation：自动 invalidate + 错误提示
  const createMutation = useCreateCIMutation();

  const richTextEnabled = isRichTextEnabled();
  // 创建态拿不到 ciId：图片先在编辑器内 blob: 占位，CI 创建成功后再上传并回写描述。
  const stagedImagesRef = useRef<Map<string, File>>(new Map());
  const stagedImageSeqRef = useRef(0);

  const handleEditorImageUpload = useCallback(async (file: File): Promise<UploadedImage> => {
    stagedImageSeqRef.current += 1;
    const stagedId = `${STAGED_ID_PREFIX}${Date.now().toString(36)}-${stagedImageSeqRef.current}`;
    stagedImagesRef.current.set(stagedId, file);
    // 仅本地预览地址；提交时会先剥离，绝不会落库
    return { url: URL.createObjectURL(file), id: stagedId, name: file.name };
  }, []);

  const handleSubmit = async (values: CIFormValues) => {
    const rawDescription = typeof values.description === 'string' ? values.description : '';
    // 富文本按纯文本口径校验长度，与后端 DTO 上限 20000 对齐
    if (
      richTextEnabled &&
      htmlToPlainText(rawDescription, Number.MAX_SAFE_INTEGER).length > DESCRIPTION_MAX_LENGTH
    ) {
      message.warning(t('ciEditor.descriptionTooLong'));
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
        message.error(t('cmdb.invalidJSON'));
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

    // 提交前剥离 blob: 占位图（占位地址绝不落库），创建成功后再上传并回写正式地址
    const stagedImageIds = richTextEnabled ? extractStagedImageIds(rawDescription) : [];
    const descriptionForCreate =
      stagedImageIds.length > 0 ? stripStagedImages(rawDescription) : rawDescription;

    try {
      const created = await createMutation.mutateAsync({
        name: values.name,
        ciTypeId: Number(values.ciTypeId),
        status: values.status,
        description: descriptionForCreate,
        attributes,
        serialNumber: values.serialNumber,
        model: values.model,
        vendor: values.vendor,
        assetTag: values.assetTag,
        location: values.location,
        assignedTo: values.assignedTo,
        ownedBy: values.ownedBy,
        environment: values.environment || '',
        criticality: values.criticality || '',
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
      });
      // mutation onSuccess 已展示 'cmdb.createCISuccess' 中文，但 i18n key 名称保留以便未来切换语言
      void message; // keep tree-shaking happy
      clearDirty();
      const id = (created as { id?: number })?.id;
      if (id && stagedImageIds.length > 0) {
        message.open({
          key: DESCRIPTION_UPLOAD_KEY,
          type: 'loading',
          content: '正在上传描述图片…',
          duration: 0,
        });
        const replacements: Record<string, StagedImageReplacement> = {};
        let imageFailures = 0;
        for (const stagedId of stagedImageIds) {
          const file = stagedImagesRef.current.get(stagedId);
          if (!file) continue;
          try {
            const uploaded = await AttachmentApi.upload(file, {
              bizType: 'cmdb_ci',
              bizId: id,
              usage: 'inline_image',
            });
            replacements[stagedId] = {
              id: uploaded.id,
              url: uploaded.previewUrl || cmdbCiAttachmentPreviewUrl(id, uploaded.id),
              name: uploaded.fileName || file.name,
            };
            stagedImagesRef.current.delete(stagedId);
          } catch {
            imageFailures += 1;
          }
        }
        if (Object.keys(replacements).length > 0) {
          try {
            await CMDBApi.updateCI(id, {
              description: replaceStagedImages(rawDescription, replacements),
            });
          } catch (writeBackError) {
            console.error('回写配置项描述图片失败', writeBackError);
            imageFailures += 1;
          }
        }
        message.destroy(DESCRIPTION_UPLOAD_KEY);
        if (imageFailures > 0) {
          message.warning(`${imageFailures} 张描述图片上传失败，可进入编辑页重新插入。`);
        }
      }
      if (id) {
        navigate(`/cmdb/cis/${id}`);
      } else {
        navigate('/cmdb');
      }
    } catch (error) {
      let errorMessage = t('cmdb.createCIFailed');
      if (error && typeof error === 'object') {
        const errObj = error as Record<string, unknown>;
        if (typeof errObj.message === 'string') {
          errorMessage = errObj.message;
        }
        if (errObj.response && typeof errObj.response === 'object') {
          const response = errObj.response as Record<string, unknown>;
          if (response.data && typeof response.data === 'object') {
            const data = response.data as Record<string, unknown>;
            if (data.message) {
              errorMessage = String(data.message);
            }
          }
          if (response.status) {
            errorMessage = `HTTP ${response.status}: ${errorMessage}`;
          }
        }
      }
      message.error(errorMessage);
    }
  };

  const formReady = !typesLoading && !cloudLoading;

  return (
    <div className='space-y-6'>
      <ManagementPageHeader
        title='录入配置项'
        description='填写新配置项的基础信息、云资源关联和扩展属性，所有字段一次录入完整。'
        notice={
          <ManagementNotice
            message='优先选择云资源引用'
            description='如果配置项来自云发现，先绑定云资源，系统会自动带出 Region、Zone、资源类型和动态属性。'
          />
        }
      />

      <Card className='rounded-xl shadow-sm'>
        {formReady ? (
          <CIEditorForm
            form={form}
            types={types}
            typesLoading={typesLoading}
            cloudResources={cloudResources}
            cloudServices={cloudServices}
            cloudLoading={cloudLoading}
            schemaFields={schemaFields}
            typeSchemaFields={typeSchemaFields}
            saving={createMutation.isPending}
            submitText='保存配置项'
            onSubmit={handleSubmit}
            onCancel={handleCancel}
            onCITypeChange={handleCITypeChange}
            onCloudResourceChange={handleCloudResourceChange}
            onDescriptionImageUpload={handleEditorImageUpload}
            onValuesChange={() => {
              markDirty();
            }}
          />
        ) : (
          <div className='flex min-h-[240px] items-center justify-center'>
            <Spin size='large' />
          </div>
        )}
      </Card>
    </div>
  );
};

export default CreateCIPage;
