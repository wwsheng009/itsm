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
$runDate = Get-Date -Format "yyyy-MM-dd"
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
    # 注意：这里刻意不挂 WebSession。PS7 的 Invoke-WebRequest 会把 -Headers 合并进
    # WebSession.Headers 并持续到后续请求（实测 7.6.1），I4 的冲突头 X-Tenant-Code
    # 会因此污染同一会话的后续调用（全部 401 TENANT_MISMATCH_REJECTED）。
    # 认证统一走 Bearer（$Sess.Token），无需 cookie 会话。
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
        # 复跑时密码已是 pass2（S3 首次已改密）：先试 pass2 可少占一次登录限流额度。
        $ntAdmin = Try-Login $adminUser $NewTenantAdminPass2
        if ($null -eq $ntAdmin) { $ntAdmin = Try-Login $adminUser $NewTenantAdminPass }
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
        $dup = ($ac.Raw -match "已存在有效分配记录") -or ($ac.Raw -match "already exists") -or ($ac.Raw -match "MSP_ALLOCATION_EXISTS") -or ($ac.Status -eq 409)
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
# G8 业务规则深化（L/W/R/Q/T）：操作链与规则逻辑（第二波）
# =============================================================================

# ---- L：工单生命周期全程 + 受保护终局/终态守卫 ----
$lTicketId = 0
if ($null -eq $acptUser -or $null -eq $mspadmin -or $null -eq $custa) {
    foreach ($idc in @("L1","L2","L3","L4","L5","L6","L7","L8","L9")) { Skip $idc "生命周期链" "角色未就绪" }
} else {
    $lc = Invoke-Api -Method Post -Path "/api/v1/tickets" -Sess $acptUser -Body @{
        title = "[ACPT-L-$ts] 生命周期样本"; description = "lifecycle sample"; priority = "low"; type = "incident"
    }
    if ($lc.Json.data.id) { $lTicketId = [int]$lc.Json.data.id }
    if ($lTicketId -le 0) {
        foreach ($idc in @("L1","L2","L3","L4","L5","L6","L7","L8","L9")) { Skip $idc "生命周期链" "样本工单创建失败 status=$($lc.Status) body=$($lc.Raw.Substring(0, [Math]::Min(140, $lc.Raw.Length)))" }
    } else {
        $l1 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$lTicketId/status" -Sess $mspadmin -Body @{ customerTenantId = $tenantA.id; status = "in_progress" }
        Assert-Case "L1" "链：new→in_progress（服务商开工）" (($l1.Status -ge 200) -and ($l1.Status -lt 300)) "status=$($l1.Status)"

        $l2 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$lTicketId/status" -Sess $mspadmin -Body @{ customerTenantId = $tenantA.id; status = "resolved" }
        Assert-Case "L2" "规则：resolved 受保护（直改被拒）" ($l2.Status -ge 400) "status=$($l2.Status) body=$($l2.Raw.Substring(0, [Math]::Min(140, $l2.Raw.Length)))"

        $l3 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$lTicketId/status" -Sess $mspadmin -Body @{ customerTenantId = $tenantA.id; status = "pending" }
        Assert-Case "L3" "链：in_progress→pending（等客户）" (($l3.Status -ge 200) -and ($l3.Status -lt 300)) "status=$($l3.Status)"

        $l4 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$lTicketId/status" -Sess $mspadmin -Body @{ customerTenantId = $tenantA.id; status = "in_progress" }
        Assert-Case "L4" "链：pending→in_progress（重启处理）" (($l4.Status -ge 200) -and ($l4.Status -lt 300)) "status=$($l4.Status)"

        $l5 = Invoke-Api -Method Post -Path "/api/v1/tickets/$lTicketId/resolve" -Sess $custa -Body @{ resolution = "[ACPT-L-resolve-$ts] 客户确认已解决" }
        Assert-Case "L5" "链：客户 resolve→resolved（提交解决方案）" (($l5.Status -ge 200) -and ($l5.Status -lt 300)) "status=$($l5.Status)"

        $l6 = Invoke-Api -Method Put -Path "/api/v1/tickets/$lTicketId/status" -Sess $custa -Body @{ status = "closed" }
        Assert-Case "L6" "链：resolved→closed（客户归档）" (($l6.Status -ge 200) -and ($l6.Status -lt 300)) "status=$($l6.Status) body=$($l6.Raw.Substring(0, [Math]::Min(120, $l6.Raw.Length)))"

        $l7 = Invoke-Api -Method Post -Path "/api/v1/msp/tickets/$lTicketId/status" -Sess $mspadmin -Body @{ customerTenantId = $tenantA.id; status = "open" }
        Assert-Case "L7" "规则：终态（closed）服务商侧守卫拒绝" ($l7.Status -ge 400) "status=$($l7.Status)"

        $l8 = Invoke-Api -Method Put -Path "/api/v1/tickets/$lTicketId/status" -Sess $custa -Body @{ status = "open" }
        Assert-Case "L8" "规则：状态机拒绝 closed→open（终态）" ($l8.Status -ge 400) "status=$($l8.Status)"

        $ld = Invoke-Api -Method Get -Path "/api/v1/tickets/$lTicketId" -Sess $acptUser
        $lst = if ($ld.Json.data.status) { $ld.Json.data.status } else { "" }
        Assert-Case "L9" "客户视角终态核对=closed" (($ld.Status -eq 200) -and ($lst -eq "closed")) "status=$lst"
    }
}

