
import React, { useState, useCallback, useEffect, useRef } from 'react';
import { Form, Input, Select, Button, Card, Space, Spin, App } from 'antd';
import { Send, Bot, CheckCircle } from 'lucide-react';
import type { A2UIComponent, A2UIDataModel, ValueDef, OptionItem, ActionDef, TextProps } from '@/types/a2ui';
import { getValueByPath, setValueByPath } from '@/types/a2ui';
import { useAuthStore } from '@/lib/store/auth-store';
import { A2UIApi } from '@/lib/api/a2ui-api';
import {
  aiInterpretTicketIntent,
  type TicketIntentFields,
  type TicketIntentInterpretation,
} from '@/lib/api/ai-api';
import IntentUnderstandingCard, {
  type IntentUnderstandingChanges,
} from './IntentUnderstandingCard';
import {
  AICreateFlowTracker,
  type AICreateSuggestedFields,
} from '@/lib/ai/ai-create-metrics';

// 解析值定义（字面量或路径）
function resolveValue(def: ValueDef | undefined, model: A2UIDataModel): unknown {
  if (!def) return undefined;

  if ('literalString' in def) return def.literalString;
  if ('literalNumber' in def) return def.literalNumber;
  if ('literalBoolean' in def) return def.literalBoolean;
  if ('path' in def) return getValueByPath(model, def.path);

  return undefined;
}

