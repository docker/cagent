param(
  [string]$EnvFile = ".env",
  [ValidateSet("groq","openrouter")] [string]$Provider = "groq"
)

# Pick a random free port between 20000-30000
$port = Get-Random -Minimum 20000 -Maximum 30000
$listen = ":$port"

Write-Host "[+] Starting smoke test using provider: $Provider on port $port"

# Start API server in the background
if ($Provider -eq "groq") {
  $apiCmd = "scripts/run_api_groq.ps1 -EnvFile `"$EnvFile`" -Listen `"$listen`""
} else {
  $apiCmd = "scripts/run_api_openrouter.ps1 -EnvFile `"$EnvFile`" -Listen `"$listen`""
}

$job = Start-Job -ScriptBlock { param($cmd) & powershell -NoProfile -ExecutionPolicy Bypass -Command $cmd } -ArgumentList $apiCmd

# Wait for the API to come up (max ~30s)
$ok = $false
for ($i=0; $i -lt 30; $i++) {
  Start-Sleep -Seconds 1
  try {
    $resp = Invoke-WebRequest -UseBasicParsing -Uri "http://localhost:$port/api/agents" -TimeoutSec 2
    if ($resp.StatusCode -eq 200) { $ok = $true; break }
  } catch {}
}
if (-not $ok) {
  Write-Error "API did not become ready on port $port in time."
  Receive-Job $job -Keep | Write-Host
  Stop-Job $job -Force | Out-Null
  exit 1
}
Write-Host "[+] API is ready on http://localhost:$port"

# Hit endpoints
$agents = Invoke-WebRequest -UseBasicParsing -Uri "http://localhost:$port/api/agents" | Select-Object -ExpandProperty Content
Write-Host "[agents]`n$agents"

# Exec mode prompts to test tools (runs separate containers; doesn't rely on API)
# Filesystem write
$key = if ($Provider -eq 'openrouter') { $Env:OPENROUTER_API_KEY } else { $Env:GROQ_API_KEY }
$fsWrite = & powershell -NoProfile -Command "docker run --rm --env-file `"$EnvFile`" -e OPENAI_API_KEY=`"$key`" -v `${PWD}.Path`:/work -w /work ascathleticsinc/cagent:latest exec examples/$Provider`_general.yaml `"Create a file smoke_fs.txt with content: smoke-ok`"" 2>&1
# Filesystem read
$fsRead = Test-Path .\smoke_fs.txt

# Shell command
$shellCmd = & powershell -NoProfile -Command "docker run --rm --env-file `"$EnvFile`" -e OPENAI_API_KEY=`"$key`" -v `${PWD}.Path`:/work -w /work ascathleticsinc/cagent:latest exec examples/$Provider`_general.yaml `"Run a shell command: echo shell-ok`"" 2>&1

# Memory (two calls)
$memWrite = & powershell -NoProfile -Command "docker run --rm --env-file `"$EnvFile`" -e OPENAI_API_KEY=`"$key`" -v `${PWD}.Path`:/work -w /work ascathleticsinc/cagent:latest exec examples/$Provider`_general.yaml `"Remember that the smoke key is 4242`"" 2>&1
$memRead  = & powershell -NoProfile -Command "docker run --rm --env-file `"$EnvFile`" -e OPENAI_API_KEY=`"$key`" -v `${PWD}.Path`:/work -w /work ascathleticsinc/cagent:latest exec examples/$Provider`_general.yaml `"What is the smoke key?`"" 2>&1

# Summarize
$pass = $true
if (-not $fsRead) { Write-Host "[FAIL] Filesystem read check: smoke_fs.txt not found"; $pass = $false } else { Write-Host "[OK] Filesystem read check" }
if ($shellCmd -notmatch "shell-ok") { Write-Host "[WARN] Shell output didn't show shell-ok. Output:"; Write-Host $shellCmd }
if ($memRead -notmatch "4242") { Write-Host "[WARN] Memory retrieval did not include 4242. Output:"; Write-Host $memRead } else { Write-Host "[OK] Memory round-trip" }

# Cleanup API job
Stop-Job $job -Force | Out-Null
Receive-Job $job -Keep | Out-Null

if ($pass) { Write-Host "[+] Smoke test completed." } else { Write-Error "[-] Smoke test failed."; exit 1 }
