#!/usr/bin/env pwsh
# =============================================================================
# TUM 平台侧租户用户管理验收脚本（API 通道；TUM-A1–A8 + 反例）
#
# 设计：docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md §6
# 运行前置：saas_msp 实例（默认 :8090）+ TENANT_USER_ADMIN_ENABLED=true + 平台 admin 账号
#
# 安全性：默认**新建隔离租户**（TUAACPT<ts>）并在其中执行破坏性动作，不触碰既有租户；
#         -TenantId 显式指定时复用既有租户（需自行承担数据影响）。
# 幂等性：每轮新建租户 + 唯一用户名，可重复执行。
#
# 产物：docs/multi-tenant/evidence/tenant-user-admin-acceptance/run-summary-<ts>.md
# 退出码：0=无 FAIL（允许 SKIP）；1=存在 FAIL
# =============================================================================
param(
    [string]$Base = "http://127.0.0.1:8090",
    [string]$AdminUser = "admin", [string]$AdminPass = "passw0rd",
    [int]$TenantId = 0,
    [string]$TenantAdminUser = "", [string]$TenantAdminPass = "",
    [switch]$SkipCreateTenant,
    [string]$OutDir = "",
    [switch]$KeepRaw
)

$ErrorActionPreference = "Stop"

# ---------- 路径与全局 ----------
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
if ([string]::IsNullOrWhiteSpace($OutDir)) { $OutDir = Join-Path $RepoRoot "docs\multi-tenant\evidence\tenant-user-admin-acceptance" }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$ts = Get-Date -Format "yyyyMMdd-HHmmss"
$RunSummaryPath = Join-Path $OutDir "run-summary-$ts.md"

$script:Results = New-Object System.Collections.Generic.List[object]
$script:RawLog = New-Object System.Collections.Generic.List[object]
$swAll = [System.Diagnostics.Stopwatch]::StartNew()

function Add-Result([string]$id, [string]$title, [string]$status, [string]$evidence) {
    $color = switch ($status) { "PASS" { "Green" } "FAIL" { "Red" } "SKIP" { "Yellow" } default { "Gray" } }
    Write-Host ("[{0}] {1} {2} — {3}" -f $status, $id, $title, $evidence) -ForegroundColor $color
    $script:Results.Add([pscustomobject]@{ ID = $id; Title = $title; Status = $status; Evidence = $evidence; Elapsed = [math]::Round($swAll.Elapsed.TotalSeconds, 1) })
}
function Pass([string]$id, [string]$title, [string]$evidence) { Add-Result $id $title "PASS" $evidence }
function Fail([string]$id, [string]$title, [string]$evidence) { Add-Result $id $title "FAIL" $evidence }
function Skip([string]$id, [string]$title, [string]$reason) { Add-Result $id $title "SKIP" $reason }
function Assert-Case([string]$id, [string]$title, [bool]$ok, [string]$evidence) { if ($ok) { Pass $id $title $evidence } else { Fail $id $title $evidence } }
function Short([string]$text, [int]$max = 180) {
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
    $refresh = $sess.Cookies.GetCookies($Base) | Where-Object { $_.Name -eq "refresh_token" } | Select-Object -First 1
    return [pscustomobject]@{
        Name = $user; Session = $sess; Token = $cookie.Value;
        RefreshToken = $(if ($null -ne $refresh) { $refresh.Value } else { "" });
        Login = $json
    }
}