// 组件递归渲染器
const A2UIComponentRenderer: React.FC<{
  component: A2UIComponent;
  model: A2UIDataModel;
  onModelChange: (path: string, value: unknown) => void;
  onAction: (name: string, context: Record<string, unknown>) => void;
  allComponents: A2UIComponent[];
}> = ({ component, model, onModelChange, onAction, allComponents }) => {
  const compDef = component.component;
  const compType = Object.keys(compDef)[0];
  const props = (compDef as Record<string, unknown>)[compType];

  // 渲染子组件
  const renderChild = (childId: string) => {
    const child = allComponents.find(c => c.id === childId);
    if (!child) return null;
    return (
      <A2UIComponentRenderer
        key={child.id}
        component={child}
        model={model}
        onModelChange={onModelChange}
        onAction={onAction}
        allComponents={allComponents}
      />
    );
  };

  switch (compType) {
    case 'Text': {
      const textProps = props as { text?: ValueDef; usageHint?: string; color?: ValueDef | string };
      const textValue = resolveValue(textProps?.text, model);
      const hint = textProps?.usageHint;
      const classes: Record<string, string> = {
        h1: 'text-2xl font-bold',
        h2: 'text-xl font-semibold',
        h3: 'text-lg font-medium',
        body: 'text-base',
        caption: 'text-sm text-gray-500',
      };

      return (
        <div
          style={{ color: typeof textProps?.color === 'string' ? textProps.color : undefined }}
          className={hint ? classes[hint] : ''}
        >
          {String(textValue ?? '')}
        </div>
      );
    }

    case 'Column':
    case 'Row': {
      const children = props as { children?: { explicitList?: string[] } };
      const childIds = children?.children?.explicitList || [];
      return (
        <div className={compType === 'Row' ? 'flex gap-2 items-center' : 'flex flex-col gap-2'}>
          {childIds.map(id => renderChild(id))}
        </div>
      );
    }

    case 'Card': {
      const child = props as { child: string };
      return (
        <Card size="small" className="my-2">
          {renderChild(child.child)}
        </Card>
      );
    }

    case 'TextField': {
      const fieldProps = props as {
        label?: { literalString: string };
        text?: ValueDef;
        placeholder?: { literalString?: string };
        enabled?: ValueDef;
        error?: ValueDef;
      };

      // 提取路径
      const getPath = (def?: ValueDef) => {
        if (!def) return undefined;
        return (def as { path?: string }).path;
      };

      const path = getPath(fieldProps?.text)?.replace('/ticket/', '') || '';
      const isEnabled = fieldProps?.enabled
        ? resolveValue(fieldProps.enabled, model) !== false
        : true;
      const errorMsg = fieldProps?.error
        ? (resolveValue(fieldProps.error, model) as string)
        : undefined;

      return (
        <Form.Item
          name={path}
          label={fieldProps?.label?.literalString}
          validateStatus={errorMsg ? 'error' : ''}
          help={errorMsg}
        >
          <Input
            placeholder={fieldProps?.placeholder?.literalString}
            disabled={!isEnabled}
            onChange={e => {
              const dataPath = getPath(fieldProps?.text);
              if (dataPath) onModelChange(dataPath, e.target.value);
            }}
          />
        </Form.Item>
      );
    }

    case 'TextAreaField': {
      const fieldProps = props as {
        label?: { literalString: string };
        text?: ValueDef;
        rows?: number;
        enabled?: ValueDef;
      };

      // 提取路径
      const getPath = (def?: ValueDef) => {
        if (!def) return undefined;
        return (def as { path?: string }).path;
      };

      const path = getPath(fieldProps?.text)?.replace('/ticket/', '') || '';
      const isEnabled = fieldProps?.enabled
        ? resolveValue(fieldProps.enabled, model) !== false
        : true;

      return (
        <Form.Item name={path} label={fieldProps?.label?.literalString}>
          <Input.TextArea
            rows={fieldProps?.rows || 3}
            disabled={!isEnabled}
            onChange={e => {
              const dataPath = getPath(fieldProps?.text);
              if (dataPath) onModelChange(dataPath, e.target.value);
            }}
          />
        </Form.Item>
      );
    }

    case 'PickerSelect': {
      const selectProps = props as {
        label?: { literalString: string };
        selection?: ValueDef;
        options?: { explicitList?: OptionItem[] };
        enabled?: ValueDef;
      };

      // 提取路径
      const getPath = (def?: ValueDef) => {
        if (!def) return undefined;
        return (def as { path?: string }).path;
      };

      const path = getPath(selectProps?.selection)?.replace('/ticket/', '') || '';
      const options = selectProps?.options?.explicitList || [];
      const isEnabled = selectProps?.enabled
        ? resolveValue(selectProps.enabled, model) !== false
        : true;

      return (
        <Form.Item name={path} label={selectProps?.label?.literalString}>
          <Select
            disabled={!isEnabled}
            onChange={value => {
              const dataPath = getPath(selectProps?.selection);
              if (dataPath) onModelChange(dataPath, value);
            }}
          >
            {options.map(opt => (
              <Select.Option key={opt.id} value={opt.id}>
                {opt.text}
              </Select.Option>
            ))}
          </Select>
        </Form.Item>
      );
    }

    case 'Button': {
      const btnProps = props as {
        child?: string;
        action?: { name: string; context: { key: string; value: ValueDef }[] };
        enabled?: ValueDef;
      };

      const isEnabled = btnProps?.enabled ? resolveValue(btnProps.enabled, model) !== false : true;

      // 获取按钮文字（从子组件）
      let buttonText = '提交';
      if (btnProps?.child) {
        const childComp = allComponents.find(c => c.id === btnProps.child);
        if (childComp && 'Text' in childComp.component) {
          const textProps = (childComp.component as { Text: TextProps }).Text;
          buttonText = (resolveValue(textProps.text, model) as string) || '提交';
        }
      }

      return (
        <Form.Item>
          <Button
            type="primary"
            disabled={!isEnabled}
            onClick={() => {
              if (btnProps?.action) {
                const context: Record<string, unknown> = {};
                btnProps.action.context.forEach(ctx => {
                  if ('path' in ctx.value) {
                    const path = ctx.value.path;
                    context[ctx.key] = getValueByPath(model, path);
                  }
                });
                onAction(btnProps.action.name, context);
              }
            }}
          >
            {buttonText}
          </Button>
        </Form.Item>
      );
    }

    default:
      return <div className="text-gray-400 text-xs">Unknown: {compType}</div>;
  }
};

// ==================== 主渲染器 ====================

export interface A2UIFormRendererProps {
  /** 受控意图（V0-02：自助问答「去建单」把问题描述带入表单；缺省为内部状态）。 */
  intent?: string;
  onIntentChange?: (value: string) => void;
  /** 埋点用页面键（缺省不落字段）。 */
  pageKey?: string;
}

