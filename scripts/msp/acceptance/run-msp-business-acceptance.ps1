#!/usr/bin/env pwsh
# =============================================================================
# MSP 多租户业务验收脚本（本机实际业务闭环，从操作者出发）
#
# 设计文档：docs/multi-tenant/plan/msp-multi-tenant-business-acceptance-design.md
# 运行前置：saas_msp 实例（:8090）+ USER_PROVISIONING_CHANNELS_ENABLED=true
#
# 场景组（执行顺序见注释，非文档分组顺序）：
#   E  环境与拓扑
#   S  平台治理（建租户→模板供给→首管理员改密→用量）      [SkipTenantLifecycle 可关]
#   P  服务商准备（建技术员、分配）
#   C  客户与用户（建号/邀请→建单→回看）
#   I  隔离反例（跨客户/客户访 MSP/未分配写/头冲突/越权）
#   A  审计
#
# 产物：docs/multi-tenant/evidence/msp-business-acceptance/run-summary-<ts>.md
# 退出码：0=全过（允许带理由 SKIP）；1=存在 FAIL
# =============================================================================
param(
    [string]$Base = "http://127.0.0.1:8090",
    [switch]$SkipTenantLifecycle,
    [switch]$SkipInvitation,
    [switch]$KeepRaw,
    [string]$OutDir = "",
    [string]$ProviderCode = "MSP001",
    [string]$CustomerACode = "MSPCUSTA",
    [string]$CustomerBCode = "MSPCUSTB",
    [string]$NewTenantCode = "MSPACPT",
    [string]$AdminUser = "admin", [string]$AdminPass = "passw0rd",
    [string]$MspAdminUser = "mspadmin", [string]$MspAdminPass = "Msp@2026Staff!",
    [string]$CustomerAdminUser = "custa_admin", [string]$CustomerAdminPass = "Cust@2026User!",
    [string]$CustomerBUser = "custb_user", [string]$CustomerBPass = "Cust@2026User!",
    [string]$AcptUserName = "acpt_user", [string]$AcptUserPass = "Acpt@2026User!",
    [string]$AgentName = "acpt_agent", [string]$AgentPass = "Acpt@2026Staff!",
    [string]$InviteEmail = "", [string]$InvitePass = "Acpt@2026Invite!",
    [string]$NewTenantAdminPass = "Acpt@2026Admin!", [string]$NewTenantAdminPass2 = "Acpt@2026Admin2!"
)

$ErrorActionPreference = "Stop"

# ---------- 路径与全局 ----------
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$BackendDir = Join-Path $RepoRoot "itsm-backend"
if ([string]::IsNullOrWhiteSpace($OutDir)) { $OutDir = Join-Path $RepoRoot "docs\multi-tenant\evidence\msp-business-acceptance" }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$ts = Get-Date -Format "yyyyMMdd-HHmmss"
$RunSummaryPath = Join-Path $OutDir "run-summary-$ts.md"
# 邀请邮箱：默认每轮唯一（已存在账号的邮箱再次邀请会在 accept 阶段返回 409
# INVITATION_EMAIL_EXISTS——设计语义：需管理员绑定/重发，不支持自助注册）。
if ([string]::IsNullOrWhiteSpace($InviteEmail)) { $InviteEmail = "acpt-invite-$ts@example.com" }

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

# ---------- HTTP ----------
function New-Session([string]$user, [string]$pass) {
    $sess = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $body = @{ username = $user; password = $pass } | ConvertTo-Json
    try {
        $resp = Invoke-WebRequest -Uri "$Base/api/v1/auth/login" -Method Post -Body $body -ContentType "application/json" -WebSession $sess -SkipHttpErrorCheck -TimeoutSec 25
    } catch {
        return $null
    }
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
        [object]$Body = $null,
        [hashtable]$Headers = @{},
        [switch]$Anonymous
    )
    $h = @{}
    if (-not $Anonymous -and $null -ne $Sess) { $h["Authorization"] = "Bearer $($Sess.Token)" }
    foreach ($k in $Headers.Keys) { $h[$k] = $Headers[$k] }
    $p = @{ Uri = "$Base$Path"; Method = $Method; Headers = $h; SkipHttpErrorCheck = $true; TimeoutSec = 45 }
    if ($null -ne $Sess) { $p["WebSession"] = $Sess.Session }
    if ($null -ne $Body) { $p["Body"] = ($Body | ConvertTo-Json -Depth 10); $p["ContentType"] = "application/json" }
    try { $resp = Invoke-WebRequest @p } catch { return [pscustomobject]@{ Status = -1; Json = $null; Raw = $_.Exception.Message } }
    $json = $null
    try { $json = $resp.Content | ConvertFrom-Json } catch { }
    if ($KeepRaw) {
        $script:RawLog.Add([pscustomobject]@{
            method  = $Method
            path    = $Path
            reqBody = $(if ($null -ne $Body) { $p["Body"] } else { "" })
            status  = [int]$resp.StatusCode
            body    = $resp.Content.Substring(0, [Math]::Min(800, $resp.Content.Length))
        })
    }
    return [pscustomobject]@{ Status = [int]$resp.StatusCode; Json = $json; Raw = $resp.Content }
}

