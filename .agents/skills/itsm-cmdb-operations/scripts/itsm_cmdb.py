#!/usr/bin/env python3
"""ITSM CMDB REST API CLI（零依赖，Python 3.8+，仅标准库）。

P0 骨架：读面全量（CI/类型/关系/视图/本体/发现健康/对账），写面暂缓（见 SKILL.md 待办）。
公共层（认证/输出/错误/token 落盘）使用共享底座 ../_lib/itsm_common.py，勿在本文件复制。

用法（demo 环境）：
  python scripts/itsm_cmdb.py login --base http://172.18.3.238:8089/api/v1 \\
      --username demo.mfg.admin --password 'Demo@2026!' --save
  python scripts/itsm_cmdb.py --base http://172.18.3.238:8089/api/v1 --token-file "$HOME/.itsm/itsm.token" cis-stats
"""
import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "_lib"))  # 分发态：单技能包内嵌 _lib
if not (Path(sys.path[0]) / "itsm_common.py").exists():
    sys.path[0] = str(Path(__file__).resolve().parents[2] / "_lib")  # 开发态：仓库 skills 根 _lib
from itsm_common import (  # noqa: E402
    add_common_args,
    add_login_parser,
    authed,
    Client,
    emit,
    fail,
    ApiError,
    save_login_token,
)


# ------------------------- 子命令实现 -------------------------

def cmd_login(client, args):
    result = client.login(args.username, args.password, args.tenant_code)
    if args.save:
        save_login_token(result, args.save, tool_name="itsm_cmdb")
    result["ok"] = True
    return result


def _get(client, path, params=None):
    if params:
        from urllib.parse import urlencode
        clean = {k: v for k, v in params.items() if v not in (None, "", [])}
        if clean:
            path = path + "?" + urlencode(clean, doseq=True)
    return client.request("GET", path)


def cmd_cis_list(client, args):
    return _get(client, "/cmdb/cis", {
        "page": args.page, "size": args.size, "ciTypeId": args.ci_type_id, "ciType": args.ci_type,
        "status": args.status, "environment": args.environment, "criticality": args.criticality,
        "search": args.search, "ciNumber": args.ci_number, "sortBy": args.sort_by,
        "sortOrder": args.sort_order, "withRelations": args.with_relations,
    })


def cmd_cis_get(client, args):
    return client.request("GET", "/cmdb/cis/%s" % args.id)


def cmd_cis_search(client, args):
    filters = {}
    for item in args.filter or []:
        if "=" not in item:
            raise ApiError("--filter 需要 key=value 形式: %s" % item)
        key, value = item.split("=", 1)
        filters[key.strip()] = value
    body = {"filters": filters, "page": args.page, "pageSize": args.page_size}
    if args.sort_by:
        body["sortBy"] = args.sort_by
    if args.sort_order:
        body["sortOrder"] = args.sort_order
    return client.request("POST", "/cmdb/cis/search", body)


def cmd_cis_stats(client, _args):
    return client.request("GET", "/cmdb/cis/stats")


def cmd_cis_history(client, args):
    return client.request("GET", "/cmdb/cis/%s/history" % args.id)


def cmd_cis_topology(client, args):
    return client.request("GET", "/cmdb/cis/%s/topology" % args.id)


def cmd_cis_impact(client, args):
    return client.request("GET", "/cmdb/cis/%s/impact-analysis" % args.id)


def cmd_cis_relationships(client, args):
    return client.request("GET", "/cmdb/cis/%s/relationships" % args.id)


def cmd_types_list(client, _args):
    return client.request("GET", "/cmdb/ci-types")


def cmd_type_get(client, args):
    return client.request("GET", "/cmdb/ci-types/%s" % args.id)


def cmd_type_attributes(client, args):
    return client.request("GET", "/cmdb/ci-types/%s/attributes" % args.id)


def cmd_ontology(client, _args):
    return client.request("GET", "/cmdb/ontology")


def cmd_capabilities(client, _args):
    return client.request("GET", "/cmdb/capabilities")


