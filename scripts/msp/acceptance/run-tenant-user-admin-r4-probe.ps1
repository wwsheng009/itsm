#!/usr/bin/env pwsh
# =============================================================================
# TUM R4 探针：写通道关闭（TENANT_USER_ADMIN_ENABLED=false）失败关闭 + 首管兜底恢复通道
#
# 用途（配合方案 §7 回滚演练；FDB-B1 阶段 7 遗留项 R4 单跑）：
#   disabled 模式（默认；要求实例以 TENANT_USER_ADMIN_ENABLED=false 启动）：
#     - 读通道不受限（列表 200）；
#     - 非首管写操作（重置/启停/强制下线）→ 403 TENANT_USER_ADMIN_DISABLED；
#     - 首管（is_bootstrap_admin）重置为兜底恢复通道 → 200，审计标注 channel=bootstrap_recovery；
#     - 兜底新口令可登录（证明恢复通道真实生效）。
#   restore 模式（开关恢复 true 后）：把首管口令重置回原值（mode=specified）+ 写通道冒烟（启停往返）。
#
# 前置：saas_msp 实例；平台 admin 凭据（默认读 $env:TEMP\fdb-creds.json 的 admin 键）。
# 安全性：目标租户默认 CUSTA1；disabled 模式会改变首管口令（随后用 restore 模式还原）。
# 产物：docs/multi-tenant/evidence/tenant-user-admin-acceptance/r4-channel-off-<ts>.md
# 退出码：0=无 FAIL；1=存在 FAIL；2=前置缺失
# =============================================================================
param(
    [ValidateSet("disabled", "restore")][string]$Mode = "disabled",
    [string]$Base = "http://127.0.0.1:8090",
    [string]$AdminUser = "admin", [string]$AdminPass = "",
    [string]$TenantCode = "CUSTA1",
    [string]$BootstrapUser = "", [string]$BootstrapPassword = "",
    [string]$OutDir = ""
)

$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
if ([string]::IsNullOrWhiteSpace($OutDir)) { $OutDir = Join-Path $RepoRoot "docs\multi-tenant\evidence\tenant-user-admin-acceptance" }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$ts = Get-Date -Format "yyyyMMdd-HHmmss"
$SummaryPath = Join-Path $OutDir "r4-channel-off-$ts.md"

# ---------- 凭据（本地临时文件；不回显） ----------
$credsFile = Join-Path $env:TEMP "fdb-creds.json"
if ([string]::IsNullOrWhiteSpace($AdminPass) -and (Test-Path $credsFile)) {
    $creds = Get-Content $credsFile -Raw | ConvertFrom-Json
    if ($null -ne $creds.admin) { $AdminPass = [string]$creds.admin }
    if ([string]::IsNullOrWhiteSpace($BootstrapPassword) -and $null -ne $creds.'admin-custa1') { $BootstrapPassword = [string]$creds.'admin-custa1' }
}
if ([string]::IsNullOrWhiteSpace($AdminPass)) { Write-Error "缺少平台 admin 口令（-AdminPass 或 $credsFile 的 admin 键）"; exit 2 }
if ([string]::IsNullOrWhiteSpace($BootstrapUser)) { $BootstrapUser = "admin-custa1" }

$script:Results = New-Object System.Collections.Generic.List[object]
function Add-Result([string]$id, [string]$title, [string]$status, [string]$evidence) {
    $color = switch ($status) { "PASS" { "Green" } "FAIL" { "Red" } "SKIP" { "Yellow" } default { "Gray" } }
    Write-Host ("[{0}] {1} {2} — {3}" -f $status, $id, $title, $evidence) -ForegroundColor $color
    $script:Results.Add([pscustomobject]@{ ID = $id; Title = $title; Status = $status; Evidence = $evidence })
}
function Pass([string]$id, [string]$title, [string]$evidence) { Add-Result $id $title "PASS" $evidence }
function Fail([string]$id, [string]$title, [string]$evidence) { Add-Result $id $title "FAIL" $evidence }
function Skip([string]$id, [string]$title, [string]$reason) { Add-Result $id $title "SKIP" $reason }
function Assert-Case([string]$id, [string]$title, [bool]$ok, [string]$evidence) { if ($ok) { Pass $id $title $evidence } else { Fail $id $title $evidence } }
function Short([string]$text, [int]$max = 160) {
    if ([string]::IsNullOrEmpty($text)) { return "" }
    if ($text.Length -le $max) { return $text }
    return $text.Substring(0, $max)
}

