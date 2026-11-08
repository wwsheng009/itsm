#!/usr/bin/env bash
#
# ITSM demo stack (itsm-demo, :8089) deployment and verification.
#
# Goals:
#   - Never build from a dirty tracked worktree unless explicitly allowed.
#   - Always back up .env.demo and the demo database before switching.
#   - Repair the uploads bind-mount ACL for the container app UID/GID before
#     starting the new backend (see 238 incident 2026-11-06).
#   - Run migrations/seed first and require exit=0 before switching services.
#   - Verify health, version, and a real comment-attachment upload + download +
#     delete before reporting success.
#
# Usage:
#   ./scripts/deploy-demo.sh [command] [options]
#
# Commands:
#   deploy       preflight -> backup -> build -> permissions -> init -> up -> smoke
#   preflight    Validate tools, env, compose file, git state, disk, DB, uploads
#   backup       Back up .env.demo and itsm_demo only
#   build        Build backend/frontend (and optionally ai-service)
#   permissions  Repair uploads ACL and run a container write probe
#   smoke        Health/version checks + real comment attachment round trip
#   status       Show demo container status and version
#   help         Show this help
#
# Options:
#   --version <tag>            Immutable image version (default: 1.8.0-dev.<8sha>)
#   --env-file <path>          Default: .env.demo
#   --compose-file <path>      Default: docker-compose.demo.yml
#   --uploads-dir <path>       Default: deploy/demo/uploads
#   --backup-root <path>       Default: backups
#   --db-container <name>      Default: itsm-postgres-prod
#   --app-uid <uid>            Override app UID detected from the backend image
#   --app-gid <gid>            Override app GID detected from the backend image
#   --smoke-url <url>          Default: http://127.0.0.1:${DEMO_HTTP_PORT:-8089}
#   --smoke-ticket-id <id>     Use a specific ticket for attachment smoke
#   --with-ai                   Build ai-service for this version
#   --reuse-ai <image:tag>      Retag an existing ai-service image for this version
#   --skip-backup              Skip DB/env backup (not recommended)
#   --skip-build               Reuse existing target images
#   --skip-smoke               Skip all post-deploy smoke checks
#   --skip-attachment-smoke    Skip the authenticated attachment round trip
#   --allow-dirty              Allow a dirty tracked worktree
#   --keep-backups <n>         Keep latest N demo release backups (default: 7)
#   --verbose                  Reserved for verbose helper output
#   -h, --help                 Show this help
#
# Examples:
#   ./scripts/deploy-demo.sh preflight
#   ./scripts/deploy-demo.sh deploy --version 1.8.0-dev.<sha>
#   ./scripts/deploy-demo.sh deploy --skip-build --version 1.8.0-dev.<sha>
#   ./scripts/deploy-demo.sh smoke --version 1.8.0-dev.<sha>
#   ./scripts/deploy-demo.sh permissions --version 1.8.0-dev.<sha>

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "${SCRIPT_DIR}/lib/common.sh"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

export DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}"

COMMAND="deploy"
TARGET_VERSION_ARG=""
ENV_FILE="$PROJECT_ROOT/.env.demo"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.demo.yml"
UPLOADS_DIR="$PROJECT_ROOT/deploy/demo/uploads"
BACKUP_ROOT="$PROJECT_ROOT/backups"
DEPLOY_STATE_DIR="$PROJECT_ROOT/.deploy/demo"
DB_CONTAINER="itsm-postgres-prod"
APP_UID_ARG=""
APP_GID_ARG=""
SMOKE_URL_ARG=""
SMOKE_TICKET_ID=""
REUSE_AI_IMAGE=""
WITH_AI=false
SKIP_BACKUP=false
SKIP_BUILD=false
SKIP_SMOKE=false
SKIP_ATTACHMENT_SMOKE=false
ALLOW_DIRTY=false
KEEP_BACKUPS=7
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-300}"
VERBOSE=false