def cmd_rel_types(client, _args):
    return client.request("GET", "/cmdb/relationship-types")


def cmd_rel_list(client, args):
    return _get(client, "/cmdb/relationships", {"page": args.page, "size": args.size})


def cmd_rel_get(client, args):
    return client.request("GET", "/cmdb/relationships/%s" % args.id)


def cmd_views_list(client, _args):
    return client.request("GET", "/cmdb/views")


def cmd_discovery_health(client, _args):
    return client.request("GET", "/cmdb/discovery/health")


def cmd_reconciliation(client, _args):
    return client.request("GET", "/cmdb/reconciliation")


def _json_kv(pairs, flag="--set"):
    """key=value 转 body 片段；value 尝试 JSON 解析（数字/布尔/对象），失败按字符串。"""
    import json as _json
    out = {}
    for item in pairs or []:
        if "=" not in item:
            raise ApiError("%s 需要 key=value 形式: %s" % (flag, item))
        key, value = item.split("=", 1)
        key = key.strip()
        try:
            out[key] = _json.loads(value)
        except ValueError:
            out[key] = value
    return out


def cmd_ci_create(client, args):
    body = _json_kv(args.set)
    for key in ("name", "status"):
        if key not in body:
            raise ApiError("创建 CI 必填 --set name=... --set status=...（如 active）")
    if args.ci_type_id:
        body["ciTypeId"] = args.ci_type_id
    return client.request("POST", "/cmdb/cis", body)


def cmd_ci_update(client, args):
    body = _json_kv(args.set)
    if not body:
        raise ApiError("更新 CI 至少一个 --set key=value；version 用 --version 指定乐观锁（0=取当前）")
    if args.version is not None:
        body["version"] = args.version
    return client.request("PUT", "/cmdb/cis/%s" % args.id, body)


def cmd_ci_delete(client, args):
    return client.request("DELETE", "/cmdb/cis/%s" % args.id)


def cmd_batch_create(client, args):
    items = []
    for item in args.item or []:
        items.append(_json_kv([item]))
    if not items:
        raise ApiError("batch-create 需要至少一个 --item（JSON 对象或 key=value 串）")
    return client.request("POST", "/cmdb/cis/batch", {"items": items})


def cmd_batch_update(client, args):
    ids = [int(x) for x in args.ids.split(",") if x.strip()]
    updates = _json_kv(args.set)
    if not ids or not updates:
        raise ApiError("batch-update 需要 --ids 1,2,3 与至少一个 --set key=value")
    if args.version is not None:
        updates["version"] = args.version
    return client.request("PUT", "/cmdb/cis/batch", {"ids": ids, "updates": updates})


def cmd_batch_delete(client, args):
    ids = [int(x) for x in args.ids.split(",") if x.strip()]
    if not ids:
        raise ApiError("batch-delete 需要 --ids 1,2,3")
    return client.request("DELETE", "/cmdb/cis/batch", {"ids": ids})


def cmd_lifecycle_update(client, args):
    if not args.status:
        raise ApiError("lifecycle-update 需要 --status（draft/online/maintenance/offline/scrapped），经 query 传参")
    path = "/cmdb/cis/%s/lifecycle?status=%s" % (args.id, args.status)
    if args.remark:
        from urllib.parse import quote
        path += "&remark=%s" % quote(args.remark, safe="")
    return client.request("PUT", path)


def cmd_lifecycle_history(client, args):
    return client.request("GET", "/cmdb/cis/%s/lifecycle/history" % args.id)


def cmd_batch_lifecycle(client, args):
    ids = [int(x) for x in args.ids.split(",") if x.strip()]
    if not ids or not args.status:
        raise ApiError("batch-lifecycle 需要 --ids 1,2,3 与 --status")
    body = {"ids": ids, "status": args.status}
    if args.remark:
        body["remark"] = args.remark
    return client.request("PUT", "/cmdb/cis/batch/lifecycle", body)


def cmd_ci_revert(client, args):
    if not args.version:
        raise ApiError("revert 需要 --version N（目标历史版本号）")
    body = {"version": args.version}
    if args.remark:
        body["remark"] = args.remark
    return client.request("POST", "/cmdb/cis/%s/revert" % args.id, body)


