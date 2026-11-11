# 238 Demo 部署证据（2026-11-11 · 1.8.0-dev.49a2ad11）

> **Status: historical** — 已被 `1.8.0-dev.ad878805` 取代，见
> [2026-11-11-demo-full-deploy-ad878805.md](./2026-11-11-demo-full-deploy-ad878805.md)。

> **状态**：success（部署成功；MT-4 回归暴露后续修复项）
> **环境**：`http://172.18.3.238:8089`
> **部署来源**：`49a2ad115b3732beaba994882130af40c7e88fa0`
> **部署 worktree**：`/home/n9e/itsm/repo-e2e-6745c998`，branch `deploy/full-49a2ad11`

```text
deployed_at=2026-11-11T11:28:35Z
version=1.8.0-dev.49a2ad11
containers=6/6 healthy
backup=/home/n9e/itsm/repo-e2e-6745c998/backups/demo-release-1.8.0-dev.49a2ad11-20261111T112258Z
backend_sha256=83501b911ffb33457f096ab5d3207152f8087b6acf5b90f8374acadaef152978
frontend_sha256=f22a65fe0675a307ed2f47085eff73e6e6ddad3a8cc242e96a77954b3400fa96
ai_service_sha256=f99367604826ed7fa1541bfa8868dee1eb7492b4307d4e55f050d35b6a2d3301
```

该版本包含 C 方案主体实现及 `jwt_tenant_source` 修复，MT-4 provider 代客建单通过。
后续 `ad878805` 仅追加“变更重复删除返回 404”的回归修复并重新全量发布。