export function A2UIFormRenderer({ intent, onIntentChange, pageKey }: A2UIFormRendererProps = {}) {
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [components, setComponents] = useState<A2UIComponent[]>([]);
  const [model, setModel] = useState<A2UIDataModel>({
    ticket: { title: '', type: 'hardware', priority: 'medium', description: '' },
    ui: { canSubmit: false, status: '', statusColor: 'gray', errors: {} },
  });
  const [internalIntent, setInternalIntent] = useState('');
  const userIntent = intent ?? internalIntent;
  const setUserIntent = useCallback(
    (value: string) => {
      if (intent === undefined) setInternalIntent(value);
      onIntentChange?.(value);
    },
    [intent, onIntentChange]
  );
  const [loading, setLoading] = useState(false);
  const [surfaceId, setSurfaceId] = useState<string | null>(null);
  const [submitted, setSubmitted] = useState(false);
  // V1-03 口语↔术语确认：理解结果与待确认的口语原文（确认/修改后才触发表单生成）。
  const [interpretation, setInterpretation] = useState<TicketIntentInterpretation | null>(null);
  const [pendingIntent, setPendingIntent] = useState('');
  // V0-02 度量：建单时长（start→提交成功）/ 误分类率代理（字段纠正）/ 任务漏斗。
  const trackerRef = useRef<AICreateFlowTracker | null>(null);
  if (trackerRef.current === null) trackerRef.current = new AICreateFlowTracker();
  const modelRef = useRef(model);
  useEffect(() => {
    modelRef.current = model;
  }, [model]);

  // 渲染 ID 列表
  const getRenderableIds = (): string[] => {
    const root = components.find(c => c.id === 'root');
    if (!root) return [];

    const compDef = root.component;
    const compType = Object.keys(compDef)[0];
    const children = (compDef as Record<string, unknown>)[compType] as {
      children?: { explicitList?: string[] };
    };

    return children?.children?.explicitList || [];
  };

  // 处理数据模型变化（双向绑定）
  const handleModelChange = useCallback((path: string, value: unknown) => {
    setModel(prev => {
      const newModel = JSON.parse(JSON.stringify(prev));
      setValueByPath(newModel, path, value);
      return newModel;
    });
  }, []);

  // 检查是否已登录：使用 zustand 的 isAuthenticated（AuthGuard 已通过 /api/v1/auth/me 探活验证）
  const checkAuth = (): boolean => {
    if (!useAuthStore.getState().isAuthenticated) {
      message.error('请先登录后再使用此功能');
      return false;
    }
    return true;
  };

  // 生成建单表单（V1-03 后：仅在用户确认/修改理解结果后调用；applied = 用户确认的术语）。
  const generateForm = async (intentText: string, applied?: TicketIntentFields) => {
    if (!checkAuth()) {
      return;
    }
    setLoading(true);
    const tracker = trackerRef.current as AICreateFlowTracker;
    tracker.start(pageKey);
    const suggested: AICreateSuggestedFields = {};
    try {
      const data = await A2UIApi.generateTicketForm(intentText, surfaceId);

      // 解析消息
      (data.messages || []).forEach((msg: string) => {
        try {
          const parsed = JSON.parse(msg);

          if ('surfaceUpdate' in parsed) {
            setSurfaceId(parsed.surfaceUpdate.surfaceId);
            const newComps = parsed.surfaceUpdate.components;
            setComponents(prev => {
              const existingIds = new Set(prev.map((c: A2UIComponent) => c.id));
              return [...prev, ...newComps.filter((c: A2UIComponent) => !existingIds.has(c.id))];
            });
          }

          if ('dataModelUpdate' in parsed) {
            const updates = parsed.dataModelUpdate.contents;
            // 捕获 AI 建议字段（误分类率代理的基线）。
            if (parsed.dataModelUpdate.path === 'ticket') {
              updates.forEach((u: { key: string; valueString?: string }) => {
                if (u.key === 'type') suggested.type = u.valueString;
                if (u.key === 'priority') suggested.priority = u.valueString;
              });
            }
            setModel(prev => {
              const newModel = JSON.parse(JSON.stringify(prev));
              updates.forEach(
                (u: {
                  key: string;
                  valueString?: string;
                  valueNumber?: number;
                  valueBoolean?: boolean;
                }) => {
                  const value = u.valueString ?? u.valueNumber ?? u.valueBoolean;
                  setValueByPath(newModel, `${parsed.dataModelUpdate.path}/${u.key}`, value);
                }
              );
              return newModel;
            });

            // 同步到 Ant Design Form
            updates.forEach((u: { key: string; valueString?: string }) => {
              if (u.valueString !== undefined) {
                form.setFieldValue(u.key, u.valueString);
              }
            });
          }

          if ('deleteSurface' in parsed) {
            setComponents([]);
            setSurfaceId(null);
          }
        } catch (e) {
          console.error('Parse message error:', e);
        }
      });

      // V1-03：用户确认/修改过的术语优先于规则解析结果（表单初值 = 用户认可的理解）。
      if (applied) {
        setModel(prev => {
          const newModel = JSON.parse(JSON.stringify(prev));
          setValueByPath(newModel, '/ticket/title', applied.title);
          setValueByPath(newModel, '/ticket/type', applied.type);
          setValueByPath(newModel, '/ticket/priority', applied.priority);
          setValueByPath(newModel, '/ticket/description', applied.description);
          return newModel;
        });
        form.setFieldValue('title', applied.title);
        form.setFieldValue('type', applied.type);
        form.setFieldValue('priority', applied.priority);
        form.setFieldValue('description', applied.description);
        // 误分类率基线 = 用户确认后的值：确认卡上的纠正已即时上报，避免重复计数。
        tracker.captureSuggested({ type: applied.type, priority: applied.priority });
      } else {
        tracker.captureSuggested(suggested);
      }
      setUserIntent('');
      setPendingIntent('');
    } catch (e) {
      console.error(e);
      message.error('请求失败，请稍后重试');
      tracker.finishFailure('generate_failed', pageKey);
    } finally {
      setLoading(false);
    }
  };

  // 发送口语：先理解（V1-03「我理解成 …」），理解失败降级为直接生成（不阻塞建单）。
  const sendToAI = async () => {
    if (!userIntent.trim()) {
      message.warning('请描述您的需求');
      return;
    }
    if (!checkAuth()) {
      return;
    }
    const utterance = userIntent.trim();
    setLoading(true);
    try {
      const result = await aiInterpretTicketIntent(utterance);
      // 理解确认阶段先清空上一次表单，避免旧表单在新理解被确认前被提交。
      setComponents([]);
      setSurfaceId(null);
      setSubmitted(false);
      setPendingIntent(utterance);
      setInterpretation(result);
      setUserIntent('');
    } catch (e) {
      console.error(e);
      await generateForm(utterance);
    } finally {
      setLoading(false);
    }
  };

  // 确认原样理解 → 生成表单（用户确认的字段作为初值）。
  const handleInterpretConfirm = () => {
    const applied = interpretation?.fields;
    const intentText = pendingIntent;
    setInterpretation(null);
    if (intentText) {
      void generateForm(intentText, applied);
    }
  };

  // 修改理解 → 用修正后的字段生成表单。
  const handleInterpretModify = (changes: IntentUnderstandingChanges) => {
    if (!interpretation) {
      return;
    }
    const applied: TicketIntentFields = { ...interpretation.fields, ...changes };
    const intentText = pendingIntent;
    setInterpretation(null);
    if (intentText) {
      void generateForm(intentText, applied);
    }
  };

  // 处理用户操作
  const handleAction = async (actionName: string, context: Record<string, unknown>) => {
    if (actionName === 'submit' && surfaceId) {
      if (!checkAuth()) {
        return;
      }
      setLoading(true);
      try {
        const data = await A2UIApi.handleTicketAction(actionName, surfaceId, context);

        // 处理响应消息
        (data.messages || []).forEach((msg: string) => {
          try {
            const parsed = JSON.parse(msg);
            if ('dataModelUpdate' in parsed) {
              const updates = parsed.dataModelUpdate.contents;
              const status = updates.find((u: { key: string }) => u.key === 'status')?.valueString;
              if (status === 'created') {
                setSubmitted(true);
                message.success('工单创建成功！');
                // 任务完成收口：先报字段纠正（误分类率代理），再报时长（latencyMs 同口径）。
                const ticket = (modelRef.current.ticket ?? {}) as AICreateSuggestedFields;
                (trackerRef.current as AICreateFlowTracker).finishSuccess(
                  { type: ticket.type, priority: ticket.priority },
                  pageKey
                );
              }
            }
            if ('deleteSurface' in parsed) {
              setComponents([]);
              setSurfaceId(null);
            }
          } catch (e) {
            console.error('Parse message error:', e);
          }
        });
      } catch (e) {
        message.error('提交失败');
        (trackerRef.current as AICreateFlowTracker).finishFailure('submit_failed', pageKey);
      } finally {
        setLoading(false);
      }
    }
  };

  const renderableIds = getRenderableIds();

  // 成功提交后的显示
  if (submitted) {
    return (
      <Card className="max-w-2xl mx-auto">
        <div className="text-center py-8">
          <CheckCircle className="w-16 h-16 text-green-500 mx-auto mb-4" />
          <h2 className="text-xl font-semibold mb-2">工单创建成功</h2>
          <p className="text-gray-500 mb-4">您的工单已成功提交</p>
          <Button
            type="primary"
            onClick={() => {
              setSubmitted(false);
              setComponents([]);
              setModel({
                ticket: { title: '', type: 'hardware', priority: 'medium', description: '' },
                ui: { canSubmit: false, status: '', statusColor: 'gray', errors: {} },
              });
            }}
          >
            创建新工单
          </Button>
        </div>
      </Card>
    );
  }

  return (
    <Card className="max-w-2xl mx-auto">
      {/* AI 输入区 */}
      <div className="mb-4 p-3 bg-gradient-to-r from-blue-50 to-purple-50 rounded-lg">
        <div className="flex items-center gap-2 mb-2">
          <Bot className="w-4 h-4 text-purple-500" />
          <span className="text-sm font-medium">描述需求，AI 自动生成表单</span>
        </div>
        <Space.Compact style={{ width: '100%' }}>
          <Input
            placeholder="例如：帮我申请一台 ThinkPad 笔记本，用于 Java 开发..."
            value={userIntent}
            onChange={e => setUserIntent(e.target.value)}
            onPressEnter={sendToAI}
          />
          <Button icon={<Send />} onClick={sendToAI} loading={loading}>
            发送
          </Button>
        </Space.Compact>
      </div>

      {/* V1-03 理解确认卡：「我理解成：网络故障（高）→ 确认 / 修改」；确认后才生成表单。 */}
      {interpretation ? (
        <IntentUnderstandingCard
          key={`${interpretation.summary}:${pendingIntent}`}
          interpretation={interpretation}
          onConfirm={handleInterpretConfirm}
          onModify={handleInterpretModify}
          pageKey={pageKey}
          loading={loading}
        />
      ) : null}

      {/* A2UI 表单渲染 */}
      {components.length > 0 ? (
        <Spin spinning={loading}>
          <Form form={form} layout="vertical">
            {renderableIds.map(id => {
              const comp = components.find(c => c.id === id);
              if (!comp) return null;
              return (
                <A2UIComponentRenderer
                  key={comp.id}
                  component={comp}
                  model={model}
                  onModelChange={handleModelChange}
                  onAction={handleAction}
                  allComponents={components}
                />
              );
            })}
          </Form>
        </Spin>
      ) : interpretation ? null : (
        <div className="text-center text-gray-400 py-8">
          <Bot className="w-12 h-12 mx-auto mb-2 opacity-50" />
          <p>描述您的需求，AI 将生成工单表单</p>
          <p className="text-xs mt-2">例如：帮我申请一台 ThinkPad 笔记本</p>
        </div>
      )}

      {/* 调试信息 */}
      {import.meta.env.DEV && components.length > 0 && (
        <details className="mt-4 text-xs text-gray-400">
          <summary>A2UI 调试信息</summary>
          <pre className="mt-2 p-2 bg-gray-100 rounded overflow-auto max-h-40">
            {JSON.stringify(components, null, 2)}
          </pre>
        </details>
      )}
    </Card>
  );
}

export default A2UIFormRenderer;
