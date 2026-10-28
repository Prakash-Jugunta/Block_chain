# PowerShell script to run the ForkRisk Milestone 5 demonstration
param (
    [string]$Mode = "live"
)

Write-Host "=====================================================" -ForegroundColor Cyan
Write-Host " Running ForkRisk Demonstration (Mode: $Mode)" -ForegroundColor Cyan
Write-Host "=====================================================" -ForegroundColor Cyan

go run ./cmd/demo --mode $Mode
if ($LASTEXITCODE -ne 0) {
    Write-Host "`nDemo execution failed with exit code $LASTEXITCODE" -ForegroundColor Red
    exit $LASTEXITCODE
}

Write-Host "`nDemo execution succeeded. Transcript saved to outputs/demo/demo_transcript.txt" -ForegroundColor Green
