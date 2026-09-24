# =============================================================================
#  Meshploy: join a Windows machine to the mesh as a mesh-only node
#
#  Windows cannot be a node in Meshploy's cluster, so it joins the WireGuard
#  mesh and nothing else: routes reach its ports over the mesh (Node + port
#  targets), and the machine itself stays unreachable from the internet.
#
#  Run in PowerShell as Administrator, with the command the console gives
#  (Cluster -> Add a node -> Mesh only -> Windows):
#
#    & ([scriptblock]::Create((irm https://api.<your-domain>/join/windows.ps1))) -Token mprov-...
#
#  -AllowPorts 11434,8080 also lets the mesh reach those TCP ports through
#  Windows Firewall, for whatever this machine serves.
#
#  Leave the mesh and remove the node from Meshploy:
#
#    & ([scriptblock]::Create((irm https://api.<your-domain>/join/windows.ps1))) -Uninstall
#
#  Written for Windows PowerShell 5.1, which every supported Windows has.
# =============================================================================
param(
  [string]$Token = "",
  [string]$Api = "",
  [string]$Name = "",
  [int[]]$AllowPorts = @(),
  [switch]$Uninstall
)

# Continue, not Stop: Windows PowerShell 5.1 turns any line a native program
# writes to stderr into a terminating error under Stop, and tailscale.exe
# writes to stderr in normal use. The web calls below stop on their own.
$ErrorActionPreference = "Continue"
$ProgressPreference = "SilentlyContinue"   # Invoke-WebRequest is many times slower with the bar
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# The gateway fills this line in with its own address when it serves the
# script at /join/windows.ps1. Matched literally by ServeJoinScript.
$DefaultApiBase = ""
if (-not $Api) { $Api = $DefaultApiBase }
$Api = $Api.TrimEnd("/")

$ConfDir   = Join-Path $env:ProgramData "Meshploy"
$Conf      = Join-Path $ConfDir "node.conf"
$Ts        = Join-Path $env:ProgramFiles "Tailscale\tailscale.exe"
$RuleGroup = "Meshploy mesh"
$MeshRange = "100.64.0.0/10"

function Info($m)    { Write-Host "  -> $m" -ForegroundColor Cyan }
function Success($m) { Write-Host "  OK $m" -ForegroundColor Green }
function Warn($m)    { Write-Host "  !  $m" -ForegroundColor Yellow }
# throw, never exit: this runs as a script block in the user's own session, and
# exit would close their PowerShell window along with the script.
function Fail($m)    { Write-Host "  X  $m" -ForegroundColor Red; throw "Meshploy join stopped." }
function Header($m)  { Write-Host ""; Write-Host $m -ForegroundColor White }

# The body of a refused request, which Invoke-RestMethod otherwise hides.
function ErrorBody($err) {
  if ($err.ErrorDetails -and $err.ErrorDetails.Message) { return $err.ErrorDetails.Message }
  return $err.Exception.Message
}

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  Fail "Run PowerShell as Administrator: this installs a service and changes the firewall."
}

function Read-Conf {
  $values = @{}
  if (Test-Path $Conf) {
    foreach ($line in Get-Content $Conf) {
      $i = $line.IndexOf("=")
      if ($i -gt 0) { $values[$line.Substring(0, $i)] = $line.Substring($i + 1) }
    }
  }
  return $values
}

