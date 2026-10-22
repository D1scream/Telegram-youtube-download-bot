# Деплой из PowerShell: запускает deploy.sh в Git Bash (в нём есть go, ssh и tar).
# Голый `bash` в PowerShell обычно WSL: там нет Go и не видны переменные окружения Windows.
#   .\deploy.ps1          — обычный деплой
#   .\deploy.ps1 -Force   — залить поверх изменений на сервере
param([switch]$Force)
$ErrorActionPreference = 'Stop'

$bash = 'C:\Program Files\Git\bin\bash.exe'
if (-not (Test-Path $bash)) {
    $git = Get-Command git -ErrorAction SilentlyContinue
    if ($git) { $bash = Join-Path (Split-Path (Split-Path $git.Source)) 'bin\bash.exe' }
}
if (-not (Test-Path $bash)) { throw 'Не найден Git Bash (bash.exe из Git for Windows).' }

$deployArgs = @("$PSScriptRoot/deploy.sh")
if ($Force) { $deployArgs += '--force' }
& $bash @deployArgs
exit $LASTEXITCODE
