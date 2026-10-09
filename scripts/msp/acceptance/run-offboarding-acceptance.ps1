#!/usr/bin/env pwsh
# =============================================================================
# IP-P2-8 离职编排验收脚本（API 通道；OFF-A1–A7）
#
# 设计：docs/multi-tenant/plan/msp-offboarding-orchestration-plan.md §3/§6
#       场景剧本：docs/multi-tenant/scenarios/11-offboarding.md
# 运行前置：saas_msp 实例（默认 :8090）+ TENANT_USER_ADMIN_ENABLED=true + 平台 admin 账号；
#          provider 管理员账号（建分配 + 反例通道）。
# 安全性：在指定 provider 租户内新建**唯一探针用户**并对其执行破坏性动作（停用/回收/审计），
#         不触碰既有用户与既有分配。默认 provider=demo01、customer=demo_customer。
# 幂等性：每轮唯一用户名，可重复执行（重复执行会多出已停用探针用户，属预期）。
# 产物：docs/multi-tenant/evidence/offboarding-acceptance/run-summary-<ts>.md
# 退出码：0=无 FAIL（允许 SKIP）；1=存在 FAIL
# =============================================================================
param(
    [string]$Base = "http://127.0.0.1:8090",
    [string]$AdminUser = "admin", [string]$AdminPass = "passw0rd",
    [string]$ProviderCode = "demo01", [string]$CustomerCode = "demo_customer",
    [string]$ProviderAdminUser = "admin-demo01", [string]$ProviderAdminPass = "Passw0rd@1234",
    [string]$OutDir = "",
    [switch]$SkipAllocation
)

$ErrorActionPreference = "Stop"

# ---------- 路径与全局 ----------
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
if ([string]::IsNullOrWhiteSpace($OutDir)) { $OutDir = Join-Path $RepoRoot "docs\multi-tenant\evidence\offboarding-acceptance" }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$ts = Get-Date -Format "yyyyMMdd-HHmmss"
$RunSummaryPath = Join-Path $OutDir "run-summary-$ts.md"

$script:Results = New-Object System.Collections.Generic.List[object]
$script:RawLog = New-Object System.Collections.Generic.List[object]
$swAll = [System.Diagnostics.Stopwatch]::StartNew()

function Add-Result([string]$id, [string]$title, [string]$status, [string]$evidence) {
    $color = switch ($status) { "PASS" { "Green" } "FAIL" { "Red" } "SKIP" { "Yellow" } default { "Gray" } }
    Write-Host ("[{0}] {1} {2} — {3}" -f $status, $id, $title, $evidence) -ForegroundColor $color
    $script:Results.Add([pscustomobject]@{ ID = $id; Title = $title; Status = $status; Evidence = $evidence })
}
function Pass([string]$id, [string]$title, [string]$evidence) { Add-Result $id $title "PASS" $evidence }
function Fail([string]$id, [string]$title, [string]$evidence) { Add-Result $id $title "FAIL" $evidence }
function Skip([string]$id, [string]$title, [string]$reason) { Add-Result $id $title "SKIP" $reason }
function Raw([string]$label, $obj) { $script:RawLog.Add([pscustomobject]@{ Label = $label; Body = ($obj | ConvertTo-Json -Depth 8) }) }

# ---------- HTTP 助手 ----------
function Get-Token($sess) {
    $csrf = Invoke-RestMethod -Uri "$Base/api/v1/csrf-token" -WebSession $sess -TimeoutSec 20
    $d = $csrf.data
    if ($d -is [string]) { return $d }
    if ($d.csrf_token) { return $d.csrf_token }
    if ($d.token) { return $d.token }
    if ($d.csrfToken) { return $d.csrfToken }
    return $null
}

function New-Session([string]$username, [string]$password) {
    $sess = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $body = @{ username = $username; password = $password } | ConvertTo-Json
    Invoke-RestMethod -Uri "$Base/api/v1/auth/login" -Method Post -ContentType 'application/json' -Body $body -WebSession $sess -TimeoutSec 20 | Out-Null
    return $sess
}