function Invoke-Api {
    param(
        [Parameter(Mandatory)][string]$Method,
        [Parameter(Mandatory)][string]$Path,
        [object]$Sess = $null,
        [object]$Body = $null,
        [switch]$Anonymous
    )
    $h = @{}
    if (-not $Anonymous -and $null -ne $Sess) { $h["Authorization"] = "Bearer $($Sess.Token)" }
    $p = @{ Uri = "$Base$Path"; Method = $Method; Headers = $h; SkipHttpErrorCheck = $true; TimeoutSec = 45 }
    if ($null -ne $Body) { $p["Body"] = ($Body | ConvertTo-Json -Depth 10); $p["ContentType"] = "application/json" }
    try { $resp = Invoke-WebRequest @p } catch { return [pscustomobject]@{ Status = -1; Json = $null; Raw = $_.Exception.Message } }
    $json = $null
    try { $json = $resp.Content | ConvertFrom-Json } catch { }
    if ($KeepRaw) {
        $script:RawLog.Add([pscustomobject]@{
            method  = $Method
            path    = $Path
            status  = [int]$resp.StatusCode
            body    = Short $resp.Content 800
        })
    }
    return [pscustomobject]@{ Status = [int]$resp.StatusCode; Json = $json; Raw = $resp.Content }
}

function Try-Login([string]$user, [string]$pass) { return (New-Session $user $pass) }
function Login-Status([string]$user, [string]$pass) {
    # 只关心成败：返回 $true/$false，避免污染全局会话。
    $s = New-Session $user $pass
    return ($null -ne $s)
}

function Write-Summary {
    $passCount = @($script:Results | Where-Object { $_.Status -eq 'PASS' }).Count
    $failCount = @($script:Results | Where-Object { $_.Status -eq 'FAIL' }).Count
    $skipCount = @($script:Results | Where-Object { $_.Status -eq 'SKIP' }).Count
    $tenantDesc = "未解析"
    if ($null -ne $tenant) { $tenantDesc = "id=$($tenant.id) code=$($tenant.code)" }

    $lines = New-Object System.Collections.Generic.List[string]
    $lines.Add("# TUM 平台侧租户用户管理验收 run-summary")
    $lines.Add("")
    $lines.Add("- 时间：$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')")
    $lines.Add("- 环境：$Base｜租户：$tenantDesc｜隔离模式：$(if ($TenantId -gt 0) { '复用既有租户' } else { '新建隔离租户' })")
    $lines.Add("- 结果：**PASS=$passCount / FAIL=$failCount / SKIP=$skipCount**（耗时 $([math]::Round($swAll.Elapsed.TotalSeconds,1))s）")
    $lines.Add("- 设计：docs/multi-tenant/plan/msp-tenant-user-management-enhancement-plan.md §6（TUM-A1–A8 + 反例）")
    $lines.Add("")
    $lines.Add("| # | 用例 | 状态 | 证据 |")
    $lines.Add("|---|---|---|---|")
    foreach ($r in $script:Results) {
        $ev = ("" + $r.Evidence) -replace '\|', '/'
        $lines.Add("| $($r.ID) | $($r.Title) | $($r.Status) | $ev |")
    }
    Set-Content -Path $RunSummaryPath -Value ($lines -join "`n") -Encoding utf8
    if ($KeepRaw) {
        $rawPath = Join-Path $OutDir "run-raw-$ts.json"
        $script:RawLog | ConvertTo-Json -Depth 6 | Set-Content -Path $rawPath -Encoding utf8
        Write-Host "raw log: $rawPath"
    }
    $color = if ($failCount -gt 0) { "Red" } else { "Green" }
    Write-Host "== Summary: PASS=$passCount FAIL=$failCount SKIP=$skipCount -> $RunSummaryPath" -ForegroundColor $color
    return $failCount
}

# =============================================================================
# E 环境与租户准备
# =============================================================================
$admin = New-Session $AdminUser $AdminPass
if ($null -eq $admin) {
    Fail "E1" "平台管理员登录" "登录失败：$AdminUser@$Base（检查实例与账号）"
    $failCount = Write-Summary
    exit 1
}
Pass "E1" "平台管理员登录" "role=$($admin.Login.data.user.role) tenant=$($admin.Login.data.user.tenantId)"
$actorUserId = [int]$admin.Login.data.user.id
$actorTenantId = [int]$admin.Login.data.user.tenantId

