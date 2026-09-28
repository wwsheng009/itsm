#!/usr/bin/env bash
# =============================================================================
# 构建 provision_tenant 二进制（用于 MSP/多租户场景的租户模板供给）
#
# 背景：生产主机无法直连 proxy.golang.org（实测超时），需走 goproxy.cn。
#      仓库无 vendor/ 目录，模块下载需联网，故用 golang 官方镜像构建。
#
# 用法（在部署主机执行）：bash build-provision-tenant.sh
# 产物：/tmp/provision_tenant_linux_amd64（即 setup-msp-tenants.sh 的默认路径）
#
# 可覆盖变量：REPO_DIR / OUT / IMAGE / GOPROXY_URL / SUDO_PASS / GOMOD_CACHE
# =============================================================================
set -euo pipefail

REPO_DIR="${REPO_DIR:-/home/n9e/itsm/repo/itsm-backend}"
OUT="${OUT:-/tmp/provision_tenant_linux_amd64}"
IMAGE="${IMAGE:-golang:1.25.13-alpine}"
GOPROXY_URL="${GOPROXY_URL:-https://goproxy.cn,direct}"
GOMOD_CACHE="${GOMOD_CACHE:-/tmp/itsm-gomod-cache}"
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
