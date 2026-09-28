#!/usr/bin/env bash
# =============================================================================
# 构建 provision_tenant 二进制（用于 MSP/多租户场景的租户模板供给）
#
# 背景：生产主机无法直连 proxy.golang.org（实测超时），需走 goproxy.cn。
#      仓库无 vendor/ 目录，模块下载需联网，故用 golang 官方镜像构建。
#
# 用法（在部署主机执行）：bash build-provision-tenant.sh
# 产物：$HOME/itsm-artifacts/provision_tenant_linux_amd64
#
# 重要（2026-09-28 实测）：本机 docker 由 snap 安装（/snap/bin/docker），守护进程拥有
# 独立 /tmp——宿主 /tmp 对 `docker cp` 与 `docker run -v` 不可见，构建产物/缓存会"写丢"。
# 因此产物与模块缓存一律放在 $HOME 下（snap 可见），不要把 OUT/GOMOD_CACHE 设到 /tmp。
#
# 可覆盖变量：REPO_DIR / OUT / IMAGE / GOPROXY_URL / SUDO_PASS / GOMOD_CACHE / ARTIFACT_DIR
# =============================================================================
set -euo pipefail

REPO_DIR="${REPO_DIR:-/home/n9e/itsm/repo/itsm-backend}"
ARTIFACT_DIR="${ARTIFACT_DIR:-$HOME/itsm-artifacts}"
OUT="${OUT:-$ARTIFACT_DIR/provision_tenant_linux_amd64}"
IMAGE="${IMAGE:-golang:1.25.13-alpine}"
GOPROXY_URL="${GOPROXY_URL:-https://goproxy.cn,direct}"
GOMOD_CACHE="${GOMOD_CACHE:-$ARTIFACT_DIR/gomod-cache}"
SUDO_PASS="${SUDO_PASS:-passw0rd}"

sdo() { echo "$SUDO_PASS" | sudo -S "$@"; }

[ -d "$REPO_DIR/cmd/provision_tenant" ] || {
  echo "未找到 $REPO_DIR/cmd/provision_tenant，请检查 REPO_DIR" >&2
  exit 1
}

sdo mkdir -p "$GOMOD_CACHE"

sdo docker run --rm \
  -v "$REPO_DIR":/src \
  -v "$(dirname "$OUT")":/out \
  -v "$GOMOD_CACHE":/go/pkg/mod \
  -w /src \
  -e "GOPROXY=$GOPROXY_URL" \
  -e GOSUMDB=off \
  "$IMAGE" \
  sh -c "go build -trimpath -o /out/$(basename "$OUT") ./cmd/provision_tenant"

echo "构建完成："
ls -la "$OUT"