$tenant = $null
if ($TenantId -gt 0) {
    $tr = Invoke-Api -Method Get -Path "/api/v1/tenants/$TenantId" -Sess $admin
    if ($tr.Status -ge 200 -and $tr.Status -lt 300) {
        $tenant = $tr.Json.data
        Pass "P1" "复用既有租户" "id=$TenantId code=$($tenant.code)（-TenantId 显式指定）"
    } else {
        Fail "P1" "复用既有租户" "status=$($tr.Status) body=$(Short $tr.Raw)"
    }
} else {
    $code = "TUAACPT" + (Get-Date -Format "MMddHHmmss")
    $cr = Invoke-Api -Method Post -Path "/api/v1/tenants" -Sess $admin -Body @{
        name = "TUM验收租户"; code = $code; type = "saas_customer"
    }
    Assert-Case "P1" "新建隔离验收租户" (($cr.Status -ge 200) -and ($cr.Status -lt 300)) "status=$($cr.Status) code=$code body=$(Short $cr.Raw 120)"
    $lr = Invoke-Api -Method Get -Path "/api/v1/tenants?page=1&pageSize=100" -Sess $admin
    $tenantList = @()
    if ($null -ne $lr.Json -and $null -ne $lr.Json.data) {
        if ($null -ne $lr.Json.data.tenants) { $tenantList = @($lr.Json.data.tenants) }
        elseif ($null -ne $lr.Json.data.items) { $tenantList = @($lr.Json.data.items) }
    }
    $tenant = $tenantList | Where-Object { $_.code -eq $code } | Select-Object -First 1
}
if ($null -eq $tenant) {
    Fail "P1b" "解析验收租户" "未找到租户记录，后续用例无法执行"
    $failCount = Write-Summary
    exit 1
}
$tenantId = [int]$tenant.id

# 首管（仅新建租户时）：bootstrap 一个专用管理员，用于通知触达与租户侧越权反例。
$tenantAdmin = $null
if ($TenantId -eq 0 -and -not $SkipCreateTenant) {
    $admUser = "tua-acpt-admin-$ts"
    $admPass = "Acpt@2026Admin!"
    $br = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/bootstrap-admin" -Sess $admin -Body @{
        username = $admUser; password = $admPass; email = "$admUser@example.com"
    }
    $bootOk = (($br.Status -ge 200) -and ($br.Status -lt 300))
    Assert-Case "P2" "首管 bootstrap（专用验收管理员）" $bootOk "status=$($br.Status) username=$admUser body=$(Short $br.Raw 120)"
    if ($bootOk) {
        $admUserId = [int]$br.Json.data.userId
        $tenantAdmin = New-Session $admUser $admPass
        if ($null -ne $tenantAdmin -and $tenantAdmin.Login.data.user.mustChangePassword -eq $true) {
            $cp = Invoke-Api -Method Post -Path "/api/v1/auth/change-password" -Sess $tenantAdmin -Body @{
                oldPassword = $admPass; newPassword = "Acpt@2026Admin2!"
            }
            $cpOk = ($cp.Status -eq 200)
            Assert-Case "P3" "首管首登强制改密（IP-P1-5 闭环）" $cpOk "status=$($cp.Status)"
            $admPass = "Acpt@2026Admin2!"
            $tenantAdmin = New-Session $admUser $admPass
        }
    }
} elseif ($TenantAdminUser -ne "" -and $TenantAdminPass -ne "") {
    $tenantAdmin = New-Session $TenantAdminUser $TenantAdminPass
    Pass "P2" "租户管理员会话（外部提供）" "user=$TenantAdminUser ok=$($null -ne $tenantAdmin)"
} else {
    Skip "P2" "租户管理员会话" "未新建租户且未提供 -TenantAdminUser/Pass，通知/越权用例将 SKIP"
}

