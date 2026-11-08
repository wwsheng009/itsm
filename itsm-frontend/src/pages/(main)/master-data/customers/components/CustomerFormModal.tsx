import { useEffect, useState } from 'react';
import {
  App,
  Col,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Modal,
  Row,
  Select,
  Tabs,
} from 'antd';
import dayjs, { type Dayjs } from 'dayjs';
import type { User } from '@/lib/api/user-api';
import type { SLADefinition } from '@/lib/api/sla-api';
import {
  masterDataService,
  type CustomerWritePayload,
  type ServiceCustomer,
} from '@/lib/services/masterDataService';
import AddressFields from './AddressFields';

interface CustomerFormModalProps {
  open: boolean;
  customer: ServiceCustomer | null;
  users: User[];
  slaDefinitions: SLADefinition[];
  onCancel: () => void;
  onSaved: () => void;
}

interface CustomerFormValues {
  name: string;
  code?: string;
  shortName?: string;
  legalName?: string;
  enName?: string;
  aliases?: string[];
  historicalNames?: string[];
  taxId?: string;
  dunsNumber?: string;
  registrationNo?: string;
  emailDomains?: string[];
  customerType?: string;
  industry?: string;
  subIndustry?: string;
  sizeClass?: string;
  ownershipType?: string;
  tier?: string;
  vipLevel?: number;
  creditRating?: string;
  website?: string;
  mainPhone?: string;
  serviceEmail?: string;
  address?: Record<string, string>;
  timezone?: string;
  language?: string;
  serviceTier?: string;
  slaDefinitionId?: number;
  supportHours?: string;
  accountManagerId?: number;
  serviceOwnerId?: number;
  csManagerId?: number;
  oncallGroupId?: number;
  billingCurrency?: string;
  paymentTerms?: string;
  creditLimit?: number;
  invoiceTitle?: string;
  billingAddress?: Record<string, string>;
  lifecycleStage?: string;
  onboardedAt?: Dayjs;
  contractStartAt?: Dayjs;
  contractEndAt?: Dayjs;
  churnAt?: Dayjs;
  churnReason?: string;
  dataSource?: string;
  status?: string;
}

const customerTypeOptions = [
  { value: 'direct', label: '直客' },
  { value: 'group', label: '集团总部' },
  { value: 'subsidiary', label: '子公司' },
  { value: 'msp_endcustomer', label: 'MSP 最终客户' },
  { value: 'internal', label: '内部客户' },
];

const tierOptions = [
  { value: 'S', label: 'S' },
  { value: 'A', label: 'A' },
  { value: 'B', label: 'B' },
  { value: 'C', label: 'C' },
];

const lifecycleOptions = [
  { value: 'prospect', label: '潜在客户' },
  { value: 'onboarding', label: '开通中' },
  { value: 'active', label: '活跃' },
  { value: 'renewal', label: '续约中' },
  { value: 'churned', label: '已流失' },
  { value: 'archived', label: '已归档' },
];

const sizeClassOptions = [
  { value: 'tiny', label: '微型' },
  { value: 'small', label: '小型' },
  { value: 'medium', label: '中型' },
  { value: 'large', label: '大型' },
  { value: 'enterprise', label: '超大型' },
];

const ownershipOptions = [
  { value: 'soe', label: '国企' },
  { value: 'private', label: '民营' },
  { value: 'foreign', label: '外资' },
  { value: 'listed', label: '上市' },
  { value: 'government', label: '政府' },
  { value: 'ngo', label: '非营利' },
];

const userOptions = (users: User[]) =>
  users.map(user => ({
    value: user.id,
    label: `${user.name || user.username}（#${user.id}）`,
  }));

const dateValue = (value?: string) => (value ? dayjs(value) : undefined);