function Try-New-Session([string[]]$users, [string[]]$passwords) {
    foreach ($u in $users) { foreach ($p in $passwords) {
        try { return @{ User = $u; Session = (New-Session $u $p) } } catch { }
    } }
    return $null
}

function Read-ErrMsg($err) {
    try { if ($err.ErrorDetails -and $err.ErrorDetails.Message) { return $err.ErrorDetails.Message } } catch { }
    try {
        if ($err.Exception.Response -and $err.Exception.Response.Content) {
            return $err.Exception.Response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
        }
    } catch { }
    return $err.Exception.Message
}

function Call($sess, [string]$method, [string]$uri, $body) {
    $headers = @{ 'X-CSRF-Token' = (Get-Token $sess) }
    $params = @{ Uri = "$Base$uri"; Method = $method; WebSession = $sess; Headers = $headers; TimeoutSec = 30 }
    if ($body) { $params.ContentType = 'application/json'; $params.Body = ($body | ConvertTo-Json -Depth 6) }
    try {
        $r = Invoke-RestMethod @params
        return @{ Ok = $true; Status = 200; Body = $r }
    } catch {
        $status = 0
        if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode }
        $raw = Read-ErrMsg $_
        $parsed = $null
        try { $parsed = ($raw | ConvertFrom-Json) } catch { }
        return @{ Ok = $false; Status = $status; Raw = $raw; Body = $parsed }
    }
}

function Pick-List($resp, [string[]]$keys) {
    if (-not $resp -or -not $resp.Ok -or -not $resp.Body.data) { return $null }
    foreach ($k in $keys) {
        $v = $resp.Body.data.$k
        if ($null -ne $v) { return @($v) }
    }
    return $null
}

function ApiErrorCode($resp) {
    if ($resp -and $resp.Body -and $resp.Body.code) { return [string]$resp.Body.code }
    return ""
}

function ApiErrorMessage($resp) {
    if ($resp -and $resp.Body -and $resp.Body.message) { return [string]$resp.Body.message }
    if ($resp -and $resp.Raw) { return $resp.Raw }
    return ""
}

Write-Host "===== IP-P2-8 离职编排验收（$Base）=====" -ForegroundColor Cyan

