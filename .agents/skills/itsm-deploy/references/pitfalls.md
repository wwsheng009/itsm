# 部署陷阱手册（references/pitfalls.md）

按踩坑概率排序。部署失败时先对照本表。

## P1: git bundle 前置提交缺失（FETCH_FAILED）

**症状**：远端 `git fetch bundle` 报 `Repository lacks these prerequisite commits: <sha>`。

**原因**：本地打 bundle 时指定的基线提交，远端仓库没有。本地比远端多出中间提交（如 docs 提交未同步）。

**修复**：以**远端当前 HEAD 的 short-sha** 为 bundle 基线重新打包：
```powershell
git bundle create ".dev/deploy/itsm-<tag>.bundle" <remote-head-sha>..HEAD
git bundle list-heads ".dev/deploy/itsm-<tag>.bundle"   # 验证包含 HEAD
```
上传后执行时**显式传 `BUNDLE=` 环境变量**（部分脚本默认文件名与实际不符）。

## P2: Windows 脚本 CRLF 导致远端 bash 报错

**症状**：`/tmp/xxx.sh: /bin/bash^M: bad interpreter` 或莫名语法错误。

**修复**：上传后执行前先转换：
```bash
sed -i 's/\r$//' /tmp/xxx.sh && bash /tmp/xxx.sh
```
或用 `remote.py --put`（二进制透传）+ `sed`，不要用会转码的通道。

## P3: demo compose 无 build 段（"no such service: itsm-frontend"）

**症状**：`docker compose -f docker-compose.demo.yml build itsm-frontend` 报 `no such service` 或 `BUILD_EXIT=1` 立即失败。

**原因**：`docker-compose.demo.yml` 只定义 image 引用（`image: itsm-frontend:${VERSION}`），**没有 build 段**。构建入口在 `docker-compose.prod.yml`（`context: ./itsm-frontend`）。

**修复**：构建用 prod compose + `.env.prod` env-file（demo 的 `.env.demo` 缺 JWT_SECRET/MINIO 等必填变量会报 interpolating 错）：
```bash
VERSION=$NEW docker compose --env-file .env.prod -f docker-compose.prod.yml build itsm-frontend
```
backend 同理。构建出的镜像与 demo 栈同名（`itsm-frontend:<tag>`），demo 直接用。

## P4: `--env-file .env.demo` 缺必填变量

**症状**：`required variable ADMIN_PASSWORD is missing a value` / `JWT_SECRET must be set` / `MINIO_ACCESS_KEY must be set`。

**原因**：prod compose 的 backend/init/worker/minio 服务要求 `.env.prod` 独有变量。

**修复**：见 P3，构建一律 `--env-file .env.prod`。

## P5: setsid 后台构建的轮询方式

**模式**（构建 3-6 分钟，SSH 会话内用 setsid+nohup 后台跑）：
```bash
rm -f /tmp/build-xxx.log
setsid nohup bash -c "cd /home/n9e/itsm/repo && VERSION=$NEW docker compose ... build ... >> /tmp/build-xxx.log 2>&1; echo BUILD_EXIT=\$? >> /tmp/build-xxx.log" >/dev/null 2>&1 &
```
**轮询**（不要开长连接傻等）：
```bash
grep -oE 'BUILD_EXIT=[0-9]+' /tmp/build-xxx.log | tail -1
docker image inspect itsm-frontend:$NEW --format '{{.Id}}' || echo IMAGE-PENDING
```
本机侧：`Start-Sleep 90` 后跑一次 remote.py 轮询，未完再 sleep 再查。

## P6: up -d 只重建受影响容器，勿 down 全栈

- 只改前端：`up -d itsm-frontend-demo`（compose 会自动重启依赖它的 nginx）
- worker/init 的镜像 tag 显示旧版但代码同源是正常现象（compose 只重建显式指定的服务）；要 tag 对齐就 `up -d itsm-worker-demo`
- **禁止** `down -v`（会删 itsm_demo 数据卷）——只有明确要求重建全新库时才用

## P7: 远端输出乱码

plink/paramiko 拉回的 UTF-8 中文在 PowerShell 常显示乱码——**不影响执行正确性**，判断成败看退出码、`grep` 的 ASCII 锚词（`BUILD_EXIT`、`healthy`、`http_code`），不要依赖肉眼中文。

## P8: 验证新代码真的进了镜像（A/B 特征对比）

release note 里"构建成功"不等于"新代码在跑"。验证法：
```bash
# 新代码特有标识（如本次的 targetPath），新旧镜像各跑一次
docker run --rm itsm-frontend:<OLD> sh -c "grep -rl targetPath /usr/share/nginx/html/assets/ | wc -l"   # 期望 0
docker run --rm itsm-frontend:<NEW> sh -c "grep -rl targetPath /usr/share/nginx/html/assets/ | wc -l"   # 期望 >=1
docker inspect -f '{{.Config.Image}}' itsm-frontend-demo   # 确认容器引用新 tag
```

## P9: demo 登录验证 401 ≠ 部署失败

后端返回 `{"code":2001,"message":"invalid credentials"}` 说明 API 链路通，只是口令不对。库内账号用：
```bash
docker exec itsm-postgres-prod psql -U itsm -d itsm_demo -tAc "SELECT id, username, active FROM users ORDER BY id LIMIT 10"
```
（注意列名是 `active` 不是 `is_active`/`status`。）口令猜不中就交用户验证 UI，不要卡在这。

## P10: remote.py 参数限制

`remote.py` 只支持 `--sudo/--put/--get/--timeout + cmd`，**没有 `--put-script`**；引号嵌套多时把远端命令写成单引号包裹（PowerShell 内层用双引号），或拆成脚本上传再执行。
