// 工作流画布组件
// Workflow Canvas Component - BPMN 设计器画布

import React, { forwardRef, lazy, Suspense } from 'react';
import { Spin } from 'antd';
import type { BpmnDesignerApi, BpmnNodeSelection } from '../BPMNDesigner';

// 动态导入 BPMN 设计器 - bpmn-js 库较大，按需加载
const BPMNDesignerLazy = lazy(() => import('../BPMNDesigner'));

const BPMNDesigner: React.FC<React.ComponentProps<typeof BPMNDesignerLazy>> = props => (
  <Suspense
    fallback={
    <div className="flex items-center justify-center h-full">
      <Spin size="large" description="加载流程设计器..." />
    </div>
    }
  >
    <BPMNDesignerLazy {...props} />
  </Suspense>
);

interface WorkflowCanvasProps {
  currentXML: string;
  onSave: (xml: string) => void;
  onChange: (xml: string) => void;
  onSelectionChange?: (selection: BpmnNodeSelection | null) => void;
  onSerializeError?: (error: string | null) => void;
}

/**
 * 用模块级 ref 桥接动态加载的 BPMNDesigner 与命令式 API。
 * 因为 lazy 组件无法直接转发 ref，使用 module-scope ref 通信。
 */
const _apiRef: { current: BpmnDesignerApi | null } = { current: null };

const WorkflowCanvas = forwardRef<BpmnDesignerApi, WorkflowCanvasProps>(function WorkflowCanvas(
  { currentXML, onSave, onChange, onSelectionChange, onSerializeError },
  _ref
) {
  return (
    <div className="h-[calc(100vh-200px)] bg-white rounded-lg shadow-sm border border-gray-200 overflow-hidden">
      <BPMNDesigner
        xml={currentXML}
        onSave={onSave}
        onChange={onChange}
        onSelectionChange={onSelectionChange}
        onSerializeError={onSerializeError}
        apiRef={_apiRef}
      />
    </div>
  );
});

/**
 * 父组件调用此函数获取 BPMNDesigner 的命令式 API 句柄
 */
export function getBpmnDesignerApi(): BpmnDesignerApi | null {
  return _apiRef.current;
}

export default WorkflowCanvas;