# ---- W：批量逐条语义（跨客户成功 + 未分配部分失败且无副作用） ----
$wTicketA = 0
if ($null -eq $acptUser -or $null -eq $mspadmin -or $tenantBTicketId -le 0) {
    foreach ($idc in @("W1","W2","W3")) { Skip $idc "批量逐条语义" "前置未就绪" }
} else {
    $wc = Invoke-Api -Method Post -Path "/api/v1/tickets" -Sess $acptUser -Body @{
        title = "[ACPT-W-$ts] 批量样本"; description = "batch sample"; priority = "low"; type = "incident"
    }
    if ($wc.Json.data.id) { $wTicketA = [int]$wc.Json.data.id }
    if ($wTicketA -le 0) {
        foreach ($idc in @("W1","W2","W3")) { Skip $idc "批量逐条语义" "样本工单创建失败 status=$($wc.Status) body=$($wc.Raw.Substring(0, [Math]::Min(140, $wc.Raw.Length)))" }
    } else {
        $w1 = Invoke-Api -Method Post -Path "/api/v1/msp/workbench/batch" -Sess $mspadmin -Body @{
            action = "reply"; payload = @{ content = "[ACPT-W1-$ts] 跨客户批量回复" };
            items = @(
                @{ ticketId = $wTicketA; customerTenantId = $tenantA.id },
                @{ ticketId = $tenantBTicketId; customerTenantId = $tenantB.id }
            )
        }
        $w1ok = ($w1.Status -eq 200) -and ($w1.Json.data.succeeded -eq 2) -and ($w1.Json.data.failed -eq 0)
        Assert-Case "W1" "批量：跨客户 A+B 各 1 条全部成功" $w1ok "status=$($w1.Status) succeeded=$($w1.Json.data.succeeded) failed=$($w1.Json.data.failed) body=$($w1.Raw.Substring(0, [Math]::Min(160, $w1.Raw.Length)))"
    }
}
if ($wTicketA -le 0 -or $null -eq $agent -or $tenantBTicketId -le 0) {
    Skip "W2" "批量部分失败逐条语义" "前置未就绪"
    Skip "W3" "被拒条目无副作用" "前置未就绪"
} else {
    $w2 = Invoke-Api -Method Post -Path "/api/v1/msp/workbench/batch" -Sess $agent -Body @{
        action = "reply"; payload = @{ content = "[ACPT-W2-$ts] 未分配目标不应落地" };
        items = @(
            @{ ticketId = $wTicketA; customerTenantId = $tenantA.id },
            @{ ticketId = $tenantBTicketId; customerTenantId = $tenantB.id }
        )
    }
    $wr = @($w2.Json.data.results)
    $w2ok = ($w2.Status -eq 200) -and ($w2.Json.data.succeeded -eq 1) -and ($w2.Json.data.failed -eq 1) `
        -and ($wr.Count -eq 2) -and ($wr[1].ok -eq $false) -and ($wr[1].reasonCode -eq "MSP_ALLOCATION_REQUIRED")
    Assert-Case "W2" "批量：agent 仅 A 成功 / B 逐条拒绝（不整体回滚）" $w2ok "succeeded=$($w2.Json.data.succeeded) failed=$($w2.Json.data.failed) results=$($w2.Raw.Substring(0, [Math]::Min(220, $w2.Raw.Length)))"

    $bCm = Invoke-Api -Method Get -Path "/api/v1/tickets/$tenantBTicketId/comments" -Sess $custb
    $noSide = -not ($bCm.Raw -match [regex]::Escape("[ACPT-W2-$ts]"))
    Assert-Case "W3" "批量：被拒条目无副作用（B 无该评论）" (($bCm.Status -eq 200) -and $noSide) "comments=$($bCm.Status) markers=$($noSide)"
}

# ---- R：分配回收即时失效（第二技术员全链路） ----
$agent2 = $null
$agent2Name = "acpt_agent2"
$agent2Pass = "Acpt@2026Staff2!"
if ($null -ne $mspadmin -and $null -ne $techRole) {
    $crA2 = Invoke-Api -Method Post -Path "/api/v1/users" -Sess $mspadmin -Body @{
        username = $agent2Name; email = "$agent2Name@msp.local"; name = "Acceptance Agent2";
        password = $agent2Pass; role = "agent"; roleIds = @($techRole.id); mspRole = "provider_agent"
    }
    $a2Reuse = ($crA2.Raw -match "USERNAME_EXISTS") -or ($crA2.Raw -match "已存在")
    $agent2 = Try-Login $agent2Name $agent2Pass
    if ($null -ne $agent2) {
        $agent2Pass2 = $agent2Pass + "2"
        $agent2 = Resolve-MustChange $agent2 $agent2Pass $agent2Pass2
    }
} elseif ($null -ne $mspadmin) {
    $agent2 = Try-Login $agent2Name $agent2Pass
}
if ($null -eq $agent2 -or $null -eq $mspadmin) {
    foreach ($idc in @("R1","R2","R3","R4","R5")) { Skip $idc "分配回收" "agent2 未就绪" }
} else {
    $agent2Id = [int]$agent2.Login.data.user.id
    $r0 = Invoke-Api -Method Get -Path "/api/v1/msp/customers" -Sess $agent2
    $preA = @($r0.Json.data.customers) | Where-Object { $_.id -eq $tenantA.id } | Select-Object -First 1
    if ($null -ne $preA) {
        Pass "R1" "分配回收：agent2→A 已有分配（复用）" "customers=$($r0.Raw.Substring(0, [Math]::Min(120, $r0.Raw.Length)))"
    } else {
        $ra = Invoke-Api -Method Post -Path "/api/v1/msp/allocations" -Sess $mspadmin -Body @{ mspUserId = $agent2Id; customerTenantId = $tenantA.id; role = "backup" }
        $raOk = (($ra.Status -ge 200) -and ($ra.Status -lt 300)) -or ($ra.Raw -match "已存在")
        Assert-Case "R1" "分配回收：建立 agent2→A 分配" $raOk "status=$($ra.Status)"
    }

    $r2 = Invoke-Api -Method Get -Path "/api/v1/msp/customers" -Sess $agent2
    $visA = @($r2.Json.data.customers) | Where-Object { $_.id -eq $tenantA.id } | Select-Object -First 1
    Assert-Case "R2" "回收前：agent2 可见客户A" ($null -ne $visA) "customers=$($r2.Raw.Substring(0, [Math]::Min(120, $r2.Raw.Length)))"

    $r3 = Invoke-Api -Method Post -Path "/api/v1/msp/allocations/deallocate" -Sess $mspadmin -Body @{ mspUserId = $agent2Id; customerTenantId = $tenantA.id; reason = "acceptance revoke" }
    $r3Ok = (($r3.Status -ge 200) -and ($r3.Status -lt 300)) -or ($r3.Raw -match "不存在|no active")
    Assert-Case "R3" "回收：解除 agent2→A（含原因）" $r3Ok "status=$($r3.Status) body=$($r3.Raw.Substring(0, [Math]::Min(140, $r3.Raw.Length)))"

    $r4 = Invoke-Api -Method Get -Path "/api/v1/msp/customers" -Sess $agent2
    $stillA = @($r4.Json.data.customers) | Where-Object { $_.id -eq $tenantA.id } | Select-Object -First 1
    Assert-Case "R4" "回收后：agent2 不再可见客户A" ($null -eq $stillA) "customers=$($r4.Raw.Substring(0, [Math]::Min(120, $r4.Raw.Length)))"

    $r5 = Invoke-Api -Method Get -Path "/api/v1/msp/customers/$($tenantA.id)/tickets" -Sess $agent2
    $r5ok = ($r5.Status -eq 403) -and ($r5.Raw -match "MSP_ALLOCATION_REQUIRED")
    Assert-Case "R5" "回收后：路径通道即时失效（403）" $r5ok "status=$($r5.Status) body=$($r5.Raw.Substring(0, [Math]::Min(140, $r5.Raw.Length)))"

    # R6：分配幂等语义——重复分配必须是可判定的 409 + reasonCode（D-2 收口）。
    $r6 = Invoke-Api -Method Post -Path "/api/v1/msp/allocations" -Sess $mspadmin -Body @{ mspUserId = [int]$mspadmin.Login.data.user.id; customerTenantId = $tenantA.id; role = "primary" }
    $r6ok = ($r6.Status -eq 409) -and ($r6.Raw -match "MSP_ALLOCATION_EXISTS")
    Assert-Case "R6" "分配幂等：重复分配返回 409 + reasonCode" $r6ok "status=$($r6.Status) body=$($r6.Raw.Substring(0, [Math]::Min(140, $r6.Raw.Length)))"
}

# ---- Q：租户硬配额（maxUsers / maxTicketsPerMonth；读用量后按 used+1 设限，复跑安全） ----
if ($null -eq $newTenant -or $null -eq $admin) {
    foreach ($idc in @("Q1","Q2","Q3","Q4","Q5","Q6")) { Skip $idc "硬配额" "验收租户未就绪" }
} else {
    # 复用 S3 已建立的验收租户管理员会话（避免额外登录 + 限流额度）。
    $qAdm = $ntAdmin
    if ($null -eq $qAdm) {
        foreach ($idc in @("Q1","Q2","Q3","Q4","Q5","Q6")) { Skip $idc "硬配额" "验收租户管理员登录失败" }
    } else {
        $uq = Invoke-Api -Method Get -Path "/api/v1/tenants/$($newTenant.id)/usage" -Sess $admin
        $usedUsers = [int]$uq.Json.data.used.users
        $usedTickets = [int]$uq.Json.data.used.ticketsThisMonth
        $q1 = Invoke-Api -Method Put -Path "/api/v1/tenants/$($newTenant.id)" -Sess $admin -Body @{
            quota = @{ maxUsers = ($usedUsers + 1); maxTicketsPerMonth = ($usedTickets + 1) }
        }
        Assert-Case "Q1" "平台设置硬配额（users=used+1 / tickets=used+1）" ($q1.Status -eq 200) "status=$($q1.Status) used=$usedUsers/$usedTickets"

        $qt1 = Invoke-Api -Method Post -Path "/api/v1/tickets" -Sess $qAdm -Body @{ title = "[ACPT-Q-$ts] 配额内样本"; description = "quota"; priority = "low"; type = "incident" }
        Assert-Case "Q2" "配额内建单成功（末位放行）" (($qt1.Status -ge 200) -and ($qt1.Status -lt 300)) "status=$($qt1.Status)"

        $qt2 = Invoke-Api -Method Post -Path "/api/v1/tickets" -Sess $qAdm -Body @{ title = "[ACPT-Q-$ts] 超额样本"; description = "quota"; priority = "low"; type = "incident" }
        $q3ok = ($qt2.Status -eq 422) -and ($qt2.Raw -match "TENANT_QUOTA_EXCEEDED")
        Assert-Case "Q3" "超配额建单被拒（422 TENANT_QUOTA_EXCEEDED）" $q3ok "status=$($qt2.Status) body=$($qt2.Raw.Substring(0, [Math]::Min(160, $qt2.Raw.Length)))"

        $pu1 = Invoke-Api -Method Post -Path "/api/v1/tenants/$($newTenant.id)/users" -Sess $admin -Body @{
            username = "acpt-q-$ts"; password = "Acpt@2026Quota!"; name = "Quota User"; email = "acpt-q-$ts@example.com"; role = "end_user"
        }
        Assert-Case "Q4" "平台供给建号成功（配额末位）" (($pu1.Status -ge 200) -and ($pu1.Status -lt 300)) "status=$($pu1.Status) body=$($pu1.Raw.Substring(0, [Math]::Min(140, $pu1.Raw.Length)))"

        $pu2 = Invoke-Api -Method Post -Path "/api/v1/tenants/$($newTenant.id)/users" -Sess $admin -Body @{
            username = "acpt-q2-$ts"; password = "Acpt@2026Quota!"; name = "Quota User 2"; email = "acpt-q2-$ts@example.com"; role = "end_user"
        }
        $q5ok = ($pu2.Status -eq 422) -and ($pu2.Raw -match "TENANT_QUOTA_EXCEEDED")
        Assert-Case "Q5" "超配额供给被拒（422 TENANT_QUOTA_EXCEEDED）" $q5ok "status=$($pu2.Status) body=$($pu2.Raw.Substring(0, [Math]::Min(160, $pu2.Raw.Length)))"

        $q6res = Invoke-Api -Method Put -Path "/api/v1/tenants/$($newTenant.id)" -Sess $admin -Body @{ quota = @{ maxUsers = 0; maxTicketsPerMonth = 0 } }
        $qt3 = Invoke-Api -Method Post -Path "/api/v1/tickets" -Sess $qAdm -Body @{ title = "[ACPT-Q-$ts] 解除配额后"; description = "quota off"; priority = "low"; type = "incident" }
        $q6ok = ($q6res.Status -eq 200) -and (($qt3.Status -ge 200) -and ($qt3.Status -lt 300))
        Assert-Case "Q6" "解除配额（0=不限）后建单恢复" $q6ok "restore=$($q6res.Status) ticket=$($qt3.Status)"
    }
}

# ---- T：租户暂停/恢复（客户面 + 服务商面可见性） ----
if ($null -eq $newTenant -or $null -eq $admin -or $null -eq $mspadmin -or $null -eq $qAdm) {
    foreach ($idc in @("T1","T2","T3","T4","T5","T6","T7")) { Skip $idc "租户暂停恢复" "前置未就绪" }
} else {
    # 服务商对该新客户租户建立分配（T 校验 CUSTOMER_INACTIVE 需要先过分配闸）
    $mspAdminId = [int]$mspadmin.Login.data.user.id
    $talloc = Invoke-Api -Method Post -Path "/api/v1/msp/allocations" -Sess $mspadmin -Body @{ mspUserId = $mspAdminId; customerTenantId = $newTenant.id; role = "primary" }
    $tallocOk = (($talloc.Status -ge 200) -and ($talloc.Status -lt 300)) -or ($talloc.Raw -match "MSP_ALLOCATION_EXISTS") -or ($talloc.Raw -match "已存在")
    if (-not $tallocOk) {
        # 兜底：以「该员工可见客户」为准（重复分配的旧版本曾返回 500 无 reasonCode）。
        $viewT = Invoke-Api -Method Get -Path "/api/v1/msp/customers" -Sess $mspadmin
        $hasT = @($viewT.Json.data.customers) | Where-Object { $_.id -eq $newTenant.id } | Select-Object -First 1
        $tallocOk = ($null -ne $hasT)
    }
    if (-not $tallocOk) {
        Skip "T1" "租户暂停恢复" "服务商对新租户分配失败 status=$($talloc.Status)"
        foreach ($idc in @("T2","T3","T4","T5","T6","T7")) { Skip $idc "租户暂停恢复" "分配未就绪" }
    } else {
        $t1 = Invoke-Api -Method Put -Path "/api/v1/tenants/$($newTenant.id)/status" -Sess $admin -Body @{ status = "suspended" }
        Assert-Case "T1" "平台暂停客户租户" ($t1.Status -eq 200) "status=$($t1.Status)"

        # 业务路由必须 fail-closed（身份端点观察见 T7）。
        $t2 = Invoke-Api -Method Get -Path "/api/v1/tickets?page=1&pageSize=5" -Sess $qAdm
        $t2ok = ($t2.Status -eq 403) -and ($t2.Raw -match "暂停|过期")
        Assert-Case "T2" "暂停后 live JWT 业务请求被拒（403）" $t2ok "status=$($t2.Status) body=$($t2.Raw.Substring(0, [Math]::Min(140, $t2.Raw.Length)))"

        $t3 = Invoke-Api -Method Get -Path "/api/v1/msp/customers/$($newTenant.id)/tickets" -Sess $mspadmin
        $t3ok = ($t3.Status -eq 403) -and ($t3.Raw -match "CUSTOMER_INACTIVE")
        Assert-Case "T3" "暂停客户对服务商不可见（CUSTOMER_INACTIVE）" $t3ok "status=$($t3.Status) body=$($t3.Raw.Substring(0, [Math]::Min(160, $t3.Raw.Length)))"

        $t4 = Invoke-Api -Method Put -Path "/api/v1/tenants/$($newTenant.id)/status" -Sess $admin -Body @{ status = "active" }
        Assert-Case "T4" "平台恢复租户 active" ($t4.Status -eq 200) "status=$($t4.Status)"

        $t5 = Invoke-Api -Method Get -Path "/api/v1/auth/me" -Sess $qAdm
        Assert-Case "T5" "恢复后客户面访问正常" ($t5.Status -eq 200) "status=$($t5.Status)"

        $t6 = Invoke-Api -Method Get -Path "/api/v1/msp/customers/$($newTenant.id)/tickets" -Sess $mspadmin
        Assert-Case "T6" "恢复后服务商面可见性正常" ($t6.Status -eq 200) "status=$($t6.Status)"

        # T7 观察项：/auth/me 不挂租户状态门禁（仅身份读取，无租户数据；已登记设计文档 D-8）。
        $t7 = Invoke-Api -Method Get -Path "/api/v1/auth/me" -Sess $qAdm
        Assert-Case "T7" "观察：/auth/me 身份端点不受租户状态门禁（D-8）" ($t7.Status -eq 200) "status=$($t7.Status)"
    }
}

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
$md.Add("> 状态：**PASS $passN / FAIL $failN / SKIP $skipN**｜日期：$runDate｜实例：``$Base``（saas_msp）｜总耗时：$totalS s｜脚本：``scripts/msp/acceptance/run-msp-business-acceptance.ps1``")
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
