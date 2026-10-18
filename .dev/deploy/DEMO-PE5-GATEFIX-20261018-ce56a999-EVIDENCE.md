# 238 demo 部署证据：项目列表「激活」失败提示修复（2026-10-18，1.8.0-dev.ce56a999）

## 问题

在 `http://172.18.3.238:8089/projects` 点击「激活」，页面仅提示「操作失败，请刷新后重试」，无法得知失败原因。

## 根因

- PE-12 状态机要求 `planning → active` 前**立项门（initiation）必须通过**；项目 #3 尚未提交立项门。
- 后端返回 `409 {"code":4090,"message":"阶段门门禁未通过","data":{"gate":"initiation",...}}`（实际文案为「阶段**门门**禁未通过」）。
- 前端列表页 `runTransition` 只透传含「未终结」的错误，其余一律替换为通用文案，导致真实的门禁原因与处理路径被吞掉。

## 修复

- `itsm-frontend/src/pages/(main)/projects/index.tsx`：
  - 剥离 `[RID: ...]` 后缀后识别阶段门禁错误，改为 `Modal.warning` 展示真实原因 + 明确指引（激活=立项门，结项=结项门），提供「去项目工作台」按钮直达 `/projects/:id`；
  - 「未终结 / 版本冲突 / 刷新」等已知错误直接透出后端消息；
  - 兼容后端「阶段门门禁未通过」与「阶段门禁未通过」两种文案写法。
- 提交：`3266f4e3`（首版提示 + 引导）、`ce56a999`（修正双「门」文案匹配）。

## 部署

| 步骤 | 内容 | 结果 |
|---|---|---|
| bundle | `itsm-pe5fix2-ce56a999.bundle`（基线=远端 3266f4e3） | checkout OK |
| A | `.env.demo VERSION=1.8.0-dev.ce56a999` | `A-STATUS: DONE` |
| B | 重建 backend + frontend | `BUILD_EXIT=0` |
| C | `up -d backend/worker/frontend` + init | 三容器 healthy，init ExitCode=0 |
| 运行态 | `itsm-backend-demo / itsm-worker-demo / itsm-frontend-demo` | 均 `1.8.0-dev.ce56a999` |
| version | `GET /api/v1/version` | `1.8.0-dev.ce56a999` |
| 健康 | `/` 与 `/api/v1/health` | 200 / 200 |

## 浏览器验证（Tabbit，真实会话）

| 项 | 结果 |
|---|---|
| 打开 `/projects?v=ce56a999` | 3 个项目，2 个「激活」入口 |
| 点击第一个「激活」 | 弹出确认弹窗（`modalCount=1`） |
| 弹窗文案 | `无法激活：立项门未通过 / 阶段门门禁未通过 / 请先在项目工作台「阶段门」提交并通过立项门（需项目经理任命 + 成员登记）。` |
| 弹窗按钮 | `去项目工作台` |
| 点击按钮 | 跳转 `http://172.18.3.238:8089/projects/3` ✅ |
| 镜像 A/B | `去项目工作台` 产物：旧 `cad4362e`=0 → 新 `ce56a999`=1 |

> 说明：本次只修「失败提示与引导」，不绕过 PE-12 门禁。项目 #3 仍需在工作台「阶段门」提交并通过立项门后才能激活（项目经理任命 + 成员登记；无审批绑定时按既有逻辑自动通过）。

## 复跑方式

```bash
python .dev/deploy/remote.py --sudo "NEW=1.8.0-dev.ce56a999 OLD_BE=1.8.0-dev.3266f4e3 OLD_FE=1.8.0-dev.3266f4e3 bash /tmp/deploy238_pe5_c.sh"
# 浏览器：/projects 点击「激活」→ 期望弹窗「无法激活：立项门未通过」+「去项目工作台」
```