# 被测用户（普通成员）
$userName = "tua-acpt-user-$ts"
$userPass = "Acpt@2026User!"
$ur = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users" -Sess $admin -Body @{
    username = $userName; name = "TUM Acpt User"; email = "$userName@example.com";
    password = $userPass; role = "end_user"
}
$userOk = (($ur.Status -ge 200) -and ($ur.Status -lt 300))
Assert-Case "P4" "平台供给建号（被测用户）" $userOk "status=$($ur.Status) username=$userName body=$(Short $ur.Raw 120)"
$userId = 0
if ($userOk) {
    if ($null -ne $ur.Json.data.id) { $userId = [int]$ur.Json.data.id }
    elseif ($null -ne $ur.Json.data.user.id) { $userId = [int]$ur.Json.data.user.id }
}
if ($userId -le 0) {
    Fail "P4b" "解析被测用户 ID" "响应缺少用户 ID，后续用例无法执行"
    $failCount = Write-Summary
    exit 1
}

# =============================================================================
# A1 读通道：列表/检索/详情字段完整
# =============================================================================
$list = Invoke-Api -Method Get -Path "/api/v1/tenants/$tenantId/users?page=1&pageSize=20" -Sess $admin
$listOk = ($list.Status -eq 200) -and ($null -ne $list.Json.data.items)
$sample = $null
if ($listOk) { $sample = @($list.Json.data.items) | Select-Object -First 1 }
$fieldOk = $null -ne $sample -and ($null -ne $sample.PSObject.Properties['mustChangePassword']) -and ($null -ne $sample.PSObject.Properties['isBootstrapAdmin']) -and ($null -ne $sample.PSObject.Properties['role'])
Assert-Case "A1" "列表可达且字段完整（mustChangePassword/role/isBootstrapAdmin）" ($listOk -and $fieldOk) "status=$($list.Status) total=$($list.Json.data.total)"

$search = Invoke-Api -Method Get -Path "/api/v1/tenants/$tenantId/users?page=1&pageSize=20&keyword=$userName" -Sess $admin
$searchOk = ($search.Status -eq 200) -and (@($search.Json.data.items).Count -eq 1)
Assert-Case "A1b" "关键字检索命中唯一用户" $searchOk "status=$($search.Status) hits=$(@($search.Json.data.items).Count)"

$detail = Invoke-Api -Method Get -Path "/api/v1/tenants/$tenantId/users/$userId" -Sess $admin
$detailOk = ($detail.Status -eq 200) -and ([int]$detail.Json.data.id -eq $userId)
Assert-Case "A1c" "详情 (tenant_id,user_id) 双键" $detailOk "status=$($detail.Status)"

# =============================================================================
# A2 重置·一次性（S1）：旧口令/旧 refresh 失效，新口令可登录且强制改密
# =============================================================================
$preSession = New-Session $userName $userPass
$oldRefresh = ""
if ($null -ne $preSession) { $oldRefresh = $preSession.RefreshToken }

$reset1 = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users/$userId/reset-password" -Sess $admin -Body @{ mode = "generated" }
$genPass = ""
if ($null -ne $reset1.Json -and $null -ne $reset1.Json.data) { $genPass = [string]$reset1.Json.data.generatedPassword }
$genOk = ($reset1.Status -eq 200) -and ($genPass.Length -ge 12) -and ($reset1.Json.data.mustChangePassword -eq $true)
Assert-Case "A2" "一次性重置（口令仅回显一次 + 强制改密）" $genOk "status=$($reset1.Status) pwdLen=$($genPass.Length)"

$oldLoginOk = Login-Status $userName $userPass
Assert-Case "A2b" "旧口令登录被拒（401）" (-not $oldLoginOk) "oldLogin=$oldLoginOk"

$newSession = New-Session $userName $genPass
$newLoginOk = ($null -ne $newSession) -and ($newSession.Login.data.user.mustChangePassword -eq $true)
Assert-Case "A2c" "新口令可登录且 mustChangePassword=true" $newLoginOk "login=$($null -ne $newSession)"

