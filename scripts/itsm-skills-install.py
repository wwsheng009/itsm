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
"""
import argparse
import hashlib
import json
import os
import shutil
import sys
import tempfile
import urllib.request
import zipfile
from datetime import datetime, timezone
from pathlib import Path

REGISTRY_FILE = ".skills-installed.json"


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


def load_manifest(args):
    if args.manifest:
        return json.loads(Path(args.manifest).read_text(encoding="utf-8")), Path(args.manifest).parent
    if args.base:
        url = args.base.rstrip("/") + "/manifest.json"
        tmp = Path(tempfile.mkdtemp(prefix="itsm-skills-")) / "manifest.json"
        urllib.request.urlretrieve(url, tmp)  # nosec - P2 服务端为可信内网
        return json.loads(tmp.read_text(encoding="utf-8")), tmp.parent
    raise SystemExit("需要 --manifest 或 --base")


def safe_extract(zip_path, target_dir):
    """防 zip-slip：所有条目必须落在 target_dir 内。"""
    target = target_dir.resolve()
    with zipfile.ZipFile(zip_path) as zf:
        for info in zf.infolist():
            dest = (target / info.filename).resolve()
            if not str(dest).startswith(str(target)):
                raise SystemExit("检测到 zip 路径逃逸: %s" % info.filename)
        zf.extractall(target)


def install_one(entry, src_dir, root):
    zip_path = src_dir / entry["file"]
    if not zip_path.exists():
        raise SystemExit("包不存在: %s" % zip_path)
    digest = sha256_file(zip_path)
    if digest != entry["sha256"]:
        raise SystemExit("sha256 校验失败: %s\n  期望 %s\n  实际 %s" % (entry["file"], entry["sha256"], digest))

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
            "installedAt": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "root": str(dest)}


def update_registry(root, records):
    reg = root / REGISTRY_FILE
    existing = {}
    if reg.exists():
        try:
            existing = {r["name"]: r for r in json.loads(reg.read_text(encoding="utf-8")).get("installed", [])}
        except (ValueError, KeyError):
            existing = {}
    for r in records:
        existing[r["name"]] = r
    reg.write_text(json.dumps({"installed": sorted(existing.values(), key=lambda x: x["name"])},
                              ensure_ascii=False, indent=2), encoding="utf-8")


def main():
    ap = argparse.ArgumentParser(description="ITSM skills 安装器（零依赖）")
    ap.add_argument("--manifest", default="", help="本地 manifest.json 路径")
    ap.add_argument("--base", default="", help="远程 base（如 http://host:8089/api/v1/skills），P2 形态")
    ap.add_argument("--name", action="append", default=[], help="要安装的技能名，可重复")
    ap.add_argument("--all", action="store_true", help="安装 manifest 中全部技能")
    ap.add_argument("--bundle", action="store_true", help="安装全量 bundle zip（itsm-skills-all.zip）")
    ap.add_argument("--host", default="codex", choices=["claude", "codex", "project"], help="宿主技能根映射")
    ap.add_argument("--project-root", default="", help="host=project 时的项目根（默认当前目录）")
    args = ap.parse_args()

    manifest, src_dir = load_manifest(args)
    root = host_root(args.host, args.project_root)
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
        safe_extract(zip_path, root)
        installed.append({"name": "itsm-skills-all", "version": manifest.get("sourceCommit", "")[:8],
                          "sha256": digest,
                          "installedAt": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
                          "root": str(root)})
    else:
        skills = manifest.get("skills") or []
        targets = skills if args.all else [s for s in skills if s["name"] in set(args.name)]
        if not args.all and not args.name:
            raise SystemExit("未指定 --name/--all/--bundle")
        wanted = set(args.name)
        missing = wanted - {s["name"] for s in skills}
        if missing and not args.all:
            raise SystemExit("manifest 中不存在: %s" % ", ".join(sorted(missing)))
        for entry in targets:
            installed.append(install_one(entry, src_dir, root))
            print("installed: %s@%s -> %s" % (entry["name"], entry.get("version", ""), root / entry["name"]))

    update_registry(root, installed)
    print("registry: %s" % (root / REGISTRY_FILE))
    print("验证示例：python <root>/<skill>/scripts/<cli>.py --help")
    return 0


if __name__ == "__main__":
    sys.exit(main())