def cmd_rel_create(client, args):
    if not args.source or not args.target or not args.rel_type:
        raise ApiError("rel-create 必填 --source CI_ID --target CI_ID --rel-type（depends_on/hosts/hosted_on/connects_to/runs_on/contains/part_of/impacts/impacted_by/owns…）")
    body = {"sourceCiId": args.source, "targetCiId": args.target, "relationshipType": args.rel_type}
    if args.strength:
        body["strength"] = args.strength
    if args.impact:
        body["impactLevel"] = args.impact
    if args.description:
        body["description"] = args.description
    if args.discovered is not None:
        body["isDiscovered"] = args.discovered.lower() in ("1", "true", "yes")
    return client.request("POST", "/cmdb/relationships", body)


def cmd_rel_update(client, args):
    body = {}
    if args.rel_type:
        body["relationshipType"] = args.rel_type
    if args.strength:
        body["strength"] = args.strength
    if args.impact:
        body["impactLevel"] = args.impact
    if args.description:
        body["description"] = args.description
    if args.active is not None:
        body["isActive"] = args.active.lower() in ("1", "true", "yes")
    if not body:
        raise ApiError("rel-update 至少一个可改字段")
    return client.request("PUT", "/cmdb/relationships/%s" % args.id, body)


def cmd_rel_delete(client, args):
    return client.request("DELETE", "/cmdb/relationships/%s" % args.id)


# ---------------- 长尾：导入导出 / 云资源三件套 / 发现（README §5 待办）----------------

def cmd_import_create(client, args):
    if not args.file_url or not args.file_type or not args.update_mode:
        raise ApiError("import-create 必填 --file-url(合法 URL) --file-type xlsx|csv --update-mode skip|overwrite|merge")
    body = {"fileUrl": args.file_url, "fileType": args.file_type, "updateMode": args.update_mode}
    if args.sheet_name:
        body["sheetName"] = args.sheet_name
    return client.request("POST", "/cmdb/import", body)


def cmd_import_status(client, args):
    return client.request("GET", "/cmdb/import/%s" % args.task_id)


def cmd_import_list(client, _args):
    return client.request("GET", "/cmdb/import")


def cmd_export_create(client, args):
    if args.export_type not in ("xlsx", "csv"):
        raise ApiError("export-create 的 --export-type 仅 xlsx|csv")
    body = {"exportType": args.export_type}
    if args.filter:
        body["filters"] = _json_kv(args.filter, flag="--filter")
    if args.fields:
        body["exportFields"] = [f for f in args.fields.split(",") if f.strip()]
    return client.request("POST", "/cmdb/export", body)


def cmd_export_status(client, args):
    return client.request("GET", "/cmdb/export/%s" % args.task_id)


def cmd_export_list(client, _args):
    return client.request("GET", "/cmdb/export")


def cmd_cloud_accounts(client, _args):
    return client.request("GET", "/cmdb/cloud-accounts")


def cmd_cloud_account_get(client, args):
    return client.request("GET", "/cmdb/cloud-accounts/%s" % args.id)


def cmd_cloud_account_create(client, args):
    body = _json_kv(args.set)
    for key in ("provider", "accountId", "accountName"):
        if key not in body:
            raise ApiError("cloud-account-create 必填 --set provider=... --set accountId=... --set accountName=...")
    return client.request("POST", "/cmdb/cloud-accounts", body)


def cmd_cloud_account_update(client, args):
    body = _json_kv(args.set)
    if not body:
        raise ApiError("cloud-account-update 至少一个 --set")
    return client.request("PUT", "/cmdb/cloud-accounts/%s" % args.id, body)


def cmd_cloud_account_delete(client, args):
    return client.request("DELETE", "/cmdb/cloud-accounts/%s" % args.id)


def cmd_cloud_services(client, _args):
    return client.request("GET", "/cmdb/cloud-services")


def cmd_cloud_service_get(client, args):
    return client.request("GET", "/cmdb/cloud-services/%s" % args.id)