function Try-Login([string]$user, [string]$pass) { return (New-Session $user $pass) }
function Ensure-Login([string]$id, [string]$title, [string]$user, [string]$pass) {
    $s = New-Session $user $pass
    if ($null -eq $s) { Fail $id $title "登录失败：$user" } else { Pass $id $title "登录成功 tenant=$($s.Login.data.user.tenantId) role=$($s.Login.data.user.role)" }
    return $s
}
# 处理 mustChangePassword 首登：尝试 pass；若要求改密则改为 pass2 并重登。
function Resolve-MustChange([object]$sess, [string]$pass, [string]$pass2) {
    if ($null -eq $sess) { return $sess }
    if (-not $sess.Login.data.user.mustChangePassword) { return $sess }
    $chg = Invoke-Api -Method Post -Path "/api/v1/auth/change-password" -Sess $sess -Body @{ oldPassword = $pass; newPassword = $pass2 }
    if ($chg.Status -lt 200 -or $chg.Status -ge 300) { return $sess }
    $relogin = New-Session $sess.Name $pass2
    if ($null -ne $relogin) { return $relogin }
    return $sess
}

# ---------- 顶部横幅 ----------
Write-Host "########################################" -ForegroundColor Cyan
Write-Host "# MSP 多租户业务验收（本机 saas_msp）" -ForegroundColor Cyan
Write-Host "# Base=$Base  OutDir=$OutDir" -ForegroundColor Cyan
Write-Host "########################################" -ForegroundColor Cyan

# =============================================================================
# G0 环境与拓扑
# =============================================================================
$h = Invoke-Api -Method Get -Path "/api/v1/health" -Anonymous
Assert-Case "E1" "健康检查" ($h.Status -eq 200) "status=$($h.Status)"

$admin = New-Session $AdminUser $AdminPass
Assert-Case "E2a" "平台管理员登录" ($null -ne $admin) "user=$AdminUser"
if ($null -eq $admin) { Write-Host "平台管理员登录失败，终止。" -ForegroundColor Red; exit 1 }

$st = Invoke-Api -Method Get -Path "/api/v1/msp/status" -Sess $admin
$stOk = ($st.Status -eq 200) -and ($st.Json.data.deploymentMode -eq "saas_msp") -and ($st.Json.data.mspRoutesEnabled -eq $true)
Assert-Case "E2" "saas_msp 模式且 MSP 路由开启" $stOk "status=$($st.Status) mode=$($st.Json.data.deploymentMode) routes=$($st.Json.data.mspRoutesEnabled)"

$tl = Invoke-Api -Method Get -Path "/api/v1/tenants?page=1&pageSize=50" -Sess $admin
$tenants = @()
if ($tl.Json -and $tl.Json.data -and $tl.Json.data.tenants) { $tenants = @($tl.Json.data.tenants) }
$prov = $tenants | Where-Object { $_.code -eq $ProviderCode } | Select-Object -First 1
$tenantA = $tenants | Where-Object { $_.code -eq $CustomerACode } | Select-Object -First 1
$tenantB = $tenants | Where-Object { $_.code -eq $CustomerBCode } | Select-Object -First 1
$topoOk = ($null -ne $prov) -and ($null -ne $tenantA) -and ($null -ne $tenantB) -and
          ($tenantA.parentTenantId -eq $prov.id) -and ($tenantA.mspProviderId -eq $prov.id) -and
          ($tenantB.parentTenantId -eq $prov.id) -and ($tenantB.mspProviderId -eq $prov.id)
Assert-Case "E3" "拓扑：provider 与客户归属" $topoOk "provider=$($prov.id) custA=$($tenantA.id)/parent=$($tenantA.parentTenantId) custB=$($tenantB.id)/parent=$($tenantB.parentTenantId)"
if (-not $topoOk) { Write-Host "拓扑不符，终止（请先执行 scripts/msp/setup-msp-tenants.sh 或检查联调库）。" -ForegroundColor Red; exit 1 }

$mspadmin = Ensure-Login "E4a" "mspadmin 登录" $MspAdminUser $MspAdminPass
$custa = Ensure-Login "E4b" "custa_admin 登录" $CustomerAdminUser $CustomerAdminPass
$custb = Ensure-Login "E4c" "custb_user 登录" $CustomerBUser $CustomerBPass
$e4ok = ($null -ne $mspadmin) -and ($null -ne $custa) -and ($null -ne $custb)
Assert-Case "E4" "四角色账号可用" $e4ok "mspadmin/custa_admin/custb_user 均登录成功"

