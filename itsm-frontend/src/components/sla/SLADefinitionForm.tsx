import React, { useEffect, useMemo, useState } from 'react';
import {
  Button,
  Checkbox,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Switch,
  TimePicker,
} from 'antd';
import dayjs, { type Dayjs } from 'dayjs';
import { SLAPriority, SLAPriorityLabels } from '@/constants/sla';
import { TicketTypeApi } from '@/lib/api/ticketTypeApi';

/**
 * SLA 定义共享表单（新建 / 编辑复用）。
 * 字段与后端 dto.CreateSLADefinitionRequest 对齐（itsm-backend/dto/sla_dto.go:8-19）。
 *
 * v1.3（方案 D-SLA-3）：serviceType 从自由文本改为**租户动态工单类型受控枚举**；
 * 留空 = 类型无关租户默认定义（L3c 兜底位）。
 *
 * P3（方案 D-SLA-6 §4.6）：补齐 businessHours（营业时间日历）/ escalationRules / conditions。
 * businessHours 键名与后端 `parseBusinessHoursConfig`（ticket_sla_service.go:407-411）一致：
 *   { work_days:[1..7], start_time:"HH:mm", end_time:"HH:mm", time_zone, is_24_7 }
 * 关闭"营业时间日历"= 空对象（24×7 连续计时，后端空日历语义）。
 */

export interface SLADefinitionFormValues {
  name: string;
  description?: string;
  serviceType?: string;
  priority: string;
  responseTime: number;
  resolutionTime: number;
  isActive: boolean;
  businessHours?: Record<string, unknown>;
  escalationRules?: Record<string, unknown>;
  conditions?: Record<string, unknown>;
}

interface SLADefinitionFormProps {
  initialValues?: Partial<SLADefinitionFormValues>;
  submitting?: boolean;
  submitText?: string;
  onSubmit: (values: SLADefinitionFormValues) => void | Promise<void>;
  onCancel: () => void;
}

/** 内部表单字段（与 API payload 形态解耦：UI 用结构化控件，提交时做转换）。 */
interface SLADefinitionFormInternal {
  name: string;
  description?: string;
  serviceType?: string;
  priority: string;
  responseTime: number;
  resolutionTime: number;
  isActive: boolean;
  businessHoursEnabled: boolean;
  workDays: number[];
  hoursRange: [Dayjs, Dayjs];
  timezone: string;
  conditionsPriorities?: string[];
  escalationRulesJson?: string;
}

const { TextArea } = Input;

const WEEKDAY_OPTIONS = [
  { label: '周一', value: 1 },
  { label: '周二', value: 2 },
  { label: '周三', value: 3 },
  { label: '周四', value: 4 },
  { label: '周五', value: 5 },
  { label: '周六', value: 6 },
  { label: '周日', value: 7 },
];

const TIMEZONE_OPTIONS = [
  { label: 'Asia/Shanghai（中国标准时间）', value: 'Asia/Shanghai' },
  { label: 'Asia/Hong_Kong', value: 'Asia/Hong_Kong' },
  { label: 'Asia/Tokyo', value: 'Asia/Tokyo' },
  { label: 'Asia/Singapore', value: 'Asia/Singapore' },
  { label: 'UTC', value: 'UTC' },
];

interface TypeOption {
  value: string;
  label: string;
}

/** 从 API 形态的 businessHours 反推内部控件初值。 */
const toInternalBusinessHours = (bh?: Record<string, unknown>) => {
  const hasWindow = !!bh && Object.keys(bh).length > 0 && bh.is_24_7 !== true;
  const workDays = Array.isArray(bh?.work_days) ? (bh?.work_days as number[]) : [1, 2, 3, 4, 5];
  const start = typeof bh?.start_time === 'string' ? (bh.start_time as string) : '09:00';
  const end = typeof bh?.end_time === 'string' ? (bh.end_time as string) : '18:00';
  const timezone = typeof bh?.time_zone === 'string' ? (bh.time_zone as string) : 'Asia/Shanghai';
  return {
    businessHoursEnabled: hasWindow,
    workDays,
    hoursRange: [dayjs(start, 'HH:mm'), dayjs(end, 'HH:mm')] as [Dayjs, Dayjs],
    timezone,
  };
};