if ($oldRefresh -ne "") {
    $replay = Invoke-Api -Method Post -Path "/api/v1/auth/refresh" -Anonymous -Body @{ refreshToken = $oldRefresh }
    Assert-Case "A2d" "重置后旧 refresh replay 被拒（TUM-D6 双吊销）" ($replay.Status -eq 401) "status=$($replay.Status)"
} else {
    Skip "A2d" "旧 refresh replay" "登录未取得 refresh_token（cookie 形态差异）"
}

# A2e：并发两次 generated——后一次生效、前一次失效（响应不泄漏前值）
$reset2 = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users/$userId/reset-password" -Sess $admin -Body @{ mode = "generated" }
$genPass2 = ""
if ($null -ne $reset2.Json -and $null -ne $reset2.Json.data) { $genPass2 = [string]$reset2.Json.data.generatedPassword }
$secondOk = ($reset2.Status -eq 200) -and ($genPass2 -ne $genPass) -and (-not (Login-Status $userName $genPass)) -and (Login-Status $userName $genPass2)
Assert-Case "A2e" "二次生成：新口令生效、旧口令失效" $secondOk "status=$($reset2.Status) changed=$($genPass2 -ne $genPass)"

# =============================================================================
# A3 重置·指定（策略拦截 + 新口令可登录）
# =============================================================================
$weak = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users/$userId/reset-password" -Sess $admin -Body @{ mode = "specified"; newPassword = "short" }
$weakOk = ($weak.Status -eq 400) -and ($weak.Raw -match "PASSWORD_POLICY_VIOLATION")
Assert-Case "A3" "指定弱口令被策略拒绝（400）" $weakOk "status=$($weak.Status) body=$(Short $weak.Raw 120)"

$strongPass = "StrongPass456"
$strong = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users/$userId/reset-password" -Sess $admin -Body @{ mode = "specified"; newPassword = $strongPass }
$strongOk = ($strong.Status -eq 200) -and ($strong.Json.data.mustChangePassword -eq $false)
Assert-Case "A3b" "指定强口令重置成功（不强制改密）" $strongOk "status=$($strong.Status)"

$strongLogin = Login-Status $userName $strongPass
$genLoginAfter = Login-Status $userName $genPass2
Assert-Case "A3c" "新口令可登录、旧一次性口令失效" ($strongLogin -and (-not $genLoginAfter)) "new=$strongLogin old=$genLoginAfter"

# =============================================================================
# A4 启停（即时失效/恢复 + 最后管理员护栏）
# =============================================================================
$dis = Invoke-Api -Method Put -Path "/api/v1/tenants/$tenantId/users/$userId/status" -Sess $admin -Body @{ active = $false }
$disOk = ($dis.Status -eq 200) -and (-not (Login-Status $userName $strongPass))
Assert-Case "A4" "停用即时生效（登录被拒）" $disOk "status=$($dis.Status)"

$en = Invoke-Api -Method Put -Path "/api/v1/tenants/$tenantId/users/$userId/status" -Sess $admin -Body @{ active = $true }
$enOk = ($en.Status -eq 200) -and (Login-Status $userName $strongPass)
Assert-Case "A4b" "启用恢复登录" $enOk "status=$($en.Status)"

if ($TenantId -eq 0 -and $null -ne $tenantAdmin -and $admUserId -gt 0) {
    $la = Invoke-Api -Method Put -Path "/api/v1/tenants/$tenantId/users/$admUserId/status" -Sess $admin -Body @{ active = $false }
    $laOk = ($la.Status -eq 409) -and ($la.Raw -match "LAST_ADMIN_PROTECTED")
    Assert-Case "A4c" "最后管理员停用被拒（409）" $laOk "status=$($la.Status) body=$(Short $la.Raw 120)"
} else {
    Skip "A4c" "最后管理员停用护栏" "复用租户/无专用首管，避免影响既有数据"
}
if ($tenant.code -eq "default") {
    $dc = Invoke-Api -Method Put -Path "/api/v1/tenants/$tenantId/users/$userId/status" -Sess $admin -Body @{ active = $false }
    $dcOk = ($dc.Status -eq 409) -and ($dc.Raw -match "TENANT_CONFIRM_REQUIRED")
    Assert-Case "A4d" "default 租户停用需确认码（409）" $dcOk "status=$($dc.Status)"
} else {
    Skip "A4d" "default 租户确认码" "目标租户非 default（安全约束：不对 default 执行破坏性操作）"
}