# =============================================================================
# G1 平台治理闭环（S）
# =============================================================================
$newTenant = $null
if ($SkipTenantLifecycle) {
    Skip "S1" "平台创建客户租户" "-SkipTenantLifecycle"
    Skip "S2" "模板供给与首管理员" "-SkipTenantLifecycle"
    Skip "S3" "新租户管理员首登改密" "-SkipTenantLifecycle"
    Skip "S4" "租户用量读取" "-SkipTenantLifecycle"
} else {
    # S1：创建（或复用）客户租户，绑定 provider
    $newTenant = $tenants | Where-Object { $_.code -eq $NewTenantCode } | Select-Object -First 1
    if ($null -ne $newTenant) {
        Pass "S1" "平台创建客户租户" "已存在复用：id=$($newTenant.id) code=$NewTenantCode"
    } else {
        $cr = Invoke-Api -Method Post -Path "/api/v1/tenants" -Sess $admin -Body @{
            name = "Acceptance Tenant"; code = $NewTenantCode; type = "msp_customer";
            parentTenantId = $prov.id; mspProviderId = $prov.id
        }
        $created = ($cr.Status -ge 200 -and $cr.Status -lt 300)
        Assert-Case "S1" "平台创建客户租户" $created "status=$($cr.Status) body=$($cr.Raw.Substring(0, [Math]::Min(200, $cr.Raw.Length)))"
        $re = Invoke-Api -Method Get -Path "/api/v1/tenants?page=1&pageSize=50" -Sess $admin
        $newTenant = @($re.Json.data.tenants) | Where-Object { $_.code -eq $NewTenantCode } | Select-Object -First 1
    }

    if ($null -eq $newTenant) {
        Skip "S2" "模板供给与首管理员" "租户 $NewTenantCode 未就绪"
        Skip "S3" "新租户管理员首登改密" "租户 $NewTenantCode 未就绪"
        Skip "S4" "租户用量读取" "租户 $NewTenantCode 未就绪"
    } else {
        # S2：模板供给 + 首管理员（CLI 部署通道，幂等）
        $adminUser = "admin-" + $NewTenantCode.ToLower()
        $adminEmail = "admin-" + $NewTenantCode.ToLower() + "@bootstrap.local"
        $provisionOk = $false
        $provisionTail = ""
        Push-Location $BackendDir
        try {
            $pout = & go run ./cmd/provision_tenant -tenant-code $NewTenantCode -create-admin -admin-username $adminUser -admin-email $adminEmail -admin-password $NewTenantAdminPass 2>&1
            $pexit = $LASTEXITCODE
            $provisionOk = ($pexit -eq 0)
            $provisionTail = (($pout | Select-Object -Last 3) -join " | ")
        } catch {
            $provisionOk = $false
            $provisionTail = $_.Exception.Message
        } finally { Pop-Location }
        Assert-Case "S2" "模板供给与首管理员（provision_tenant）" $provisionOk "exit=$pexit tail=$($provisionTail.Substring(0, [Math]::Min(240, $provisionTail.Length)))"

        # S3：首登（强制改密）→ 重登录 → 身份核对
        $ntAdmin = Try-Login $adminUser $NewTenantAdminPass
        if ($null -eq $ntAdmin) { $ntAdmin = Try-Login $adminUser $NewTenantAdminPass2 }
        $ntAdmin = Resolve-MustChange $ntAdmin $NewTenantAdminPass $NewTenantAdminPass2
        $me = if ($null -ne $ntAdmin) { Invoke-Api -Method Get -Path "/api/v1/auth/me" -Sess $ntAdmin } else { $null }
        $meTenant = if ($me) { if ($me.Json.data.user) { $me.Json.data.user.tenantId } else { $me.Json.data.tenantId } } else { $null }
        $s3ok = ($null -ne $ntAdmin) -and ($meTenant -eq $newTenant.id)
        Assert-Case "S3" "新租户管理员首登改密并落新租户" $s3ok "user=$adminUser tenant=$meTenant 期望=$($newTenant.id)"

        # S4：平台治理读取（用量）
        $us = Invoke-Api -Method Get -Path "/api/v1/tenants/$($newTenant.id)/usage" -Sess $admin
        $s4ok = ($us.Status -eq 200) -and ($null -ne $us.Json.data)
        Assert-Case "S4" "平台租户用量读取" $s4ok "status=$($us.Status) used=$($us.Raw.Substring(0, [Math]::Min(160, $us.Raw.Length)))"
    }
}

# =============================================================================
# G2 服务商准备（P1–P2；P3 分配在 I3/I6 反例之后执行）
# =============================================================================
$p1 = Invoke-Api -Method Get -Path "/api/v1/msp/context" -Sess $mspadmin
Assert-Case "P1" "服务商上下文" (($p1.Status -eq 200) -and ($null -ne $p1.Json.data)) "status=$($p1.Status)"