# ---------- HTTP（与 TUM 验收脚本同构） ----------
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
    return [pscustomobject]@{ Name = $user; Session = $sess; Token = $cookie.Value; Login = $json }
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
    $p = @{ Uri = "$Base$Path"; Method = $Method; Headers = $h; SkipHttpErrorCheck = $true; TimeoutSec = 45 }
    if ($null -ne $Body) { $p["Body"] = ($Body | ConvertTo-Json -Depth 10); $p["ContentType"] = "application/json" }
    try { $resp = Invoke-WebRequest @p } catch { return [pscustomobject]@{ Status = -1; Json = $null; Raw = $_.Exception.Message } }
    $json = $null
    try { $json = $resp.Content | ConvertFrom-Json } catch { }
    return [pscustomobject]@{ Status = [int]$resp.StatusCode; Json = $json; Raw = $resp.Content }
}
function Login-Status([string]$user, [string]$pass) { return ($null -ne (New-Session $user $pass)) }

# ---------- 汇总（定义先于主流程，早退路径也可用） ----------
function Write-Summary {
    $passCount = @($script:Results | Where-Object { $_.Status -eq 'PASS' }).Count
    $failCount = @($script:Results | Where-Object { $_.Status -eq 'FAIL' }).Count
    $skipCount = @($script:Results | Where-Object { $_.Status -eq 'SKIP' }).Count
    $tenantDesc = "未解析"
    if ($null -ne $tenant) { $tenantDesc = "code=$($tenant.code) id=$($tenant.id)" }
    $lines = New-Object System.Collections.Generic.List[string]
    $lines.Add("# TUM R4 探针 run-summary（写通道关闭 / 兜底恢复）")
    $lines.Add("")
    $lines.Add("- 时间：$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')")
    $lines.Add("- 模式：$Mode｜环境：$Base｜租户：$tenantDesc｜开关：$(if ($Mode -eq 'disabled') { 'TENANT_USER_ADMIN_ENABLED=false' } else { '已恢复 true' })")
    $lines.Add("- 结果：**PASS=$passCount / FAIL=$failCount / SKIP=$skipCount**")
    $lines.Add("- 状态：**实测**｜定位：TUM R4 回滚演练（设计见 TUM 计划 §7；TUM-D3 首管兜底恢复通道）")
    $lines.Add("")
    $lines.Add("| # | 用例 | 状态 | 证据 |")
    $lines.Add("|---|---|---|---|")
    foreach ($r in $script:Results) {
        $ev = ("" + $r.Evidence) -replace '\|', '/'
        $lines.Add("| $($r.ID) | $($r.Title) | $($r.Status) | $ev |")
    }
    $lines.Add("")
    $lines.Add("> 注：本探针在 disabled 模式会生成一次性首管口令（不落盘、不回显）；随后以 restore 模式还原为本地凭据文件中的原口令。")
    Set-Content -Path $SummaryPath -Value ($lines -join "`n") -Encoding utf8
    Write-Host "run-summary: $SummaryPath"
    return $failCount
}

# ---------- 主流程 ----------
$admin = New-Session $AdminUser $AdminPass
Assert-Case "E1" "平台管理员登录" ($null -ne $admin) "user=$AdminUser"
if ($null -eq $admin) { Write-Summary; exit 1 }

$tl = Invoke-Api -Method Get -Path "/api/v1/tenants?page=1&pageSize=100" -Sess $admin
$tenant = $null
if ($tl.Status -eq 200 -and $null -ne $tl.Json.data) {
    $items = @()
    if ($null -ne $tl.Json.data.tenants) { $items = @($tl.Json.data.tenants) }
    elseif ($null -ne $tl.Json.data.items) { $items = @($tl.Json.data.items) }
    $tenant = $items | Where-Object { $_.code -eq $TenantCode } | Select-Object -First 1
}
Assert-Case "P0" "解析目标租户" ($null -ne $tenant) "code=$TenantCode"
if ($null -eq $tenant) { Write-Summary; exit 1 }
$tenantId = [int]$tenant.id

$list = Invoke-Api -Method Get -Path "/api/v1/tenants/$tenantId/users?page=1&pageSize=100" -Sess $admin
$users = @()
if ($list.Status -eq 200 -and $null -ne $list.Json.data.items) { $users = @($list.Json.data.items) }
$boot = $users | Where-Object { $_.isBootstrapAdmin -eq $true } | Select-Object -First 1
if ($null -eq $boot -and $BootstrapUser -ne "") { $boot = $users | Where-Object { $_.username -eq $BootstrapUser } | Select-Object -First 1 }
$nonBoot = $users | Where-Object { $_.isBootstrapAdmin -ne $true } | Select-Object -First 1
Assert-Case "P1" "选样（首管 + 非首管用户）" ($null -ne $boot -and $null -ne $nonBoot) "boot=$($boot.username) nonBoot=$($nonBoot.username)"
if ($null -eq $boot -or $null -eq $nonBoot) { Write-Summary; exit 1 }

