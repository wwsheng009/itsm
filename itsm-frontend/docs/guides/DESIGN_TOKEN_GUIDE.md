# 设计令牌使用指南

## 目的
统一使用设计令牌，替换内联样式，提升代码可维护性和设计一致性。

## 设计令牌映射表

### 1. 颜色 (Colors)

| 内联样式 | 设计令牌（CSS 变量优先） | 用途 |
|---------|---------|------|
| `color: '#3b82f6'` | `var(--color-primary-text)` | 主色文字/图标（暗色自动提亮） |
| `color: '#52c41a'` | `var(--color-success-text)` | 成功文字/图标 |
| `color: '#faad14'` | `var(--color-warning-text)` | 警告文字/图标 |
| `color: '#ff4d4f'` | `var(--color-error-text)` | 错误文字/图标 |
| `color: '#1e293b'` | `var(--color-text-primary)` | 主文本色 |
| `color: '#64748b'` | `var(--color-text-secondary)` | 次要文本色 |
| `background: '#ffffff'` | `var(--color-surface-primary)` | 卡片/内容表面 |
| `background: '#f8fafc'` | `var(--color-page-background)` | 页面背景 |
| `borderColor: '#e2e8f0'` | `var(--color-border-primary)` | 边框颜色 |

> `colors.ts` / `DESIGN.colors` 是 JS/图表侧的颜色源；**UI 组件应优先使用 CSS 变量**，否则无法跟随暗色主题切换。

### 2. 间距 (Spacing)

| 内联样式 | 设计令牌 | 用途 |
|---------|---------|------|
| `padding: '4px'` | `spacing.xs` | 极小间距 |
| `padding: '8px'` | `spacing.sm` | 小间距 |
| `padding: '16px'` | `spacing.md` | 中等间距 |
| `padding: '24px'` | `spacing.lg` | 大间距 |
| `padding: '32px'` | `spacing.xl` | 超大间距 |
| `margin: '16px'` | `spacing.md` | 中等外边距 |
| `margin: '24px'` | `spacing.lg` | 大外边距 |

### 3. 圆角 (Border Radius)

| 内联样式 | 设计令牌 | 用途 |
|---------|---------|------|
| `borderRadius: '8px'` | `radius.sm` | 小圆角 |
| `borderRadius: '12px'` | `radius.md` | 中等圆角 |
| `borderRadius: '16px'` | `radius.lg` | 大圆角 |
| `borderRadius: '9999px'` | `radius.full` | 完整圆角（按钮、头像） |

### 4. 阴影 (Shadows)

| 内联样式 | 设计令牌 | 用途 |
|---------|---------|------|
| `boxShadow: '0 1px 3px 0 rgba(0, 0, 0, 0.1)'` | `shadows.card` | 卡片阴影 |
| `boxShadow: '0 4px 6px -1px rgba(0, 0, 0, 0.1)'` | `shadows.cardHover` | 卡片悬停阴影 |
| `boxShadow: '0 10px 40px -10px rgba(0,0,0,0.15)'` | `shadows.dropdown` | 下拉菜单阴影 |

### 5. 字体 (Typography)

| 内联样式 | 设计令牌 | 用途 |
|---------|---------|------|
| `fontSize: '0.75rem'` | `typography.fontSize.xs` | 极小字体 |
| `fontSize: '0.875rem'` | `typography.fontSize.sm` | 小字体 |
| `fontSize: '1rem'` | `typography.fontSize.base` | 基础字体 |
| `fontSize: '1.125rem'` | `typography.fontSize.lg` | 大字体 |
| `fontSize: '1.25rem'` | `typography.fontSize.xl` | 超大字体 |
| `fontFamily: 'Inter, ...'` | `typography.fontFamily.sans` | 无衬线字体 |
| `fontFamily: 'JetBrains Mono, ...'` | `typography.fontFamily.mono` | 等宽字体 |

### 6. 过渡 (Transitions)

| 内联样式 | 设计令牌 | 用途 |
|---------|---------|------|
| `transition: '150ms ease'` | `transitions.fast` | 快速过渡 |
| `transition: '300ms ease'` | `transitions.normal` | 正常过渡 |
| `transition: '500ms ease'` | `transitions.slow` | 慢速过渡 |

## 使用示例

### ❌ 错误示例（raw hex）
```tsx
<Card style={{
  padding: '24px',
  borderRadius: '12px',
  background: '#ffffff',
  boxShadow: '0 1px 3px 0 rgba(0, 0, 0, 0.1)'
}}>
  <Text style={{ color: '#1e293b', fontSize: '1.125rem' }}>标题</Text>
</Card>
```

### ✅ 正确示例（CSS 变量）
```tsx
<Card style={{
  padding: 24,
  borderRadius: 12,
  background: 'var(--color-surface-primary)',
  boxShadow: 'var(--shadow-sm)'
}}>
  <Text style={{ 
    color: 'var(--color-text-primary)',
    fontSize: 18
  }}>标题</Text>
</Card>
```

### 🎯 最佳实践（CSS Modules）
```tsx
// Component.module.css
.card {
  padding: var(--spacing-lg);
  border-radius: var(--radius-md);
  background: var(--color-surface);
  box-shadow: var(--shadow-card);
}

.title {
  color: var(--color-text);
  font-size: var(--font-size-lg);
}

// Component.tsx
<Card className={styles.card}>
  <Text className={styles.title}>标题</Text>
</Card>
```

## 替换策略

### 阶段1: 新代码使用设计令牌
- 所有新代码必须使用设计令牌
- Code Review时检查内联样式使用

### 阶段2: 逐步替换现有内联样式
- 优先替换高频使用的组件
- 按模块逐步替换

### 阶段3: 建立CSS Module系统
- 将复用样式提取到CSS Module
- 建立全局样式变量

## 检查工具

### 自动化检查
```bash
# 硬编码颜色审计（报告 + JSON）
npm run audit:colors
node scripts/audit-hardcoded-colors.mjs --json > /tmp/color-audit.json

# 查找内联 raw hex（快速抽查）
rg -n "color:\\s*'#|background(Color)?:\\s*'#|borderColor:\\s*'#" src
```

### ESLint规则
```json
{
  "rules": {
    "react/style-prop-object": "warn",
    "no-inline-styles/no-inline-styles": "warn"
  }
}
```

## 注意事项

1. **性能考虑**: 设计令牌不会影响运行时性能
2. **主题切换**: 使用设计令牌可以轻松实现主题切换
3. **维护性**: 集中管理样式，修改更方便
4. **一致性**: 确保整个应用视觉一致

## 下一步行动

1. 安装ESLint插件检查内联样式
2. 创建CSS Module模板
3. 逐步替换高频组件的内联样式
4. 建立样式Code Review规范

---

**更新日期**: 2026-11-13
**维护者**: ITSM前端团队
