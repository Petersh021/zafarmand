#Requires -Version 5.1

<#
.SYNOPSIS
Runs the destructive Stage 14 PostgreSQL acceptance checks against one
script-owned disposable database.

.DESCRIPTION
The script prompts interactively for the local PostgreSQL administrator
password, creates one exact database whose name ends in `_test`, runs the
opt-in Go integration tests and migration CLI status/up checks, and drops only the
database it created. Credentials live only in process memory during execution;
they are never written to a file, command argument, or persistent environment
variable. Every pre-existing process environment value is restored afterward.
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# The fixed name and suffix satisfy the Go integration test's destructive-test
# guard. A pre-existing database is never reused or removed by this script.
$testDatabaseName = "zafarmand_stage14_codex_test"
$postgresHost = "localhost"
$postgresPort = 5432
$postgresAdministrator = "postgres"
$postgresMaintenanceDatabase = "postgres"
$integrationConfirmation = "stage13-disposable-database"
$postgresDefaultExecutable = "C:\Program Files\PostgreSQL\18\bin\psql.exe"

# Record every process variable the checker will temporarily replace. This
# makes direct invocation as safe as the separate visible process used by
# Codex: existing development credentials and Go settings survive unchanged.
$managedEnvironmentNames = @(
    "PGPASSWORD",
    "DATABASE_URL",
    "ZAFARMAND_TEST_DATABASE_URL",
    "ZAFARMAND_TEST_DATABASE_CONFIRM",
    "ZAFARMAND_TEST_CLUSTER_CONFIRM",
    "GOCACHE"
)
$originalProcessEnvironment = @{}
foreach ($environmentName in $managedEnvironmentNames) {
    $environmentValue = [Environment]::GetEnvironmentVariable(
        $environmentName,
        [EnvironmentVariableTarget]::Process
    )
    $originalProcessEnvironment[$environmentName] = [pscustomobject]@{
        Exists = $null -ne $environmentValue
        Value = $environmentValue
    }
}

<#
.SYNOPSIS
Runs one native command and converts its nonzero exit code into a terminating
PowerShell error.

.PARAMETER FilePath
The trusted executable path selected by the script.

.PARAMETER Arguments
The separate argument values supplied without constructing a shell command.

.PARAMETER Description
A credential-free operation label used if the native command fails.
#>
function Invoke-CheckedNativeCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [Parameter(Mandatory = $true)]
        [string[]]$Arguments,

        [Parameter(Mandatory = $true)]
        [string]$Description
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Description failed with exit code $LASTEXITCODE."
    }
}

<#
.SYNOPSIS
Builds the common psql connection arguments for the local administrator.

.PARAMETER DatabaseName
The fixed maintenance or disposable database selected by the caller.
#>
function New-PostgresConnectionArguments {
    param(
        [Parameter(Mandatory = $true)]
        [string]$DatabaseName
    )

    return @(
        "--no-psqlrc",
        "--no-password",
        "--set", "ON_ERROR_STOP=on",
        "--host", $postgresHost,
        "--port", $postgresPort.ToString(),
        "--username", $postgresAdministrator,
        "--dbname", $DatabaseName
    )
}

# Resolve psql from PATH first and use the verified PostgreSQL 18 installation
# path only as a local fallback. Neither branch downloads or installs software.
$psqlCommand = Get-Command "psql.exe" -ErrorAction SilentlyContinue
if ($null -ne $psqlCommand) {
    $psqlExecutable = $psqlCommand.Source
}
elseif (Test-Path -LiteralPath $postgresDefaultExecutable) {
    $psqlExecutable = $postgresDefaultExecutable
}
else {
    throw "psql.exe was not found in PATH or the verified PostgreSQL 18 location."
}

# Refuse to continue if a future edit weakens the disposable database naming
# boundary before any connection or destructive operation is attempted.
if ($testDatabaseName -notmatch "^[a-z0-9_]+_test$") {
    throw "The Stage 14 database name must be a simple identifier ending in _test."
}

$securePassword = Read-Host `
    "Enter the password for local PostgreSQL role '$postgresAdministrator'" `
    -AsSecureString
$passwordPointer = [IntPtr]::Zero
$plainPassword = $null
$databaseCreated = $false
$checksPassed = $false
$cleanupPassed = $true

