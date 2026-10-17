#!/usr/bin/env python3
"""ITSM skills 零依赖安装器（Python 3.8+，仅标准库）。

从 manifest.json 下载/读取技能 zip → sha256 校验 → 按 host 映射解压到技能根 → 幂等登记 .skills-installed.json。

用法：
  # 从本地 manifest 安装单技能
  python itsm-skills-install.py --manifest manifest.json --name itsm-cmdb-operations --host codex
  # 安装全量 bundle
  python itsm-skills-install.py --manifest manifest.json --all --host claude
  # 直接从 URL 拉取 manifest（P2 资源页形态）
  python itsm-skills-install.py --base http://172.18.3.238:8089/api/v1/skills --all --host codex
  # 项目级安装
  python itsm-skills-install.py --manifest manifest.json --name itsm-ticket-operations --host project --project-root /path/to/repo

P3 增强（版本治理）：
  # 版本锁定：只装指定版本（version=manifest 的 version 字段，如 2026-10-17.3bef4257）
  python itsm-skills-install.py --manifest manifest.json --name itsm-cmdb-operations@2026-10-17.3bef4257 --host codex
  # 升级前检查（dry-run：对比 registry 已装版本与 manifest 目标，不落盘）
  python itsm-skills-install.py --manifest manifest.json --all --host codex --check
  # 安装态完整性校验（抽查 registry 中 sha256 vs 重新打包摘要；差异=被本地改动）
  python itsm-skills-install.py --host codex --verify
  # 升级自动备份上一版到 <root>/.skills-backup/<name>-<旧版本>-<时间戳>.zip；一键回滚
  python itsm-skills-install.py --host codex --rollback itsm-cmdb-operations
"""
import argparse
import hashlib
import io
import json
import shutil
import sys
import tempfile
import urllib.request
import zipfile
from datetime import datetime, timezone
from pathlib import Path

REGISTRY_FILE = ".skills-installed.json"
BACKUP_DIR = ".skills-backup"


class _QuietWriter(io.TextIOBase):
    """stdout 写入失败（Windows 管道被 | Select-Object -First 截断）时静默丢弃，
    避免安装成功却因日志写出失败而报错/非零退出。"""

    def write(self, s):  # noqa: D102
        try:
            sys.__stdout__.write(s)
        except (OSError, ValueError):
            pass
        return len(s)

    def flush(self):  # noqa: D102
        try:
            sys.__stdout__.flush()
        except (OSError, ValueError):
            pass


def host_root(host, project_root=None):
    if host == "claude":
        return Path.home() / ".claude" / "skills"
    if host == "codex":
        return Path.home() / ".aicli" / ".agents" / "skills"
    if host == "project":
        base = Path(project_root) if project_root else Path.cwd()
        return base / ".agents" / "skills"
    raise SystemExit("未知 host: %s（支持 claude|codex|project）" % host)


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as fh:
        for chunk in iter(lambda: fh.read(1 << 16), b""):
            h.update(chunk)
    return h.hexdigest()


def dir_sha256(path):
    """目录内容摘要（相对路径+内容），用于安装态完整性校验。"""
    h = hashlib.sha256()
    for p in sorted(Path(path).rglob("*")):
        if p.is_file():
            rel = p.relative_to(path).as_posix()
            if rel == REGISTRY_FILE or rel.startswith(BACKUP_DIR):
                continue
            h.update(rel.encode("utf-8"))
            h.update(b"\0")
            h.update(hashlib.sha256(p.read_bytes()).digest())
    return h.hexdigest()


def load_manifest(args):
    if args.manifest:
        return json.loads(Path(args.manifest).read_text(encoding="utf-8")), Path(args.manifest).parent
    if args.base:
        url = args.base.rstrip("/") + "/manifest.json"
        tmp = Path(tempfile.mkdtemp(prefix="itsm-skills-")) / "manifest.json"
        urllib.request.urlretrieve(url, tmp)  # nosec - P2 服务端为可信内网
        return json.loads(tmp.read_text(encoding="utf-8")), tmp.parent
    raise SystemExit("需要 --manifest 或 --base")


def load_registry(root):
    reg = root / REGISTRY_FILE
    if not reg.exists():
        return {}
    try:
        return {r["name"]: r for r in json.loads(reg.read_text(encoding="utf-8")).get("installed", [])}
    except (ValueError, KeyError):
        return {}


def save_registry(root, records):
    reg = root / REGISTRY_FILE
    existing = load_registry(root)
    for r in records:
        existing[r["name"]] = r
    reg.write_text(json.dumps({"installed": sorted(existing.values(), key=lambda x: x["name"])},
                              ensure_ascii=False, indent=2), encoding="utf-8")


def safe_extract(zip_path, target_dir):
    """防 zip-slip：所有条目必须落在 target_dir 内。"""
    target = target_dir.resolve()
    with zipfile.ZipFile(zip_path) as zf:
        for info in zf.infolist():
            dest = (target / info.filename).resolve()
            if not str(dest).startswith(str(target)):
                raise SystemExit("检测到 zip 路径逃逸: %s" % info.filename)
        zf.extractall(target)