# =============================================================================
# A5 审计（按 target 过滤 + 明文口令掩码）
# =============================================================================
$al = Invoke-Api -Method Get -Path "/api/v1/audit-logs?targetTenantId=$tenantId&targetUserId=$userId&pageSize=100" -Sess $admin
$logs = @()
if ($al.Status -eq 200 -and $null -ne $al.Json.data.logs) { $logs = @($al.Json.data.logs) }
$actions = @($logs | ForEach-Object { $_.action })
$a5ok = ($al.Status -eq 200) -and ($actions -contains "user.admin_password_reset") -and ($actions -contains "user.admin_status")
Assert-Case "A5" "治理动作审计按 target_tenant_id/target_user_id 可查" $a5ok "status=$($al.Status) rows=$($logs.Count) actions=$($actions -join ',')"

$leak = $false
foreach ($row in $logs) {
    if ($null -ne $row.requestBody) {
        if ($row.requestBody -match [regex]::Escape($strongPass)) { $leak = $true }
        if ($genPass2 -ne "" -and ($row.requestBody -match [regex]::Escape($genPass2))) { $leak = $true }
    }
}
Assert-Case "A5b" "审计请求体不含明文口令（掩码）" (-not $leak) "leak=$leak"

# =============================================================================
# S3 安全事件：强制下线（仅吊销会话）+ 审计行
# =============================================================================
$preForce = New-Session $userName $strongPass
$fl = Invoke-Api -Method Post -Path "/api/v1/tenants/$tenantId/users/$userId/force-logout" -Sess $admin -Body @{}
Assert-Case "S3" "强制下线（仅吊销会话）" ($fl.Status -eq 200) "status=$($fl.Status) body=$(Short $fl.Raw 100)"

if ($null -ne $preForce -and $preForce.RefreshToken -ne "") {
    $rr2 = Invoke-Api -Method Post -Path "/api/v1/auth/refresh" -Anonymous -Body @{ refreshToken = $preForce.RefreshToken }
    Assert-Case "S3b" "强制下线后 refresh replay 被拒" ($rr2.Status -eq 401) "status=$($rr2.Status)"
} else {
    Skip "S3b" "强制下线 refresh replay" "未取得 refresh_token"
}
$al2 = Invoke-Api -Method Get -Path "/api/v1/audit-logs?targetTenantId=$tenantId&targetUserId=$userId&action=user.admin_force_logout&pageSize=20" -Sess $admin
$flRows = 0
if ($al2.Status -eq 200 -and $null -ne $al2.Json.data.logs) { $flRows = @($al2.Json.data.logs).Count }
Assert-Case "S3c" "强制下线审计行（target_user_id）" ($al2.Status -eq 200 -and $flRows -ge 1) "rows=$flRows"

# =============================================================================
# A8 对账与通知触达
# =============================================================================
$freshList = Invoke-Api -Method Get -Path "/api/v1/tenants/$tenantId/users?page=1&pageSize=1" -Sess $admin
$total = -1
if ($freshList.Status -eq 200) { $total = [int]$freshList.Json.data.total }
$usage = Invoke-Api -Method Get -Path "/api/v1/tenants/$tenantId/usage" -Sess $admin
$usageUsers = -1
if ($usage.Status -eq 200 -and $null -ne $usage.Json.data.used) { $usageUsers = [int]$usage.Json.data.used.users }
Assert-Case "A8" "列表 total 与配额 users 用量一致（C4 对账）" ($total -ge 0 -and $total -eq $usageUsers) "total=$total usage=$usageUsers"

