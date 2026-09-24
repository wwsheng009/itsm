
import React, { useState, useEffect, useCallback, useRef, lazy, Suspense } from 'react';
import {
  Table,
  Card,
  Button,
  Input,
  Space,
  Select,
  Tag,
  Modal,
  Form,
  message,
  Popconfirm,
  Row,
  Col,
  Statistic,
  Descriptions,
  Typography,
  Empty,
  Tooltip,
} from 'antd';
import { Search, Plus, Pencil, Trash2, Eye, RotateCcw, AlertCircle, CheckCircle, XCircle } from 'lucide-react';
import type { ColumnsType } from 'antd/es/table';
import type { KEDBCreateRequest, KEDBResponse, KEDBStatsResponse, KEDBUpdateRequest } from '@/lib/api/kedb-api';
import { KEDBApi } from '@/lib/api/kedb-api';
import type { UploadedImage } from '@/components/common/rich-text/RichTextEditor';
import RichTextContent from '@/components/common/rich-text/RichTextContent';
import { AttachmentApi, knownErrorAttachmentPreviewUrl } from '@/lib/api/attachment-api';
import { htmlToPlainText, isRichTextEnabled } from '@/lib/rich-text/sanitize';
import {
  STAGED_ID_PREFIX,
  extractStagedImageIds,
  replaceStagedImages,
  stripStagedImages,
  type StagedImageReplacement,
} from '@/lib/rich-text/staged-images';

const { TextArea } = Input;
const { Text } = Typography;

// 富文本编辑器按需加载：VITE_RICH_TEXT=off 时该分支不渲染，不会请求编辑器 chunk
const RichTextEditorLazy = lazy(() => import('@/components/common/rich-text/RichTextEditor'));

const RichTextEditor: React.FC<React.ComponentProps<typeof RichTextEditorLazy>> = props => (
  <Suspense fallback={<div className="rich-text-editor" style={{ minHeight: 140 }} />}>
    <RichTextEditorLazy {...props} />
  </Suspense>
);

/** 解决方案纯文本长度上限（与后端 DTO 的 20000 上限对齐） */
const SOLUTION_MAX_LENGTH = 20000;
const SOLUTION_UPLOAD_KEY = 'known-error-solution-images';

// 状态配置
const statusConfig: Record<string, { color: string; text: string }> = {
  draft: { color: 'default', text: '草稿' },
  active: { color: 'processing', text: '活动中' },
  resolved: { color: 'success', text: '已解决' },
  deprecated: { color: 'error', text: '已废弃' },
};

// 严重程度配置
const severityConfig: Record<string, { color: string; text: string }> = {
  critical: { color: 'red', text: '严重' },
  high: { color: 'orange', text: '高' },
  medium: { color: 'gold', text: '中' },
  low: { color: 'green', text: '低' },
};

