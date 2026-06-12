$ErrorActionPreference = "Stop"
$OpensearchDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$CacheDir = Join-Path $OpensearchDir ".cache"
New-Item -ItemType Directory -Force -Path $CacheDir | Out-Null

docker run --rm `
    -e HF_XET_HIGH_PERFORMANCE=1 `
    -v "${CacheDir}:/data" `
    -v "$(Join-Path $OpensearchDir 'download_model.py'):/download_model.py:ro" `
    python:3.12-slim `
    bash -c "pip install -q huggingface_hub hf_transfer && python /download_model.py"

if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
