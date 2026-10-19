import React, { useState } from 'react';
import { Card, Space } from 'antd';
import A2UIFormRenderer from '@/components/a2ui/A2UIFormRenderer';
import SelfServiceAssistant from '@/components/ai/self-service/SelfServiceAssistant';

/**
 * V0-02 员工侧「一句话建单」页。
 *
 * 结构：先自助（知识库问答，命中可自助解决）→ 未解决再把问题描述带入 AI 建单表单
 * （`A2UIFormRenderer` 受控 intent；生成草稿 → 用户核对/修改 → 提交，不自动提交）。
 * 度量：`self_service_*` 与 `ai_create_*` 事件（自助解决率/建单时长/误分类率代理）见 `lib/ai/ai-create-metrics`。
 */
export default function AICreateTicketPage() {
  // 自助问答「去建单」写入 intent；表单生成后清空（受控）。
  const [intent, setIntent] = useState('');

  return (
    <div className="max-w-4xl mx-auto p-6">
      <div className="mb-6">
        <h2 className="text-2xl font-bold text-gray-900 m-0">AI 创建工单</h2>
        <p className="text-gray-500 mt-1">
          先用自助问答查一下；没有答案再用一句话建单（AI 生成表单，提交前可核对/修改）
        </p>
      </div>

      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <SelfServiceAssistant pageKey="ticket.aiCreate" onProceedToCreate={setIntent} />
        <div id="ai-create-form">
          <Card>
            <A2UIFormRenderer intent={intent} onIntentChange={setIntent} pageKey="ticket.aiCreate" />
          </Card>
        </div>
      </Space>
    </div>
  );
}