# P2：服务商管理员建技术员（绑定 msp_tech 角色；幂等复用）
$ro = Invoke-Api -Method Get -Path "/api/v1/roles" -Sess $mspadmin
$techRole = @($ro.Json.data.roles) | Where-Object { $_.code -eq "msp_tech" } | Select-Object -First 1
$agent = $null
$agentPwd = $AgentPass
if ($null -eq $techRole) {
    Skip "P2" "服务商建号（技术员）" "provider 租户缺少 msp_tech 角色"
    Skip "P10" "分配后技术员可见工单" "技术员未就绪"
    Skip "I3" "未分配技术员写操作被拒" "技术员未就绪"
    Skip "I6" "未分配技术员路径通道被拒" "技术员未就绪"
} else {
    $body = @{
        username = $AgentName; email = "$AgentName@msp.local"; name = "Acceptance Agent";
        password = $AgentPass; role = "agent"; roleIds = @($techRole.id); mspRole = "provider_agent"
    }
    $cr = Invoke-Api -Method Post -Path "/api/v1/users" -Sess $mspadmin -Body $body
    $createdAlready = ($cr.Raw -match "USERNAME_EXISTS") -or ($cr.Raw -match "用户名已存在") -or ($cr.Raw -match "已存在")
    $p2ok = (($cr.Status -ge 200) -and ($cr.Status -lt 300)) -or $createdAlready
    Assert-Case "P2" "服务商建号（技术员+msp_tech）" $p2ok "status=$($cr.Status) mode=$(if ($createdAlready) { 'reuse' } else { 'created' })"
    $agent = Try-Login $AgentName $AgentPass
    if ($null -ne $agent) {
        $agentPwd2 = $AgentPass + "2"
        $agent = Resolve-MustChange $agent $AgentPass $agentPwd2
        if ($agent.Login.data.user.mustChangePassword -eq $true) { $agentPwd = $agentPwd2 }
    }
    if ($null -eq $agent) { Skip "P2b" "技术员登录" "登录失败（$AgentName）" } else { Pass "P2b" "技术员登录" "tenant=$($agent.Login.data.user.tenantId) mspRole=$($agent.Login.data.user.mspRole)" }
}

# =============================================================================
# G3 客户与用户（C1 已并入 E4b；C2 建号；C2b 邀请；C3 建单）
# =============================================================================
# C2：客户管理员建用户（幂等）
$acptUser = $null
$acptPwd = $AcptUserPass
$cr2 = Invoke-Api -Method Post -Path "/api/v1/users" -Sess $custa -Body @{
    username = $AcptUserName; email = "$AcptUserName@customer.local"; name = "Acceptance User";
    password = $AcptUserPass; role = "end_user"
}
$c2reuse = ($cr2.Raw -match "USERNAME_EXISTS") -or ($cr2.Raw -match "用户名已存在") -or ($cr2.Raw -match "已存在")
$c2ok = (($cr2.Status -ge 200) -and ($cr2.Status -lt 300)) -or $c2reuse
Assert-Case "C2" "客户管理员建用户" $c2ok "status=$($cr2.Status) mode=$(if ($c2reuse) { 'reuse' } else { 'created' })"
$acptUser = Try-Login $AcptUserName $AcptUserPass
if ($null -ne $acptUser) {
    $acptPwd2 = $AcptUserPass + "2"
    $acptUser = Resolve-MustChange $acptUser $AcptUserPass $acptPwd2
    if ($acptUser.Login.data.user.mustChangePassword -eq $true) { $acptPwd = $acptPwd2 }
}

# C2b：邀请 → 落地 → 接受（可选）
if ($SkipInvitation) {
    Skip "C2b" "邀请→落地→接受" "-SkipInvitation"
} else {
    $roC = Invoke-Api -Method Get -Path "/api/v1/roles" -Sess $custa
    $endUserRole = @($roC.Json.data.roles) | Where-Object { $_.code -eq "end_user" } | Select-Object -First 1
    if ($null -eq $endUserRole) {
        Skip "C2b" "邀请→落地→接受" "客户租户缺少 end_user 角色"
    } else {
        $inv = Invoke-Api -Method Post -Path "/api/v1/users/invitations" -Sess $custa -Body @{ email = $InviteEmail; roleId = $endUserRole.id }
        $invReuse = ($inv.Raw -match "EMAIL_EXISTS") -or ($inv.Raw -match "ALREADY")
        if ($invReuse) {
            $invUser = Try-Login $InviteEmail.Split("@")[0] $InvitePass
            Assert-Case "C2b" "邀请→落地→接受" ($null -ne $invUser) "邀请邮箱已存在，复用已有用户并登录校验"
        } else {
            $invOk = ($inv.Status -ge 200) -and ($inv.Status -lt 300)
            $inviteUrl = if ($invOk) { $inv.Json.data.inviteUrl } else { "" }
            $token = ""
            if ($inviteUrl -match "token=([^&]+)") { $token = $Matches[1] }
            elseif ($inviteUrl -match "/invite/([A-Za-z0-9_\-]+)") { $token = $Matches[1] }
            $inspectOk = $false
            if ($token -ne "") {
                $ins = Invoke-Api -Method Get -Path "/api/v1/auth/invitations/$token" -Anonymous
                $inspectOk = ($ins.Status -eq 200)
            }
            $acceptOk = $false
            if ($inspectOk) {
                $acc = Invoke-Api -Method Post -Path "/api/v1/auth/invitations/$token/accept" -Anonymous -Body @{ password = $InvitePass; name = "Acceptance Invite" }
                $acceptOk = ($acc.Status -ge 200) -and ($acc.Status -lt 300)
            }
            $invLogin = $null
            if ($acceptOk) { $invLogin = Try-Login $InviteEmail.Split("@")[0] $InvitePass }
            $s2bOk = $invOk -and $inspectOk -and $acceptOk -and ($null -ne $invLogin)
            Assert-Case "C2b" "邀请→落地→接受" $s2bOk "invite=$($inv.Status) url=$($inviteUrl) tokenLen=$($token.Length) inspect=$($inspectOk) accept=$($acceptOk) login=$($null -ne $invLogin)"
        }
    }
}

