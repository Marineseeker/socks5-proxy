param(
    [Parameter(Position = 0)]
    [string]$Message = "",

    [string]$Remote = "origin",

    [string]$Branch = "",

    [switch]$NoPull
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Run-Git {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    & git @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "git $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ([string]::IsNullOrWhiteSpace($scriptDir)) {
    $scriptDir = Get-Location
}

Push-Location $scriptDir
try {
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
        throw "git command was not found. Please install Git and add it to PATH."
    }

    Run-Git -Arguments @("rev-parse", "--is-inside-work-tree") | Out-Null

    $repoRoot = (& git rev-parse --show-toplevel).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($repoRoot)) {
        throw "Unable to detect Git repository root."
    }

    Set-Location $repoRoot

    $remoteNames = & git remote
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to read Git remotes."
    }

    if ($remoteNames -notcontains $Remote) {
        throw "Remote '$Remote' does not exist. Run: git remote add $Remote <repository-url>"
    }

    if ([string]::IsNullOrWhiteSpace($Branch)) {
        $Branch = (& git rev-parse --abbrev-ref HEAD).Trim()
        if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($Branch)) {
            throw "Unable to detect current branch."
        }
    }

    if ($Branch -eq "HEAD") {
        throw "Detached HEAD detected. Please switch to a normal branch first."
    }

    if ([string]::IsNullOrWhiteSpace($Message)) {
        $Message = "chore: update project $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')"
    }

    Write-Host "Repository: $repoRoot"
    Write-Host "Remote:     $Remote"
    Write-Host "Branch:     $Branch"
    Write-Host "Message:    $Message"

    Write-Host ""
    Write-Host "Staging changes..."
    Run-Git -Arguments @("add", "-A")

    $staged = & git diff --cached --name-only
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to inspect staged changes."
    }

    if ([string]::IsNullOrWhiteSpace(($staged -join "").Trim())) {
        Write-Host "No changes to commit."
    }
    else {
        Write-Host "Files to commit:"
        $staged | ForEach-Object { Write-Host "  $_" }

        Write-Host ""
        Write-Host "Creating commit..."
        Run-Git -Arguments @("commit", "-m", $Message)
    }

    if (-not $NoPull) {
        Write-Host ""
        Write-Host "Fetching remote updates..."
        Run-Git -Arguments @("fetch", $Remote)

        $upstreamOutput = & git rev-parse --abbrev-ref --symbolic-full-name "@{u}" 2>$null
        if ($LASTEXITCODE -eq 0 -and $null -ne $upstreamOutput) {
            $upstream = ($upstreamOutput | Select-Object -First 1).ToString().Trim()
            if (-not [string]::IsNullOrWhiteSpace($upstream)) {
                Write-Host "Rebasing on $upstream..."
                Run-Git -Arguments @("pull", "--rebase", $Remote, $Branch)
            }
            else {
                Write-Host "No upstream branch configured. Skipping pull --rebase."
            }
        }
        else {
            Write-Host "No upstream branch configured. Skipping pull --rebase."
        }
    }

    Write-Host ""
    Write-Host "Pushing to $Remote/$Branch..."
    Run-Git -Arguments @("push", "-u", $Remote, $Branch)

    Write-Host ""
    Write-Host "Done. Changes were committed and pushed to $Remote/$Branch."
}
finally {
    Pop-Location
}