if ($Mode -eq "disabled") {
    # 读通道不受限
    Assert-Case "R4a" "读通道不受限（列表 200）" ($list.Status -eq 200) "status=$($list.Status) total=$($list.Json.data.total)"

    # 非首管写操作三连 → 403 + TENANT_USER_ADMIN_DISABLED
    $rb = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users/$($nonBoot.id)/reset-password" -Sess $admin -Body @{ mode = "generated" }
    Assert-Case "R4b" "非首管重置被拒（403 TENANT_USER_ADMIN_DISABLED）" (($rb.Status -eq 403) -and ($rb.Raw -match "TENANT_USER_ADMIN_DISABLED")) "status=$($rb.Status) body=$(Short $rb.Raw 120)"

    $rc = Invoke-Api -Method Put -Path "/api/v1/tenants/$tenantId/users/$($nonBoot.id)/status" -Sess $admin -Body @{ active = $false }
    Assert-Case "R4c" "非首管启停被拒（403）" (($rc.Status -eq 403) -and ($rc.Raw -match "TENANT_USER_ADMIN_DISABLED")) "status=$($rc.Status) body=$(Short $rc.Raw 120)"

    $rd = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users/$($nonBoot.id)/force-logout" -Sess $admin -Body @{}
    Assert-Case "R4d" "非首管强制下线被拒（403）" (($rd.Status -eq 403) -and ($rd.Raw -match "TENANT_USER_ADMIN_DISABLED")) "status=$($rd.Status) body=$(Short $rd.Raw 120)"

    # 首管兜底恢复通道（默认可用，不受开关约束）
    $re = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users/$($boot.id)/reset-password" -Sess $admin -Body @{ mode = "generated" }
    $gen = ""
    if ($null -ne $re.Json -and $null -ne $re.Json.data) { $gen = [string]$re.Json.data.generatedPassword }
    Assert-Case "R4e" "首管重置兜底可用（200，不受开关约束）" (($re.Status -eq 200) -and ($gen.Length -ge 12)) "status=$($re.Status) pwdLen=$($gen.Length)"

    $bootLogin = $false
    if ($gen -ne "") { $bootLogin = Login-Status $boot.username $gen }
    Assert-Case "R4f" "兜底新口令可登录（恢复通道生效）" $bootLogin "login=$bootLogin"

    $al = Invoke-Api -Method Get -Path "/api/v1/audit-logs?targetTenantId=$tenantId&targetUserId=$($boot.id)&action=user.admin_password_reset&pageSize=20" -Sess $admin
    $chOk = ($al.Status -eq 200) -and ($al.Raw -match "bootstrap_recovery")
    Assert-Case "R4g" "审计标注 channel=bootstrap_recovery" $chOk "status=$($al.Status) channelSeen=$chOk"
}
else {
    # restore：首管口令还原 + 写通道冒烟
    if ([string]::IsNullOrWhiteSpace($BootstrapPassword)) { Write-Error "restore 模式需要 -BootstrapPassword（或 $credsFile 的 admin-custa1 键）"; exit 2 }
    $rs = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users/$($boot.id)/reset-password" -Sess $admin -Body @{ mode = "specified"; newPassword = $BootstrapPassword }
    Assert-Case "RS1" "首管口令还原（mode=specified，200）" (($rs.Status -eq 200) -and ($rs.Json.data.mustChangePassword -eq $false)) "status=$($rs.Status)"

    $loginOk = Login-Status $boot.username $BootstrapPassword
    Assert-Case "RS2" "还原口令可登录" $loginOk "login=$loginOk"

    $w1 = Invoke-Api -Method Put -Path "/api/v1/tenants/$tenantId/users/$($nonBoot.id)/status" -Sess $admin -Body @{ active = $false }
    $w2 = Invoke-Api -Method Put -Path "/api/v1/tenants/$tenantId/users/$($nonBoot.id)/status" -Sess $admin -Body @{ active = $true }
    Assert-Case "RS3" "写通道冒烟（停用→启用 200/200）" (($w1.Status -eq 200) -and ($w2.Status -eq 200)) "disable=$($w1.Status) enable=$($w2.Status)"
}

$failCount = Write-Summary
if ($failCount -gt 0) { exit 1 } else { exit 0 }
