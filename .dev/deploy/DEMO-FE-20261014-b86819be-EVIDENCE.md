# 238 demo 栈前端更新证据：Dashboard KPI 磁贴点击下转（2026-10-14）

## 变更内容

提交 `b86819be`：`feat(dashboard): KPI 磁贴点击下转对应处理界面（工单状态过滤/SLA/报表）`

- `KPIMetric` 新增 `targetPath` 字段（dashboard.types.ts）
- 8 个 KPI 磁贴配置下转目标（useDashboardData.ts）：
  - 总工单数 → `/tickets?tab=list`
  - 待处理 → `/tickets?tab=list&status=open`
  - 处理中 → `/tickets?tab=list&status=in_progress`
  - 已完成 → `/tickets?tab=list&status=resolved`
  - 超时工单 → `/tickets?tab=list&overdue=true`
  - SLA 达成率 → `/sla-monitor`
  - 平均响应/解决时间 → `/reports`
- KPI 卡片支持点击/hover/键盘可访问（KPICards.tsx）
- `/tickets` 页新增 `status` 深链解析（tickets/index.tsx，`TicketStatus` 枚举白名单校验）

## 部署路径（.dev/deploy 既有 L3/B2 模式）

| 步骤 | 脚本 | 结果 |
|---|---|---|
| A: fetch/checkout + .env.demo VERSION + retag backend/ai | `deploy238_demo_fe_a.sh` | b86819be checked out；VERSION=1.8.0-dev.b86819be；retag ok |
| B: 构建 itsm-frontend:$NEW（prod compose 上下文 + .env.prod） | `deploy238_demo_fe_b.sh` | BUILD_EXIT=0 |
| C: up -d 重建 itsm-frontend-demo + 健康检查 | `deploy238_demo_fe_c.sh` | healthy；front-http=200 |

## 验证证据（238 侧）

- `docker inspect itsm-frontend-demo` → `itsm-frontend:1.8.0-dev.b86819be`
- A/B 对比（`grep -rl targetPath /usr/share/nginx/html/assets/`）：
  - 旧镜像 `1.8.0-dev.22ee3603`：**0** 处
  - 新镜像 `1.8.0-dev.b86819be`：**1** 处（dashboard 懒加载 chunk `index-d1ZJg9ui.js`）
- `http://127.0.0.1:8089/` → 200，标题「AI-Native ITSM - AI驱动的IT服务管理系统」
- `/login` → 200
- `itsm-backend-demo` readyz（127.0.0.1:8091）→ 200
- demo 栈 7 容器全部 healthy（backend/frontend/ai-service 已切到 b86819be 镜像；
  worker 镜像 tag 仍显示 22ee3603 为 up -d 未重建，代码同源无差异——如需对齐可
  `docker compose --env-file .env.demo -f docker-compose.demo.yml up -d itsm-worker-demo`）
- 本机直连 `http://172.18.3.238:8089/` → 200

## 注意

- demo 管理员密码以 `.env.demo` 的 `DEMO_ADMIN_PASSWORD` 为准（库内实际口令以当时 init 播种值为准）。
- 磁贴下转效果为纯前端行为，浏览器强刷（Ctrl+Shift+R）即可看到（SPA 资源带 hash 指纹）。