def backup_existing(root, name, prev_version):
    """升级前把已装版本打包到 .skills-backup/；返回备份路径（无已装版本返回 None）。"""
    dest = root / name
    if not dest.exists():
        return None
    backup_root = root / BACKUP_DIR
    backup_root.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    safe_name = name.replace("/", "_")
    # 版本串可能含 Windows 非法字符（ rolled-back:xxx 的冒号等），统一清洗
    safe_ver = (prev_version or "unknown").replace(":", "_").replace("\\", "_").replace("/", "_")
    zip_path = backup_root / ("%s-%s-%s.zip" % (safe_name, safe_ver, stamp))
    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as zf:
        for p in sorted(dest.rglob("*")):
            if p.is_file():
                zf.write(p, p.relative_to(dest).as_posix())
    return zip_path


def install_one(entry, src_dir, root, wanted_version=None):
    if wanted_version and entry.get("version") != wanted_version:
        raise SystemExit(
            "版本锁定不匹配: %s 期望 %s，manifest 提供 %s" % (entry["name"], wanted_version, entry.get("version", ""))
        )
    zip_path = src_dir / entry["file"]
    if not zip_path.exists():
        raise SystemExit("包不存在: %s" % zip_path)
    digest = sha256_file(zip_path)
    if digest != entry["sha256"]:
        raise SystemExit("sha256 校验失败: %s\n  期望 %s\n  实际 %s" % (entry["file"], entry["sha256"], digest))

    registry = load_registry(root)
    prev = registry.get(entry["name"])
    if prev and prev.get("version") != entry.get("version"):
        bak = backup_existing(root, entry["name"], prev.get("version"))
        if bak:
            print("backup: %s@%s -> %s" % (entry["name"], prev.get("version"), bak))

    dest = root / entry["name"]
    if dest.exists():
        shutil.rmtree(dest)  # 幂等：重装覆盖
    dest.mkdir(parents=True, exist_ok=True)
    safe_extract(zip_path, dest)

    # 布局自检：SKILL.md 必须存在；脚本域还需 _lib（单技能包已内嵌）
    if not (dest / "SKILL.md").exists():
        raise SystemExit("包布局异常：缺少 SKILL.md（%s）" % dest)
    if (dest / "scripts").exists() and not (dest / "_lib" / "itsm_common.py").exists():
        raise SystemExit("包布局异常：含 scripts 但缺 _lib/itsm_common.py（%s）" % dest)
    return {"name": entry["name"], "version": entry.get("version", ""), "sha256": digest,
            "contentSha256": dir_sha256(dest),
            "installedAt": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "root": str(dest)}


def cmd_check(manifest, root, wanted_names):
    """dry-run：对比已装版本 vs manifest 目标。"""
    registry = load_registry(root)
    rows = []
    for entry in manifest.get("skills") or []:
        name = entry["name"]
        if wanted_names and name not in wanted_names:
            continue
        cur = registry.get(name, {})
        rows.append((name, cur.get("version", "(未安装)"), entry.get("version", "?"),
                     "up-to-date" if cur.get("version") == entry.get("version") else "update-available"))
    if not rows:
        print("无可比对的技能（registry 为空或名称不匹配）")
        return
    print("%-34s %-28s %-28s %s" % ("skill", "installed", "manifest", "status"))
    for name, cur, tgt, st in rows:
        print("%-34s %-28s %-28s %s" % (name, cur, tgt, st))


def cmd_verify(root, names):
    """安装态完整性：registry.contentSha256 vs 当前目录重算。"""
    registry = load_registry(root)
    if not registry:
        print("registry 为空，无可校验内容")
        return
    bad = 0
    for name, rec in sorted(registry.items()):
        if names and name not in names:
            continue
        dest = root / name
        if not dest.exists():
            print("%-34s MISSING" % name)
            bad += 1
            continue
        expect = rec.get("contentSha256")
        if not expect:
            print("%-34s no-contentSha256（旧版 registry，重装一次即可补齐）" % name)
            continue
        actual = dir_sha256(dest)
        if actual == expect:
            print("%-34s OK" % name)
        else:
            print("%-34s MODIFIED（本地有改动；重装可恢复 manifest 版本，--rollback 可回备份）" % name)
            bad += 1
    if bad:
        raise SystemExit(1)


def cmd_rollback(root, name):
    """从最近的备份恢复技能目录。"""
    backup_root = root / BACKUP_DIR
    cands = sorted(backup_root.glob("%s-*.zip" % name)) if backup_root.exists() else []
    if not cands:
        raise SystemExit("没有可回滚的备份: %s" % name)
    latest = cands[-1]
    registry = load_registry(root)
    rec = registry.get(name, {})
    bak = backup_existing(root, name, rec.get("version") or "current")  # 回滚前也留快照
    if bak:
        print("pre-rollback snapshot: %s" % bak)
    dest = root / name
    if dest.exists():
        shutil.rmtree(dest)
    dest.mkdir(parents=True, exist_ok=True)
    safe_extract(latest, dest)
    rec.update({"name": name, "version": "rolled-back:%s" % latest.stem,
                "contentSha256": dir_sha256(dest),
                "installedAt": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
                "root": str(dest)})
    save_registry(root, [rec])
    print("rolled back: %s <- %s" % (dest, latest.name))