try {
    # psql and pgx both need a plain value at their native process boundary.
    # Keep it only in this process and release the unmanaged copy in the finally
    # block even when authentication or a test fails.
    $passwordPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR(
        $securePassword
    )
    $plainPassword = [Runtime.InteropServices.Marshal]::PtrToStringBSTR(
        $passwordPointer
    )
    $env:PGPASSWORD = $plainPassword

    $maintenanceArguments = New-PostgresConnectionArguments `
        -DatabaseName $postgresMaintenanceDatabase

    # Authentication is checked before database existence. The query returns
    # only 0 or 1 and cannot print a credential.
    $existenceArguments = $maintenanceArguments + @(
        "--tuples-only",
        "--no-align",
        "--command",
        "SELECT COUNT(*) FROM pg_database WHERE datname = '$testDatabaseName';"
    )
    $databaseCount = & $psqlExecutable @existenceArguments
    if ($LASTEXITCODE -ne 0) {
        throw "PostgreSQL authentication or connectivity check failed."
    }
    if (($databaseCount | Out-String).Trim() -ne "0") {
        throw "Disposable database '$testDatabaseName' already exists; it was not changed."
    }

    # The identifier is the fixed, regex-validated script constant above. The
    # script records ownership immediately so cleanup can remove only this
    # newly created database.
    Invoke-CheckedNativeCommand `
        -FilePath $psqlExecutable `
        -Arguments ($maintenanceArguments + @(
            "--command",
            "CREATE DATABASE $testDatabaseName;"
        )) `
        -Description "Create the Stage 14 disposable database"
    $databaseCreated = $true

    # Percent-encode the password before placing the temporary value in a URL.
    # These environment variables are temporary process values restored in
    # finally; no command prints either credential-bearing URL.
    $escapedPassword = [Uri]::EscapeDataString($plainPassword)
    $testDatabaseURL = (
        "postgres://{0}:{1}@{2}:{3}/{4}?sslmode=disable" -f
        $postgresAdministrator,
        $escapedPassword,
        $postgresHost,
        $postgresPort,
        $testDatabaseName
    )
    $env:ZAFARMAND_TEST_DATABASE_URL = $testDatabaseURL
    $env:ZAFARMAND_TEST_DATABASE_CONFIRM = $integrationConfirmation
    # This helper owns one database, not the PostgreSQL cluster. Explicitly
    # suppress the independent cluster-global role-DDL test even if the caller
    # had that CI-only opt-in in the parent PowerShell process.
    [Environment]::SetEnvironmentVariable(
        "ZAFARMAND_TEST_CLUSTER_CONFIRM",
        $null,
        [EnvironmentVariableTarget]::Process
    )
    $env:DATABASE_URL = $testDatabaseURL

    # The managed execution environment cannot write Go's default user cache.
    # A task-specific directory under the operating-system temporary root keeps
    # build artifacts outside the repository and is restored after the check.
    $env:GOCACHE = Join-Path `
        ([IO.Path]::GetTempPath()) `
        "zafarmand-go-build-cache"

    # Run from the repository root regardless of the caller's current path.
    # The integration suite first proves real DDL, rollback, key backfill,
    # exact name/email storage, replay behavior, and database constraints.
    Push-Location -LiteralPath $PSScriptRoot
    try {
        Invoke-CheckedNativeCommand `
            -FilePath "go" `
            -Arguments @("test", "-count=1", "-run", "Postgres", "./...") `
            -Description "Run Stage 14 PostgreSQL integration tests"

        # Exercise the public migration command after the integration tests
        # return the disposable schema to an empty state.
        Invoke-CheckedNativeCommand `
            -FilePath "go" `
            -Arguments @("run", ".", "migrate", "status") `
            -Description "Read initial migration status"
        Invoke-CheckedNativeCommand `
            -FilePath "go" `
            -Arguments @("run", ".", "migrate", "up") `
            -Description "Apply Stage 14 migrations"
        Invoke-CheckedNativeCommand `
            -FilePath "go" `
            -Arguments @("run", ".", "migrate", "status") `
            -Description "Read final migration status"
    }
    finally {
        Pop-Location
    }

    $checksPassed = $true
}
catch {
    # The error message is designed by this script or the checked project
    # commands. Passwords and connection URLs are never included deliberately.
    Write-Host ""
    Write-Host "Stage 14 PostgreSQL check failed: $($_.Exception.Message)" `
        -ForegroundColor Red
}
finally {
    if ($databaseCreated) {
        try {
            # Drop only the exact database created by this invocation. All Go
            # pools have closed before this cleanup begins.
            $maintenanceArguments = New-PostgresConnectionArguments `
                -DatabaseName $postgresMaintenanceDatabase
            Invoke-CheckedNativeCommand `
                -FilePath $psqlExecutable `
                -Arguments ($maintenanceArguments + @(
                    "--command",
                    "DROP DATABASE $testDatabaseName;"
                )) `
                -Description "Remove the Stage 14 disposable database"
        }
        catch {
            $cleanupPassed = $false
            Write-Host "Disposable database cleanup failed: $($_.Exception.Message)" `
                -ForegroundColor Red
        }
    }

    # Restore every original process value before releasing the unmanaged
    # password buffer. A variable that was originally absent is removed, while
    # an existing value—including an empty string—is restored exactly.
    foreach ($environmentName in $managedEnvironmentNames) {
        $originalValue = $originalProcessEnvironment[$environmentName]
        if ($originalValue.Exists) {
            [Environment]::SetEnvironmentVariable(
                $environmentName,
                $originalValue.Value,
                [EnvironmentVariableTarget]::Process
            )
        }
        else {
            [Environment]::SetEnvironmentVariable(
                $environmentName,
                $null,
                [EnvironmentVariableTarget]::Process
            )
        }
    }
    $plainPassword = $null
    if ($passwordPointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordPointer)
    }
}

if ($checksPassed -and $cleanupPassed) {
    Write-Host ""
    Write-Host "Stage 14 PostgreSQL checks passed; the disposable database was removed." `
        -ForegroundColor Green
    exit 0
}

Write-Host ""
Read-Host "Press Enter to close this Stage 14 check"
exit 1