# ── Leave ────────────────────────────────────────────────────────────────────
if ($Uninstall) {
  Header "Leaving the Meshploy mesh"
  $c = Read-Conf
  if ($c.NODE_ID -and $c.MESHPLOY_API_URL -and $c.NODE_SECRET) {
    # Before logging out: the API is reached over the mesh.
    try {
      $body = @{ node_secret = $c.NODE_SECRET; node_id = $c.NODE_ID } | ConvertTo-Json
      Invoke-RestMethod -Method Delete -Uri "$($c.MESHPLOY_API_URL)/api/v1/nodes/self-deregister" `
        -ContentType "application/json" -Body $body -TimeoutSec 10 -ErrorAction Stop | Out-Null
      Success "Removed from Meshploy"
    } catch {
      Warn "Meshploy refused: $(ErrorBody $_). Remove the node in the console: Nodes -> Remove"
    }
  } else {
    Warn "No node identity in $Conf; remove the node in the console if it is listed."
  }
  Get-NetFirewallRule -Group $RuleGroup -ErrorAction SilentlyContinue | Remove-NetFirewallRule
  if (Test-Path $Ts) {
    & $Ts logout 2>$null | Out-Null
    Success "Logged out of the mesh. Tailscale itself is left installed."
  }
  Remove-Item -Recurse -Force $ConfDir -ErrorAction SilentlyContinue
  Success "This machine has left the Meshploy mesh."
  return
}

function Set-MeshPorts($ports) {
  Get-NetFirewallRule -Group $RuleGroup -ErrorAction SilentlyContinue | Remove-NetFirewallRule
  New-NetFirewallRule -DisplayName "Meshploy mesh: TCP $($ports -join ',')" -Group $RuleGroup `
    -Direction Inbound -Protocol TCP -LocalPort $ports -RemoteAddress $MeshRange -Action Allow -Profile Any | Out-Null
  Success "The mesh ($MeshRange) may reach TCP $($ports -join ', '). Nothing else can."
}

# ── Only the firewall, on a machine already joined ───────────────────────────
if (-not $Token -and $AllowPorts.Count -gt 0 -and (Test-Path $Conf)) {
  Header "Windows Firewall"
  Set-MeshPorts $AllowPorts
  return
}

# ── Join ─────────────────────────────────────────────────────────────────────
if (-not $Token) { Fail "A provisioning token is required: copy the command from the console (Cluster -> Add a node -> Mesh only -> Windows)." }
if (-not $Token.StartsWith("mprov-")) { Fail "That is not a provisioning token (mprov-...)." }
if (-not $Api) { Fail "The gateway's address is needed: add -Api https://api.<your-domain>" }

Header "Asking $Api for this machine's mesh credentials"
try {
  $prov = Invoke-RestMethod -Method Post -Uri "$Api/api/v1/nodes/provision" `
    -ContentType "application/json" -Body (@{ token = $Token } | ConvertTo-Json) -TimeoutSec 20 -ErrorAction Stop
} catch {
  # The API answers the same way for every bad token on purpose.
  Fail "The gateway refused this token. It may be used, expired, or for another server. $(ErrorBody $_)"
}
if (-not $prov.headscale_url -or -not $prov.preauth_key) { Fail "The gateway's answer had no mesh credentials." }
$ApiMesh = if ($prov.api_mesh_url) { $prov.api_mesh_url } else { "http://100.64.0.1:4000" }
if ($prov.mesh_role -and $prov.mesh_role -ne "mesh") {
  Warn "This token was made for a '$($prov.mesh_role)' node. Windows cannot be in the cluster, so it joins as mesh only."
}
Success "Provisioned: mesh at $($prov.headscale_url)"

Header "Tailscale"
if (-not (Test-Path $Ts)) {
  $arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
  $msi = Join-Path $env:TEMP "tailscale-setup-$arch.msi"
  Info "Downloading Tailscale ($arch)..."
  Invoke-WebRequest -Uri "https://pkgs.tailscale.com/stable/tailscale-setup-latest-$arch.msi" -OutFile $msi -UseBasicParsing -ErrorAction Stop
  Info "Installing it silently..."
  # TS_NOLAUNCH keeps the tray app from opening on a machine nobody may be
  # watching; unattended mode is set by `tailscale up` below.
  $p = Start-Process msiexec.exe -ArgumentList "/i `"$msi`" /quiet /norestart TS_NOLAUNCH=true" -Wait -PassThru
  Remove-Item $msi -Force -ErrorAction SilentlyContinue
  if ($p.ExitCode -ne 0 -and $p.ExitCode -ne 3010) { Fail "The Tailscale installer failed (exit $($p.ExitCode))." }
  if (-not (Test-Path $Ts)) { Fail "Tailscale installed, but $Ts is missing." }
  Success "Tailscale installed"
} else {
  Success "Tailscale found"
}
# The service takes a moment to answer after install.
for ($i = 0; $i -lt 15; $i++) {
  & $Ts status --json 2>$null | Out-Null
  if ($LASTEXITCODE -eq 0) { break }
  Start-Sleep -Seconds 1
}

if (-not $Name) { $Name = $env:COMPUTERNAME }
$Name = ($Name.ToLower() -replace "[^a-z0-9-]", "-" -replace "-+", "-").Trim("-")
if (-not $Name) { $Name = "windows" }

Header "Joining the Meshploy mesh as '$Name'"
# --unattended keeps the connection up with nobody logged in, which is what a
# machine serving the mesh needs; without it Tailscale on Windows follows the
# signed-in user and drops at logout.
& $Ts up "--login-server=$($prov.headscale_url)" "--authkey=$($prov.preauth_key)" "--hostname=$Name" `
  --unattended --accept-routes --force-reauth --reset
if ($LASTEXITCODE -ne 0) { Fail "tailscale up failed. Check: & '$Ts' status" }

$MeshIp = ""
for ($i = 0; $i -lt 15; $i++) {
  $MeshIp = (& $Ts ip -4 2>$null | Select-Object -First 1)
  if ($MeshIp) { $MeshIp = $MeshIp.Trim(); break }
  Start-Sleep -Seconds 1
}
if (-not $MeshIp) { Fail "No mesh address after 15 s. Check: & '$Ts' status" }
Success "On the mesh at $MeshIp"

Header "Registering with Meshploy"
$ready = $false
for ($i = 0; $i -lt 12; $i++) {
  try {
    Invoke-WebRequest -Uri "$ApiMesh/api/v1/auth/login" -UseBasicParsing -TimeoutSec 4 -ErrorAction Stop | Out-Null
    $ready = $true; break
  } catch {
    # Any HTTP answer, even an error, means the API is reachable.
    if ($_.Exception.Response) { $ready = $true; break }
  }
  if ($i -eq 0) { Info "Waiting for $ApiMesh over the mesh..." }
  Start-Sleep -Seconds 5
}
if (-not $ready) {
  Fail "Cannot reach $ApiMesh over the mesh after 60 s, so this machine is on the mesh but not registered. A token hands out its mesh key once: fix the connection, then run a new command from the console."
}

try {
  $body = @{ token = $Token; name = $Name; tailscale_ip = $MeshIp; mesh_role = "mesh"; os = "windows" } | ConvertTo-Json
  $reg = Invoke-RestMethod -Method Post -Uri "$ApiMesh/api/v1/nodes/self-register" `
    -ContentType "application/json" -Body $body -TimeoutSec 15 -ErrorAction Stop
} catch {
  Fail "Meshploy did not register this machine: $(ErrorBody $_)"
}
if (-not $reg.id) { Fail "Meshploy's answer had no node id." }

New-Item -ItemType Directory -Force -Path $ConfDir | Out-Null
Set-Content -Path $Conf -Encoding ASCII -Value @(
  "NODE_ID=$($reg.id)", "NODE_NAME=$Name", "MESHPLOY_API_URL=$ApiMesh", "NODE_SECRET=$($reg.node_secret)"
)
# The node secret removes this node from Meshploy: SYSTEM and Administrators only.
& icacls.exe $ConfDir /inheritance:r /grant:r "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F" | Out-Null
Success "Registered as '$Name' (mesh only). Identity kept in $Conf, for leaving later."

if ($AllowPorts.Count -gt 0) {
  Header "Windows Firewall"
  Set-MeshPorts $AllowPorts
}

Write-Host ""
Write-Host "  This Windows machine is a mesh-only node." -ForegroundColor Green
Write-Host ""
Write-Host "  Node      $Name"
Write-Host "  Mesh IP   $MeshIp"
Write-Host ""
Write-Host "  Route to a port on it: Project -> Routes -> New route -> Node + port."
if ($AllowPorts.Count -eq 0) {
  Write-Host "  Windows Firewall blocks incoming connections by default. Run the same"
  Write-Host "  command with -AllowPorts <port> and without -Token to let the mesh reach a port."
}
Write-Host "  Metrics are not collected from Windows nodes yet."
Write-Host "  A machine that sleeps goes offline; keep it awake if it serves anything."
