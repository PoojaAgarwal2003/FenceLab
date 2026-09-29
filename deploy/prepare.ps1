$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    if ((Test-Path 'deploy\.env') -or (Test-Path 'deploy\pki')) {
        throw 'Refusing to replace deployment identity/configuration; use the existing setup.'
    }
    & go build -o bin\fencelab.exe ./cmd/fencelab
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    & .\bin\fencelab.exe cluster-pki -dir deploy\pki
    if ($LASTEXITCODE -ne 0) { throw 'Credential generation failed.' }
    [IO.File]::WriteAllText((Join-Path (Get-Location) 'deploy\.env'), "FENCELAB_UID=65532`nFENCELAB_GID=65532`n")
    Write-Output 'Prepared credentials for Docker Desktop Linux containers. Restrict deploy\pki ACLs and keep issuer keys offline.'
} finally {
    Pop-Location
}