function Invoke-OffboardingChecks {
# ---------- OFF-0 平台登录 ----------
$plat = Try-New-Session @($AdminUser, "root") @($AdminPass, "passw0rd")
if (-not $plat) { Fail "OFF-0" "平台登录" "无法登录（$AdminUser）"; return }
$platSess = $plat.Session
$me = Call $platSess 'GET' '/api/v1/auth/me' $null
if ($me.Ok -and $me.Body.data.role -eq 'super_admin') {
    Pass "OFF-0" "平台登录" ("role=super_admin tenantId=" + $me.Body.data.tenantId)
} else {
    Fail "OFF-0" "平台登录" ("role=" + $me.Body.data.role)
}

# ---------- OFF-1 定位 provider / customer 租户 ----------
$tenants = Call $platSess 'GET' '/api/v1/tenants?page=1&pageSize=100' $null
$items = Pick-List $tenants @('tenants', 'items', 'list')
$provider = $null; $customer = $null
if ($items) {
    $provider = $items | Where-Object { $_.code -eq $ProviderCode } | Select-Object -First 1
    if (-not $provider) { $provider = $items | Where-Object { $_.type -eq 'msp_provider' } | Select-Object -First 1 }
    $customer = $items | Where-Object { $_.code -eq $CustomerCode } | Select-Object -First 1
    if (-not $customer) { $customer = $items | Where-Object { $_.type -in @('msp_customer', 'saas_customer') } | Select-Object -First 1 }
}
if (-not $provider -or -not $customer) {
    Fail "OFF-1" "定位租户" "provider/customer 未找到（provider=$ProviderCode customer=$CustomerCode）"
    return
}
$script:providerCode = $provider.code; $script:providerId = $provider.id
Pass "OFF-1" "定位租户" ("provider={0}(id={1}) customer={2}(id={3})" -f $provider.code, $provider.id, $customer.code, $customer.id)

# ---------- OFF-2 平台 TUM 建号（provider 员工） ----------
$username = "offacpt_$ts"
$provision = Call $platSess 'POST' "/api/v1/tenants/$($provider.id)/users" @{
    username = $username; email = "$username@example.com"; name = "Offboard Acceptance $ts"
    password = 'Pr0be@2026!'; role = 'end_user'; mspRole = 'provider_agent'
}
Raw "provision" $provision.Body
$find = Call $platSess 'GET' "/api/v1/tenants/$($provider.id)/users?keyword=$username" $null
$found = Pick-List $find @('items', 'users', 'list')
$newUserId = $null
if ($found -and $found.Count -gt 0) { $newUserId = $found[0].id }
if ($provision.Ok -and $newUserId) {
    Pass "OFF-2" "平台建号（探针用户）" "username=$username id=$newUserId"
} else {
    Fail "OFF-2" "平台建号（探针用户）" ("http=" + $provision.Status + " msg=" + (ApiErrorMessage $provision))
    return
}
$script:probeUsername = $username; $script:probeUserId = $newUserId

# ---------- OFF-3 provider 管理员 + 分配 ----------
$prov = Try-New-Session @($ProviderAdminUser) @($ProviderAdminPass, "passw0rd")
$activeBefore = 0
if (-not $prov) {
    Skip "OFF-3" "provider 分配" "provider 管理员登录失败，分配相关检查跳过"
} elseif ($SkipAllocation) {
    Skip "OFF-3" "provider 分配" "-SkipAllocation"
} else {
    $alloc = Call $prov.Session 'POST' '/api/v1/msp/allocations' @{ mspUserId = $newUserId; customerTenantId = $customer.id; role = 'backup' }
    Raw "alloc_create" $alloc.Body
    if (-not $alloc.Ok) {
        Fail "OFF-3" "provider 分配" ("http=" + $alloc.Status + " msg=" + (ApiErrorMessage $alloc))
    } else {
        $allocs = Call $prov.Session 'GET' '/api/v1/msp/allocations?page=1&pageSize=100' $null
        $allocItems = Pick-List $allocs @('items', 'allocations', 'list')
        if ($allocItems) { $activeBefore = @($allocItems | Where-Object { $_.mspUserId -eq $newUserId -and -not $_.deassignedAt }).Count }
        if ($activeBefore -eq 1) { Pass "OFF-3" "provider 分配" "active_allocations_before=1" }
        else { Fail "OFF-3" "provider 分配" "active_allocations_before=$activeBefore" }
    }

    # ---------- OFF-4 反例：非平台面调用平台端点 ----------
    $neg = Call $prov.Session 'POST' "/api/v1/tenants/$($provider.id)/users/$newUserId/offboard" $null
    Raw "offboard_as_provider_admin" $neg.Body
    $negCode = ApiErrorCode $neg
    if ($neg.Status -eq 403) { Pass "OFF-4" "非平台面反例" ("http=403 code=$negCode msg=" + (ApiErrorMessage $neg)) }
    else { Fail "OFF-4" "非平台面反例" ("http=" + $neg.Status + " code=$negCode") }
}

# ---------- OFF-5 平台 offboard ----------
$off = Call $platSess 'POST' "/api/v1/tenants/$($provider.id)/users/$newUserId/offboard" $null
Raw "offboard_platform" $off.Body
$offData = $null
if ($off.Ok) { $offData = $off.Body.data }
$expectRevoked = 0; if ($activeBefore -eq 1) { $expectRevoked = 1 }
if ($off.Ok -and $offData.deactivated -eq $true -and $offData.allocationsRevoked -eq $expectRevoked) {
    Pass "OFF-5" "平台 offboard" ("deactivated=True revoked={0} pending={1}" -f $offData.allocationsRevoked, $offData.allocationsPending)
} else {
    Fail "OFF-5" "平台 offboard" ("http=" + $off.Status + " code=" + (ApiErrorCode $off) + " msg=" + (ApiErrorMessage $off))
}

# ---------- OFF-6 数据核验：停用 + 分配清零 ----------
$detail = Call $platSess 'GET' "/api/v1/tenants/$($provider.id)/users/$newUserId" $null
if ($detail.Ok -and $detail.Body.data.active -eq $false) { Pass "OFF-6a" "停用生效" "active=false" }
else { Fail "OFF-6a" "停用生效" ("active=" + $detail.Body.data.active) }

if ($prov -and -not $SkipAllocation) {
    $allocs2 = Call $prov.Session 'GET' '/api/v1/msp/allocations?page=1&pageSize=100' $null
    $allocItems2 = Pick-List $allocs2 @('items', 'allocations', 'list')
    $activeAfter = 0
    if ($allocItems2) { $activeAfter = @($allocItems2 | Where-Object { $_.mspUserId -eq $newUserId -and -not $_.deassignedAt }).Count }
    if ($activeAfter -eq 0) { Pass "OFF-6b" "分配清零" "active_allocations_after=0" }
    else { Fail "OFF-6b" "分配清零" "active_allocations_after=$activeAfter" }
} else {
    Skip "OFF-6b" "分配清零" "provider 会话或分配步骤不可用"
}

# ---------- OFF-7 幂等重跑 + 审计 ----------
$off2 = Call $platSess 'POST' "/api/v1/tenants/$($provider.id)/users/$newUserId/offboard" $null
Raw "offboard_rerun" $off2.Body
if ($off2.Ok -and $off2.Body.data.alreadyInactive -eq $true -and $off2.Body.data.allocationsRevoked -eq 0) {
    Pass "OFF-7a" "幂等重跑" "alreadyInactive=True revoked=0"
} else {
    Fail "OFF-7a" "幂等重跑" ("http=" + $off2.Status + " code=" + (ApiErrorCode $off2))
}

$audits = Call $platSess 'GET' '/api/v1/audit-logs?action=user.admin_offboard&page=1&pageSize=50' $null
$auditItems = Pick-List $audits @('items', 'logs', 'list')
$rows = @()
if ($auditItems) { $rows = @($auditItems | Where-Object { $_.targetUserId -eq $newUserId }) }
if ($rows.Count -ge 2) {
    Pass "OFF-7b" "治理审计留痕" ("rows={0} source={1} targetTenantId={2}" -f $rows.Count, $rows[0].source, $rows[0].targetTenantId)
} else {
    Fail "OFF-7b" "治理审计留痕" "rows=$($rows.Count)"
}

}

