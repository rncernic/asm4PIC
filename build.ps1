# build.ps1 - Cross-compilation script for Go (PowerShell)

# Define the targets
$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; Name = "asm4PIC_win_amd64.exe" },
    @{ GOOS = "linux"; GOARCH = "amd64"; Name = "asm4PIC_linux_amd64" },
    @{ GOOS = "linux"; GOARCH = "arm64"; Name = "asm4PIC_linux_arm64" },
    @{ GOOS = "darwin"; GOARCH = "amd64"; Name = "asm4PIC_macos_amd64" },
    @{ GOOS = "darwin"; GOARCH = "arm64"; Name = "asm4PIC_macos_arm64" }
)

# Output directory
$outputDir = "build"
if (-not (Test-Path $outputDir)) {
    New-Item -Path $outputDir -ItemType Directory | Out-Null
}

Write-Host "--- Starting Go Cross-Compilation ---"

foreach ($target in $targets) {
    Write-Host "Building for $($target.GOOS)/$($target.GOARCH)..." -ForegroundColor Yellow
    
    $env:GOOS = $target.GOOS
    $env:GOARCH = $target.GOARCH
    
    # Use the -ldflags='-s -w' flag to strip debugging symbols and reduce binary size
    go build -ldflags='-s -w' -o "$outputDir\$($target.Name)"
    
    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: Build failed for $($target.GOOS)/$($target.GOARCH)." -ForegroundColor Red
    } else {
        Write-Host "SUCCESS: Saved to $outputDir\$($target.Name)" -ForegroundColor Green
    }
}

# Clean up environment variables
Remove-Item Env:\GOOS
Remove-Item Env:\GOARCH

Write-Host "--- Cross-Compilation Complete! ---"