def cmd_cloud_resources(client, args):
    return _get(client, "/cmdb/cloud-resources", {
        "page": args.page, "size": args.size, "provider": args.provider, "region": args.region,
    })


def cmd_cloud_resource_get(client, args):
    return client.request("GET", "/cmdb/cloud-resources/%s" % args.id)


def cmd_cloud_resource_create(client, args):
    body = _json_kv(args.set)
    for key in ("cloudAccountId", "serviceId", "resourceId"):
        if key not in body:
            raise ApiError("cloud-resource-create 必填 --set cloudAccountId=N --set serviceId=N --set resourceId=...")
    return client.request("POST", "/cmdb/cloud-resources", body)


def cmd_cloud_resource_update(client, args):
    body = _json_kv(args.set)
    if not body:
        raise ApiError("cloud-resource-update 至少一个 --set")
    return client.request("PUT", "/cmdb/cloud-resources/%s" % args.id, body)


def cmd_cloud_resource_delete(client, args):
    return client.request("DELETE", "/cmdb/cloud-resources/%s" % args.id)


def cmd_discovery_sources(client, _args):
    return client.request("GET", "/cmdb/discovery/sources")


def cmd_discovery_source_create(client, args):
    body = _json_kv(args.set)
    for key in ("name", "sourceType"):
        if key not in body:
            raise ApiError("discovery-source-create 必填 --set name=... --set sourceType=...（reconcilePolicy 可选 manual|discovered_wins|cmdb_wins）")
    return client.request("POST", "/cmdb/discovery/sources", body)


def cmd_discovery_job_create(client, args):
    if not args.source_id:
        raise ApiError("discovery-job-create 需要 --source-id（DiscoveryJobRequest {sourceId}）")
    return client.request("POST", "/cmdb/discovery/jobs", {"sourceId": args.source_id})


def cmd_discovery_results(client, args):
    return _get(client, "/cmdb/discovery/results", {"page": args.page, "size": args.size})


COMMANDS = {
    "login": (cmd_login, False),
    "cis-list": (cmd_cis_list, True),
    "cis-get": (cmd_cis_get, True),
    "cis-search": (cmd_cis_search, True),
    "cis-stats": (cmd_cis_stats, True),
    "cis-history": (cmd_cis_history, True),
    "cis-topology": (cmd_cis_topology, True),
    "cis-impact": (cmd_cis_impact, True),
    "cis-relationships": (cmd_cis_relationships, True),
    "types-list": (cmd_types_list, True),
    "type-get": (cmd_type_get, True),
    "type-attributes": (cmd_type_attributes, True),
    "ontology": (cmd_ontology, True),
    "capabilities": (cmd_capabilities, True),
    "rel-types": (cmd_rel_types, True),
    "rel-list": (cmd_rel_list, True),
    "rel-get": (cmd_rel_get, True),
    "views-list": (cmd_views_list, True),
    "discovery-health": (cmd_discovery_health, True),
    "reconciliation": (cmd_reconciliation, True),
    "ci-create": (cmd_ci_create, True),
    "ci-update": (cmd_ci_update, True),
    "ci-delete": (cmd_ci_delete, True),
    "batch-create": (cmd_batch_create, True),
    "batch-update": (cmd_batch_update, True),
    "batch-delete": (cmd_batch_delete, True),
    "lifecycle-update": (cmd_lifecycle_update, True),
    "lifecycle-history": (cmd_lifecycle_history, True),
    "batch-lifecycle": (cmd_batch_lifecycle, True),
    "revert": (cmd_ci_revert, True),
    "rel-create": (cmd_rel_create, True),
    "rel-update": (cmd_rel_update, True),
    "rel-delete": (cmd_rel_delete, True),
    "import-create": (cmd_import_create, True),
    "import-status": (cmd_import_status, True),
    "import-list": (cmd_import_list, True),
    "export-create": (cmd_export_create, True),
    "export-status": (cmd_export_status, True),
    "export-list": (cmd_export_list, True),
    "cloud-accounts": (cmd_cloud_accounts, True),
    "cloud-account-get": (cmd_cloud_account_get, True),
    "cloud-account-create": (cmd_cloud_account_create, True),
    "cloud-account-update": (cmd_cloud_account_update, True),
    "cloud-account-delete": (cmd_cloud_account_delete, True),
    "cloud-services": (cmd_cloud_services, True),
    "cloud-service-get": (cmd_cloud_service_get, True),
    "cloud-resources": (cmd_cloud_resources, True),
    "cloud-resource-get": (cmd_cloud_resource_get, True),
    "cloud-resource-create": (cmd_cloud_resource_create, True),
    "cloud-resource-update": (cmd_cloud_resource_update, True),
    "cloud-resource-delete": (cmd_cloud_resource_delete, True),
    "discovery-sources": (cmd_discovery_sources, True),
    "discovery-source-create": (cmd_discovery_source_create, True),
    "discovery-job-create": (cmd_discovery_job_create, True),
    "discovery-results": (cmd_discovery_results, True),
}


