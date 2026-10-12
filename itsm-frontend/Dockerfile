# syntax=docker/dockerfile:1.7
#
# ITSM Frontend — multi-stage Vite build producing a static SPA served by nginx.
#
# Stages:
#   deps       — install dependencies (cached, pinned by package-lock.json)
#   builder    — `npm run build` (Vite) → dist/
#   production — nginx:alpine serving dist/ (no Node runtime)
#   runner     — Vite dev server with hot reload (local / dev compose only)
#
# Deployment notes (docs/plan/vite-migration-plan.md §5.9, 方案 i):
#   - The runtime image is nginx:alpine and listens on 3000, i.e. the port the
#     Next.js standalone server used. nginx/conf.d/default.conf therefore keeps
#     proxying "location /" to http://itsm-frontend:3000 unchanged.
#   - VITE_* variables are baked into the bundle at build time (same constraint
#     as the former NEXT_PUBLIC_*), so one image cannot be reconfigured at
#     runtime — ITSM_BACKEND_URL / NODE_ENV / no_proxy are no longer needed here.

# ============================================================
# Stage 1: Dependencies
# ============================================================
FROM node:24-alpine AS deps

WORKDIR /app

# Keep the install contract aligned with local development and CI.
ENV NPM_CONFIG_LEGACY_PEER_DEPS=true

ARG NPM_REGISTRY
RUN npm config set registry ${NPM_REGISTRY:-https://registry.npmjs.org} \
    && npm config set fund false \
    && npm config set audit false

COPY package.json package-lock.json ./

# NODE_ENV must NOT be "production" in this stage: the Vite build needs
# devDependencies (vite, typescript, tailwindcss, ...).
RUN --mount=type=cache,target=/root/.npm \
    npm ci --ignore-scripts

# ============================================================
# Stage 2: Build (static assets → dist/)
# ============================================================
FROM node:24-alpine AS builder

WORKDIR /app

# VITE_* vars are baked at build time. The API URL defaults to empty so the
# browser talks to the same origin (nginx:80 → backend:8090) instead of hitting
# the backend container directly. See Fix #5: audit log Docker IP leak.
ARG VITE_ENABLE_AI=true
ARG VITE_API_URL=
ARG VITE_API_VERSION=v1
ARG VITE_API_TIMEOUT=30000

ENV NODE_ENV=production \
    VITE_ENABLE_AI=${VITE_ENABLE_AI} \
    VITE_API_URL=${VITE_API_URL} \
    VITE_API_VERSION=${VITE_API_VERSION} \
    VITE_API_TIMEOUT=${VITE_API_TIMEOUT}

COPY --from=deps /app/node_modules ./node_modules
COPY . .

# Fails loudly if the Vite output contract (dist/index.html) ever drifts.
RUN npm run build && test -f dist/index.html

# ============================================================
# Stage 3a: Production runtime (static SPA behind nginx)
# ============================================================
FROM nginx:alpine AS production

# Static-only server: SPA history fallback, /assets/ + index.html cache policy,
# /health probe and the /uploads/ alias for the shared uploads volume.
COPY nginx/default.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

# OB-12（2026-10-12，238 生产部署实测发现）：nginx:alpine 的
#   10-listen-on-ipv6-by-default.sh 在部分宿主（本机为 snap-docker）会卡在
#   `apk manifest nginx`（实测 8s 超时不返回）→ 容器 nginx 永不启动（3000 拒绝连接）。
# 本镜像的 default.conf 自带完整 server 块（含 listen 3000），无需该脚本改写：
RUN rm -f /docker-entrypoint.d/10-listen-on-ipv6-by-default.sh

# Matches the upstream port used by nginx/conf.d/default.conf and compose.
EXPOSE 3000

# Inherits nginx:alpine's default CMD ["nginx", "-g", "daemon off;"].

# ============================================================
# Stage 3b: Development runtime (Vite dev server + HMR)
# ============================================================
FROM node:24-alpine AS runner

WORKDIR /app

# Persist deps OUTSIDE the bind-mounted source tree so the container always has
# a working `vite` binary regardless of whether docker-compose first-created
# /app/node_modules as an empty volume.
COPY --from=deps /app/node_modules /opt/deps/node_modules
COPY --from=deps /app/package.json /app/package-lock.json /opt/deps/

# Host-mounted source code goes at /app (docker-compose.dev.yml).
COPY . .

ENV NODE_ENV=development \
    PORT=3000

EXPOSE 3000

# Runtime package bootstrap: the named volume mounted at /app/node_modules is
# EMPTY on first start, so it is populated from /opt/deps/node_modules. The
# sentinel file makes the copy a one-time cost per volume lifecycle.
# The dev server itself is started from the locally installed Vite binary; it
# reads ITSM_BACKEND_URL for the /api proxy defined in vite.config.ts.
RUN cat <<'SH' > /entrypoint-vite.sh
#!/bin/sh
set -e
SENTINEL=/app/node_modules/.itsm-deps-copy-ok
if [ ! -f "$SENTINEL" ]; then
  echo "[entrypoint-vite] Populating /app/node_modules from /opt/deps/node_modules..." >&2
  mkdir -p /app/node_modules/.bin
  cp -a /opt/deps/node_modules/. /app/node_modules/ 2>/dev/null || true
  touch "$SENTINEL" 2>/dev/null || true
  echo "[entrypoint-vite] /app/node_modules populated." >&2
fi
for p in /app/node_modules/.bin/vite /opt/deps/node_modules/.bin/vite; do
  if [ -f "$p" ]; then
    echo "[entrypoint-vite] using: $p" >&2
    exec node "$p" dev --host 0.0.0.0 --port 3000
  fi
done
echo "[entrypoint-vite] FAILED to locate vite binary." >&2
ls /app/node_modules/.bin 2>&1 | head -30 >&2
exit 1
SH
RUN chmod +x /entrypoint-vite.sh

CMD ["/entrypoint-vite.sh"]