# C3：客户用户建单（主线工单）
$ticketId = 0
if ($null -eq $acptUser) {
    Fail "C3" "客户用户建单" "acpt_user 未就绪，无法建单"
} else {
    $tk = Invoke-Api -Method Post -Path "/api/v1/tickets" -Sess $acptUser -Body @{
        title = "[ACPT-$ts] 多租户业务验收工单"; description = "MSP 业务闭环验收（自动生成）";
        priority = "high"; type = "incident"
    }
    $ticketId = if ($tk.Json.data.id) { [int]$tk.Json.data.id } else { 0 }
    $ticketNo = if ($tk.Json.data.ticketNumber) { $tk.Json.data.ticketNumber } else { "" }
    $c3ok = ($tk.Status -ge 200) -and ($tk.Status -lt 300) -and ($ticketId -gt 0)
    Assert-Case "C3" "客户用户建单" $c3ok "status=$($tk.Status) id=$ticketId number=$ticketNo"
}

# =============================================================================
# G6 隔离反例（前置半场）——I3/I6 必须在「分配」之前执行
# =============================================================================
if ($null -eq $agent -or $ticketId -le 0) {
    Skip "I3" "未分配技术员写操作被拒" "技术员或工单未就绪"
    Skip "I6" "未分配技术员路径通道被拒" "技术员或工单未就绪"
} else {
    if ($null -eq $tenantBTicketId) { $tenantBTicketId = 0 }
    # 隔离样本：客户B 的工单（acpt_agent 未分配到客户B；客户B 作为「未分配目标」）。
    if ($tenantBTicketId -le 0 -and $null -ne $custb) {
        $bcreate = Invoke-Api -Method Post -Path "/api/v1/tickets" -Sess $custb -Body @{
            title = "[ACPT-B-$ts] 客户B 隔离样本"; description = "isolation sample"; priority = "low"; type = "incident"
        }
        if ($bcreate.Json.data.id) { $tenantBTicketId = [int]$bcreate.Json.data.id }
    }
    $i3 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$tenantBTicketId/reply" -Sess $agent -Body @{ customerTenantId = $tenantB.id; content = "[I3] 未分配客户不应成功" }
    $i3ok = ($i3.Status -eq 403) -and ($i3.Raw -match "MSP_ALLOCATION_REQUIRED")
    Assert-Case "I3" "未分配技术员写操作被拒" $i3ok "status=$($i3.Status) body=$($i3.Raw.Substring(0, [Math]::Min(160, $i3.Raw.Length)))"

    $i6 = Invoke-Api -Method Get -Path "/api/v1/msp/customers/$($tenantB.id)/tickets" -Sess $agent
    $i6ok = ($i6.Status -eq 403) -and ($i6.Raw -match "MSP_ALLOCATION_REQUIRED")
    Assert-Case "I6" "未分配技术员路径通道被拒" $i6ok "status=$($i6.Status) body=$($i6.Raw.Substring(0, [Math]::Min(160, $i6.Raw.Length)))"
}