# ------------------------- CLI -------------------------

def build_parser():
    parser = argparse.ArgumentParser(prog="itsm_cmdb.py", description="ITSM CMDB REST API CLI（零依赖）")
    add_common_args(parser)
    sub = parser.add_subparsers(dest="command", required=True)
    common_sub = argparse.ArgumentParser(add_help=False)
    add_common_args(common_sub, suppressed=True)

    def add(name, help_text):
        return sub.add_parser(name, help=help_text, parents=[common_sub])

    add_login_parser(sub, add)

    cl = add("cis-list", "GET /cmdb/cis（列表/过滤/分页）")
    cl.add_argument("--page", type=int, default=1)
    cl.add_argument("--size", type=int, default=20, help="每页数量（1-200）")
    cl.add_argument("--ci-type-id", type=int, default=0)
    cl.add_argument("--ci-type", default="")
    cl.add_argument("--status", default="")
    cl.add_argument("--environment", default="")
    cl.add_argument("--criticality", default="")
    cl.add_argument("--search", default="", help="模糊搜索（名称/序列号/型号/厂商/云资源ID/位置等宽语义）")
    cl.add_argument("--ci-number", default="", help="CI 唯一业务编号精确匹配")
    cl.add_argument("--sort-by", default="")
    cl.add_argument("--sort-order", default="", choices=["", "asc", "desc"])
    cl.add_argument("--with-relations", action="store_true")

    g = add("cis-get", "GET /cmdb/cis/:id（详情）")
    g.add_argument("id")

    cs = add("cis-search", "POST /cmdb/cis/search（高级搜索，filters JSON）")
    cs.add_argument(
        "--filter",
        action="append",
        default=[],
        help="key=value，可重复；合法键=CISearchFilter json 字段（keyword/ciTypeId/status/environment/"
        "criticality/assetTag/serialNumber/vendor/location/assignedTo/ownedBy/tagIds/attributes/"
        "dateFrom/dateTo/cloudProvider/cloudRegion/cloudResourceId）；未知键被服务端静默忽略",
    )
    cs.add_argument("--page", type=int, default=1)
    cs.add_argument("--page-size", type=int, default=20)
    cs.add_argument("--sort-by", default="")
    cs.add_argument("--sort-order", default="", choices=["", "asc", "desc"])

    add("cis-stats", "GET /cmdb/cis/stats（按状态/类型/环境/关键度分组统计）")

    for name, help_text in [
        ("cis-history", "GET /cmdb/cis/:id/history（变更历史）"),
        ("cis-topology", "GET /cmdb/cis/:id/topology（拓扑）"),
        ("cis-impact", "GET /cmdb/cis/:id/impact-analysis（影响分析）"),
        ("cis-relationships", "GET /cmdb/cis/:id/relationships（关系）"),
    ]:
        c = add(name, help_text)
        c.add_argument("id")

    add("types-list", "GET /cmdb/ci-types")
    tg = add("type-get", "GET /cmdb/ci-types/:id")
    tg.add_argument("id")
    ta = add("type-attributes", "GET /cmdb/ci-types/:id/attributes（属性定义）")
    ta.add_argument("id")

    add("ontology", "GET /cmdb/ontology（AI-Native 本体自描述：类型×属性×关系词表×枚举）")
    add("capabilities", "GET /cmdb/capabilities")
    add("rel-types", "GET /cmdb/relationship-types")

    rl = add("rel-list", "GET /cmdb/relationships")
    rl.add_argument("--page", type=int, default=1)
    rl.add_argument("--size", type=int, default=20)
    rg = add("rel-get", "GET /cmdb/relationships/:id")
    rg.add_argument("id")

    add("views-list", "GET /cmdb/views（保存的搜索视图）")
    add("discovery-health", "GET /cmdb/discovery/health")
    add("reconciliation", "GET /cmdb/reconciliation（对账）")

    # ---------------- 写面（cmdb:write / cmdb:delete）----------------
    cc = add("ci-create", "POST /cmdb/cis（创建 CI；--set key=value 可重复，value 支持 JSON 字面量）")
    cc.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    cc.add_argument("--ci-type-id", type=int, default=0)

    cu = add("ci-update", "PUT /cmdb/cis/:id（部分更新；--version 乐观锁）")
    cu.add_argument("id")
    cu.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    cu.add_argument("--version", type=int, default=None)

    cd = add("ci-delete", "DELETE /cmdb/cis/:id（软删）")
    cd.add_argument("id")

    bc = add("batch-create", "POST /cmdb/cis/batch（≤100 条）")
    bc.add_argument("--item", action="append", default=[], metavar="KEY=VALUE|JSON", help="一个 CI 的字段；可重复")

    bu = add("batch-update", "PUT /cmdb/cis/batch（同字段批量改）")
    bu.add_argument("--ids", required=True, help="逗号分隔 CI id")
    bu.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    bu.add_argument("--version", type=int, default=None)

    bd = add("batch-delete", "DELETE /cmdb/cis/batch")
    bd.add_argument("--ids", required=True, help="逗号分隔 CI id")

    lu = add("lifecycle-update", "PUT /cmdb/cis/:id/lifecycle?status=&remark=（query 传参，非 JSON body）")
    lu.add_argument("id")
    lu.add_argument("--status", required=True, choices=["draft", "online", "maintenance", "offline", "scrapped"])
    lu.add_argument("--remark", default="")

    lh = add("lifecycle-history", "GET /cmdb/cis/:id/lifecycle/history")
    lh.add_argument("id")

    bl = add("batch-lifecycle", "PUT /cmdb/cis/batch/lifecycle")
    bl.add_argument("--ids", required=True)
    bl.add_argument("--status", required=True, choices=["draft", "online", "maintenance", "offline", "scrapped"])
    bl.add_argument("--remark", default="")

    rv = add("revert", "POST /cmdb/cis/:id/revert（版本回滚）")
    rv.add_argument("id")
    rv.add_argument("--version", type=int, required=True)
    rv.add_argument("--remark", default="")

    rc = add("rel-create", "POST /cmdb/relationships（创建关系）")
    rc.add_argument("--source", type=int, required=True, help="源 CI id")
    rc.add_argument("--target", type=int, required=True, help="目标 CI id")
    rc.add_argument("--rel-type", required=True,
                    help="depends_on/hosts/hosted_on/connects_to/runs_on/contains/part_of/impacts/impacted_by/owns")
    rc.add_argument("--strength", default="", choices=["", "critical", "high", "medium", "low"])
    rc.add_argument("--impact", default="", choices=["", "critical", "high", "medium", "low"])
    rc.add_argument("--description", default="")
    rc.add_argument("--discovered", default=None, help="true/false（isDiscovered）")

    ru = add("rel-update", "PUT /cmdb/relationships/:id")
    ru.add_argument("id")
    ru.add_argument("--rel-type", default="")
    ru.add_argument("--strength", default="", choices=["", "critical", "high", "medium", "low"])
    ru.add_argument("--impact", default="", choices=["", "critical", "high", "medium", "low"])
    ru.add_argument("--description", default="")
    ru.add_argument("--active", default=None, help="true/false（isActive）")

    rd = add("rel-delete", "DELETE /cmdb/relationships/:id")
    rd.add_argument("id")

    # ---------------- 长尾：导入导出（任务制）/ 云三件套 / 发现 ----------------
    ic = add("import-create", "POST /cmdb/import（ImportCIRequest：fileUrl/fileType/updateMode 必填）")
    ic.add_argument("--file-url", default="")
    ic.add_argument("--file-type", default="", choices=["", "xlsx", "csv"])
    ic.add_argument("--update-mode", default="", choices=["", "skip", "overwrite", "merge"])
    ic.add_argument("--sheet-name", default="")
    isx = add("import-status", "GET /cmdb/import/:task_id")
    isx.add_argument("task_id")
    add("import-list", "GET /cmdb/import")

    ec = add("export-create", "POST /cmdb/export（ExportCIRequest：exportType 必填；filters/fields 可选）")
    ec.add_argument("--export-type", required=True, choices=["xlsx", "csv"])
    ec.add_argument("--filter", action="append", default=[], metavar="KEY=VALUE")
    ec.add_argument("--fields", default="", help="逗号分隔导出字段")
    esx = add("export-status", "GET /cmdb/export/:task_id")
    esx.add_argument("task_id")
    add("export-list", "GET /cmdb/export")

    ca = add("cloud-accounts", "GET /cmdb/cloud-accounts")
    cag = add("cloud-account-get", "GET /cmdb/cloud-accounts/:id")
    cag.add_argument("id")
    cac = add("cloud-account-create", "POST /cmdb/cloud-accounts（--set provider/accountId/accountName 必填）")
    cac.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    cau = add("cloud-account-update", "PUT /cmdb/cloud-accounts/:id")
    cau.add_argument("id")
    cau.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    cad = add("cloud-account-delete", "DELETE /cmdb/cloud-accounts/:id")
    cad.add_argument("id")

    cs = add("cloud-services", "GET /cmdb/cloud-services")
    csg = add("cloud-service-get", "GET /cmdb/cloud-services/:id")
    csg.add_argument("id")

    crl = add("cloud-resources", "GET /cmdb/cloud-resources（分页+provider/region 过滤）")
    crl.add_argument("--page", type=int, default=1)
    crl.add_argument("--size", type=int, default=20)
    crl.add_argument("--provider", default="")
    crl.add_argument("--region", default="")
    crg = add("cloud-resource-get", "GET /cmdb/cloud-resources/:id")
    crg.add_argument("id")
    crc = add("cloud-resource-create", "POST /cmdb/cloud-resources（--set cloudAccountId/serviceId/resourceId 必填）")
    crc.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    cru = add("cloud-resource-update", "PUT /cmdb/cloud-resources/:id")
    cru.add_argument("id")
    cru.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    crd = add("cloud-resource-delete", "DELETE /cmdb/cloud-resources/:id")
    crd.add_argument("id")

    dsl = add("discovery-sources", "GET /cmdb/discovery/sources")
    dsc = add("discovery-source-create", "POST /cmdb/discovery/sources（--set name/sourceType 必填）")
    dsc.add_argument("--set", action="append", default=[], metavar="KEY=VALUE")
    djc = add("discovery-job-create", "POST /cmdb/discovery/jobs（{sourceId}）")
    djc.add_argument("--source-id", default="")
    drl = add("discovery-results", "GET /cmdb/discovery/results")
    drl.add_argument("--page", type=int, default=1)
    drl.add_argument("--size", type=int, default=20)

    return parser


def main(argv=None):
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")
    args = build_parser().parse_args(argv)
    pretty = not args.compact
    handler, needs_auth = COMMANDS[args.command]
    try:
        client = authed(args) if needs_auth else Client(args.base, "", args.timeout)
        result = handler(client, args)
        emit(result if result is not None else {"ok": True}, pretty)
        return 0
    except ApiError as err:
        fail(err, pretty)
    except KeyboardInterrupt:
        return 130


if __name__ == "__main__":
    sys.exit(main())
