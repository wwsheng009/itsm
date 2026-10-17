# Demo 部署证据 — 238（2026-10-17）

- Git SHA：`e345ea15`（分支 `deploy-fix-e345ea15`，源分支 `feat/msp-p0-hardening`）
- 版本 tag：`1.8.0-dev.e345ea15`
- 部署模式：docker-compose.demo（itsm-demo，8089）
- 镜像：`itsm-backend:1.8.0-dev.e345ea15`（`BUILD_EXIT=0`）；frontend / ai 未重建，按旧镜像 retag 到新版本号

## 部署内容（1 commit：f4a04b6f..e345ea15）

| 域 | 内容 |
|---|---|
| skills 分发面 | `serveSkillsFile` 响应头顺序修复（`Content-Disposition` / `X-SHA256` / `Content-Length` 此前在真实响应中全丢）；新增 `GET /skills-dist/manifest` 裸清单端点；install.md 生成器补齐端点一览、schemaVersion、宿主映射表、`--skills-root`、验收清单、升级/回滚 |
| 安装器 | 信封兼容 + 取不到 skills 显式报错；`--skills-root`；`--base` 三端点回落；安装后内建自检；全量包根级 `_lib` 校验兜底 |
| 知识库 | Markdown 正文不再被 HTML 实体化（按 contentType 分流消毒）；`normalizeArticleContentType` 前移；`Detect` 忽略围栏代码块 |
| 其它 | 资源中心菜单 permission_code 守卫用例转绿；知识库 API 参考文档 §10 更新；dist/skills 重新打包（`2026-10-17.e345ea15`） |

## 部署过程

| 步骤 | 命令 / 动作 | 结果 |
|---|---|---|
| 1 | 本地 commit `e345ea15` | 10 files, +609/−47 |
| 2 | `git bundle create f4a04b6f..HEAD` → `remote.py --put` | 21 KB 增量包上传 |
| 3 | 远端 `git fetch /tmp/itsm-fix.bundle HEAD:refs/heads/deploy-fix-e345ea15` + `checkout` | HEAD=`e345ea153`，工作区无冲突 |
| 4 | `tar -xzf skills-dist2.tgz -C .`（产物落到 `repo/skills/`，compose 挂 `./skills:/skills-dist:ro`） | manifest sha `8a7879f7…` 与本地一致 |
| 5 | `VERSION=1.8.0-dev.e345ea15 docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-backend`（setsid 后台） | `BUILD_EXIT=0`，约 4 分钟 |
| 6 | `.env.demo` VERSION 切换 + frontend/ai retag + `up -d itsm-backend-demo itsm-worker-demo` | 两容器 Recreated → healthy |

## 验证证据

| 项 | 结果 |
|---|---|
| 容器 | `itsm-backend-demo` / `itsm-worker-demo` 均 `itsm-backend:1.8.0-dev.e345ea15` healthy |
| 站点 | `http://127.0.0.1:8089/` → 200；前端/ai/nginx/redis 原容器保持 healthy |
| `/api/v1/skills-dist/manifest` | 200，裸 JSON（无 `code`/`data` 字段），`sourceCommit=e345ea15…`，`version=2026-10-17.e345ea15` |
| `/api/v1/skills-dist/registry` | 200，统一信封，`sourceCommit` 同上 |
| 下载响应头 | `Content-Type: application/zip`、`Content-Length`、`Content-Disposition: attachment; filename="…zip"`、`X-Sha256` **全部存在**（修复前仅 Content-Type，靠 Go 内容嗅探碰巧正确） |
| 摘要交叉校验 | `X-Sha256` == `manifest.skills[].sha256`（knowledge 包 `d599c782…`，ticket 包 `e91f073b…`），且与本地 `dist/skills/manifest.json` 逐字段一致 |
| `/skills-dist/install.md` | 含 `schemaVersion=1`、`/bundle/download`、`--skills-root`、升级与回滚段 |
| 知识库实体化 | 新建 markdown 探针文章（id 27）：`d = {"skills": [1]}`、`a & b`、`< 10`、`> 0`、直引号 **全部原样落库**，无 `&#34;` / `&amp;` / `&lt;`；探针已删除 |
| 回归 | 既有文章 id 26 仍可读取（200） |

## 部署后追加修复（部署验证时暴露）

用线上清单跑一次真实升级（`--base … --all --skills-root`）时，安装器报 `包不存在: …\Temp\itsm-skills-xxxx\itsm-change-operations.zip`：

- **根因**：`--base` 模式只拉清单、不拉 zip，`install_one` 随后在临时目录找包直接失败——正是此前沉淀的「自动下载名不副实」缺口，文档写一条命令装完，实际跑不通。
- **修复**：新增 `ensure_package()`，`--base` 模式下缺失即按 `/<skill>/download` 或 `/bundle/download` 拉取（仍走 sha256 校验）；`install_one` 与 bundle 分支统一走它。
- **上线**：安装器单文件重新上传到 `repo/skills/`（无需重启，bind mount 按请求读取），线上 `sha256=43a3f44a…` 与本地一致。
- **端到端验证**：`--base http://172.18.3.238:8089/api/v1/skills-dist --all --skills-root ~/.workbuddy-ai/skills` → 8 个技能从 `2026-10-17.d510b4b4` 升级到 `2026-10-17.e345ea15`，旧版自动备份进 `.skills-backup/`，自检 **8 通过 / 0 失败**。

## 遗留项

1. **前端/ai 未重建**：仅 retag 到新版本号，`itsm-frontend-demo` 仍跑 `1.8.0-dev.skills-install` 的镜像内容（本次无前端改动）；`itsm-ai-service-demo` 容器仍是 `22ee3603`（历史上 `.env.demo` 改版未重启，属既有不一致）。
2. **生产栈 8088 未部署**：本次仅 demo。生产栈仍为 `1.8.0-dev.a29ad103`，如需同步按同流程 `up -d itsm-backend-prod itsm-worker-prod`。
3. **`scripts/package-skills.ps1` 在 Windows PowerShell 5.1 下不可直接运行**（UTF-8 无 BOM 导致中文乱码；`Get-ChildItem | Remove-Item` 管道绑定失败；结尾 `Remove-Item -Recurse` 触发批量删除守卫）。本次用临时副本（转 BOM + `ForEach-Object { Remove-Item -LiteralPath }`）跑通后删除，源脚本未改，建议后续根治。
4. **远端仓库未推 origin**：`deploy-fix-e345ea15` 仅存在于 238 本地；`feat/msp-p0-hardening` 分支未 push。
5. 磁盘 90%（11G 空闲），下次构建前留意。
