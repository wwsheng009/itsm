#!/usr/bin/env pwsh
# =============================================================================
# 工作台跨租户单据 + 批量护栏探针（OB-6 / OB-7，FDB-B1 后续覆盖补强）
#
# 模式：
#   seed     —— 以目标租户管理员（默认 admin-custa1）播种样本：变更 / 问题 / 服务请求 各 1 条；
#   verify   —— 以 MSP 员工（默认 prov.agent1）验证读面：/msp/workbench/{changes,problems,service-requests}
#               能读到样本（按标题前缀匹配）；并记录 tasks 投影数量；
#   boundary —— 批量护栏端到端（MSP 员工）：
#               B1 101 条 → 400 BATCH_LIMIT_EXCEEDED；
#               B2 含失效条目（不存在的 ticketId）→ 200 逐条结果（成功 + 失败给原因）；
#               B3 高危动作 delete → 403 ACTION_NOT_ALLOWED；
#               B4 未分配客户条目（CUSTB1）→ 逐条失败 MSP_ALLOCATION_REQUIRED；
#   all      —— seed + verify + boundary。
#
# 前置：saas_msp 实例；凭据默认读 $env:TEMP\fdb-creds.json（admin / admin-custa1 / prov.agent1）。
# 产物：docs/multi-tenant/evidence/workbench-probes/run-summary-<ts>.md
# 退出码：0=无 FAIL；1=存在 FAIL；2=前置缺失
# =============================================================================
param(
    [ValidateSet("seed", "verify", "boundary", "all")][string]$Mode = "all",
    [string]$Base = "http://127.0.0.1:8090",
    [int]$TenantId = 4,
    [string]$TenantCode = "CUSTA1",
    [string]$AdminUser = "admin-custa1", [string]$AdminPass = "",
    [string]$MspUser = "prov.agent1", [string]$MspPass = "",
    [int]$ValidTicketId = 5, [int]$UnallocatedTicketId = 2, [int]$UnallocatedTenantId = 5,
    [string]$OutDir = ""
)

$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
if ([string]::IsNullOrWhiteSpace($OutDir)) { $OutDir = Join-Path $RepoRoot "docs\multi-tenant\evidence\workbench-probes" }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$ts = Get-Date -Format "yyyyMMdd-HHmmss"
$SummaryPath = Join-Path $OutDir "run-summary-$ts.md"

# ---------- 凭据（本地临时文件；不回显） ----------
$credsFile = Join-Path $env:TEMP "fdb-creds.json"
if (Test-Path $credsFile) {
    $creds = Get-Content $credsFile -Raw | ConvertFrom-Json
    if ([string]::IsNullOrWhiteSpace($AdminPass) -and $null -ne $creds.'admin-custa1') { $AdminPass = [string]$creds.'admin-custa1' }
    if ([string]::IsNullOrWhiteSpace($MspPass) -and $null -ne $creds.'prov.agent1') { $MspPass = [string]$creds.'prov.agent1' }
}
if ([string]::IsNullOrWhiteSpace($AdminPass)) { Write-Error "缺少租户管理员口令（-AdminPass 或 $credsFile 的 admin-custa1 键）"; exit 2 }
if ([string]::IsNullOrWhiteSpace($MspPass)) { Write-Error "缺少 MSP 员工口令（-MspPass 或 $credsFile 的 prov.agent1 键）"; exit 2 }

$script:Results = New-Object System.Collections.Generic.List[object]
function Add-Result([string]$id, [string]$title, [string]$status, [string]$evidence) {
    $color = switch ($status) { "PASS" { "Green" } "FAIL" { "Red" } "SKIP" { "Yellow" } default { "Gray" } }
    Write-Host ("[{0}] {1} {2} — {3}" -f $status, $id, $title, $evidence) -ForegroundColor $color
    $script:Results.Add([pscustomobject]@{ ID = $id; Title = $title; Status = $status; Evidence = $evidence })
}
function Pass([string]$id, [string]$title, [string]$evidence) { Add-Result $id $title "PASS" $evidence }
function Fail([string]$id, [string]$title, [string]$evidence) { Add-Result $id $title "FAIL" $evidence }
function Assert-Case([string]$id, [string]$title, [bool]$ok, [string]$evidence) { if ($ok) { Pass $id $title $evidence } else { Fail $id $title $evidence } }
function Short([string]$text, [int]$max = 200) {
    if ([string]::IsNullOrEmpty($text)) { return "" }
    if ($text.Length -le $max) { return $text }
    return $text.Substring(0, $max)
}

