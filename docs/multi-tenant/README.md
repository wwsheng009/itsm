# 多租户 · 多客户单一服务商（MSP）运营手册

> **状态**：当前
> **更新日期**：2026-09-28
> **选型依据**：[ADR-004：多客户管理场景租户模型选型](../architecture/adr-004-multi-customer-tenant-model-selection.md)

## 目标读者与适用场景

本目录面向"**一家服务商（MSP）同时服务多个客户，且客户需要登录系统自行处理业务**"的部署与运营场景，读者包括：

- 平台架构师：评估租户模型、隔离边界与扩展点；
- 部署/运维工程师：完成 `saas_msp` 模式部署、配置与验证；
- 服务商管理员：开通客户租户、分配工程师、日常跨客户运营；
- 客户方管理员：在自有租户内管理用户、角色与业务数据。

不适用于：单企业内部使用（`private` 模式，见 `docs/install.md`）、纯 SaaS 自助注册（`saas` 模式）。

## 核心模型（一句话）

**服务商 = 一个 `msp_provider` 租户；每个客户 = 一个 `msp_customer` 租户；服务商员工通过 `MSPAllocation` 被授权到多个客户租户，经 `X-Customer-Tenant-ID` 或切换租户跨客户操作。**

## 文档导航

| 文档 | 内容 | 主要读者 |
|---|---|---|
| [01-architecture.md](./01-architecture.md) | 场景与总体架构：租户模型、隔离四层、MSP 授权模型、跨租户数据流、设计约束 | 架构师 / 研发 |
| [02-deployment-and-configuration.md](./02-deployment-and-configuration.md) | 部署与配置：`saas_msp` 模式、环境变量、RLS 灰度、tenant guard、验证清单 | 运维 / 部署 |
| [03-customer-dimension.md](./03-customer-dimension.md) | 客户维度：客户租户生命周期、字段配置、客户用户与 RBAC、登录解析、退租 | 服务商管理员 / 客户管理员 |
| [04-provider-dimension.md](./04-provider-dimension.md) | 服务商维度：provider 租户、`msp_role`、分配与回收、跨客户操作与权限矩阵 | 服务商管理员 |
| [05-usage-guide.md](./05-usage-guide.md) | 使用指南：客户端与 MSP 端操作路径（控制台/接口/CLI）与典型流程 | 全体使用者 |
| [06-verification-and-troubleshooting.md](./06-verification-and-troubleshooting.md) | 验证与排障：验收清单、常见问题、错误码与定位方法 | 运维 / 测试 |
| [07-known-gaps.md](./07-known-gaps.md) | 已确认产品缺口：实测现象、运维规避、建议修复与优先级 | 产品 / 研发 / 运维 |

## 角色与术语

| 术语 | 含义 |
|---|---|
| 租户（Tenant） | 最高级数据归属与隔离边界；`ent/schema/tenant.go` |
| 服务商租户 | `type = msp_provider`，承载服务商员工与其 MSP 管理面 |
| 客户租户 | `type = msp_customer`，承载一个客户的用户与全部业务数据 |
| MSPAllocation | 服务商员工 ↔ 客户租户的分配记录（`primary/backup/specialist`，含 `deassigned_at`） |
| `X-Customer-Tenant-ID` | 服务商员工跨客户操作时携带的目标客户租户标识，必须命中其分配列表 |
| `msp_role` | 服务商员工在 provider 租户内的角色（如 `provider_admin` / `provider_agent`） |
| 部署模式 | `private` / `saas` / `saas_msp`；仅后两者开放 `/api/v1/msp/*` |

## 与既有文档的关系

- 选型结论与行动项：[ADR-004](../architecture/adr-004-multi-customer-tenant-model-selection.md)；
- 技术全景与运营案例：`docs/articles/05-multi-tenant-msp-operations.md`；
- 生产初始化与发布：[production-initialization.md](../delivery/production-initialization.md)；
- CMDB 可见性口径：[ADR-003](../architecture/adr-003-cmdb-tenant-wide-default.md)（租户内共享、租户间隔离）。

## 运维脚本（scripts/msp/）

| 脚本 | 作用 |
|---|---|
| `scripts/msp/build-provision-tenant.sh` | 构建 `provision_tenant` 二进制（内网走 goproxy.cn），产物 `$HOME/itsm-artifacts/provision_tenant_linux_amd64`（snap docker 可见路径） |
| `scripts/msp/setup-msp-tenants.sh` | 一键初始化：租户创建 → 模板供给 → MSP 授权 → 首个用户 → 分配 → 隔离性验证（幂等） |

使用方式与阶段说明见 [02 文档 §10](./02-deployment-and-configuration.md)；实测输出见 [06 文档 §7](./06-verification-and-troubleshooting.md)。

## 维护约定

- 本目录为长期文档，文件名不带日期；变更须同步更新受影响文档与 ADR-004 的行动项状态；
- 引用代码请标注 `文件:行号`，行号随重构漂移时应以最新代码为准并更新引用。