BACKUP_DIR=""
STACK_SWITCHED=false
ENV_MUTATED=false
CURRENT_VERSION=""
TARGET_VERSION=""
APP_UID=""
APP_GID=""
PYTHON_BIN=""
USE_SUDO_FOR_DOCKER=false
DOCKER=()

usage() {
    sed -n '2,52p' "$0" | sed 's/^# \{0,1\}//'
}

fail() {
    log_error "$*"
    exit 1
}

resolve_path() {
    local p="$1"
    if [[ "$p" = /* ]]; then
        printf '%s\n' "$p"
    else
        printf '%s\n' "$PROJECT_ROOT/$p"
    fi
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        deploy|preflight|backup|build|permissions|smoke|status|help)
            COMMAND="$1"; shift ;;
        --version|--env-file|--compose-file|--uploads-dir|--backup-root|--db-container|--app-uid|--app-gid|--smoke-url|--smoke-ticket-id|--reuse-ai|--keep-backups)
            [[ $# -ge 2 ]] || fail "Option $1 requires a value"
            case "$1" in
                --version) TARGET_VERSION_ARG="$2" ;;
                --env-file) ENV_FILE="$2" ;;
                --compose-file) COMPOSE_FILE="$2" ;;
                --uploads-dir) UPLOADS_DIR="$2" ;;
                --backup-root) BACKUP_ROOT="$2" ;;
                --db-container) DB_CONTAINER="$2" ;;
                --app-uid) APP_UID_ARG="$2" ;;
                --app-gid) APP_GID_ARG="$2" ;;
                --smoke-url) SMOKE_URL_ARG="$2" ;;
                --smoke-ticket-id) SMOKE_TICKET_ID="$2" ;;
                --reuse-ai) REUSE_AI_IMAGE="$2" ;;
                --keep-backups) KEEP_BACKUPS="$2" ;;
            esac
            shift 2 ;;
        --version=*|--env-file=*|--compose-file=*|--uploads-dir=*|--backup-root=*|--db-container=*|--app-uid=*|--app-gid=*|--smoke-url=*|--smoke-ticket-id=*|--reuse-ai=*|--keep-backups=*)
            key="${1%%=*}"
            value="${1#*=}"
            set -- "$key" "$value" "${@:2}" ;;
        --with-ai) WITH_AI=true; shift ;;
        --skip-backup) SKIP_BACKUP=true; shift ;;
        --skip-build) SKIP_BUILD=true; shift ;;
        --skip-smoke) SKIP_SMOKE=true; shift ;;
        --skip-attachment-smoke) SKIP_ATTACHMENT_SMOKE=true; shift ;;
        --allow-dirty) ALLOW_DIRTY=true; shift ;;
        --verbose) VERBOSE=true; shift ;;
        -h|--help) COMMAND="help"; shift ;;
        *) fail "Unknown option: $1 (use --help)" ;;
    esac
done

export VERBOSE

if [[ "$COMMAND" == "help" ]]; then
    usage
    exit 0
fi

ENV_FILE="$(resolve_path "$ENV_FILE")"
COMPOSE_FILE="$(resolve_path "$COMPOSE_FILE")"
UPLOADS_DIR="$(resolve_path "$UPLOADS_DIR")"
BACKUP_ROOT="$(resolve_path "$BACKUP_ROOT")"

load_env() {
    [[ -f "$ENV_FILE" ]] || fail "Environment file not found: $ENV_FILE"
    source_env_file "$ENV_FILE"
    CURRENT_VERSION="${VERSION:-unknown}"

    if [[ -n "$TARGET_VERSION_ARG" ]]; then
        TARGET_VERSION="$TARGET_VERSION_ARG"
    elif [[ "$COMMAND" == "smoke" || "$COMMAND" == "status" ]]; then
        TARGET_VERSION="$CURRENT_VERSION"
    else
        command -v git >/dev/null 2>&1 || fail "git is required to derive the release version"
        TARGET_VERSION="1.8.0-dev.$(git rev-parse --short=8 HEAD)"
    fi

    [[ "$TARGET_VERSION" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] \
        || fail "Invalid image version/tag: $TARGET_VERSION"

    export VERSION="$TARGET_VERSION"
    BACKEND_IMAGE="itsm-backend:$TARGET_VERSION"
    FRONTEND_IMAGE="itsm-frontend:$TARGET_VERSION"
    AI_IMAGE="itsm-ai-service:$TARGET_VERSION"
    SMOKE_URL="${SMOKE_URL_ARG:-http://127.0.0.1:${DEMO_HTTP_PORT:-8089}}"
    SMOKE_URL="${SMOKE_URL%/}"

    if command -v python3 >/dev/null 2>&1; then
        PYTHON_BIN="python3"
    elif command -v python >/dev/null 2>&1; then
        PYTHON_BIN="python"
    else
        PYTHON_BIN=""
    fi
}

ensure_docker_access() {
    if docker info >/dev/null 2>&1; then
        DOCKER=(docker)
        return 0
    fi
    if command -v sudo >/dev/null 2>&1 && sudo docker info >/dev/null 2>&1; then
        DOCKER=(sudo docker)
        USE_SUDO_FOR_DOCKER=true
        log_warn "Docker requires sudo; sudo timestamp must stay valid for this run"
        return 0
    fi
    fail "Docker daemon is unavailable (and sudo docker is unavailable)"
}

run_docker() {
    "${DOCKER[@]}" "$@"
}

compose() {
    run_docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"
}

image_exists() {
    run_docker image inspect "$1" >/dev/null 2>&1
}

container_exists() {
    run_docker inspect "$1" >/dev/null 2>&1
}

tracked_worktree_dirty() {
    [[ -n "$(git status --porcelain --untracked-files=no 2>/dev/null)" ]]
}

set_env_value() {
    local file="$1" key="$2" value="$3" tmp owner="" group="" mode=""
    owner="$(stat -c '%u' "$file" 2>/dev/null || true)"
    group="$(stat -c '%g' "$file" 2>/dev/null || true)"
    mode="$(stat -c '%a' "$file" 2>/dev/null || true)"
    tmp="$(mktemp)"
    awk -v key="$key" -v value="$value" '
        BEGIN { replaced = 0 }
        $0 ~ "^" key "=" { print key "=" value; replaced = 1; next }
        { print }
        END { if (replaced == 0) print key "=" value }
    ' "$file" > "$tmp"
    if [[ -n "$mode" ]]; then
        chmod "$mode" "$tmp"
    fi
    mv "$tmp" "$file"
    if [[ "$(id -u)" == "0" && -n "$owner" && -n "$group" ]]; then
        chown "$owner:$group" "$file"
    fi
}

sha256_file() {
    local file="$1" out="$2"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$file" > "$out"
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$file" > "$out"
    else
        fail "Neither sha256sum nor shasum is available"
    fi
}

adopt_artifact_owner() {
    local path="$1"
    if [[ "$(id -u)" == "0" && -n "${SUDO_UID:-}" && -n "${SUDO_GID:-}" && -e "$path" ]]; then
        chown -R "${SUDO_UID}:${SUDO_GID}" "$path"
    fi
}

cleanup_on_exit() {
    local rc=$?
    if [[ $rc -ne 0 ]]; then
        log_error "Command '$COMMAND' failed (exit $rc)"
        if [[ "$STACK_SWITCHED" != "true" && "$ENV_MUTATED" == "true" && -n "$BACKUP_DIR" && -f "$BACKUP_DIR/.env.demo.before" ]]; then
            cp -p "$BACKUP_DIR/.env.demo.before" "$ENV_FILE"
            log_warn "Restored $ENV_FILE because the running stack was not switched"
        fi
        if [[ -n "$BACKUP_DIR" ]]; then
            adopt_artifact_owner "$BACKUP_DIR"
            log_info "Backup/rollback evidence: $BACKUP_DIR"
        fi
    fi
}
trap cleanup_on_exit EXIT

preflight_checks() {
    log_phase "Preflight"
    local errors=0

    command -v docker >/dev/null 2>&1 || { log_error "docker is not installed"; errors=$((errors + 1)); }
    command -v curl >/dev/null 2>&1 || { log_error "curl is not installed"; errors=$((errors + 1)); }
    command -v git >/dev/null 2>&1 || { log_error "git is not installed"; errors=$((errors + 1)); }

    if ! compose config --quiet; then
        log_error "Compose config validation failed: $COMPOSE_FILE"
        errors=$((errors + 1))
    else
        log_success "Compose config is valid"
    fi

    local required=(DEMO_DB_NAME DB_USER DEMO_ADMIN_PASSWORD REDIS_PASSWORD DEMO_JWT_SECRET)
    local var
    for var in "${required[@]}"; do
        if [[ -z "${!var:-}" ]]; then
            log_error "Required variable $var is empty in $ENV_FILE"
            errors=$((errors + 1))
        fi
    done

    if tracked_worktree_dirty && [[ "$ALLOW_DIRTY" != "true" ]]; then
        log_error "Tracked worktree has uncommitted changes; commit/stash or pass --allow-dirty"
        errors=$((errors + 1))
    else
        log_success "Git worktree contract: $([[ "$ALLOW_DIRTY" == "true" ]] && echo 'dirty allowed' || echo 'clean')"
    fi

    git rev-parse --is-inside-work-tree >/dev/null 2>&1 \
        && log_success "Git commit: $(git rev-parse --short=8 HEAD) ($(git rev-parse --abbrev-ref HEAD))" \
        || { log_error "Not inside a git worktree"; errors=$((errors + 1)); }

    if ! require_disk_space 5; then
        errors=$((errors + 1))
    fi

    if [[ "$SKIP_BACKUP" != "true" ]]; then
        if ! container_exists "$DB_CONTAINER"; then
            log_error "Database container not found: $DB_CONTAINER (use --db-container or --skip-backup)"
            errors=$((errors + 1))
        elif ! run_docker exec "$DB_CONTAINER" pg_dump --version >/dev/null 2>&1; then
            log_error "pg_dump is unavailable in $DB_CONTAINER"
            errors=$((errors + 1))
        fi
    else
        log_warn "Database backup disabled by --skip-backup"
    fi

    if [[ "$SKIP_SMOKE" != "true" && -z "$PYTHON_BIN" ]]; then
        log_error "python3/python is required for the smoke test"
        errors=$((errors + 1))
    fi

    if command -v setfacl >/dev/null 2>&1; then
        log_success "setfacl is available"
    else
        log_warn "setfacl is missing; permissions command will fall back to chown"
    fi

    if container_exists "itsm-backend-demo"; then
        if run_docker exec itsm-backend-demo sh -c \
            'touch /app/uploads/tickets/.itsm-preflight-write && rm -f /app/uploads/tickets/.itsm-preflight-write'; then
            log_success "Current backend can write /app/uploads/tickets"
        else
            log_warn "Current backend cannot write /app/uploads/tickets; deploy will repair ACL before switch"
        fi
    fi

    if [[ $errors -gt 0 ]]; then
        log_error "Preflight failed with $errors error(s)"
        return 1
    fi
    log_success "Preflight passed"
}

backup_state() {
    log_phase "Backup"
    if [[ "$SKIP_BACKUP" == "true" ]]; then
        log_warn "Backup skipped by --skip-backup"
        return 0
    fi

    local ts
    ts="$(date -u +%Y%m%dT%H%M%SZ)"
    BACKUP_DIR="$BACKUP_ROOT/demo-release-${TARGET_VERSION}-${ts}"
    mkdir -p "$BACKUP_DIR"
    chmod 700 "$BACKUP_DIR"
    cp -p "$ENV_FILE" "$BACKUP_DIR/.env.demo.before"

    {
        echo "created_at=$(date -u +%FT%TZ)"
        echo "target_version=$TARGET_VERSION"
        echo "previous_version=$CURRENT_VERSION"
        echo "git_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
        echo "git_commit=$(git rev-parse HEAD 2>/dev/null || echo unknown)"
        echo "env_file=$ENV_FILE"
        echo "compose_file=$COMPOSE_FILE"
        echo "db_container=$DB_CONTAINER"
        echo "db_name=${DEMO_DB_NAME:-unknown}"
        echo "rollback_env=$BACKUP_DIR/.env.demo.before"
        echo "previous_images:"
        local name
        for name in itsm-backend-demo itsm-worker-demo itsm-frontend-demo itsm-ai-service-demo; do
            run_docker inspect "$name" --format '  {{.Name}} {{.Config.Image}} {{.Image}}' 2>/dev/null || echo "  $name missing"
        done
    } > "$BACKUP_DIR/rollback.txt"

    if ! container_exists "$DB_CONTAINER"; then
        fail "Database container not found: $DB_CONTAINER"
    fi

    local dump="$BACKUP_DIR/itsm_demo.dump"
    log_info "Dumping ${DEMO_DB_NAME:-itsm_demo} from $DB_CONTAINER"
    if ! run_docker exec "$DB_CONTAINER" pg_dump -U "${DB_USER:-itsm}" -d "${DEMO_DB_NAME:-itsm_demo}" -Fc > "$dump"; then
        rm -f "$dump"
        fail "pg_dump failed; no services were changed"
    fi
    if [[ ! -s "$dump" ]]; then
        rm -f "$dump"
        fail "pg_dump produced an empty file; no services were changed"
    fi
    sha256_file "$dump" "$dump.sha256"
    cp -p "$ENV_FILE" "$BACKUP_DIR/.env.demo.before"
    adopt_artifact_owner "$BACKUP_DIR"
    log_success "Backup saved: $BACKUP_DIR"
}

run_build_images() {
    local -a services=("$@")
    if [[ "$USE_SUDO_FOR_DOCKER" == "true" ]]; then
        sudo env VERSION="$TARGET_VERSION" DOCKER_BUILDKIT=1 \
            bash "$PROJECT_ROOT/scripts/build-images.sh" "$TARGET_VERSION" "" "${services[@]}"
    else
        VERSION="$TARGET_VERSION" DOCKER_BUILDKIT=1 \
            bash "$PROJECT_ROOT/scripts/build-images.sh" "$TARGET_VERSION" "" "${services[@]}"
    fi
}

build_images() {
    log_phase "Build images"
    local -a services=(backend frontend)

    if [[ "$SKIP_BUILD" == "true" ]]; then
        log_warn "Build skipped by --skip-build"
    else
        [[ "$WITH_AI" == "true" ]] && services+=(ai-service)
        run_build_images "${services[@]}"
    fi

    if [[ "$WITH_AI" != "true" ]]; then
        if [[ -n "$REUSE_AI_IMAGE" ]]; then
            image_exists "$REUSE_AI_IMAGE" || fail "AI image to reuse not found: $REUSE_AI_IMAGE"
            run_docker tag "$REUSE_AI_IMAGE" "$AI_IMAGE"
            log_success "Retagged $REUSE_AI_IMAGE -> $AI_IMAGE"
        elif ! image_exists "$AI_IMAGE"; then
            fail "AI image missing for $TARGET_VERSION; pass --with-ai or --reuse-ai <image:tag>"
        fi
    fi

    local image
    for image in "$BACKEND_IMAGE" "$FRONTEND_IMAGE" "$AI_IMAGE"; do
        image_exists "$image" || fail "Required image missing after build: $image"
        log_success "Image ready: $image"
    done
}

resolve_app_ids() {
    if [[ -n "$APP_UID_ARG" && -n "$APP_GID_ARG" ]]; then
        APP_UID="$APP_UID_ARG"
        APP_GID="$APP_GID_ARG"
    else
        local ids=""
        if image_exists "$BACKEND_IMAGE"; then
            ids="$(run_docker run --rm --entrypoint sh "$BACKEND_IMAGE" -c 'id -u app; id -g app' 2>/dev/null || true)"
        elif container_exists "itsm-backend-demo"; then
            ids="$(run_docker exec itsm-backend-demo sh -c 'id -u app; id -g app' 2>/dev/null || true)"
        fi
        if [[ -z "$ids" ]]; then
            fail "Cannot determine app UID/GID; build the backend image or pass --app-uid/--app-gid"
        fi
        APP_UID="$(printf '%s\n' "$ids" | sed -n '1p')"
        APP_GID="$(printf '%s\n' "$ids" | sed -n '2p')"
    fi
    [[ "$APP_UID" =~ ^[0-9]+$ && "$APP_GID" =~ ^[0-9]+$ ]] \
        || fail "Invalid app UID/GID: '$APP_UID:$APP_GID'"
    log_info "Container app identity: ${APP_UID}:${APP_GID}"
}

repair_permissions() {
    log_phase "Uploads permissions"
    resolve_app_ids

    mkdir -p "$UPLOADS_DIR/tickets" 2>/dev/null || {
        sudo mkdir -p "$UPLOADS_DIR/tickets"
    }

    if command -v setfacl >/dev/null 2>&1; then
        if ! setfacl -R -m "u:${APP_UID}:rwx" -m "g:${APP_GID}:rwx" "$UPLOADS_DIR" 2>/dev/null; then
            log_warn "ACL update requires elevated privileges; retrying with sudo"
            sudo setfacl -R -m "u:${APP_UID}:rwx" -m "g:${APP_GID}:rwx" "$UPLOADS_DIR"
        fi
        if ! find "$UPLOADS_DIR" -type d -exec setfacl -d -m "u:${APP_UID}:rwx" -m "g:${APP_GID}:rwx" {} + 2>/dev/null; then
            sudo find "$UPLOADS_DIR" -type d -exec setfacl -d -m "u:${APP_UID}:rwx" -m "g:${APP_GID}:rwx" {} +
        fi
        log_success "ACL grants app ${APP_UID}:${APP_GID} rwx (with directory inheritance)"
    else
        log_warn "setfacl not available; falling back to chown ${APP_UID}:${APP_GID}"
        sudo chown -R "${APP_UID}:${APP_GID}" "$UPLOADS_DIR"
        log_warn "Host user may need sudo to read newly created uploads"
    fi
}

verify_upload_write() {
    log_info "Verifying bind-mount write access as ${APP_UID}:${APP_GID}"
    if ! run_docker run --rm --user "${APP_UID}:${APP_GID}" \
        -v "$UPLOADS_DIR:/app/uploads" --entrypoint sh "$BACKEND_IMAGE" \
        -c 'mkdir -p /app/uploads/tickets/.probe && touch /app/uploads/tickets/.probe/write-ok && rm -rf /app/uploads/tickets/.probe'; then
        fail "Uploads bind mount is not writable by the backend runtime user"
    fi
    log_success "Uploads bind mount is writable"
}

wait_for_init() {
    local name="itsm-init-demo" timeout="${1:-360}" elapsed=0 state status exit_code
    while [[ $elapsed -lt $timeout ]]; do
        state="$(run_docker inspect "$name" --format '{{.State.Status}} {{.State.ExitCode}}' 2>/dev/null || echo 'missing 1')"
        read -r status exit_code <<< "$state"
        if [[ "$status" == "exited" ]]; then
            if [[ "$exit_code" == "0" ]]; then
                # Bootstrap currently logs filesystem migration failures without
                # always propagating a non-zero exit code. Treat that log marker
                # as a hard release failure so a partial schema never goes live.
                if run_docker logs "$name" 2>&1 | grep -q "filesystem migrations apply failed"; then
                    log_error "$name logged a filesystem migration failure"
                    run_docker logs --tail 120 "$name" >&2 || true
                    return 1
                fi
                log_success "$name completed with exit=0"
                return 0
            fi
            log_error "$name exited with $exit_code"
            run_docker logs --tail 120 "$name" >&2 || true
            return 1
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done
    log_error "$name did not finish within ${timeout}s (last state: ${state:-unknown})"
    run_docker logs --tail 120 "$name" >&2 || true
    return 1
}

wait_for_container_health() {
    local name="$1" timeout="${2:-$HEALTH_TIMEOUT}" elapsed=0 state status health
    while [[ $elapsed -lt $timeout ]]; do
        state="$(run_docker inspect "$name" --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' 2>/dev/null || echo 'missing none')"
        read -r status health <<< "$state"
        if [[ "$status" == "running" && "$health" == "healthy" ]]; then
            log_success "$name is healthy"
            return 0
        fi
        if [[ "$status" == "running" && "$health" == "none" ]]; then
            log_success "$name is running (no healthcheck)"
            return 0
        fi
        if [[ "$status" == "exited" ]]; then
            log_error "$name exited unexpectedly"
            run_docker logs --tail 80 "$name" >&2 || true
            return 1
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done
    log_error "$name did not become healthy within ${timeout}s (last state: ${state:-unknown})"
    run_docker logs --tail 80 "$name" >&2 || true
    return 1
}

switch_stack() {
    log_phase "Switch demo stack"
    set_env_value "$ENV_FILE" VERSION "$TARGET_VERSION"
    ENV_MUTATED=true
    grep -q "^VERSION=$TARGET_VERSION$" "$ENV_FILE" || fail "Failed to update VERSION in $ENV_FILE"
    compose config --quiet

    log_info "Running migration/seed init with $BACKEND_IMAGE"
    compose up -d --force-recreate --no-deps itsm-init-demo
    wait_for_init

    STACK_SWITCHED=true
    compose up -d

    local name
    for name in itsm-redis-demo itsm-ai-service-demo itsm-backend-demo itsm-worker-demo itsm-frontend-demo itsm-nginx-demo; do
        wait_for_container_health "$name" "${HEALTH_TIMEOUT:-300}"
    done
}

smoke_test() {
    log_phase "Smoke test"
    local url="$SMOKE_URL"
    [[ -n "$PYTHON_BIN" ]] || fail "python3/python is required for smoke tests"
    log_info "Target URL: $url"
    wait_for_http "$url/health" "demo health" 60 2 || fail "HTTP health check failed: $url/health"
    curl -fsS --max-time 10 "$url/api/v1/readyz" >/dev/null || fail "Readiness check failed: $url/api/v1/readyz"

    local version_json actual_version
    version_json="$(curl -fsS --max-time 10 "$url/api/v1/version")" || fail "Version endpoint failed"
    actual_version="$(printf '%s' "$version_json" | "$PYTHON_BIN" -c '
import json, sys
payload = json.load(sys.stdin)
data = payload.get("data") if isinstance(payload, dict) else {}
print((data or {}).get("version") or (payload.get("version") if isinstance(payload, dict) else "") or "")
')" || fail "Version endpoint returned invalid JSON"
    if [[ "$actual_version" != "$TARGET_VERSION" ]]; then
        fail "Version mismatch; expected $TARGET_VERSION, got: ${actual_version:-<empty>}"
    fi
    log_success "Version endpoint reports $TARGET_VERSION"

    if [[ "$SKIP_ATTACHMENT_SMOKE" == "true" ]]; then
        log_warn "Attachment upload smoke skipped by --skip-attachment-smoke"
        return 0
    fi
    [[ -n "${DEMO_ADMIN_PASSWORD:-}" ]] || fail "DEMO_ADMIN_PASSWORD is empty; cannot run authenticated attachment smoke"

    local report="${BACKUP_DIR:-$DEPLOY_STATE_DIR}/smoke-attachment.json"
    mkdir -p "$(dirname "$report")"
    local -a args=(
        --base-url "$url"
        --admin-user "${SMOKE_ADMIN_USER:-admin}"
        --password-env DEMO_ADMIN_PASSWORD
        --report "$report"
        --timeout 30
    )
    if [[ -n "$SMOKE_TICKET_ID" ]]; then
        args+=(--ticket-id "$SMOKE_TICKET_ID")
    fi
    if ! "$PYTHON_BIN" "$PROJECT_ROOT/scripts/demo/smoke_attachment_upload.py" "${args[@]}"; then
        fail "Attachment upload smoke failed (report: $report)"
    fi
    adopt_artifact_owner "$(dirname "$report")"
    log_success "Attachment upload/preview/delete smoke passed"
}

write_release_summary() {
    log_phase "Release evidence"
    local dir="${BACKUP_DIR:-$DEPLOY_STATE_DIR}"
    mkdir -p "$dir"
    chmod 700 "$dir"

    {
        echo "release_status=success"
        echo "deployed_at=$(date -u +%FT%TZ)"
        echo "version=$TARGET_VERSION"
        echo "git_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
        echo "git_commit=$(git rev-parse HEAD 2>/dev/null || echo unknown)"
        echo "smoke_url=$SMOKE_URL"
        echo "containers:"
        run_docker ps --filter 'name=itsm-.*-demo' --format '  {{.Names}} {{.Image}} {{.Status}}' || true
        echo "images:"
        local image
        for image in "$BACKEND_IMAGE" "$FRONTEND_IMAGE" "$AI_IMAGE"; do
            run_docker image inspect "$image" --format '  {{.RepoTags}} {{.Id}} {{.Created}}' 2>/dev/null || true
        done
        echo "rollback:"
        echo "  previous_version=$CURRENT_VERSION"
        if [[ -n "$BACKUP_DIR" ]]; then
            echo "  env_before=$BACKUP_DIR/.env.demo.before"
            echo "  db_dump=$BACKUP_DIR/itsm_demo.dump"
        else
            echo "  backup=skipped"
        fi
    } > "$dir/release.txt"
    cp -p "$ENV_FILE" "$dir/.env.demo.deployed"
    chmod 600 "$dir/.env.demo.deployed"
    adopt_artifact_owner "$dir"

    if [[ "$KEEP_BACKUPS" =~ ^[0-9]+$ ]] && [[ "$KEEP_BACKUPS" -gt 0 ]]; then
        local old
        while IFS= read -r old; do
            if [[ -n "$old" ]]; then
                rm -rf "$old" 2>/dev/null || log_warn "Could not prune old backup (permissions): $old"
            fi
        done < <(ls -1dt "$BACKUP_ROOT"/demo-release-* 2>/dev/null | tail -n +$((KEEP_BACKUPS + 1)))
    fi
    log_success "Release evidence: $dir/release.txt"
}

show_status() {
    log_phase "Demo status"
    compose ps
    echo
    curl -fsS --max-time 10 "$SMOKE_URL/api/v1/version" || true
    echo
}

main() {
    load_env
    ensure_docker_access

    case "$COMMAND" in
        preflight)
            preflight_checks
            ;;
        backup)
            backup_state
            ;;
        build)
            build_images
            ;;
        permissions)
            repair_permissions
            verify_upload_write
            ;;
        smoke)
            smoke_test
            ;;
        status)
            show_status
            ;;
        deploy)
            preflight_checks
            backup_state
            build_images
            repair_permissions
            verify_upload_write
            switch_stack
            if [[ "$SKIP_SMOKE" == "true" ]]; then
                log_warn "Post-deploy smoke skipped by --skip-smoke"
            else
                smoke_test
            fi
            write_release_summary
            print_banner "Demo deploy complete" \
                "Version:  $TARGET_VERSION" \
                "URL:      $SMOKE_URL" \
                "Evidence: ${BACKUP_DIR:-$DEPLOY_STATE_DIR}"
            ;;
        *)
            usage
            exit 2
            ;;
    esac
}

main