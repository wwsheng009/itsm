/**
 * V2-01 个性化记忆：Dock 输入框上方的记忆条（常用语 / 个人模板 / 记忆管理）。
 *
 * 交互契约（方案 §7 V2-01）：
 * - 点击 chip = **只预填输入框**（`onInsert`），绝不自动发送/提交；
 * - 「存为常用语 / 存为模板」= 显式保存当前输入（用户主动提供，可随时在管理面删除）；
 * - 管理面展示「最近处理方式」（纯元数据：动作键 + 时间），并提供用户级关闭与清空；
 * - 租户策略关闭 / 埋点 off → 本组件不渲染（零请求由 store 保证）；
 *   用户显式关闭 → 渲染「重新开启」入口（区别于租户策略，不误导）。
 */
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { App, Button, Input, Modal, Popover, Tag, Tooltip, Typography, theme } from 'antd';
import { BookmarkPlus, FileText, History, Trash2 } from 'lucide-react';

import { trackAssistantEvent } from '@/lib/ai/telemetry';
import { recordAssistantMemoryAction } from '@/lib/ai/memory/assistant-memory-store';
import { useAssistantMemory } from '@/lib/ai/memory/use-assistant-memory';
import {
  MEMORY_MAX_TEMPLATE_BODY_CHARS,
  MEMORY_MAX_TEMPLATE_NAME_CHARS,
  recentActionLabel,
} from '@/lib/ai/memory/types';

const { Text } = Typography;

export const ASSISTANT_MEMORY_CHIPS_TESTID = 'assistant-memory-chips';
export const ASSISTANT_MEMORY_CHIP_TESTID = 'assistant-memory-chip';
export const ASSISTANT_MEMORY_MANAGE_TESTID = 'assistant-memory-manage';
export const ASSISTANT_MEMORY_SAVE_PHRASE_TESTID = 'assistant-memory-save-phrase';
export const ASSISTANT_MEMORY_SAVE_TEMPLATE_TESTID = 'assistant-memory-save-template';
export const ASSISTANT_MEMORY_TEMPLATE_MODAL_TESTID = 'assistant-memory-template-modal';
export const ASSISTANT_MEMORY_TEMPLATE_NAME_TESTID = 'assistant-memory-template-name';
export const ASSISTANT_MEMORY_TEMPLATE_BODY_TESTID = 'assistant-memory-template-body';
export const ASSISTANT_MEMORY_REENABLE_TESTID = 'assistant-memory-reenable';
export const ASSISTANT_MEMORY_CLEAR_TESTID = 'assistant-memory-clear';
export const ASSISTANT_MEMORY_DISABLE_TESTID = 'assistant-memory-disable';

interface MemoryChip {
  kind: 'phrase' | 'template';
  id: string;
  label: string;
  insertText: string;
}

export interface AssistantMemoryChipsProps {
  /** 当前输入（存为常用语/模板的素材；空文本时保存按钮禁用）。 */
  query: string;
  /** 点击 chip：只预填输入框（调用方负责 setQuery + 聚焦），绝不自动发送。 */
  onInsert: (text: string) => void;
}