Invoke-OffboardingChecks

# ---------- 汇总与产物 ----------
$failed = @($script:Results | Where-Object { $_.Status -eq 'FAIL' }).Count
$passed = @($script:Results | Where-Object { $_.Status -eq 'PASS' }).Count
$skipped = @($script:Results | Where-Object { $_.Status -eq 'SKIP' }).Count
$swAll.Stop()

$lines = New-Object System.Collections.Generic.List[string]
$lines.Add("# 离职编排验收 run-summary（$ts）")
$lines.Add("")
$lines.Add("- 目标：$Base；provider=$($script:providerCode)（id=$($script:providerId)）；探针用户：$($script:probeUsername)（id=$($script:probeUserId)）")
$lines.Add("- 结果：PASS=$passed FAIL=$failed SKIP=$skipped；耗时 $([math]::Round($swAll.Elapsed.TotalSeconds,1))s")
$lines.Add("")
$lines.Add("| ID | 检查 | 结果 | 证据 |")
$lines.Add("|---|---|---|---|")
foreach ($r in $script:Results) { $lines.Add("| $($r.ID) | $($r.Title) | $($r.Status) | $($r.Evidence) |") }
$lines.Add("")
$lines.Add("## 原始响应（节选）")
foreach ($raw in $script:RawLog) {
    $body = $raw.Body
    if ($body.Length -gt 1200) { $body = $body.Substring(0, 1200) + "…(truncated)" }
    $lines.Add("")
    $lines.Add("### $($raw.Label)")
    $lines.Add('```json')
    $lines.Add($body)
    $lines.Add('```')
}
$lines | Set-Content -Path $RunSummaryPath -Encoding utf8

Write-Host ""
Write-Host "===== 汇总：PASS=$passed FAIL=$failed SKIP=$skipped =====" -ForegroundColor Cyan
Write-Host "run-summary: $RunSummaryPath"
if ($failed -gt 0) { exit 1 } else { exit 0 }