if ($null -ne $tenantAdmin) {
    $notif = Invoke-Api -Method Get -Path "/api/v1/notifications?page=1&pageSize=100" -Sess $tenantAdmin
    $items = @()
    if ($notif.Status -eq 200 -and $null -ne $notif.Json.data.notifications) { $items = @($notif.Json.data.notifications) }
    $hits = @($items | Where-Object { $_.type -eq "user_admin_action" }).Count
    $nLeak = $false
    if ($notif.Raw -match [regex]::Escape($strongPass)) { $nLeak = $true }
    if ($genPass2 -ne "" -and ($notif.Raw -match [regex]::Escape($genPass2))) { $nLeak = $true }
    Assert-Case "A8b" "治理动作站内通知触达目标租户管理员（不含口令，C5/S2）" (($notif.Status -eq 200) -and ($hits -ge 1) -and (-not $nLeak)) "status=$($notif.Status) hits=$hits leak=$nLeak"
} else {
    Skip "A8b" "治理动作站内通知触达" "无租户管理员会话（复用租户未提供凭据）"
}

# =============================================================================
# R 反例（必须被拒绝）
# =============================================================================
if ($null -ne $tenantAdmin) {
    $r1 = Invoke-Api -Method Get -Path "/api/v1/tenants/$tenantId/users" -Sess $tenantAdmin
    $r1ok = ($r1.Status -eq 403) -and ($r1.Raw -match "PLATFORM_SCOPE_REQUIRED")
    Assert-Case "R1" "租户管理员调用平台面被拒（403 PLATFORM_SCOPE_REQUIRED）" $r1ok "status=$($r1.Status) body=$(Short $r1.Raw 120)"
} else {
    Skip "R1" "租户管理员越权反例" "无租户管理员会话"
}

$tl = Invoke-Api -Method Get -Path "/api/v1/tenants?page=1&pageSize=100" -Sess $admin
$allTenants = @()
if ($null -ne $tl.Json -and $null -ne $tl.Json.data) {
    if ($null -ne $tl.Json.data.tenants) { $allTenants = @($tl.Json.data.tenants) }
    elseif ($null -ne $tl.Json.data.items) { $allTenants = @($tl.Json.data.items) }
}
$other = $allTenants | Where-Object { [int]$_.id -ne $tenantId } | Select-Object -First 1
if ($null -ne $other) {
    $r2 = Invoke-Api -Method Get -Path "/api/v1/tenants/$([int]$other.id)/users/$userId" -Sess $admin
    $r2ok = ($r2.Status -eq 404) -and ($r2.Raw -match "TENANT_USER_NOT_FOUND")
    Assert-Case "R2" "跨租户 IDOR 拼路径被拒（404 TENANT_USER_NOT_FOUND）" $r2ok "status=$($r2.Status) body=$(Short $r2.Raw 120)"
} else {
    Skip "R2" "跨租户 IDOR 反例" "环境仅一个租户，无法构造"
}

if ($actorUserId -gt 0 -and $actorTenantId -gt 0) {
    $r3 = Invoke-Api -Method Put -Path "/api/v1/tenants/$actorTenantId/users/$actorUserId/status" -Sess $admin -Body @{ active = $false }
    $r3ok = ($r3.Status -eq 409) -and ($r3.Raw -match "SELF_OPERATION_FORBIDDEN")
    Assert-Case "R3" "平台管理员停用自己被拒（409 SELF_OPERATION_FORBIDDEN）" $r3ok "status=$($r3.Status) body=$(Short $r3.Raw 120)"
} else {
    Skip "R3" "自停用反例" "登录响应缺少 actor 用户/租户信息"
}

Skip "R4" "写通道未开 403（TENANT_USER_ADMIN_DISABLED）" "需以 TENANT_USER_ADMIN_ENABLED=false 重启实例后单独执行（见方案 §7 回滚）"

# =============================================================================
# 汇总
# =============================================================================
$failCount = Write-Summary
if ($failCount -gt 0) { exit 1 } else { exit 0 }
