# 演示数据集种子器（scripts/demo）

多租户演示环境的数据播种工具：为独立演示栈（`docker-compose.demo.yml`，入口 `:8089`）
创建/补齐一套完整演示数据（租户、账号、主数据、全状态业务单据、跨租户协同）。

## 用法

```bash
python scripts/demo/seed_demo_dataset.py \
  --base-url http://172.18.3.238:8089 \
  --admin-password 'Demo@2026!Admin'
```

常用参数：

| 参数 | 说明 |
|---|---|
| `--base-url` | 演示入口地址（默认 `http://172.18.3.238:8089`；支持 `DEMO_BASE_URL`） |
| `--admin-user` | 平台管理员用户名（默认 `admin`） |
| `--admin-password` | 平台管理员口令（或环境变量 `DEMO_ADMIN_PASSWORD`） |
| `--user-password` | 租户演示账号统一口令（默认 `Demo@2026!`；`DEMO_USER_PASSWORD`） |
| `--stages` | 逗号分隔阶段：`tenants,users,allocations,masterdata,records,cross,verify` |
| `--dry-run` | 只打印将要创建的内容，不发写请求 |
| `--verbose` | 打印每次 HTTP 调用 |
| `--report` | JSON 报告输出路径（默认 `scripts/demo/out/demo_seed_report.json`） |

## 阶段说明

1. **tenants**：创建演示服务商 + 3 客户租户并执行「租户模板开通」（幂等，按 code 去重）；
2. **users**：平台通道建号（15 个多角色/岗位账号；`USERNAME_EXISTS` 视为已存在）；
3. **allocations**：MSP 分配（7 项；按现值集合去重）；
4. **masterdata**：知识库文章（发布）+ CMDB 配置项；
5. **records**：业务单据（全 7 状态 + 评论 + MSP 托管标记）；状态与规格不符时删除重建（自愈）；
6. **cross**：跨租户协同（服务商在工作台对 MSP 托管单回复 + 推进状态；按标记去重）；
7. **verify**：按角色视角清点（服务台/员工/管理员/服务商工作台）并输出核对表。

## 数据集定义位置

- 租户/用户/分配：见文件 `# 1.` 段（`TENANTS` / `USERS` / `ALLOCATIONS`）；
- 知识库/CMDB：见 `# 4.` 段（`KB_ARTICLES` / `CI_SAMPLES`）；
- 业务单据：见 `# 4.` 段（`TICKET_SPECS`：requester 身份、assignee 归属、状态路径、评论）。

## 契约备注（实测）

- 写请求需 CSRF：每次写前重新获取 `/api/v1/csrf-token`（令牌写后轮换）；
- 登录限流默认 10 次/分钟：脚本内置会话缓存与 `retryAfterSeconds` 退避重试；
- 工单状态机（单一事实源 `common.IsValidTicketStatusTransition`）：
  `new→open/assigned/in_progress/cancelled`；`open→in_progress/pending/resolved/cancelled`；
  `pending→in_progress/resolved/open/cancelled`；`resolved→closed/in_progress/open`；
  `closed/cancelled` 为终态；
- 状态入口：`PUT /tickets/:id/status`、`POST /tickets/:id/resolve|close`；
  MSP 托管单走 `POST /msp/tickets/:id/status|reply`（终态/无变化会被拒绝，脚本已规避）。
