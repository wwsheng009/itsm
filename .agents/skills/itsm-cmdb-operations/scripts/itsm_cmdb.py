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

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "_lib"))
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