# ---------- HTTP ----------
function New-Session([string]$user, [string]$pass) {
    $sess = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $body = @{ username = $user; password = $pass } | ConvertTo-Json
    try {
        $resp = Invoke-WebRequest -Uri "$Base/api/v1/auth/login" -Method Post -Body $body -ContentType "application/json" -WebSession $sess -SkipHttpErrorCheck -TimeoutSec 25
    } catch { return $null }
    $json = $null
    try { $json = $resp.Content | ConvertFrom-Json } catch { }
    if ($null -eq $json -or $json.code -ne 0) { return $null }
    $cookie = $sess.Cookies.GetCookies($Base) | Where-Object { $_.Name -eq "access_token" } | Select-Object -First 1
    return [pscustomobject]@{ Name = $user; Session = $sess; Token = $cookie.Value }
}
function Invoke-Api {
    param(
        [Parameter(Mandatory)][string]$Method,
        [Parameter(Mandatory)][string]$Path,
        [object]$Sess = $null,
        [object]$Body = $null
    )
    $h = @{}
    if ($null -ne $Sess) { $h["Authorization"] = "Bearer $($Sess.Token)" }
    $p = @{ Uri = "$Base$Path"; Method = $Method; Headers = $h; SkipHttpErrorCheck = $true; TimeoutSec = 60 }
    if ($null -ne $Body) { $p["Body"] = ($Body | ConvertTo-Json -Depth 10); $p["ContentType"] = "application/json" }
    try { $resp = Invoke-WebRequest @p } catch { return [pscustomobject]@{ Status = -1; Json = $null; Raw = $_.Exception.Message } }
    $json = $null
    try { $json = $resp.Content | ConvertFrom-Json } catch { }
    return [pscustomobject]@{ Status = [int]$resp.StatusCode; Json = $json; Raw = $resp.Content }
}

# ---------- 汇总 ----------
function Write-Summary {
    $passCount = @($script:Results | Where-Object { $_.Status -eq 'PASS' }).Count
    $failCount = @($script:Results | Where-Object { $_.Status -eq 'FAIL' }).Count
    $lines = New-Object System.Collections.Generic.List[string]
    $lines.Add("# 工作台跨租户单据 + 批量护栏探针 run-summary（OB-6/OB-7）")
    $lines.Add("")
    $lines.Add("- 时间：$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')")
    $lines.Add("- 模式：$Mode｜环境：$Base｜目标租户：id=$TenantId（$TenantCode）")
    $lines.Add("- 结果：**PASS=$passCount / FAIL=$failCount**")
    $lines.Add("- 状态：**实测**｜定位：OB-6/OB-7 探针（三类单据跨租户读面 + 批量护栏边界端到端；设计见 backlog）")
    $lines.Add("")
    $lines.Add("| # | 用例 | 状态 | 证据 |")
    $lines.Add("|---|---|---|---|")
    foreach ($r in $script:Results) {
        $ev = ("" + $r.Evidence) -replace '\|', '/'
        $lines.Add("| $($r.ID) | $($r.Title) | $($r.Status) | $ev |")
    }
    Set-Content -Path $SummaryPath -Value ($lines -join "`n") -Encoding utf8
    Write-Host "run-summary: $SummaryPath"
    return $failCount
}

# ---------- 主流程 ----------
$admin = New-Session $AdminUser $AdminPass
Assert-Case "E1" "租户管理员登录（$AdminUser）" ($null -ne $admin) "ok=$($null -ne $admin)"
$msp = New-Session $MspUser $MspPass
Assert-Case "E2" "MSP 员工登录（$MspUser）" ($null -ne $msp) "ok=$($null -ne $msp)"
if ($null -eq $admin -or $null -eq $msp) { Write-Summary; exit 1 }

$sampleTitlePrefix = "[FDB-B1]"