def main():
    # Windows 管道截断（如 | Select-Object -First N）会把 stdout 变成坏管道，
    # 后续 print 抛 OSError/EINVAL 导致「安装成功却报错」。换用容错 writer。
    try:
        sys.stdout = _QuietWriter()
    except (AttributeError, OSError):
        pass
    ap = argparse.ArgumentParser(description="ITSM skills 安装器（零依赖）")
    ap.add_argument("--manifest", default="", help="本地 manifest.json 路径")
    ap.add_argument("--base", default="", help="远程 base（如 http://host:8089/api/v1/skills），P2 形态")
    ap.add_argument("--name", action="append", default=[],
                    help="要安装的技能名，可重复；支持 name@version 版本锁定")
    ap.add_argument("--all", action="store_true", help="安装 manifest 中全部技能")
    ap.add_argument("--bundle", action="store_true", help="安装全量 bundle zip（itsm-skills-all.zip）")
    ap.add_argument("--host", default="codex", choices=["claude", "codex", "project"], help="宿主技能根映射")
    ap.add_argument("--project-root", default="", help="host=project 时的项目根（默认当前目录）")
    ap.add_argument("--check", action="store_true", help="dry-run：仅对比已装与 manifest 版本")
    ap.add_argument("--verify", action="store_true", help="安装态完整性校验（contentSha256）")
    ap.add_argument("--rollback", default="", metavar="SKILL_NAME", help="从最近备份回滚指定技能")
    args = ap.parse_args()

    root = host_root(args.host, args.project_root)

    # P3：verify / rollback / check 可独立运行（无需 manifest）
    if args.verify:
        root.mkdir(parents=True, exist_ok=True)
        cmd_verify(root, set(args.name))
        return 0
    if args.rollback:
        root.mkdir(parents=True, exist_ok=True)
        cmd_rollback(root, args.rollback)
        return 0

    manifest, src_dir = load_manifest(args)
    if args.check:
        root.mkdir(parents=True, exist_ok=True)
        wanted = {n.split("@", 1)[0] for n in args.name}
        cmd_check(manifest, root, wanted if wanted else set())
        return 0

    root.mkdir(parents=True, exist_ok=True)
    print("host=%s root=%s" % (args.host, root))

    installed = []
    if args.bundle:
        bundle = manifest.get("bundle") or {}
        if not bundle:
            raise SystemExit("manifest 无 bundle 段")
        zip_path = src_dir / bundle["file"]
        digest = sha256_file(zip_path)
        if digest != bundle["sha256"]:
            raise SystemExit("bundle sha256 校验失败")
        # bundle 是整根覆盖：同样留备份（按 manifest 版本对比 registry 任意一条）
        registry = load_registry(root)
        prev_any = next(iter(registry.values()), None)
        if prev_any and prev_any.get("version") != (manifest.get("sourceCommit") or "")[:8]:
            bak = backup_existing(root, "itsm-skills-all", prev_any.get("version"))
            if bak:
                print("backup: bundle -> %s" % bak)
        safe_extract(zip_path, root)
        installed.append({"name": "itsm-skills-all", "version": manifest.get("sourceCommit", "")[:8],
                          "sha256": digest,
                          "installedAt": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
                          "root": str(root)})
    else:
        skills = manifest.get("skills") or []
        # --name 支持 name@version 版本锁定：裸名用于匹配 manifest，@ 后缀作为期望版本
        requested = {n.split("@", 1)[0]: (n.split("@", 1)[1] if "@" in n else None) for n in args.name}
        targets = skills if args.all else [s for s in skills if s["name"] in requested]
        if not args.all and not args.name:
            raise SystemExit("未指定 --name/--all/--bundle")
        missing = set(requested) - {s["name"] for s in skills}
        if missing and not args.all:
            raise SystemExit("manifest 中不存在: %s" % ", ".join(sorted(missing)))
        for entry in targets:
            pin = requested.get(entry["name"])
            installed.append(install_one(entry, src_dir, root, wanted_version=pin))
            print("installed: %s@%s -> %s" % (entry["name"], entry.get("version", ""), root / entry["name"]))

    save_registry(root, installed)
    print("registry: %s" % (root / REGISTRY_FILE))
    print("验证示例：python <root>/<skill>/scripts/<cli>.py --help")
    return 0


if __name__ == "__main__":
    code = main()
    try:
        sys.stdout.flush()
    except (OSError, ValueError):
        pass  # 管道截断时的收尾写入失败不影响安装结果
    sys.exit(code)