const AssistantMemoryChips: React.FC<AssistantMemoryChipsProps> = ({ query, onInsert }) => {
  const { token } = theme.useToken();
  const { message, modal } = App.useApp();
  const {
    mode,
    status,
    memory,
    error,
    ready,
    userDisabled,
    savePhrase,
    saveTemplate,
    removePhrase,
    removeTemplate,
    clearAll,
    setUserEnabled,
  } = useAssistantMemory();

  const [templateOpen, setTemplateOpen] = useState(false);
  const [templateName, setTemplateName] = useState('');
  const [templateBody, setTemplateBody] = useState('');
  const shownRef = useRef(false);

  const chips = useMemo<MemoryChip[]>(
    () => [
      ...memory.phrases.map(phrase => ({
        kind: 'phrase' as const,
        id: `phrase:${phrase}`,
        label: phrase,
        insertText: phrase,
      })),
      ...memory.templates.map(tpl => ({
        kind: 'template' as const,
        id: `template:${tpl.name}`,
        label: tpl.name,
        insertText: tpl.body || tpl.name,
      })),
    ],
    [memory]
  );

  // 展示埋点：仅当确有内容可展示时上报一次（suggestion_shown 复用，cardId=memory）。
  useEffect(() => {
    if (!ready || shownRef.current) return;
    if (chips.length === 0 && memory.recent.length === 0) return;
    shownRef.current = true;
    trackAssistantEvent('suggestion_shown', {
      cardId: 'memory',
      extra: {
        phrases: memory.phrases.length,
        templates: memory.templates.length,
        recent: memory.recent.length,
      },
    });
  }, [ready, chips.length, memory]);

  if (status === 'disabled') {
    if (!userDisabled) return null; // 租户策略 / 埋点 off：策略面处理，不渲染
    return (
      <div style={{ marginBottom: 6, textAlign: 'right' }}>
        <Text type="secondary" style={{ fontSize: 11 }}>
          个性化记忆已关闭
        </Text>
        <Button
          size="small"
          type="link"
          data-testid={ASSISTANT_MEMORY_REENABLE_TESTID}
          onClick={() => void setUserEnabled(true)}
        >
          重新开启
        </Button>
      </div>
    );
  }

  if (status !== 'ready') return null;

  const handleChip = (chip: MemoryChip) => {
    onInsert(chip.insertText);
    recordAssistantMemoryAction(chip.kind === 'phrase' ? 'memory.phrase_used' : 'memory.template_used');
    trackAssistantEvent('suggestion_clicked', { cardId: 'memory', extra: { kind: chip.kind } });
  };

  const handleSavePhrase = async () => {
    const text = query.trim();
    if (!text) return;
    await savePhrase(text);
    message.success('已记住这句常用语');
  };

  const openTemplateModal = () => {
    setTemplateName('');
    setTemplateBody(query.trim());
    setTemplateOpen(true);
  };

  const handleSaveTemplate = async () => {
    if (!templateName.trim()) return;
    await saveTemplate(templateName, templateBody);
    setTemplateOpen(false);
    message.success('已保存个人模板');
  };

  const handleDisable = () => {
    modal.confirm({
      title: '关闭个性化记忆？',
      content: '关闭后不再记录常用语、模板与最近处理方式；已保存内容保留，可随时重新开启。',
      okText: '关闭',
      cancelText: '取消',
      onOk: () => void setUserEnabled(false),
    });
  };

  const handleClear = () => {
    modal.confirm({
      title: '清空全部记忆？',
      content: '常用语、个人模板与最近处理记录将被删除（云端与本机），不可恢复。',
      okText: '清空',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: () => void clearAll(),
    });
  };

  const formatTime = (at: string) => at.slice(0, 16).replace('T', ' ');

  const manageContent = (
    <div style={{ width: 272 }} data-testid="assistant-memory-manage-panel">
      <Text strong style={{ fontSize: 12 }}>
        常用语（{memory.phrases.length}）
      </Text>
      {memory.phrases.length === 0 ? (
        <div>
          <Text type="secondary" style={{ fontSize: 11 }}>
            暂无（输入后点书签图标保存）
          </Text>
        </div>
      ) : (
        memory.phrases.map(phrase => (
          <div key={phrase} style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 2 }}>
            <Text ellipsis style={{ fontSize: 12, flex: 1 }} title={phrase}>
              {phrase}
            </Text>
            <Button
              size="small"
              type="text"
              danger
              icon={<Trash2 size={12} />}
              aria-label={`删除常用语 ${phrase}`}
              onClick={() => void removePhrase(phrase)}
            />
          </div>
        ))
      )}

      <div style={{ marginTop: 10 }}>
        <Text strong style={{ fontSize: 12 }}>
          个人模板（{memory.templates.length}）
        </Text>
      </div>
      {memory.templates.length === 0 ? (
        <div>
          <Text type="secondary" style={{ fontSize: 11 }}>
            暂无（输入后点文档图标保存）
          </Text>
        </div>
      ) : (
        memory.templates.map(tpl => (
          <div key={tpl.name} style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 2 }}>
            <Text ellipsis style={{ fontSize: 12, flex: 1 }} title={tpl.name}>
              {tpl.name}
            </Text>
            <Button
              size="small"
              type="text"
              danger
              icon={<Trash2 size={12} />}
              aria-label={`删除模板 ${tpl.name}`}
              onClick={() => void removeTemplate(tpl.name)}
            />
          </div>
        ))
      )}

      <div style={{ marginTop: 10 }}>
        <Text strong style={{ fontSize: 12 }}>
          最近处理方式（仅元数据）
        </Text>
      </div>
      {memory.recent.length === 0 ? (
        <div>
          <Text type="secondary" style={{ fontSize: 11 }}>
            暂无记录
          </Text>
        </div>
      ) : (
        memory.recent.slice(0, 5).map(item => (
          <div key={item.key}>
            <Text type="secondary" style={{ fontSize: 11 }}>
              · {recentActionLabel(item.key)}（{formatTime(item.at)}）
            </Text>
          </div>
        ))
      )}

      {mode === 'local' ? (
        <div style={{ marginTop: 8 }}>
          <Text type="secondary" style={{ fontSize: 11 }}>
            匿名档：记忆仅存本机，不上传。
          </Text>
        </div>
      ) : null}

      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          gap: 8,
          marginTop: 10,
          paddingTop: 8,
          borderTop: `1px solid ${token.colorBorderSecondary}`,
        }}
      >
        <Button size="small" data-testid={ASSISTANT_MEMORY_DISABLE_TESTID} onClick={handleDisable}>
          关闭个性化记忆
        </Button>
        <Button size="small" danger data-testid={ASSISTANT_MEMORY_CLEAR_TESTID} onClick={handleClear}>
          清空记忆
        </Button>
      </div>
    </div>
  );

  return (
    <div
      data-testid={ASSISTANT_MEMORY_CHIPS_TESTID}
      style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 6, marginBottom: 6 }}
    >
      {chips.map(chip => (
        <Tooltip key={chip.id} title={chip.kind === 'template' ? `模板：${chip.label}` : '常用语'}>
          <Tag
            data-testid={ASSISTANT_MEMORY_CHIP_TESTID}
            data-kind={chip.kind}
            style={{ cursor: 'pointer', margin: 0, maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
            onClick={() => handleChip(chip)}
          >
            {chip.label}
          </Tag>
        </Tooltip>
      ))}

      <Popover trigger="click" placement="topRight" content={manageContent}>
        <Button
          size="small"
          type="text"
          icon={<History size={13} />}
          data-testid={ASSISTANT_MEMORY_MANAGE_TESTID}
          aria-label="记忆管理"
        >
          记忆
        </Button>
      </Popover>

      <Tooltip title="把当前输入存为常用语">
        <Button
          size="small"
          type="text"
          icon={<BookmarkPlus size={13} />}
          data-testid={ASSISTANT_MEMORY_SAVE_PHRASE_TESTID}
          aria-label="存为常用语"
          disabled={!query.trim()}
          onClick={() => void handleSavePhrase()}
        />
      </Tooltip>
      <Tooltip title="把当前输入存为个人模板">
        <Button
          size="small"
          type="text"
          icon={<FileText size={13} />}
          data-testid={ASSISTANT_MEMORY_SAVE_TEMPLATE_TESTID}
          aria-label="存为模板"
          disabled={!query.trim()}
          onClick={openTemplateModal}
        />
      </Tooltip>

      {error === 'save_failed' ? (
        <Text type="danger" style={{ fontSize: 11 }}>
          记忆保存失败，已暂存本机
        </Text>
      ) : null}
      {error === 'sync_failed' ? (
        <Text type="secondary" style={{ fontSize: 11 }}>
          云端同步不可用，展示本机缓存
        </Text>
      ) : null}

      <Modal
        title="存为个人模板"
        open={templateOpen}
        onCancel={() => setTemplateOpen(false)}
        onOk={() => void handleSaveTemplate()}
        okText="保存"
        cancelText="取消"
        okButtonProps={{ disabled: !templateName.trim() }}
      >
        <div data-testid={ASSISTANT_MEMORY_TEMPLATE_MODAL_TESTID}>
          <Input
            placeholder={`模板名（≤${MEMORY_MAX_TEMPLATE_NAME_CHARS} 字）`}
            value={templateName}
            maxLength={MEMORY_MAX_TEMPLATE_NAME_CHARS}
            onChange={e => setTemplateName(e.target.value)}
            data-testid={ASSISTANT_MEMORY_TEMPLATE_NAME_TESTID}
            style={{ marginBottom: 8 }}
          />
          <Input.TextArea
            placeholder={`模板内容（≤${MEMORY_MAX_TEMPLATE_BODY_CHARS} 字，仅你自己可见）`}
            value={templateBody}
            maxLength={MEMORY_MAX_TEMPLATE_BODY_CHARS}
            onChange={e => setTemplateBody(e.target.value)}
            autoSize={{ minRows: 3, maxRows: 8 }}
            data-testid={ASSISTANT_MEMORY_TEMPLATE_BODY_TESTID}
          />
        </div>
      </Modal>

    </div>
  );
};

export default AssistantMemoryChips;