# =============================================================================
# G3 续：C4 客户用户自查工单
# =============================================================================
if ($null -eq $acptUser -or $ticketId -le 0) {
    Skip "C4" "客户用户自查工单" "acpt_user 或工单未就绪"
} else {
    $dt = Invoke-Api -Method Get -Path "/api/v1/tickets/$ticketId" -Sess $acptUser
    $ls = Invoke-Api -Method Get -Path "/api/v1/tickets?page=1&pageSize=50" -Sess $acptUser
    $dtStatus = if ($dt.Json.data.status) { $dt.Json.data.status } else { "" }
    $lsContains = ($ls.Raw -match """id"":\s*$ticketId\b")
    $c4ok = ($dt.Status -eq 200) -and ($dtStatus -ne "") -and $lsContains
    Assert-Case "C4" "客户用户自查工单" $c4ok "detail=$($dt.Status)/status=$dtStatus listContains=$lsContains"
}

# =============================================================================
# G2 续：P3 分配技术员→客户A（幂等）
# =============================================================================
$agentAllocated = $false
if ($null -eq $agent) {
    Skip "P3" "服务商分配技术员到客户A" "技术员未就绪"
} else {
    # 分配真值以「该员工可见客户」为准：/msp/allocations 只返回当前调用者自己的记录，
    # 不能用于核对他人分配（v1.53 验收发现）。
    $custView = Invoke-Api -Method Get -Path "/api/v1/msp/customers" -Sess $agent
    $hasA = @($custView.Json.data.customers) | Where-Object { $_.id -eq $tenantA.id } | Select-Object -First 1
    $hasB = @($custView.Json.data.customers) | Where-Object { $_.id -eq $tenantB.id } | Select-Object -First 1
    if ($null -ne $hasA) {
        # B 必须保持未分配（供 I3/I6 反例）；若已分配则反例如实 FAIL，提示前置被破坏。
        $agentAllocated = $true
        Pass "P3" "服务商已分配技术员到客户A" "customers=A(已分配) B=$(if ($null -eq $hasB) { '未分配' } else { '已分配' })"
    } else {
        $agentId = [int]$agent.Login.data.user.id
        if ($agentId -le 0) {
            $ul = Invoke-Api -Method Get -Path "/api/v1/users?page=1&pageSize=100&search=$AgentName" -Sess $mspadmin
            $found = @($ul.Json.data.users) | Where-Object { $_.username -eq $AgentName } | Select-Object -First 1
            if ($null -ne $found) { $agentId = [int]$found.id }
        }
        $ac = Invoke-Api -Method Post -Path "/api/v1/msp/allocations" -Sess $mspadmin -Body @{ mspUserId = $agentId; customerTenantId = $tenantA.id; role = "primary" }
        $dup = ($ac.Raw -match "已存在有效分配记录") -or ($ac.Raw -match "already exists")
        $agentAllocated = (($ac.Status -ge 200) -and ($ac.Status -lt 300)) -or $dup
        Assert-Case "P3" "服务商分配技术员到客户A" $agentAllocated "status=$($ac.Status) body=$($ac.Raw.Substring(0, [Math]::Min(160, $ac.Raw.Length)))"
    }
}

# =============================================================================
# G4 服务商接单（P4–P10）
# =============================================================================
$replyMarker = "[ACPT-reply-$ts]"
if ($ticketId -le 0) {
    Skip "P4" "工作台可见新工单" "工单未就绪"
    Skip "P5" "工作台回复" "工单未就绪"
    Skip "P6" "工作台改状态 in_progress" "工单未就绪"
    Skip "P7" "工作台指派" "工单未就绪"
    Skip "P8" "工作台批量护栏" "工单未就绪"
    Skip "P9" "工作台改状态 resolved" "工单未就绪"
} else {
    $w4 = Invoke-Api -Method Get -Path "/api/v1/msp/workbench/tickets?customerTenantIds=$($tenantA.id)" -Sess $mspadmin
    $w4item = @($w4.Json.data.items) | Where-Object { $_.id -eq $ticketId } | Select-Object -First 1
    $p4ok = ($w4.Status -eq 200) -and ($null -ne $w4item)
    Assert-Case "P4" "工作台可见新工单" $p4ok "status=$($w4.Status) found=$($null -ne $w4item)"

    $p5 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$ticketId/reply" -Sess $mspadmin -Body @{ customerTenantId = $tenantA.id; content = $replyMarker }
    Assert-Case "P5" "工作台回复" (($p5.Status -ge 200) -and ($p5.Status -lt 300)) "status=$($p5.Status)"

    $p6 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$ticketId/status" -Sess $mspadmin -Body @{ customerTenantId = $tenantA.id; status = "in_progress" }
    Assert-Case "P6" "工作台改状态 in_progress" (($p6.Status -ge 200) -and ($p6.Status -lt 300)) "status=$($p6.Status) body=$($p6.Raw.Substring(0, [Math]::Min(120, $p6.Raw.Length)))"

    $p7 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$ticketId/assign" -Sess $mspadmin -Body @{ customerTenantId = $tenantA.id }
    Assert-Case "P7" "工作台指派" (($p7.Status -ge 200) -and ($p7.Status -lt 300)) "status=$($p7.Status)"

    $p8a = Invoke-Api -Method Post -Path "/api/v1/msp/workbench/batch" -Sess $mspadmin -Body @{
        action = "reply"; items = @(@{ ticketId = $ticketId; customerTenantId = $tenantA.id }); payload = @{ content = "[ACPT-batch-$ts]" }
    }
    $p8aok = ($p8a.Status -eq 200) -and ($p8a.Json.data.succeeded -eq 1) -and ($p8a.Json.data.failed -eq 0)
    Assert-Case "P8a" "工作台批量回复（成功批）" $p8aok "status=$($p8a.Status) succeeded=$($p8a.Json.data.succeeded) failed=$($p8a.Json.data.failed)"

    $items101 = @(1..101 | ForEach-Object { @{ ticketId = $ticketId; customerTenantId = $tenantA.id } })
    $p8b = Invoke-Api -Method Post -Path "/api/v1/msp/workbench/batch" -Sess $mspadmin -Body @{ action = "reply"; items = $items101; payload = @{ content = "[ACPT-batch-overflow]" } }
    $p8bRejected = ($p8b.Status -eq 400) -or ($p8b.Status -eq 422)
    Assert-Case "P8b" "工作台批量护栏（101 条拒绝）" $p8bRejected "status=$($p8b.Status) body=$($p8b.Raw.Substring(0, [Math]::Min(160, $p8b.Raw.Length)))"

    # 状态机：resolved 必须经 ResolveTicket 提交解决方案（工作台只允许 in_progress→pending 等低危转移）。
    $p9 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$ticketId/status" -Sess $mspadmin -Body @{ customerTenantId = $tenantA.id; status = "pending" }
    Assert-Case "P9" "工作台改状态 pending（待客户确认）" (($p9.Status -ge 200) -and ($p9.Status -lt 300)) "status=$($p9.Status) body=$($p9.Raw.Substring(0, [Math]::Min(120, $p9.Raw.Length)))"
}

if ($null -eq $agent -or $ticketId -le 0 -or (-not $agentAllocated)) {
    Skip "P10" "分配后技术员可见工单" "技术员/工单/分配未就绪"
} else {
    $w10 = Invoke-Api -Method Get -Path "/api/v1/msp/workbench/tickets?customerTenantIds=$($tenantA.id)" -Sess $agent
    $w10item = @($w10.Json.data.items) | Where-Object { $_.id -eq $ticketId } | Select-Object -First 1
    Assert-Case "P10" "分配后技术员可见工单" (($w10.Status -eq 200) -and ($null -ne $w10item)) "status=$($w10.Status) found=$($null -ne $w10item)"
}

# =============================================================================
# G5 客户回看（C5）
# =============================================================================
if ($null -eq $acptUser -or $ticketId -le 0) {
    Skip "C5" "客户回看状态与回复" "acpt_user 或工单未就绪"
} else {
    $d5 = Invoke-Api -Method Get -Path "/api/v1/tickets/$ticketId" -Sess $acptUser
    $st5 = if ($d5.Json.data.status) { $d5.Json.data.status } else { "" }
    $cm5 = Invoke-Api -Method Get -Path "/api/v1/tickets/$ticketId/comments" -Sess $acptUser
    $replySeen = ($cm5.Raw -match [regex]::Escape($replyMarker))
    $c5ok = ($d5.Status -eq 200) -and ($st5 -eq "pending") -and ($cm5.Status -eq 200) -and $replySeen
    Assert-Case "C5" "客户回看状态与回复" $c5ok "detail=$($d5.Status)/status=$st5 comments=$($cm5.Status) replySeen=$replySeen"
}

# C6：客户确认解决（状态机要求 resolved 经 ResolveTicket 提交解决方案）
if ($null -eq $custa -or $ticketId -le 0) {
    Skip "C6" "客户确认解决（ResolveTicket）" "custa_admin 或工单未就绪"
} else {
    $rs = Invoke-Api -Method Post -Path "/api/v1/tickets/$ticketId/resolve" -Sess $custa -Body @{ resolution = "[ACPT-resolve-$ts] 客户确认已解决" }
    $d6 = Invoke-Api -Method Get -Path "/api/v1/tickets/$ticketId" -Sess $acptUser
    $st6 = if ($d6.Json.data.status) { $d6.Json.data.status } else { "" }
    $c6ok = ($rs.Status -ge 200) -and ($rs.Status -lt 300) -and ($st6 -eq "resolved")
    Assert-Case "C6" "客户确认解决（ResolveTicket）" $c6ok "resolve=$($rs.Status) status=$st6"
}

# =============================================================================
# G6 隔离反例（后半场）I1/I2/I4/I5
# =============================================================================
# I1：跨客户读取（客户B 的工单，客户A 用户不可见）
if ($null -eq $tenantBTicketId) { $tenantBTicketId = 0 }
if ($tenantBTicketId -le 0 -and $null -ne $custb) {
    $bcreate = Invoke-Api -Method Post -Path "/api/v1/tickets" -Sess $custb -Body @{
        title = "[ACPT-B-$ts] 客户B 隔离样本"; description = "isolation sample"; priority = "low"; type = "incident"
    }
    if ($bcreate.Json.data.id) { $tenantBTicketId = [int]$bcreate.Json.data.id }
}
if ($null -eq $acptUser -or $tenantBTicketId -le 0) {
    Skip "I1" "跨客户读取被拒" "客户B 工单或 acpt_user 未就绪"
} else {
    $i1 = Invoke-Api -Method Get -Path "/api/v1/tickets/$tenantBTicketId" -Sess $acptUser
    $i1ok = ($i1.Status -eq 403) -or ($i1.Status -eq 404)
    Assert-Case "I1" "跨客户读取被拒" $i1ok "status=$($i1.Status) body=$($i1.Raw.Substring(0, [Math]::Min(120, $i1.Raw.Length)))"
}

if ($null -eq $custa) {
    Skip "I2" "客户访问 MSP 面被拒" "custa_admin 未就绪"
} else {
    $i2 = Invoke-Api -Method Get -Path "/api/v1/msp/customers" -Sess $custa
    Assert-Case "I2" "客户访问 MSP 面被拒" ($i2.Status -eq 403) "status=$($i2.Status)"
}

if ($null -eq $acptUser) {
    Skip "I4" "Header/JWT 冲突拒绝" "acpt_user 未就绪"
} else {
    $i4 = Invoke-Api -Method Get -Path "/api/v1/tickets" -Sess $acptUser -Headers @{ "X-Tenant-Code" = $CustomerBCode }
    $i4ok = ($i4.Status -eq 401) -and ($i4.Raw -match "TENANT_MISMATCH_REJECTED")
    Assert-Case "I4" "Header/JWT 冲突拒绝（07:G9）" $i4ok "status=$($i4.Status) body=$($i4.Raw.Substring(0, [Math]::Min(160, $i4.Raw.Length)))"
}

if ($null -eq $acptUser) {
    Skip "I5" "越权建租户被拒" "acpt_user 未就绪"
} else {
    $i5 = Invoke-Api -Method Post -Path "/api/v1/tenants" -Sess $acptUser -Body @{ name = "Should Not Exist"; code = "ACPT-NO-$ts"; type = "internal" }
    Assert-Case "I5" "越权建租户被拒" ($i5.Status -eq 403) "status=$($i5.Status) body=$($i5.Raw.Substring(0, [Math]::Min(160, $i5.Raw.Length)))"
}

# =============================================================================
# G7 审计（A1–A3）
# =============================================================================
$a1 = Invoke-Api -Method Get -Path "/api/v1/audit-logs?targetTenantId=$($tenantA.id)&source=workbench&page=1&pageSize=100" -Sess $mspadmin
$wa = ([regex]::Matches($a1.Raw, "workbench\.action")).Count
Assert-Case "A1" "工作台动作审计可查（provider 侧）" (($a1.Status -eq 200) -and ($wa -ge 3)) "status=$($a1.Status) workbench.action=$wa"

$a2 = Invoke-Api -Method Get -Path "/api/v1/msp/audit/summary" -Sess $mspadmin
Assert-Case "A2" "服务商审计看板" (($a2.Status -eq 200) -and ($null -ne $a2.Json.data)) "status=$($a2.Status)"

$a3 = Invoke-Api -Method Get -Path "/api/v1/audit-logs?targetTenantId=$($tenantB.id)&page=1&pageSize=200" -Sess $custa
$pda = $a3.Raw -match "probe_denied"
Assert-Case "A3" "Header 冲突拒绝留痕（客户侧）" (($a3.Status -eq 200) -and $pda) "status=$($a3.Status) probe_denied=$pda"

# =============================================================================
# 汇总与 run-summary
# =============================================================================
$passN = @($script:Results | Where-Object { $_.Status -eq "PASS" }).Count
$failN = @($script:Results | Where-Object { $_.Status -eq "FAIL" }).Count
$skipN = @($script:Results | Where-Object { $_.Status -eq "SKIP" }).Count
$totalS = [math]::Round($swAll.Elapsed.TotalSeconds, 1)

$md = New-Object System.Collections.Generic.List[string]
$md.Add("# MSP 多租户业务验收 run-summary（$ts）")
$md.Add("")
$md.Add("> 实例：``$Base``（saas_msp）｜总耗时：$totalS s｜**PASS $passN / FAIL $failN / SKIP $skipN**｜脚本：``scripts/msp/acceptance/run-msp-business-acceptance.ps1``")
$md.Add("")
$md.Add("| ID | 场景 | 状态 | 证据 |")
$md.Add("|---|---|---|---|")
foreach ($r in $script:Results) {
    $ev = $r.Evidence -replace "\|", "\|"
    $md.Add("| $($r.ID) | $($r.Title) | $($r.Status) | ``$ev`` |")
}
if ($failN -gt 0) {
    $md.Add("")
    $md.Add("## 失败明细")
    foreach ($r in @($script:Results | Where-Object { $_.Status -eq "FAIL" })) {
        $md.Add("- **$($r.ID) $($r.Title)**：$($r.Evidence)")
    }
}
if ($skipN -gt 0) {
    $md.Add("")
    $md.Add("## SKIP 登记")
    foreach ($r in @($script:Results | Where-Object { $_.Status -eq "SKIP" })) {
        $md.Add("- $($r.ID) $($r.Title)：$($r.Evidence)")
    }
}
$md -join "`n" | Set-Content -Path $RunSummaryPath -Encoding utf8
if ($KeepRaw) {
    $rawPath = Join-Path $OutDir "raw-$ts.json"
    $script:RawLog | ConvertTo-Json -Depth 5 | Set-Content -Path $rawPath -Encoding utf8
    Write-Host "raw: $rawPath" -ForegroundColor DarkGray
}

Write-Host ""
Write-Host "==== 验收汇总：PASS=$passN FAIL=$failN SKIP=$skipN 耗时=${totalS}s ====" -ForegroundColor Cyan
Write-Host "run-summary: $RunSummaryPath" -ForegroundColor Cyan
if ($failN -gt 0) { exit 1 } else { exit 0 }