if ($Mode -in @("seed", "all")) {
    # 幂等播种：先查同名样本，存在则跳过（列表响应字段按域兼容 items/changes/problems/serviceRequests）。
    function Ensure-Sample([string]$listPath, [string]$createPath, [hashtable]$body, [string]$caseId, [string]$label, [string]$title) {
        $ls = Invoke-Api -Method Get -Path $listPath -Sess $admin
        $arr = @()
        if ($ls.Status -eq 200 -and $null -ne $ls.Json.data) {
            foreach ($k in @('items', 'changes', 'problems', 'serviceRequests')) {
                $v = $ls.Json.data.$k
                if ($null -ne $v) { $arr = @($v); break }
            }
        }
        $exists = @($arr | Where-Object { $_.title -eq $title }).Count -ge 1
        if ($exists) { Pass $caseId "播种$label（已存在，跳过）" "exists=true"; return }
        $cr = Invoke-Api -Method Post -Path $createPath -Sess $admin -Body $body
        Assert-Case $caseId "播种$label" (($cr.Status -ge 200) -and ($cr.Status -lt 300)) "status=$($cr.Status) body=$(Short $cr.Raw 160)"
    }

    Ensure-Sample "/api/v1/changes?page=1&pageSize=50" "/api/v1/changes" @{
        title = "$sampleTitlePrefix 跨租户变更样本（OB-6）"; description = "跨租户变更读面样本：验证工作台变更页签与状态推进。"
        type = "normal"; priority = "medium"; riskLevel = "low"
    } "S1" "变更样本" "$sampleTitlePrefix 跨租户变更样本（OB-6）"

    Ensure-Sample "/api/v1/problems?page=1&pageSize=50" "/api/v1/problems" @{
        title = "$sampleTitlePrefix 跨租户问题样本（OB-6）"; description = "跨租户问题读面样本：验证工作台问题页签。"; priority = "medium"
    } "S2" "问题样本" "$sampleTitlePrefix 跨租户问题样本（OB-6）"

    # 服务请求需要 catalogId；目标租户无目录项时先建一条最小目录（S3a）。
    $catalogId = 0
    $cl = Invoke-Api -Method Get -Path "/api/v1/service-catalogs?page=1&size=10" -Sess $admin
    if ($cl.Status -eq 200 -and $null -ne $cl.Json.data -and $null -ne $cl.Json.data.catalogs -and @($cl.Json.data.catalogs).Count -ge 1) {
        $catalogId = [int]@($cl.Json.data.catalogs)[0].id
        Pass "S3a" "服务目录项就绪（复用既有）" "catalogId=$catalogId"
    } else {
        $cc = Invoke-Api -Method Post -Path "/api/v1/service-catalogs" -Sess $admin -Body @{
            name = "$sampleTitlePrefix 服务目录项（OB-6）"; category = "custom"; description = "OB-6 服务请求样本前置目录项。"
        }
        if (($cc.Status -ge 200) -and ($cc.Status -lt 300) -and $null -ne $cc.Json.data) { $catalogId = [int]$cc.Json.data.id }
        Assert-Case "S3a" "服务目录项创建（SR 前置）" ($catalogId -gt 0) "status=$($cc.Status) catalogId=$catalogId body=$(Short $cc.Raw 120)"
    }
    if ($catalogId -gt 0) {
        Ensure-Sample "/api/v1/service-requests?page=1&size=50" "/api/v1/service-requests" @{
            catalogId = $catalogId; title = "$sampleTitlePrefix 跨租户服务请求样本（OB-6）"; reason = "跨租户服务请求读面样本：验证工作台服务请求页签。"
            complianceAck = $true
        } "S3" "服务请求样本" "$sampleTitlePrefix 跨租户服务请求样本（OB-6）"
    } else {
        Fail "S3" "播种服务请求样本" "前置目录项缺失"
    }
}

if ($Mode -in @("verify", "all")) {
    foreach ($pair in @(@("changes", "V1", "变更"), @("problems", "V2", "问题"), @("service-requests", "V3", "服务请求"))) {
        $slug = $pair[0]; $id = $pair[1]; $label = $pair[2]
        $r = Invoke-Api -Method Get -Path "/api/v1/msp/workbench/$($slug)?limit=50" -Sess $msp
        $items = @()
        if ($r.Status -eq 200 -and $null -ne $r.Json.data.items) { $items = @($r.Json.data.items) }
        # 注：标题前缀含 `[`，-like 通配符会误判；用 Contains 做字面匹配。
        $hit = @($items | Where-Object { $_.title -and $_.title.Contains($sampleTitlePrefix) }).Count
        Assert-Case $id "读面可见$label 样本（/msp/workbench/$slug）" (($r.Status -eq 200) -and ($hit -ge 1)) "status=$($r.Status) items=$($items.Count) sampleHits=$hit"
    }
    $tk = Invoke-Api -Method Get -Path "/api/v1/msp/workbench/tasks?limit=50" -Sess $msp
    $tkItems = 0
    if ($tk.Status -eq 200 -and $null -ne $tk.Json.data.items) { $tkItems = @($tk.Json.data.items).Count }
    Pass "V4" "待办任务投影读面" "status=$($tk.Status) items=$tkItems"
}