export default function KnownErrorsPage() {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<KEDBResponse[]>([]);
  const [stats, setStats] = useState<KEDBStatsResponse | null>(null);
  const [pagination, setPagination] = useState({ current: 1, pageSize: 20, total: 0 });
  const [searchParams, setSearchParams] = useState({
    keyword: '',
    status: '',
    category: '',
    severity: '',
  });
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [isViewModalOpen, setIsViewModalOpen] = useState(false);
  const [editingRecord, setEditingRecord] = useState<KEDBResponse | null>(null);
  const [viewRecord, setViewRecord] = useState<KEDBResponse | null>(null);
  const [form] = Form.useForm();
  const [categories, setCategories] = useState<string[]>([]);

  const richTextEnabled = isRichTextEnabled();
  // 编辑器宿主 ID：编辑态已有 knownErrorId（图片即时上传）；创建态为 null（blob 占位）
  const knownErrorIdRef = useRef<number | null>(null);
  const stagedImagesRef = useRef<Map<string, File>>(new Map());
  const stagedImageSeqRef = useRef(0);

  const resetEditorImages = () => {
    stagedImagesRef.current.clear();
    stagedImageSeqRef.current = 0;
  };

  const handleEditorImageUpload = useCallback(async (file: File): Promise<UploadedImage> => {
    const hostId = knownErrorIdRef.current;
    if (hostId) {
      const uploaded = await AttachmentApi.upload(file, {
        bizType: 'known_error',
        bizId: hostId,
        usage: 'inline_image',
      });
      return {
        url: uploaded.previewUrl || knownErrorAttachmentPreviewUrl(hostId, uploaded.id),
        id: uploaded.id,
        name: uploaded.fileName || file.name,
      };
    }
    stagedImageSeqRef.current += 1;
    const stagedId = `${STAGED_ID_PREFIX}${Date.now().toString(36)}-${stagedImageSeqRef.current}`;
    stagedImagesRef.current.set(stagedId, file);
    // 仅本地预览地址；提交时先剥离，创建成功拿到 knownErrorId 后再上传回写
    return { url: URL.createObjectURL(file), id: stagedId, name: file.name };
  }, []);

  useEffect(() => {
    fetchData();
    fetchStats();
    fetchCategories();
  }, []);

  const fetchData = async () => {
    setLoading(true);
    try {
      const params = {
        page: pagination.current,
        pageSize: pagination.pageSize,
        ...searchParams,
      };
      // 移除空参数
      Object.keys(params).forEach((key) => {
        if (params[key as keyof typeof params] === '') {
          delete params[key as keyof typeof params];
        }
      });
      const response = await KEDBApi.getKnownErrors(params);
      setData(response.items);
      setPagination((prev) => ({ ...prev, total: response.total }));
    } catch (error) {
      message.error('获取数据失败');
    } finally {
      setLoading(false);
    }
  };

  const fetchStats = async () => {
    try {
      const response = await KEDBApi.getStats();
      setStats(response);
    } catch (error) {
      console.error('获取统计失败', error);
    }
  };

  const fetchCategories = async () => {
    try {
      const response = await KEDBApi.getCategories();
      setCategories(response.categories || []);
    } catch (error) {
      console.error('获取分类失败', error);
    }
  };

  const handleSearch = () => {
    setPagination((prev) => ({ ...prev, current: 1 }));
    fetchData();
  };

  const handleReset = () => {
    setSearchParams({ keyword: '', status: '', category: '', severity: '' });
    setPagination((prev) => ({ ...prev, current: 1 }));
    fetchData();
  };

  const handleTableChange = (pag: any) => {
    setPagination({ ...pagination, current: pag.current, pageSize: pag.pageSize });
    fetchData();
  };

  const handleAdd = () => {
    setEditingRecord(null);
    knownErrorIdRef.current = null;
    resetEditorImages();
    form.resetFields();
    setIsModalOpen(true);
  };

  const handleEdit = (record: KEDBResponse) => {
    setEditingRecord(record);
    knownErrorIdRef.current = record.id;
    resetEditorImages();
    form.setFieldsValue({
      title: record.title,
      description: record.description,
      symptoms: record.symptoms,
      rootCause: record.rootCause,
      workaround: record.workaround,
      resolution: record.resolution,
      category: record.category,
      severity: record.severity,
      status: record.status,
      affectedProducts: record.affectedProducts,
      affectedCis: record.affectedCis,
      keywords: record.keywords,
    });
    setIsModalOpen(true);
  };

  const handleView = (record: KEDBResponse) => {
    setViewRecord(record);
    setIsViewModalOpen(true);
  };

  const handleDelete = async (id: number) => {
    try {
      await KEDBApi.deleteKnownError(id);
      message.success('删除成功');
      fetchData();
      fetchStats();
    } catch (error) {
      message.error('删除失败');
    }
  };

  const handlePromote = async (id: number) => {
    try {
      await KEDBApi.promoteToKnownError(id);
      message.success('晋升成功');
      fetchData();
      fetchStats();
    } catch (error) {
      message.error('晋升失败');
    }
  };

  const handleSubmit = async () => {
    let values: KEDBCreateRequest & KEDBUpdateRequest;
    try {
      values = await form.validateFields();
    } catch {
      return;
    }

    const rawWorkaround = typeof values.workaround === 'string' ? values.workaround : '';
    const rawResolution = typeof values.resolution === 'string' ? values.resolution : '';
    // 富文本按纯文本口径校验长度（临时/永久解决方案各自上限 20000）
    if (richTextEnabled) {
      const workaroundLength = htmlToPlainText(rawWorkaround, Number.MAX_SAFE_INTEGER).length;
      const resolutionLength = htmlToPlainText(rawResolution, Number.MAX_SAFE_INTEGER).length;
      if (workaroundLength > SOLUTION_MAX_LENGTH || resolutionLength > SOLUTION_MAX_LENGTH) {
        message.warning('临时/永久解决方案各最多 20000 字，请精简后再提交');
        return;
      }
    }

    // 创建态：先剥离 blob: 占位图（绝不落库），创建成功后再上传并回写两个字段
    const stagedImageIds = richTextEnabled
      ? Array.from(
          new Set([
            ...extractStagedImageIds(rawWorkaround),
            ...extractStagedImageIds(rawResolution),
          ])
        )
      : [];
    const payload = {
      ...values,
      workaround:
        stagedImageIds.length > 0 ? stripStagedImages(rawWorkaround) : rawWorkaround,
      resolution:
        stagedImageIds.length > 0 ? stripStagedImages(rawResolution) : rawResolution,
    };

    try {
      let hostId = editingRecord?.id ?? 0;
      if (editingRecord) {
        await KEDBApi.updateKnownError(editingRecord.id, payload);
        message.success('更新成功');
      } else {
        const created = await KEDBApi.createKnownError(payload);
        hostId = Number(created?.id ?? 0);
        knownErrorIdRef.current = hostId || null;
        message.success('创建成功');
      }

      if (hostId > 0 && stagedImageIds.length > 0) {
        message.open({
          key: SOLUTION_UPLOAD_KEY,
          type: 'loading',
          content: '正在上传解决方案图片…',
          duration: 0,
        });
        const replacements: Record<string, StagedImageReplacement> = {};
        let imageFailures = 0;
        for (const stagedId of stagedImageIds) {
          const file = stagedImagesRef.current.get(stagedId);
          if (!file) continue;
          try {
            const uploaded = await AttachmentApi.upload(file, {
              bizType: 'known_error',
              bizId: hostId,
              usage: 'inline_image',
            });
            replacements[stagedId] = {
              id: uploaded.id,
              url: uploaded.previewUrl || knownErrorAttachmentPreviewUrl(hostId, uploaded.id),
              name: uploaded.fileName || file.name,
            };
            stagedImagesRef.current.delete(stagedId);
          } catch {
            imageFailures += 1;
          }
        }
        if (Object.keys(replacements).length > 0) {
          try {
            await KEDBApi.updateKnownError(hostId, {
              workaround: replaceStagedImages(rawWorkaround, replacements),
              resolution: replaceStagedImages(rawResolution, replacements),
            });
          } catch (writeBackError) {
            console.error('回写解决方案图片失败', writeBackError);
            imageFailures += 1;
          }
        }
        message.destroy(SOLUTION_UPLOAD_KEY);
        if (imageFailures > 0) {
          message.warning(`${imageFailures} 张解决方案图片上传失败，可编辑后再补充。`);
        }
      }

      setIsModalOpen(false);
      resetEditorImages();
      knownErrorIdRef.current = null;
      fetchData();
      fetchStats();
    } catch (error) {
      message.destroy(SOLUTION_UPLOAD_KEY);
      message.error('操作失败');
    }
  };

  const columns: ColumnsType<KEDBResponse> = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 60,
      fixed: 'left',
    },
    {
      title: '标题',
      dataIndex: 'title',
      fixed: 'left',
      width: 200,
      ellipsis: true,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (status: string) => {
        const config = statusConfig[status] || { color: 'default', text: status };
        return <Tag color={config.color}>{config.text}</Tag>;
      },
    },
    {
      title: '严重程度',
      dataIndex: 'severity',
      width: 100,
      render: (severity: string) => {
        const config = severityConfig[severity] || { color: 'default', text: severity };
        return <Tag color={config.color}>{config.text}</Tag>;
      },
    },
    {
      title: '分类',
      dataIndex: 'category',
      width: 100,
    },
    {
      title: '发生次数',
      dataIndex:'occurrenceCount',
      width: 100,
    },
    {
      title: '关键词',
      dataIndex: 'keywords',
      width: 150,
      ellipsis: true,
      render: (keywords: string[]) =>
        keywords?.slice(0, 3).map((k, i) => (
          <Tag key={i} style={{ marginRight: 4 }}>
            {k}
          </Tag>
        )),
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      width: 160,
      render: (text: string) => new Date(text).toLocaleString(),
    },
    {
      title: '操作',
      key: 'action',
      width: 180,
      fixed: 'right',
      render: (_, record) => (
        <Space size="small">
          <Tooltip title="查看详情">
            <Button
              type="link"
              size="small"
              icon={<Eye />}
              onClick={() => handleView(record)}
            />
          </Tooltip>
          <Tooltip title="编辑">
            <Button
              type="link"
              size="small"
              icon={<Pencil />}
              onClick={() => handleEdit(record)}
            />
          </Tooltip>
          {record.status === 'draft' && (
            <Tooltip title="晋升为正式已知错误">
              <Button
                type="link"
                size="small"
                icon={<CheckCircle />}
                onClick={() => handlePromote(record.id)}
              />
            </Tooltip>
          )}
          <Popconfirm
            title="确认删除"
            description="确定要删除这条已知错误吗？"
            onConfirm={() => handleDelete(record.id)}
            okText="确认"
            cancelText="取消"
          >
            <Tooltip title="删除">
              <Button type="link" size="small" danger icon={<Trash2 />} />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div style={{ padding: 24 }}>
      <Space orientation="vertical" size="large" style={{ width: '100%' }}>
        {/* 统计卡片 */}
        {stats && (
          <Row gutter={16}>
            <Col span={6}>
              <Card>
                <Statistic title="总数" value={stats.total} />
              </Card>
            </Col>
            <Col span={6}>
              <Card>
                <Statistic
                  title="活动中"
                  value={stats.active}
                  styles={{ content: { color: '#1890ff' } }}
                />
              </Card>
            </Col>
            <Col span={6}>
              <Card>
                <Statistic
                  title="已解决"
                  value={stats.resolved}
                  styles={{ content: { color: '#52c41a' } }}
                />
              </Card>
            </Col>
            <Col span={6}>
              <Card>
                <Statistic
                  title="已废弃"
                  value={stats.deprecated}
                  styles={{ content: { color: '#ff4d4f' } }}
                />
              </Card>
            </Col>
          </Row>
        )}

        {/* 搜索栏 */}
        <Card>
          <Space wrap>
            <Input.Search
              placeholder="搜索标题、描述、症状..."
              style={{ width: 250 }}
              allowClear
              onSearch={handleSearch}
              onChange={(e) =>
                setSearchParams((prev) => ({ ...prev, keyword: e.target.value }))
              }
            />
            <Select
              placeholder="状态"
              style={{ width: 120 }}
              allowClear
              value={searchParams.status || undefined}
              onChange={(value) =>
                setSearchParams((prev) => ({ ...prev, status: value || '' }))
              }
            >
              <Select.Option value="draft">草稿</Select.Option>
              <Select.Option value="active">活动中</Select.Option>
              <Select.Option value="resolved">已解决</Select.Option>
              <Select.Option value="deprecated">已废弃</Select.Option>
            </Select>
            <Select
              placeholder="严重程度"
              style={{ width: 120 }}
              allowClear
              value={searchParams.severity || undefined}
              onChange={(value) =>
                setSearchParams((prev) => ({ ...prev, severity: value || '' }))
              }
            >
              <Select.Option value="critical">严重</Select.Option>
              <Select.Option value="high">高</Select.Option>
              <Select.Option value="medium">中</Select.Option>
              <Select.Option value="low">低</Select.Option>
            </Select>
            <Select
              placeholder="分类"
              style={{ width: 150 }}
              allowClear
              value={searchParams.category || undefined}
              onChange={(value) =>
                setSearchParams((prev) => ({ ...prev, category: value || '' }))
              }
            >
              {categories.map((cat) => (
                <Select.Option key={cat} value={cat}>
                  {cat}
                </Select.Option>
              ))}
            </Select>
            <Space>
              <Button type="primary" icon={<Search />} onClick={handleSearch}>
                搜索
              </Button>
              <Button onClick={handleReset}>重置</Button>
            </Space>
          </Space>
        </Card>

        {/* 数据表格 */}
        <Card
          title="已知错误列表"
          extra={
            <Button type="primary" icon={<Plus />} onClick={handleAdd}>
              新建已知错误
            </Button>
          }
        >
          <Table
            columns={columns}
            dataSource={data}
            loading={loading}
            rowKey="id"
            scroll={{ x: 1200 }}
            pagination={{
              ...pagination,
              showSizeChanger: true,
              showQuickJumper: true,
              showTotal: (total) => `共 ${total} 条`,
              pageSizeOptions: ['10', '20', '50', '100'],
            }}
            onChange={handleTableChange}
          />
        </Card>
      </Space>

      {/* 新建/编辑 Modal */}
      <Modal
        title={editingRecord ? '编辑已知错误' : '新建已知错误'}
        open={isModalOpen}
        onOk={handleSubmit}
        onCancel={() => setIsModalOpen(false)}
        width={700}
        okText="确认"
        cancelText="取消"
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="title"
            label="标题"
            rules={[{ required: true, message: '请输入标题' }]}
          >
            <Input placeholder="请输入标题" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <TextArea rows={2} placeholder="请输入描述" />
          </Form.Item>
          <Form.Item name="symptoms" label="症状描述">
            <TextArea rows={2} placeholder="请输入症状描述" />
          </Form.Item>
          <Form.Item name="rootCause" label="根本原因">
            <TextArea rows={2} placeholder="请输入根本原因" />
          </Form.Item>
          <Form.Item name="workaround" label="临时解决方案">
            {richTextEnabled ? (
              <RichTextEditor
                placeholder="请输入临时解决方案"
                minHeight={140}
                onUploadImage={handleEditorImageUpload}
                dataTestId="known-error-workaround-input"
              />
            ) : (
              <TextArea
                rows={2}
                placeholder="请输入临时解决方案"
                data-testid="known-error-workaround-input"
              />
            )}
          </Form.Item>
          <Form.Item name="resolution" label="永久解决方案">
            {richTextEnabled ? (
              <RichTextEditor
                placeholder="请输入永久解决方案"
                minHeight={140}
                onUploadImage={handleEditorImageUpload}
                dataTestId="known-error-resolution-input"
              />
            ) : (
              <TextArea
                rows={2}
                placeholder="请输入永久解决方案"
                data-testid="known-error-resolution-input"
              />
            )}
          </Form.Item>
          <Row gutter={16}>
            <Col span={8}>
              <Form.Item name="category" label="分类">
                <Input placeholder="请输入分类" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item name="severity" label="严重程度">
                <Select placeholder="请选择">
                  <Select.Option value="critical">严重</Select.Option>
                  <Select.Option value="high">高</Select.Option>
                  <Select.Option value="medium">中</Select.Option>
                  <Select.Option value="low">低</Select.Option>
                </Select>
              </Form.Item>
            </Col>
            {editingRecord && (
              <Col span={8}>
                <Form.Item name="status" label="状态">
                  <Select placeholder="请选择">
                    <Select.Option value="draft">草稿</Select.Option>
                    <Select.Option value="active">活动中</Select.Option>
                    <Select.Option value="resolved">已解决</Select.Option>
                    <Select.Option value="deprecated">已废弃</Select.Option>
                  </Select>
                </Form.Item>
              </Col>
            )}
          </Row>
          <Form.Item name="affectedProducts" label="受影响的产品/服务">
            <Select
              mode="tags"
              placeholder="输入后按回车添加"
              style={{ width: '100%' }}
            />
          </Form.Item>
          <Form.Item name="affectedCis" label="受影响的配置项">
            <Select
              mode="tags"
              placeholder="输入后按回车添加"
              style={{ width: '100%' }}
            />
          </Form.Item>
          <Form.Item name="keywords" label="关键词">
            <Select
              mode="tags"
              placeholder="输入后按回车添加"
              style={{ width: '100%' }}
            />
          </Form.Item>
        </Form>
      </Modal>

      {/* 查看详情 Modal */}
      <Modal
        title="已知错误详情"
        open={isViewModalOpen}
        onCancel={() => setIsViewModalOpen(false)}
        footer={
          <Button onClick={() => setIsViewModalOpen(false)}>关闭</Button>
        }
        width={800}
      >
        {viewRecord && (
          <Descriptions column={2} bordered>
            <Descriptions.Item label="ID">{viewRecord.id}</Descriptions.Item>
            <Descriptions.Item label="状态">
              <Tag color={statusConfig[viewRecord.status]?.color}>
                {statusConfig[viewRecord.status]?.text}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="标题" span={2}>
              {viewRecord.title}
            </Descriptions.Item>
            <Descriptions.Item label="描述" span={2}>
              {viewRecord.description || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="症状描述" span={2}>
              {viewRecord.symptoms || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="根本原因" span={2}>
              {viewRecord.rootCause || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="临时解决方案" span={2}>
              <RichTextContent content={viewRecord.workaround} emptyText="-" />
            </Descriptions.Item>
            <Descriptions.Item label="永久解决方案" span={2}>
              <RichTextContent content={viewRecord.resolution} emptyText="-" />
            </Descriptions.Item>
            <Descriptions.Item label="分类">{viewRecord.category}</Descriptions.Item>
            <Descriptions.Item label="严重程度">
              <Tag color={severityConfig[viewRecord.severity]?.color}>
                {severityConfig[viewRecord.severity]?.text}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="发生次数">
              {viewRecord.occurrenceCount}
            </Descriptions.Item>
            <Descriptions.Item label="关键词" span={2}>
              {viewRecord.keywords?.map((k, i) => (
                <Tag key={i}>{k}</Tag>
              )) || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="受影响的产品" span={2}>
              {viewRecord.affectedProducts?.join(', ') || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="受影响的配置项" span={2}>
              {viewRecord.affectedCis?.join(', ') || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="创建时间">
              {new Date(viewRecord.createdAt).toLocaleString()}
            </Descriptions.Item>
            <Descriptions.Item label="更新时间">
              {new Date(viewRecord.updatedAt).toLocaleString()}
            </Descriptions.Item>
          </Descriptions>
        )}
      </Modal>
    </div>
  );
}