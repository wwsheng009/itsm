
import React, { useState, useEffect, useRef } from 'react';
import {
  Card,
  Table,
  Button,
  Space,
  Typography,
  Tag,
  Select,
  Modal,
  Timeline,
  Alert,
  Tooltip,
  Row,
  Col,
  Descriptions,
  message,
  Spin,
} from 'antd';
import {
  GitBranch,
  History,
  RotateCcw,
  Eye,
  GitMerge,
  GitCommit,
  User,
  Calendar,
  FileText,
  AlertTriangle,
  CheckCircle,
} from 'lucide-react';
import { KnowledgeBaseApi } from '@/lib/api/knowledge-base-api';
import type { ArticleVersion } from '@/types/knowledge-base';
import { format } from 'date-fns';
import ArticleContentRenderer from './ArticleContentRenderer';
import { resolveArticleContentType } from '@/lib/knowledge/article-content-type';

const { Title, Text, Paragraph } = Typography;

interface ArticleVersionControlProps {
  articleId: string;
  /** 列表为空时的兜底当前版本号；正常由版本列表的最大版本号推导 */
  currentVersion?: number;
  onVersionChange?: (version: number) => void;
}

const ArticleVersionControl: React.FC<ArticleVersionControlProps> = ({
  articleId,
  currentVersion = 1,
  onVersionChange,
}) => {
  const [versions, setVersions] = useState<ArticleVersion[]>([]);
  const [loading, setLoading] = useState(false);
  const [compareModalVisible, setCompareModalVisible] = useState(false);
  const [selectedVersions, setSelectedVersions] = useState<[number, number] | null>(null);
  const [compareResult, setCompareResult] = useState<any>(null);
  const [previewVersion, setPreviewVersion] = useState<ArticleVersion | null>(null);
  const [previewModalVisible, setPreviewModalVisible] = useState(false);
  // 加载请求序号：只接受最新一次请求的结果，过期响应（StrictMode 双挂载、
  // 切换文章后的旧请求）直接丢弃，避免重复 setState 与重复报错。
  const loadSeqRef = useRef(0);

  // 版本快照在每次写入后生成，版本号最大的那一行即「当前正文」。
  // 父组件传入的 currentVersion 只作为列表为空时的兜底，避免列表与统计口径不一致。
  const latestVersion =
    versions.length > 0 ? Math.max(...versions.map(v => v.version)) : currentVersion;

  // 加载版本历史
  useEffect(() => {
    loadVersions();
    return () => {
      // 卸载/切换文章时让在途请求失效
      loadSeqRef.current += 1;
    };
  }, [articleId]);

  const loadVersions = async () => {
    const seq = ++loadSeqRef.current;
    try {
      setLoading(true);
      const versionHistory = await KnowledgeBaseApi.getArticleVersions(articleId);
      if (seq !== loadSeqRef.current) return;
      const list = Array.isArray(versionHistory) ? versionHistory : [];
      setVersions([...list].sort((a, b) => b.version - a.version));
    } catch (error) {
      if (seq !== loadSeqRef.current) return;
      // 固定 key：并发请求/重试产生的同一条错误只保留一个 toast，不再叠加
      message.error({ key: 'kb-article-versions-load', content: '加载版本历史失败' });
    } finally {
      if (seq === loadSeqRef.current) setLoading(false);
    }
  };

  // 恢复到指定版本（表格按钮与预览弹窗共用同一次确认）
  const handleRestoreVersion = (version: number) => {
    Modal.confirm({
      title: '确认恢复版本',
      content: `确定要恢复到版本 ${version} 吗？这将创建一个新的版本。`,
      onOk: async () => {
        try {
          await KnowledgeBaseApi.restoreVersion(articleId, version);
          message.success({ key: 'kb-article-version-restore', content: '版本恢复成功' });
          loadVersions();
          onVersionChange?.(version);
        } catch (error) {
          // 在 onOk 内消化异常：Modal 不会把 rejection 抛成未处理错误；
          // 固定 key 保证重复点击/重试只保留一条提示。
          message.error({ key: 'kb-article-version-restore', content: '恢复版本失败' });
        }
      },
    });
  };

  // 比较版本
  const handleCompareVersions = async () => {
    if (!selectedVersions || selectedVersions[0] === selectedVersions[1]) {
      message.warning('请选择两个不同的版本进行比较');
      return;
    }

    try {
      setLoading(true);
      const result = await KnowledgeBaseApi.compareVersions(
        articleId,
        selectedVersions[0],
        selectedVersions[1]
      );
      setCompareResult(result);
      setCompareModalVisible(true);
    } catch (error) {
      message.error({ key: 'kb-article-version-compare', content: '比较版本失败' });
    } finally {
      setLoading(false);
    }
  };

  // 预览版本
  const handlePreviewVersion = (version: ArticleVersion) => {
    setPreviewVersion(version);
    setPreviewModalVisible(true);
  };

  // 版本表格列
  const versionColumns = [
    {
      title: '版本',
      dataIndex: 'version',
      key: 'version',
      width: 80,
      render: (version: number, record: ArticleVersion) => (
        <Space>
          <GitCommit className="w-4 h-4 text-blue-500" />
          <Tag color={version === latestVersion ? 'green' : 'default'}>
            v{version}
            {version === latestVersion && ' (当前)'}
          </Tag>
        </Space>
      ),
    },
    {
      title: '变更说明',
      dataIndex: 'changeLog',
      key: 'changeLog',
      render: (text: string) => (
        <Text ellipsis={{ tooltip: text }} style={{ maxWidth: 200 }}>
          {text || '无变更说明'}
        </Text>
      ),
    },
    {
      title: '创建者',
      dataIndex: 'createdByName',
      key: 'createdByName',
      render: (name: string) => (
        <Space>
          <User className="w-4 h-4 text-gray-500" />
          {name}
        </Space>
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (date: string) => (
        <Space>
          <Calendar className="w-4 h-4 text-gray-500" />
          {format(new Date(date), 'yyyy-MM-dd HH:mm')}
        </Space>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 200,
      render: (_: unknown, record: ArticleVersion) => (
        <Space>
          <Tooltip title="预览版本">
            <Button
              type="text"
              icon={<Eye className="w-4 h-4" />}
              onClick={() => handlePreviewVersion(record)}
            />
          </Tooltip>

          {record.version !== latestVersion && (
            <Tooltip title="恢复到此版本">
              <Button
                type="text"
                icon={<RotateCcw className="w-4 h-4 text-orange-500" />}
                onClick={() => handleRestoreVersion(record.version)}
              />
            </Tooltip>
          )}
        </Space>
      ),
    },
  ];

  // 比较结果渲染
  const renderCompareResult = () => {
    if (!compareResult) return null;

    return (
      <div className="space-y-4">
        <Alert
          title="版本差异"
          description={`比较版本 ${selectedVersions?.[0]} 和版本 ${selectedVersions?.[1]}`}
          type="info"
          showIcon
        />

        {compareResult.changes && compareResult.changes.length > 0 ? (
          <div>
            <Title level={5}>变更详情</Title>
            {compareResult.changes.map((change: any, index: number) => (
              <Card key={index} size="small" className="mb-2">
                <Space>
                  <Tag
                    color={
                      change.type === 'added' ? 'green' : change.type === 'removed' ? 'red' : 'blue'
                    }
                  >
                    {change.type === 'added' ? '新增' : change.type === 'removed' ? '删除' : '修改'}
                  </Tag>
                  <Paragraph code style={{ margin: 0 }} ellipsis={{ rows: 3, expandable: true }}>
                    {change.content}
                  </Paragraph>
                </Space>
              </Card>
            ))}
          </div>
        ) : (
          <Alert title="无差异" description="两个版本之间没有发现差异" type="success" showIcon />
        )}

        {compareResult.diff && (
          <div>
            <Title level={5}>详细差异</Title>
            <Card size="small">
              <pre className="whitespace-pre-wrap text-xs bg-gray-50 p-3 rounded">
                {compareResult.diff}
              </pre>
            </Card>
          </div>
        )}
      </div>
    );
  };

  return (
    <Card
      title={
        <Space>
          <History className="w-5 h-5 text-blue-600" />
          <span>版本控制</span>
        </Space>
      }
      extra={
        <Space>
          <Button
            icon={<GitMerge className="w-4 h-4" />}
            onClick={() => {
              if (versions.length >= 2) {
                setSelectedVersions([versions[0].version, versions[1].version]);
                setCompareModalVisible(true);
              } else {
                message.warning('需要至少两个版本才能进行比较');
              }
            }}
            disabled={versions.length < 2}
          >
            比较版本
          </Button>
        </Space>
      }
    >
      {/* 版本历史表格 */}
      <Table
        columns={versionColumns}
        dataSource={versions}
        rowKey="version"
        loading={loading}
        scroll={{ x: 'max-content' }}
        pagination={false}
        size="small"
        className="version-table mb-4"
      />

      {/* 版本统计 */}
      <Row gutter={16} className="mt-4">
        <Col span={6}>
          <Card size="small">
            <div className="text-center">
              <div className="text-2xl font-bold text-blue-600">{versions.length}</div>
              <Text type="secondary">总版本数</Text>
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card size="small">
            <div className="text-center">
              <div className="text-2xl font-bold text-green-600">{latestVersion}</div>
              <Text type="secondary">当前版本</Text>
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card size="small">
            <div className="text-center">
              <div className="text-2xl font-bold text-orange-600">
                {versions.filter(v => v.changeLog).length}
              </div>
              <Text type="secondary">有变更记录</Text>
            </div>
          </Card>
        </Col>
        <Col span={6}>
          <Card size="small">
            <div className="text-center">
              <div className="text-2xl font-bold text-purple-600">
                {versions.filter(v => v.version !== latestVersion).length}
              </div>
              <Text type="secondary">可恢复版本</Text>
            </div>
          </Card>
        </Col>
      </Row>

      {/* 版本比较模态框 */}
      <Modal
        title={
          <Space>
            <GitBranch className="w-5 h-5" />
            版本比较
          </Space>
        }
        open={compareModalVisible}
        onCancel={() => {
          setCompareModalVisible(false);
          setCompareResult(null);
          setSelectedVersions(null);
        }}
        width={800}
        footer={null}
      >
        {!compareResult ? (
          <div className="space-y-4">
            <Title level={5}>选择比较版本</Title>
            <Row gutter={16}>
              <Col span={12}>
                <Text strong>源版本</Text>
                <Select
                  style={{ width: '100%', marginTop: 8 }}
                  value={selectedVersions?.[0]}
                  onChange={value => setSelectedVersions([value, selectedVersions?.[1] || 0])}
                  placeholder="选择源版本"
                  options={versions.map(version => ({
                    value: version.version,
                    label: `v${version.version} - ${format(new Date(version.createdAt), 'yyyy-MM-dd')}`,
                  }))}
                />
              </Col>
              <Col span={12}>
                <Text strong>目标版本</Text>
                <Select
                  style={{ width: '100%', marginTop: 8 }}
                  value={selectedVersions?.[1]}
                  onChange={value => setSelectedVersions([selectedVersions?.[0] || 0, value])}
                  placeholder="选择目标版本"
                  options={versions.map(version => ({
                    value: version.version,
                    label: `v${version.version} - ${format(new Date(version.createdAt), 'yyyy-MM-dd')}`,
                  }))}
                />
              </Col>
            </Row>
            <div className="text-center mt-4">
              <Button
                type="primary"
                onClick={handleCompareVersions}
                loading={loading}
                disabled={!selectedVersions || selectedVersions[0] === selectedVersions[1]}
              >
                开始比较
              </Button>
            </div>
          </div>
        ) : (
          renderCompareResult()
        )}
      </Modal>

      {/* 版本预览模态框 */}
      <Modal
        title={
          <Space>
            <FileText className="w-5 h-5" />
            版本预览 - v{previewVersion?.version}
          </Space>
        }
        open={previewModalVisible}
        onCancel={() => {
          setPreviewModalVisible(false);
          setPreviewVersion(null);
        }}
        width={800}
        footer={[
          <Button key="close" onClick={() => setPreviewModalVisible(false)}>
            关闭
          </Button>,
          previewVersion && previewVersion.version !== latestVersion && (
            <Button
              key="restore"
              type="primary"
              onClick={() => {
                handleRestoreVersion(previewVersion!.version);
                setPreviewModalVisible(false);
              }}
            >
              恢复到此版本
            </Button>
          ),
        ]}
      >
        {previewVersion && (
          <div className="space-y-4">
            <Descriptions column={2} bordered size="small">
              <Descriptions.Item label="版本号">v{previewVersion.version}</Descriptions.Item>
              <Descriptions.Item label="创建者">{previewVersion.createdByName}</Descriptions.Item>
              <Descriptions.Item label="创建时间">
                {format(new Date(previewVersion.createdAt), 'yyyy-MM-dd HH:mm:ss')}
              </Descriptions.Item>
              <Descriptions.Item label="变更说明">
                {previewVersion.changeLog || '无'}
              </Descriptions.Item>
            </Descriptions>

            {previewVersion.summary && (
              <div>
                <Title level={5}>摘要</Title>
                <Paragraph>{previewVersion.summary}</Paragraph>
              </div>
            )}

            <div>
              <Title level={5}>内容预览</Title>
              <Card size="small">
                {/*
                  版本正文按「内容类型」分发渲染（与详情页同一组件、同一兜底口径）：
                  历史版本没有类型标记时，由 resolveArticleContentType 按内容形态判定，
                  Markdown 版本不再被 DOMPurify 当 HTML 整段吞掉。
                */}
                <ArticleContentRenderer
                  content={previewVersion.content}
                  contentType={resolveArticleContentType({
                    contentType: previewVersion.contentType,
                    content: previewVersion.content,
                  })}
                />
              </Card>
            </div>
          </div>
        )}
      </Modal>
    </Card>
  );
};

export default ArticleVersionControl;