const SLADefinitionForm: React.FC<SLADefinitionFormProps> = ({
  initialValues,
  submitting = false,
  submitText = '保存',
  onSubmit,
  onCancel,
}) => {
  const [form] = Form.useForm<SLADefinitionFormInternal>();
  const [typeOptions, setTypeOptions] = useState<TypeOption[]>([]);
  const [typesLoading, setTypesLoading] = useState(false);
  const [typeLoadFailed, setTypeLoadFailed] = useState(false);
  const serviceTypeValue = Form.useWatch('serviceType', form);
  const businessHoursEnabled = Form.useWatch('businessHoursEnabled', form);

  // 动态工单类型下拉（受控枚举）：读取当前租户的 active 类型配置。
  // 失败时退化为自由输入，避免阻塞定义维护（后端匹配按 code 精确比对）。
  useEffect(() => {
    let cancelled = false;
    (async () => {
      setTypesLoading(true);
      try {
        const resp = await TicketTypeApi.list({ status: 'active', pageSize: 200 });
        if (cancelled) return;
        const items = resp?.types ?? [];
        setTypeOptions(
          items.map(t => ({ value: t.code, label: `${t.name}（${t.code}）` }))
        );
      } catch {
        if (!cancelled) setTypeLoadFailed(true);
      } finally {
        if (!cancelled) setTypesLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const legacyValueNotice = useMemo(() => {
    if (!serviceTypeValue || typeOptions.length === 0) return null;
    return typeOptions.some(o => o.value === serviceTypeValue)
      ? null
      : '该值不在当前租户类型配置中（历史数据不生效，请重新选择）';
  }, [serviceTypeValue, typeOptions]);

  const initialBH = useMemo(() => toInternalBusinessHours(initialValues?.businessHours), [initialValues?.businessHours]);
  const initialEscalationJson = useMemo(
    () =>
      initialValues?.escalationRules && Object.keys(initialValues.escalationRules).length > 0
        ? JSON.stringify(initialValues.escalationRules, null, 2)
        : '',
    [initialValues?.escalationRules]
  );
  const initialConditions = useMemo(
    () => (Array.isArray(initialValues?.conditions?.priority) ? (initialValues?.conditions?.priority as string[]) : []),
    [initialValues?.conditions]
  );

  /** 表单初值：默认值 → 结构化 businessHours/escalation/conditions 回填 → API 标量字段回填。 */
  const formInitialValues = useMemo<Partial<SLADefinitionFormInternal>>(() => {
    const defaults: Partial<SLADefinitionFormInternal> = {
      priority: SLAPriority.P3,
      responseTime: 60,
      resolutionTime: 240,
      isActive: true,
      businessHoursEnabled: initialBH.businessHoursEnabled,
      workDays: initialBH.workDays,
      hoursRange: initialBH.hoursRange,
      timezone: initialBH.timezone,
      escalationRulesJson: initialEscalationJson,
      conditionsPriorities: initialConditions,
    };
    if (initialValues?.name !== undefined) defaults.name = initialValues.name;
    if (initialValues?.description !== undefined) defaults.description = initialValues.description;
    if (initialValues?.serviceType !== undefined) defaults.serviceType = initialValues.serviceType;
    if (initialValues?.priority !== undefined) defaults.priority = initialValues.priority;
    if (initialValues?.responseTime !== undefined) defaults.responseTime = initialValues.responseTime;
    if (initialValues?.resolutionTime !== undefined) defaults.resolutionTime = initialValues.resolutionTime;
    if (initialValues?.isActive !== undefined) defaults.isActive = initialValues.isActive;
    return defaults;
  }, [initialValues, initialBH, initialEscalationJson, initialConditions]);

  /** 内部字段 → API payload（§4.6：营业时间空对象=24×7）。 */
  const handleFinish = (raw: SLADefinitionFormInternal) => {
    const values: SLADefinitionFormValues = {
      name: raw.name,
      description: raw.description,
      serviceType: raw.serviceType,
      priority: raw.priority,
      responseTime: raw.responseTime,
      resolutionTime: raw.resolutionTime,
      isActive: raw.isActive,
    };
    if (raw.businessHoursEnabled) {
      values.businessHours = {
        is_24_7: false,
        work_days: raw.workDays ?? [],
        start_time: raw.hoursRange?.[0]?.format('HH:mm') ?? '09:00',
        end_time: raw.hoursRange?.[1]?.format('HH:mm') ?? '18:00',
        time_zone: raw.timezone || 'Asia/Shanghai',
      };
    } else {
      values.businessHours = {}; // 24×7 连续计时（后端空日历语义）
    }
    if (raw.escalationRulesJson && raw.escalationRulesJson.trim()) {
      try {
        const parsed = JSON.parse(raw.escalationRulesJson);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          values.escalationRules = parsed as Record<string, unknown>;
        }
      } catch {
        /* 已被校验器拦截；此处仅防御 */
      }
    }
    if (raw.conditionsPriorities && raw.conditionsPriorities.length > 0) {
      values.conditions = { priority: raw.conditionsPriorities };
    }
    return onSubmit(values);
  };

  return (
    <Form<SLADefinitionFormInternal>
      form={form}
      layout="vertical"
      onFinish={(raw) => void handleFinish(raw)}
      initialValues={formInitialValues}
      style={{ maxWidth: 720 }}
    >
      <Form.Item
        name="name"
        label="名称"
        rules={[{ required: true, message: '请输入 SLA 名称' }]}
      >
        <Input placeholder="例如：标准服务 SLA" maxLength={100} />
      </Form.Item>

      <Form.Item name="description" label="描述">
        <TextArea rows={3} placeholder="SLA 适用范围与说明（可选）" maxLength={500} />
      </Form.Item>

      <Space size="large" style={{ display: 'flex', alignItems: 'flex-start' }}>
        <Form.Item
          name="serviceType"
          label="服务类型（工单类型）"
          style={{ minWidth: 260 }}
          extra={
            legacyValueNotice ??
            '留空 = 类型无关租户默认（作为兜底定义生效）；否则按该类型的 code 精确匹配'
          }
          validateStatus={legacyValueNotice ? 'warning' : undefined}
        >
          {typeLoadFailed ? (
            <Input placeholder="类型列表加载失败，可手动输入类型 code" maxLength={50} />
          ) : (
            <Select
              allowClear
              showSearch
              loading={typesLoading}
              placeholder="选择工单类型（留空 = 类型无关默认）"
              options={typeOptions}
              optionFilterProp="label"
              style={{ width: 260 }}
              notFoundContent={typesLoading ? '加载中…' : '暂无可选类型（可留空）'}
            />
          )}
        </Form.Item>

        <Form.Item name="priority" label="优先级" rules={[{ required: true }]}>
          <Select
            style={{ width: 180 }}
            options={Object.values(SLAPriority).map(p => ({
              value: p,
              label: SLAPriorityLabels[p],
            }))}
          />
        </Form.Item>

        <Form.Item name="isActive" label="启用" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="停用" />
        </Form.Item>
      </Space>

      <Space size="large" style={{ display: 'flex', alignItems: 'flex-start' }}>
        <Form.Item
          name="responseTime"
          label="响应时限（分钟）"
          rules={[{ required: true, message: '请输入响应时限' }]}
        >
          <InputNumber min={1} max={100000} style={{ width: 180 }} />
        </Form.Item>

        <Form.Item
          name="resolutionTime"
          label="解决时限（分钟）"
          rules={[{ required: true, message: '请输入解决时限' }]}
        >
          <InputNumber min={1} max={100000} style={{ width: 180 }} />
        </Form.Item>
      </Space>

      <Form.Item
        name="businessHoursEnabled"
        label="营业时间日历"
        valuePropName="checked"
        extra="关闭 = 24×7 连续计时；开启后按所选工作日与时段累计计时（后端 businessHours 日历）"
      >
        <Switch checkedChildren="按时段" unCheckedChildren="24×7" />
      </Form.Item>

      {businessHoursEnabled && (
        <>
          <Form.Item name="workDays" label="工作日" rules={[{ required: true, message: '至少选择一个工作日' }]}>
            <Checkbox.Group options={WEEKDAY_OPTIONS} />
          </Form.Item>
          <Space size="large" style={{ display: 'flex', alignItems: 'flex-start' }}>
            <Form.Item
              name="hoursRange"
              label="工作时段"
              rules={[{ required: true, message: '请选择工作时段' }]}
            >
              <TimePicker.RangePicker format="HH:mm" minuteStep={5} />
            </Form.Item>
            <Form.Item name="timezone" label="时区">
              <Select style={{ width: 260 }} options={TIMEZONE_OPTIONS} />
            </Form.Item>
          </Space>
        </>
      )}

      <Form.Item
        name="conditionsPriorities"
        label="适用优先级（conditions）"
        extra="留空 = 不限定优先级；勾选后仅这些优先级命中本定义（conditions.priority）"
      >
        <Select
          mode="multiple"
          allowClear
          placeholder="不限"
          style={{ maxWidth: 420 }}
          options={Object.values(SLAPriority).map(p => ({ value: p, label: SLAPriorityLabels[p] }))}
        />
      </Form.Item>

      <Form.Item
        name="escalationRulesJson"
        label="升级规则（escalationRules，JSON 对象，可选）"
        rules={[
          {
            validator: async (_, value: string) => {
              if (!value || !value.trim()) return;
              try {
                const parsed = JSON.parse(value);
                if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                  throw new Error('not-object');
                }
              } catch {
                throw new Error('JSON 格式错误：需为 JSON 对象（如 {"levels": []}）');
              }
            },
          },
        ]}
      >
        <TextArea rows={4} placeholder='例如：{"levels": [{"pct": 80, "notify": "manager"}]}' />
      </Form.Item>

      <Form.Item style={{ marginTop: 8 }}>
        <Space>
          <Button type="primary" htmlType="submit" loading={submitting}>
            {submitText}
          </Button>
          <Button onClick={onCancel} disabled={submitting}>
            取消
          </Button>
        </Space>
      </Form.Item>
    </Form>
  );
};

export default SLADefinitionForm;