if ($Mode -in @("boundary", "all")) {
    # B1：101 条 → 400 BATCH_LIMIT_EXCEEDED
    $items101 = @(); for ($i = 0; $i -lt 101; $i++) { $items101 += @{ ticketId = $ValidTicketId; customerTenantId = $TenantId } }
    $b1 = Invoke-Api -Method Post -Path "/api/v1/msp/workbench/batch" -Sess $msp -Body @{ action = "reply"; items = $items101; payload = @{ content = "[FDB-B1] 批量边界探针" } }
    Assert-Case "B1" "101 条超限被拒（400 BATCH_LIMIT_EXCEEDED）" (($b1.Status -eq 400) -and ($b1.Raw -match "BATCH_LIMIT_EXCEEDED")) "status=$($b1.Status) body=$(Short $b1.Raw 140)"

    # B2：含失效条目（不存在的 ticketId）→ 200 逐条结果（成功 + 失败给原因）
    $b2 = Invoke-Api -Method Post -Path "/api/v1/msp/workbench/batch" -Sess $msp -Body @{
        action = "reply"; payload = @{ content = "[FDB-B1] 批量边界探针（逐条）" }
        items = @(@{ ticketId = $ValidTicketId; customerTenantId = $TenantId }, @{ ticketId = 999999; customerTenantId = $TenantId })
    }
    $okCount = -1; $failCount = -1; $reason = ""
    if ($b2.Status -eq 200 -and $null -ne $b2.Json.data) {
        $okCount = [int]$b2.Json.data.succeeded; $failCount = [int]$b2.Json.data.failed
        $reason = (@($b2.Json.data.results | Where-Object { $_.ok -eq $false }) | Select-Object -First 1).reasonCode
    }
    Assert-Case "B2" "含失效条目：成功 1 + 失败 1（给原因）" (($b2.Status -eq 200) -and ($okCount -eq 1) -and ($failCount -eq 1) -and (-not [string]::IsNullOrEmpty($reason))) "status=$($b2.Status) ok=$okCount failed=$failCount reason=$reason"

    # B3：高危动作 delete → 403 ACTION_NOT_ALLOWED
    $b3 = Invoke-Api -Method Post -Path "/api/v1/msp/workbench/batch" -Sess $msp -Body @{
        action = "delete"; items = @(@{ ticketId = $ValidTicketId; customerTenantId = $TenantId })
    }
    Assert-Case "B3" "高危动作 delete 被拒（403 ACTION_NOT_ALLOWED）" (($b3.Status -eq 403) -and ($b3.Raw -match "ACTION_NOT_ALLOWED")) "status=$($b3.Status) body=$(Short $b3.Raw 140)"

    # B4：未分配客户条目 → 逐条失败 MSP_ALLOCATION_REQUIRED（其余条目不受影响）
    $b4 = Invoke-Api -Method Post -Path "/api/v1/msp/workbench/batch" -Sess $msp -Body @{
        action = "reply"; payload = @{ content = "[FDB-B1] 批量边界探针（未分配）" }
        items = @(@{ ticketId = $ValidTicketId; customerTenantId = $TenantId }, @{ ticketId = $UnallocatedTicketId; customerTenantId = $UnallocatedTenantId })
    }
    $b4Reason = ""
    if ($b4.Status -eq 200 -and $null -ne $b4.Json.data) {
        $b4Reason = (@($b4.Json.data.results | Where-Object { $_.ok -eq $false }) | Select-Object -First 1).reasonCode
    }
    Assert-Case "B4" "未分配客户条目逐条失败（MSP_ALLOCATION_REQUIRED）" (($b4.Status -eq 200) -and ($b4Reason -eq "MSP_ALLOCATION_REQUIRED")) "status=$($b4.Status) reason=$b4Reason"
}

$failCount = Write-Summary
if ($failCount -gt 0) { exit 1 } else { exit 0 }
