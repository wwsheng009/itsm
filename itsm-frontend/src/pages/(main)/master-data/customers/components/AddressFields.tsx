import { Col, Form, Input, Row } from 'antd';

interface AddressFieldsProps {
  prefix: string[];
}

const fields = [
  { key: 'country', label: '国家/地区', placeholder: '中国' },
  { key: 'province', label: '省/直辖市', placeholder: '上海市' },
  { key: 'city', label: '城市', placeholder: '上海市' },
  { key: 'district', label: '区/县', placeholder: '浦东新区' },
  { key: 'street', label: '详细地址', placeholder: '张江路 1 号' },
  { key: 'postalCode', label: '邮编', placeholder: '201203' },
];

export default function AddressFields({ prefix }: AddressFieldsProps) {
  return (
    <Row gutter={12}>
      {fields.map(field => (
        <Col key={field.key} xs={24} md={8}>
          <Form.Item name={[...prefix, field.key]} label={field.label}>
            <Input placeholder={field.placeholder} />
          </Form.Item>
        </Col>
      ))}
    </Row>
  );
}