export default function CustomerFormModal({
  open,
  customer,
  users,
  slaDefinitions,
  onCancel,
  onSaved,
}: CustomerFormModalProps) {
  const { message } = App.useApp();
  const [form] = Form.useForm<CustomerFormValues>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    if (customer) {
      form.resetFields();
      form.setFieldsValue({
        name: customer.name,
        code: customer.code,
        shortName: customer.shortName,
        legalName: customer.legalName,
        enName: customer.enName,
        aliases: customer.aliases ?? [],
        historicalNames: customer.historicalNames ?? [],
        taxId: customer.taxId,
        dunsNumber: customer.dunsNumber,
        registrationNo: customer.registrationNo,
        emailDomains: customer.emailDomains ?? [],
        customerType: customer.customerType || 'direct',
        industry: customer.industry,
        subIndustry: customer.subIndustry,
        sizeClass: customer.sizeClass,
        ownershipType: customer.ownershipType,
        tier: customer.tier || 'C',
        vipLevel: customer.vipLevel,
        creditRating: customer.creditRating,
        website: customer.website,
        mainPhone: customer.mainPhone,
        serviceEmail: customer.serviceEmail,
        address: (customer.address ?? {}) as Record<string, string>,
        timezone: customer.timezone,
        language: customer.language || 'zh-CN',
        serviceTier: customer.serviceTier,
        slaDefinitionId: customer.slaDefinitionId ?? undefined,
        supportHours: customer.supportHours,
        accountManagerId: customer.accountManagerId ?? undefined,
        serviceOwnerId: customer.serviceOwnerId ?? undefined,
        csManagerId: customer.csManagerId ?? undefined,
        oncallGroupId: customer.oncallGroupId ?? undefined,
        billingCurrency: customer.billingCurrency,
        paymentTerms: customer.paymentTerms,
        creditLimit: customer.creditLimit ?? undefined,
        invoiceTitle: customer.invoiceTitle,
        billingAddress: (customer.billingAddress ?? {}) as Record<string, string>,
        lifecycleStage: customer.lifecycleStage || 'prospect',
        onboardedAt: dateValue(customer.onboardedAt),
        contractStartAt: dateValue(customer.contractStartAt),
        contractEndAt: dateValue(customer.contractEndAt),
        churnAt: dateValue(customer.churnAt),
        churnReason: customer.churnReason,
        dataSource: customer.dataSource,
        status: customer.status || 'active',
      });
      return;
    }
    form.resetFields();
    form.setFieldsValue({
      customerType: 'direct',
      tier: 'C',
      lifecycleStage: 'prospect',
      language: 'zh-CN',
      dataSource: 'manual',
      status: 'active',
      vipLevel: 0,
      aliases: [],
      historicalNames: [],
      emailDomains: [],
      address: {},
      billingAddress: {},
    });
  }, [customer, form, open]);

  const submit = async () => {
    const values = await form.validateFields();
    const payload: CustomerWritePayload = {
      name: values.name,
      code: values.code,
      shortName: values.shortName,
      legalName: values.legalName,
      enName: values.enName,
      aliases: values.aliases ?? [],
      historicalNames: values.historicalNames ?? [],
      taxId: values.taxId,
      dunsNumber: values.dunsNumber,
      registrationNo: values.registrationNo,
      emailDomains: values.emailDomains ?? [],
      customerType: values.customerType,
      industry: values.industry,
      subIndustry: values.subIndustry,
      sizeClass: values.sizeClass,
      ownershipType: values.ownershipType,
      tier: values.tier,
      vipLevel: values.vipLevel ?? 0,
      creditRating: values.creditRating,
      website: values.website,
      mainPhone: values.mainPhone,
      serviceEmail: values.serviceEmail,
      address: values.address ?? {},
      timezone: values.timezone,
      language: values.language,
      serviceTier: values.serviceTier,
      slaDefinitionId: values.slaDefinitionId ?? null,
      supportHours: values.supportHours,
      coverageScope: customer?.coverageScope ?? {},
      accountManagerId: values.accountManagerId ?? null,
      serviceOwnerId: values.serviceOwnerId ?? null,
      csManagerId: values.csManagerId ?? null,
      oncallGroupId: values.oncallGroupId ?? null,
      billingCurrency: values.billingCurrency,
      paymentTerms: values.paymentTerms,
      creditLimit: values.creditLimit ?? null,
      invoiceTitle: values.invoiceTitle,
      billingAddress: values.billingAddress ?? {},
      lifecycleStage: values.lifecycleStage,
      onboardedAt: values.onboardedAt?.toISOString() ?? null,
      contractStartAt: values.contractStartAt?.toISOString() ?? null,
      contractEndAt: values.contractEndAt?.toISOString() ?? null,
      churnAt: values.churnAt?.toISOString() ?? null,
      churnReason: values.churnReason,
      dataSource: values.dataSource ?? customer?.dataSource ?? 'manual',
      status: values.status,
      linkedCustomerTenantId: customer?.linkedCustomerTenantId ?? null,
      version: customer?.version,
    };
    setSaving(true);
    try {
      if (customer) {
        await masterDataService.updateCustomer(customer.id, payload);
        message.success('客户主数据已更新');
      } else {
        await masterDataService.createCustomer(payload);
        message.success('客户主数据已创建');
      }
      onSaved();
    } catch (error) {
      message.error((error as Error).message || '保存客户主数据失败');
    } finally {
      setSaving(false);
    }
  };

  const tabs = [
    {
      key: 'basic',
      label: '基本信息',
      forceRender: true,
      children: (
        <Row gutter={12}>
          <Col xs={24} md={12}>
            <Form.Item name="name" label="客户名称" rules={[{ required: true }]}>
              <Input placeholder="例如：北京某某科技有限公司" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="legalName" label="法定全称">
              <Input placeholder="合同、发票使用的全称" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="shortName" label="简称">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="enName" label="英文名">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="code" label="客户编码" extra="留空时自动生成 CUS-0001">
              <Input placeholder="CUS-0001" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item
              name="customerType"
              label="客户类型"
              rules={[{ required: true, message: '请选择客户类型' }]}
            >
              <Select options={customerTypeOptions} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="aliases" label="别名">
              <Select mode="tags" tokenSeparators={[',', '，']} placeholder="可多个" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="historicalNames" label="历史名称">
              <Select mode="tags" tokenSeparators={[',', '，']} placeholder="可多个" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="taxId" label="统一社会信用代码 / 税号">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="registrationNo" label="工商注册号">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="dunsNumber" label="邓白氏编码">
              <Input />
            </Form.Item>
          </Col>
        </Row>
      ),
    },
    {
      key: 'classification',
      label: '分类分级',
      forceRender: true,
      children: (
        <Row gutter={12}>
          <Col xs={24} md={12}>
            <Form.Item name="industry" label="行业">
              <Input placeholder="例如：制造业" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="subIndustry" label="细分行业">
              <Input />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="sizeClass" label="规模">
              <Select allowClear options={sizeClassOptions} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="ownershipType" label="所有制">
              <Select allowClear options={ownershipOptions} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="tier" label="等级" rules={[{ required: true }]}>
              <Select options={tierOptions} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="vipLevel" label="VIP 级别">
              <InputNumber min={0} max={5} className="w-full" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="creditRating" label="信用评级">
              <Input />
            </Form.Item>
          </Col>
        </Row>
      ),
    },
    {
      key: 'contact',
      label: '联系方式',
      forceRender: true,
      children: (
        <>
          <Row gutter={12}>
            <Col xs={24} md={12}>
              <Form.Item name="website" label="官网">
                <Input placeholder="https://" />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="mainPhone" label="总机">
                <Input />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="serviceEmail" label="服务受理邮箱">
                <Input placeholder="support@example.com" />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="emailDomains" label="主域名">
                <Select mode="tags" tokenSeparators={[',', '，']} placeholder="example.com" />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="timezone" label="时区">
                <Input placeholder="Asia/Shanghai" />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="language" label="通知语言">
                <Input placeholder="zh-CN" />
              </Form.Item>
            </Col>
          </Row>
          <AddressFields prefix={['address']} />
        </>
      ),
    },
    {
      key: 'service',
      label: '服务配置',
      forceRender: true,
      children: (
        <Row gutter={12}>
          <Col xs={24} md={12}>
            <Form.Item name="serviceTier" label="服务档位">
              <Input placeholder="基础 / 标准 / 尊享" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="slaDefinitionId" label="默认 SLA 定义">
              <Select
                allowClear
                showSearch
                optionFilterProp="label"
                options={slaDefinitions.map(item => ({
                  value: item.id,
                  label: `${item.name}（#${item.id}）`,
                }))}
              />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="supportHours" label="服务时间">
              <Select
                allowClear
                options={[
                  { value: '7x24', label: '7x24' },
                  { value: '5x8', label: '5x8' },
                  { value: 'custom', label: '自定义' },
                ]}
              />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="accountManagerId" label="客户经理">
              <Select allowClear showSearch optionFilterProp="label" options={userOptions(users)} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="serviceOwnerId" label="服务负责人">
              <Select allowClear showSearch optionFilterProp="label" options={userOptions(users)} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="csManagerId" label="客户成功经理">
              <Select allowClear showSearch optionFilterProp="label" options={userOptions(users)} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="oncallGroupId" label="默认值班组 ID">
              <InputNumber min={1} className="w-full" />
            </Form.Item>
          </Col>
        </Row>
      ),
    },
    {
      key: 'billing',
      label: '财务结算',
      forceRender: true,
      children: (
        <>
          <Row gutter={12}>
            <Col xs={24} md={12}>
              <Form.Item name="billingCurrency" label="结算币种">
                <Input placeholder="CNY" />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="paymentTerms" label="账期">
                <Select
                  allowClear
                  options={[
                    { value: 'prepaid', label: '预付' },
                    { value: 'net30', label: 'Net 30' },
                    { value: 'net60', label: 'Net 60' },
                    { value: 'net90', label: 'Net 90' },
                  ]}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="creditLimit" label="信用额度">
                <InputNumber min={0} className="w-full" />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="invoiceTitle" label="开票抬头">
                <Input />
              </Form.Item>
            </Col>
          </Row>
          <AddressFields prefix={['billingAddress']} />
        </>
      ),
    },
    {
      key: 'lifecycle',
      label: '生命周期',
      forceRender: true,
      children: (
        <Row gutter={12}>
          <Col xs={24} md={12}>
            <Form.Item name="lifecycleStage" label="阶段" rules={[{ required: true }]}>
              <Select options={lifecycleOptions} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="status" label="状态">
              <Select
                options={[
                  { value: 'active', label: '活跃' },
                  { value: 'inactive', label: '停用' },
                  { value: 'suspended', label: '暂停' },
                ]}
              />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="onboardedAt" label="开通时间">
              <DatePicker showTime className="w-full" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="contractStartAt" label="合同开始">
              <DatePicker showTime className="w-full" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="contractEndAt" label="合同结束">
              <DatePicker showTime className="w-full" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item name="churnAt" label="流失时间">
              <DatePicker showTime className="w-full" />
            </Form.Item>
          </Col>
          <Col xs={24}>
            <Form.Item name="churnReason" label="流失原因">
              <Input.TextArea rows={2} />
            </Form.Item>
          </Col>
        </Row>
      ),
    },
  ];

  return (
    <Modal
      title={customer ? '编辑客户主数据' : '新增客户主数据'}
      open={open}
      onOk={submit}
      onCancel={onCancel}
      confirmLoading={saving}
      width={980}
      destroyOnHidden
    >
      <Form form={form} layout="vertical">
        <Tabs items={tabs} />
      </Form>
    </Modal>
  );
}
