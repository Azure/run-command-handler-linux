#requires -Version 7.3
<#
.SYNOPSIS
Validates the harness locally without logging into Azure or creating resources.
#>
[CmdletBinding()]
param([switch] $SkipBuild)

$ErrorActionPreference = 'Stop'
$harnessPath = Join-Path $PSScriptRoot 'Test-RunCommand.ps1'
$parseErrors = $null
$tokens = $null
$null = [Management.Automation.Language.Parser]::ParseFile($harnessPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw ($parseErrors.Message -join "`n") }
. $harnessPath

$testRoot = Join-Path ([IO.Path]::GetTempPath()) "runcommand-harness-tests-$([guid]::NewGuid().ToString('N'))"
$null = New-Item -ItemType Directory -Path $testRoot
$script:PrivateDirectory = $testRoot
$script:OutputDirectory = $testRoot
$script:Secrets = [Collections.Generic.List[string]]::new()
$script:Results = [Collections.Generic.List[hashtable]]::new()
$script:VmName = 'test-vm'
$script:ResourceGroup = 'test-rg'
$script:Location = 'centraluseuap'
$script:VmId = '/subscriptions/test/resourceGroups/test-rg/providers/Microsoft.Compute/virtualMachines/test-vm'
$script:SubscriptionId = 'test-subscription'
$passed = 0

function Check {
    param([bool] $Condition, [string] $Message)
    if (-not $Condition) { throw $Message }
}

function Expect-Error {
    param([scriptblock] $Action, [string] $Pattern)
    $caught = $null
    try { & $Action }
    catch { $caught = $_.Exception.Message }
    Check ($null -ne $caught -and $caught -match $Pattern) "Expected error matching '$Pattern'; received '$caught'."
}

function Test-Local {
    param([string] $Name, [scriptblock] $Test)
    & $Test
    $script:passed++
    Write-Host "PASS offline: $Name" -ForegroundColor Green
}

function Get-TestResource {
    param([bool] $Immediate = $true)
    @{
        properties = @{
            provisioningState = 'Succeeded'
            runImmediately = $Immediate
            instanceView = @{ executionState = 'Succeeded'; exitCode = 0; output = 'ok'; error = '' }
        }
    }
}

function New-TestArmToken {
    param([hashtable] $Claims = @{
        oid = '11111111-1111-1111-1111-111111111111'
        tid = '22222222-2222-2222-2222-222222222222'
    })
    $payload = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes(($Claims | ConvertTo-Json -Compress)))
    "test-header.$($payload.TrimEnd('=').Replace('+', '-').Replace('/', '_')).test-signature"
}

try {
    Test-Local 'direct RunCommand request contract and no caller mutation' {
        $properties = @{ source = @{ scriptUri = 'https://example.invalid/script?sig=secret' }; runAsPassword = 'secret-password' }
        foreach ($immediate in @($false, $true)) {
            $request = New-RunCommandRequest $properties $immediate
            Check ($request.location -eq $script:Location) 'Incorrect resource location.'
            Check (-not $request.ContainsKey('resources')) 'RunCommand still uses an ARM template.'
            $actual = $request.properties
            Check ($actual.runAsPassword -eq 'secret-password') 'Protected command properties were lost.'
            if ($immediate) { Check ($actual.runImmediately -eq $true) 'Missing immediate property.' }
            else { Check (-not $actual.ContainsKey('runImmediately')) 'Traditional execution was marked immediate.' }
        }
        Check (-not $properties.ContainsKey('runImmediately')) 'Input properties were mutated.'
    }
    Test-Local 'Azure CLI scoping without interactive authentication' {
        function Invoke-Native {
            param($Executable, $Arguments, $Activity, $TimeoutSeconds, [switch]$AllowFailure)
            $script:CapturedArguments = $Arguments
            @{ ExitCode = 0; StdOut = '{"ok":true}'; StdErr = '' }
        }
        $null = Invoke-Az @('account', 'show') 'test'
        Check ($script:CapturedArguments -contains '--subscription') 'Normal calls were not subscription scoped.'
        Check ($script:CapturedArguments -contains '--only-show-errors') 'Normal CLI warnings were not suppressed.'
        $null = Invoke-Az @('account', 'show') 'checking the current account' -WithoutSubscription
        Check ($script:CapturedArguments -notcontains '--subscription') 'The default-account check was subscription scoped.'
        Check (-not (Get-Command Invoke-Native).Parameters.ContainsKey('Interactive')) 'The unsafe live output reader was retained.'
    }
    Test-Local 'intermittent storage authorization recovers without changing authentication' {
        $script:StorageAttempts = 0
        function Start-Sleep { param($Seconds) }
        function Invoke-Native {
            param($Executable, $Arguments, $Activity, $TimeoutSeconds, [switch]$AllowFailure)
            $script:StorageAttempts++
            $authIndex = [array]::IndexOf($Arguments, '--auth-mode')
            Check ($authIndex -ge 0 -and $Arguments[$authIndex + 1] -eq 'login') 'A retry changed Entra authentication.'
            if ($script:StorageAttempts -eq 1) {
                return @{ ExitCode = 1; StdOut = ''; StdErr = 'You do not have the required permissions needed to perform this operation.' }
            }
            @{ ExitCode = 0; StdOut = '{"uploaded":true}'; StdErr = '' }
        }
        $result = Invoke-Az @('storage', 'blob', 'upload', '--auth-mode', 'login') 'uploading a guest configuration' -RetryStorageAuthorization
        Check ($result.uploaded -and $script:StorageAttempts -eq 2) 'The transient permission failure was not retried.'
    }
    Test-Local 'persistent storage authorization is bounded and retains the actual error' {
        $script:StorageAttempts = 0
        function Start-Sleep { param($Seconds) [Threading.Thread]::Sleep(600) }
        function Invoke-Native {
            param($Executable, $Arguments, $Activity, $TimeoutSeconds, [switch]$AllowFailure)
            $script:StorageAttempts++
            @{ ExitCode = 1; StdOut = ''; StdErr = 'AuthorizationPermissionMismatch: permission remains denied.' }
        }
        $timer = [Diagnostics.Stopwatch]::StartNew()
        $messages = & {
            Expect-Error {
                Invoke-Az @('storage', 'blob', 'upload', '--auth-mode', 'login') 'uploading a guest configuration' `
                    -RetryStorageAuthorization -AuthorizationTimeoutSeconds 1
            } 'did not recover within 1 seconds.*AuthorizationPermissionMismatch'
        } 6>&1 | Out-String
        Check ($timer.Elapsed.TotalSeconds -lt 5) 'The authorization deadline was not enforced.'
        Check ($script:StorageAttempts -ge 2) 'The bounded authorization wait never retried.'
        Check ([regex]::Matches($messages, 'individual retries are suppressed').Count -eq 1) 'Retry notices spammed the console.'
    }
    Test-Local 'unrelated errors and conditional-access failures are never retried' {
        function Start-Sleep { param($Seconds) throw 'An unexpected retry was attempted.' }
        function Invoke-Native {
            param($Executable, $Arguments, $Activity, $TimeoutSeconds, [switch]$AllowFailure)
            @{ ExitCode = 1; StdOut = ''; StdErr = $script:StorageError }
        }
        foreach ($errorMessage in @('BlobNotFound: missing blob', 'AADSTS530084: 403 blocked by conditional access')) {
            $script:StorageError = $errorMessage
            Expect-Error {
                Invoke-Az @('storage', 'blob', 'download', '--auth-mode', 'login') 'downloading a blob' -RetryStorageAuthorization
            } ([regex]::Escape($errorMessage))
        }
        Expect-Error {
            Invoke-Az @('group', 'create') 'creating a group' -RetryStorageAuthorization
        } 'only be used with Azure CLI storage'
    }
    Test-Local 'blob uploads, downloads, and SAS generation all enable bounded authorization retries' {
        function Invoke-Az {
            param($Arguments, $Activity, $Output, [switch]$RetryStorageAuthorization)
            Check ([bool]$RetryStorageAuthorization) "Storage helper omitted authorization retries: $Activity"
            if ($Output -eq 'tsv') { return 'https://example.invalid/blob?sig=fixture' }
        }
        $script:StorageName = 'offline-storage'
        Send-Blob (Join-Path $testRoot 'fixture') 'fixture'
        Receive-Blob 'fixture' (Join-Path $testRoot 'fixture')
        Check ((New-BlobSas 'fixture') -like 'https://example.invalid/*') 'SAS generation did not return its result.'
    }
    Test-Local 'a failed configuration upload never attempts nonexistent blob cleanup' {
        $script:ConfigurationCleanupCalls = 0
        function Send-Blob { param($Path, $Name) throw 'configuration upload denied' }
        function Invoke-Az {
            param($Arguments, $Activity, $Output, [switch]$AllowFailure, [switch]$RetryStorageAuthorization, $AuthorizationTimeoutSeconds)
            $script:ConfigurationCleanupCalls++
            throw 'Unexpected blob cleanup after a failed upload.'
        }
        Expect-Error { Invoke-CustomScript install @{ hashes = @{} } } 'configuration upload denied'
        Check ($script:ConfigurationCleanupCalls -eq 0) 'The nonexistent configuration blob was deleted.'
        Check (@(Get-ChildItem -LiteralPath $testRoot -Filter '*guest-*.json').Count -eq 0) 'Temporary configuration files were left behind.'
    }
    Test-Local 'uploaded configurations are removed even if CustomScript execution fails' {
        $script:ConfigurationCleanupCalls = 0
        $script:GuestScriptUri = 'https://example.invalid/guest'
        $script:BinaryUris = @('https://example.invalid/main', 'https://example.invalid/service')
        function Send-Blob { param($Path, $Name) }
        function New-BlobSas { param($Name) "https://example.invalid/$Name" }
        function Invoke-Az {
            param($Arguments, $Activity, $Output, [switch]$AllowFailure, [switch]$RetryStorageAuthorization, $AuthorizationTimeoutSeconds, $TimeoutSeconds)
            if ($Arguments[0] -eq 'vm') { throw 'guest installation failed' }
            Check ($Arguments[0] -eq 'storage' -and $Arguments[2] -eq 'delete') 'Unexpected cleanup request.'
            Check ([bool]$RetryStorageAuthorization -and $AuthorizationTimeoutSeconds -eq 120) 'Configuration cleanup retries were not bounded.'
            $script:ConfigurationCleanupCalls++
            @{ ExitCode = 0; StdOut = ''; StdErr = '' }
        }
        Expect-Error { Invoke-CustomScript install @{ hashes = @{} } } 'guest installation failed'
        Check ($script:ConfigurationCleanupCalls -eq 1) 'Uploaded configuration was not removed.'
        Check (@(Get-ChildItem -LiteralPath $testRoot -Filter '*guest-*.json').Count -eq 0) 'Protected local configuration files were left behind.'
    }
    Test-Local 'CSE collection waits for the archive rather than a VM-wide long-running update' {
        $script:ArchiveChecks = 0
        function Send-Blob { param($Path, $Name) }
        function New-BlobSas { param($Name) "https://example.invalid/$Name" }
        function Invoke-Az {
            param($Arguments, $Activity, $Output, $TimeoutSeconds, [switch]$AllowFailure,
                [switch]$RetryStorageAuthorization, $AuthorizationTimeoutSeconds)
            if ($Arguments[0] -eq 'vm') {
                Check ($Arguments -contains '--no-wait' -and $TimeoutSeconds -eq 300) 'Collection waited for a VM-wide update.'
                return
            }
            if ($Arguments[2] -eq 'exists') {
                Check ($Arguments -contains 'fixture.tar.gz') 'The wrong diagnostic blob was polled.'
                $script:ArchiveChecks++
                if ($script:ArchiveChecks -eq 1) { return 'false' }
                return 'true'
            }
            Check ($Arguments[2] -eq 'delete') 'Unexpected CSE storage call.'
            return @{ ExitCode = 0; StdOut = ''; StdErr = '' }
        }
        function Wait-Condition {
            param($Activity, $Probe, $TimeoutSeconds)
            Check ($TimeoutSeconds -eq 240) 'Diagnostic archive polling was not bounded.'
            Check (-not (& $Probe)) 'A nonexistent archive was counted as uploaded.'
            Check ([bool](& $Probe)) 'An uploaded archive was not detected.'
        }
        Invoke-CustomScript collect @{ blobName = 'fixture.tar.gz'; uploadUri = 'https://example.invalid/upload' }
        Check ($script:ArchiveChecks -eq 2) 'CSE archive upload was not polled.'
    }
    Test-Local 'management calls do not enable response-header debug tracing' {
        function Invoke-Native {
            param($Executable, $Arguments, $Activity, $TimeoutSeconds, [switch]$AllowFailure)
            Check ($Arguments -notcontains '--debug' -and $Arguments -contains '--only-show-errors') 'Management calls still enable header tracing.'
            return @{
                ExitCode = 0; StdOut = '{}'
                StdErr = "Response headers: {'x-ms-request-id': '11111111-2222-3333-4444-555555555555', 'x-ms-correlation-request-id': '22222222-3333-4444-5555-666666666666'}"
            }
        }
        $captured = & {
            $null = Invoke-Az @('resource', 'update') 'testing quiet management calls'
        } 6>&1 | Out-String
        Check (-not $captured.Contains('x-ms-')) 'HTTP response header IDs are still traced.'
    }
    Test-Local 'missing login fails explicitly without trying to sign in' {
        function Invoke-Az {
            param($Arguments, $Activity, [switch]$WithoutSubscription, [switch]$AllowFailure, $TimeoutSeconds)
            Check (($Arguments -join ' ') -eq 'account show') 'Unexpected Azure request while checking login.'
            @{ ExitCode = 1; StdOut = ''; StdErr = 'Please run az login.' }
        }
        Expect-Error { Get-LoggedInAzureAccount } 'Run "az login"'
    }
    Test-Local 'expired ARM credentials require manual login without Microsoft Graph' {
        function Invoke-Az {
            param($Arguments, $Activity, $Output, [switch]$WithoutSubscription, [switch]$AllowFailure, $TimeoutSeconds)
            if ($Arguments[1] -eq 'show') {
                return @{ ExitCode = 0; StdOut = '{"id":"existing-subscription","user":{"type":"user"}}'; StdErr = '' }
            }
            Check ($Arguments[1] -eq 'get-access-token' -and $Arguments -contains 'arm') 'Credential validation requested a non-ARM token.'
            @{ ExitCode = 1; StdOut = ''; StdErr = 'Interactive authentication is required.' }
        }
        Expect-Error { Get-LoggedInAzureAccount } 'Run "az login"'
    }
    Test-Local 'existing login resolves the tenant user from ARM without exposing the token' {
        function Invoke-Az {
            param($Arguments, $Activity, $Output, [switch]$WithoutSubscription, [switch]$AllowFailure, $TimeoutSeconds)
            if ($Arguments[1] -eq 'show') {
                return @{
                    ExitCode = 0
                    StdOut = '{"id":"existing-subscription","tenantId":"22222222-2222-2222-2222-222222222222","user":{"type":"user"}}'
                    StdErr = ''
                }
            }
            Check ($Arguments[0] -eq 'account' -and $Arguments[1] -eq 'get-access-token' -and $Arguments -contains 'arm') 'An automatic login or Graph lookup was attempted.'
            @{ ExitCode = 0; StdOut = (New-TestArmToken); StdErr = '' }
        }
        $output = & { $script:ResolvedAccount = Get-LoggedInAzureAccount } 6>&1 | Out-String
        Check ($script:ResolvedAccount.id -eq 'existing-subscription') 'The default subscription was not preserved.'
        Check ($script:ResolvedAccount.userObjectId -eq '11111111-1111-1111-1111-111111111111') 'The object ID was not taken from the ARM token.'
        Check (-not $output.Contains((New-TestArmToken))) 'An access token was printed.'
        Check (-not (($script:ResolvedAccount | ConvertTo-Json -Depth 10).Contains((New-TestArmToken)))) 'An access token was returned as account metadata.'
        Check ((Protect-Text (New-TestArmToken)) -eq '<redacted>') 'An access token was not protected from later diagnostic output.'
    }
    Test-Local 'invalid ARM identity claims fail before creating resources' {
        $tenant = '22222222-2222-2222-2222-222222222222'
        Expect-Error { Get-ArmTokenObjectId 'not-a-token' $tenant } 'not a JWT'
        Expect-Error { Get-ArmTokenObjectId 'header.%.signature' $tenant } 'not valid base64'
        Expect-Error { Get-ArmTokenObjectId 'header.bm90LWpzb24.signature' $tenant } 'not valid JSON'
        Expect-Error { Get-ArmTokenObjectId (New-TestArmToken @{ tid = $tenant }) $tenant } 'valid user object ID'
        Expect-Error { Get-ArmTokenObjectId (New-TestArmToken @{ oid = 'not-a-guid'; tid = $tenant }) $tenant } 'valid user object ID'
        Expect-Error { Get-ArmTokenObjectId (New-TestArmToken) '33333333-3333-3333-3333-333333333333' } 'tenant does not match'
    }
    Test-Local 'native stdout, stderr, exit status, and argument boundaries' {
        $code = '[Console]::Out.Write($args[0]); [Console]::Error.Write("stderr"); exit 7'
        $path = Join-Path $testRoot 'native.ps1'
        Set-Content -LiteralPath $path -Value $code
        $result = Invoke-Native pwsh @('-NoProfile', '-File', $path, 'a b & c') 'testing native argument forwarding' -AllowFailure
        Check ($result.ExitCode -eq 7) 'Native failure status was lost.'
        Check ($result.StdOut.Trim() -eq 'a b & c') 'Arguments were split or interpreted as shell syntax.'
        Check ($result.StdErr.Contains('stderr')) 'Native stderr was lost.'
        Expect-Error { Invoke-Native pwsh @('-NoProfile', '-File', $path, 'text') 'expected failure' } 'exit 7'
    }
    Test-Local 'native children inherit the current PowerShell filesystem location' {
        Push-Location $testRoot
        try {
            $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes('(Get-Location).ProviderPath'))
            $result = Invoke-Native pwsh @('-NoProfile', '-EncodedCommand', $encoded) 'checking native working directory'
            Check ($result.StdOut.Trim() -eq $testRoot) 'The child process ignored Push-Location.'
        }
        finally { Pop-Location }
    }
    Test-Local 'date-shaped native arguments remain exact strings across cultures' {
        $path = Join-Path $testRoot 'native-dates.ps1'
        Set-Content -LiteralPath $path -Value 'foreach ($argument in $args) { [Console]::WriteLine([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($argument))) }'
        $values = @('2026-11-06T02:41:14Z', '2026-11-06T02:41:14.1234567Z', '2026-11-06T03:41:14+01:00')
        $oldCulture = [Threading.Thread]::CurrentThread.CurrentCulture
        try {
            foreach ($culture in @('de-DE', 'en-US', 'ar-SA')) {
                [Threading.Thread]::CurrentThread.CurrentCulture = [Globalization.CultureInfo]::GetCultureInfo($culture)
                $result = Invoke-Native pwsh (@('-NoProfile', '-File', $path) + $values) "preserving native arguments under $culture"
                $lines = @($result.StdOut.Trim() -split '\r?\n')
                Check ($lines.Count -eq $values.Count) 'Native argument count changed.'
                for ($index = 0; $index -lt $values.Count; $index++) {
                    $actual = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($lines[$index]))
                    Check ($actual -ceq $values[$index]) "Date-shaped argument was reformatted under ${culture}: $actual"
                }
            }
        }
        finally { [Threading.Thread]::CurrentThread.CurrentCulture = $oldCulture }
    }
    Test-Local 'SAS expiry uses invariant Gregorian UTC formatting for every culture' {
        function Invoke-Az {
            param($Arguments, $Activity, $Output, [switch]$RetryStorageAuthorization)
            $index = [array]::IndexOf($Arguments, '--expiry')
            $expiry = $Arguments[$index + 1]
            Check ($expiry -match '^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$') "SAS expiry is not ISO UTC: $expiry"
            $parsed = [DateTime]::ParseExact($expiry, "yyyy-MM-dd'T'HH:mm:ss'Z'",
                [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::AssumeUniversal).ToUniversalTime()
            Check ($parsed -gt [DateTime]::UtcNow.AddHours(3.99) -and $parsed -le [DateTime]::UtcNow.AddHours(4.01)) `
                'SAS expiry used a locale calendar or changed the four-hour lifetime.'
            return 'https://example.invalid/blob'
        }
        $oldCulture = [Threading.Thread]::CurrentThread.CurrentCulture
        try {
            foreach ($culture in @('de-DE', 'en-US', 'ar-SA', 'fi-FI')) {
                [Threading.Thread]::CurrentThread.CurrentCulture = [Globalization.CultureInfo]::GetCultureInfo($culture)
                $null = New-BlobSas 'fixture'
            }
        }
        finally { [Threading.Thread]::CurrentThread.CurrentCulture = $oldCulture }
    }
    Test-Local 'actual Azure CLI command forwarding without login' {
        $version = Invoke-Az @('version') 'checking the local Azure CLI version' -WithoutSubscription
        Check ($version.ContainsKey('azure-cli')) 'Azure CLI output was not parsed correctly.'
    }
    Test-Local 'actual Windows Azure CLI preserves REST query parameters over loopback HTTP' {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
        $listener.Start()
        $port = $listener.LocalEndpoint.Port
        $server = $null
        $oldNoProxy = $env:NO_PROXY
        try {
            $env:NO_PROXY = if ($oldNoProxy) { "$oldNoProxy,127.0.0.1" } else { '127.0.0.1' }
            $server = Start-ThreadJob -ArgumentList $listener -ScriptBlock {
                param($socket)
                $client = $socket.AcceptTcpClient()
                try {
                    $stream = $client.GetStream()
                    $reader = [IO.StreamReader]::new($stream, [Text.Encoding]::ASCII, $false, 1024, $true)
                    try {
                        $requestLine = $reader.ReadLine()
                        while ($reader.ReadLine()) {}
                        $body = [Text.Encoding]::UTF8.GetBytes((@{
                            requestTarget = $requestLine.Split(' ')[1]
                        } | ConvertTo-Json -Compress))
                        $header = [Text.Encoding]::ASCII.GetBytes(
                            "HTTP/1.1 200 OK`r`nx-ms-request-id: 11111111-2222-3333-4444-555555555555`r`nContent-Type: application/json`r`nContent-Length: $($body.Length)`r`nConnection: close`r`n`r`n"
                        )
                        $stream.Write($header, 0, $header.Length)
                        $stream.Write($body, 0, $body.Length)
                        $stream.Flush()
                    }
                    finally { $reader.Dispose() }
                }
                finally { $client.Dispose() }
            }
            $captured = & {
                $script:LoopbackResponse = Invoke-Az @(
                    'rest', '--method', 'get', '--url', "http://127.0.0.1:$port/runCommands/test",
                    '--url-parameters', 'api-version=2024-07-01', '$expand=instanceView',
                    '--skip-authorization-header'
                ) 'checking REST query forwarding through the actual Azure CLI launcher' -WithoutSubscription -TimeoutSeconds 30
            } 6>&1 | Out-String
            $response = $script:LoopbackResponse
            Check (-not $captured.Contains('11111111-2222-3333-4444-555555555555')) 'The real HTTP response request ID is still traced.'
            $target = [Uri]::UnescapeDataString($response.requestTarget)
            Check ($target -match '[?&]api-version=2024-07-01(?:&|$)') 'The API version did not reach the HTTP server.'
            Check ($target -match '[?&]\$expand=instanceView(?:&|$)') 'The expand parameter was interpreted by the shell or lost.'
            $null = Wait-Job -Job $server -Timeout 5
            Check ($server.State -eq 'Completed') 'The loopback REST request did not complete.'
            $null = Receive-Job -Job $server -ErrorAction Stop
        }
        finally {
            $env:NO_PROXY = $oldNoProxy
            $listener.Stop()
            if ($server) {
                if ($server.State -in @('Running', 'NotStarted')) { Stop-Job -Job $server }
                Remove-Job -Job $server -Force
            }
        }
    }
    Test-Local 'output is read only after the native writer exits' {
        $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes(
            'Write-Output "first"; Start-Sleep -Seconds 2; Write-Output "second"'
        ))
        $result = Invoke-Native pwsh @('-NoProfile', '-EncodedCommand', $encoded) 'waiting for redirected output to close'
        Check ($result.StdOut.Contains('first') -and $result.StdOut.Contains('second')) 'Redirected output was lost or read while still locked.'
    }
    Test-Local 'stale registrations are reusable and the current host is never terminated' {
        $identity = Join-Path $testRoot 'instance-fixture.ps1'
        $registration = Enter-ValidationInstance -ScriptPath $identity
        $statePath = $registration.Name
        try {
            Expect-Error { Enter-ValidationInstance -ScriptPath $identity } 'current PowerShell process or its ancestor'
            $registration.Dispose()
            $registration = Enter-ValidationInstance -ScriptPath $identity
            Check ($registration.CanWrite) 'A stale registration blocked the next run.'
        }
        finally {
            $registration.Dispose()
            Remove-Item -LiteralPath $statePath -Force
        }
    }
    Test-Local 'a replacement run terminates only its registered predecessor and native children' {
        $identity = Join-Path $testRoot 'previous-instance-fixture.ps1'
        $ready = Join-Path $testRoot 'previous-ready.json'
        $childOutput = Join-Path $testRoot 'previous.out'
        $childError = Join-Path $testRoot 'previous.err'
        $sleepEncoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes('Start-Sleep -Seconds 60'))
        $code = @"
. '$($harnessPath.Replace("'", "''"))'
`$lease = Enter-ValidationInstance -ScriptPath '$($identity.Replace("'", "''"))'
try {
    `$child = Start-Process pwsh -ArgumentList @('-NoProfile', '-EncodedCommand', '$sleepEncoded') -PassThru
    `$state = @{ ChildId = `$child.Id; StatePath = `$lease.Name } | ConvertTo-Json
    [IO.File]::WriteAllText('$($ready.Replace("'", "''")).tmp', `$state)
    [IO.File]::Move('$($ready.Replace("'", "''")).tmp', '$($ready.Replace("'", "''"))')
    Start-Sleep -Seconds 60
}
finally { `$lease.Dispose() }
"@
        $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($code))
        $predecessor = Start-Process pwsh -ArgumentList @('-NoProfile', '-EncodedCommand', $encoded) `
            -RedirectStandardOutput $childOutput -RedirectStandardError $childError -PassThru
        $native = $null
        $replacement = $null
        $statePath = $null
        try {
            Wait-Condition -Activity 'waiting for the isolated process fixture to register' -TimeoutSeconds 30 -IntervalSeconds 1 -Probe {
                if ($predecessor.HasExited) { throw "Process fixture failed: $([IO.File]::ReadAllText($childError))" }
                Test-Path -LiteralPath $ready
            }
            $state = [IO.File]::ReadAllText($ready) | ConvertFrom-Json -AsHashtable
            $statePath = $state.StatePath
            $native = Get-Process -Id $state.ChildId
            $null = $native.Handle
            $replacement = Enter-ValidationInstance -ScriptPath $identity
            Check ($predecessor.WaitForExit(5000)) 'Previous validation host is still running.'
            Check ($native.WaitForExit(5000)) 'The previous native child is still running.'
            Check (-not (Get-Process -Id $PID).HasExited) 'The current host was affected.'
        }
        finally {
            if ($replacement) { $replacement.Dispose() }
            foreach ($process in @($predecessor, $native)) {
                if ($process) {
                    if (-not $process.HasExited) {
                        Stop-Process -Id $process.Id -Force
                        $null = $process.WaitForExit(5000)
                    }
                    $process.Dispose()
                }
            }
            Test-Local 'legacy absolute -File runs are stopped without touching a different script path' {
                $identity = Join-Path $testRoot 'legacy-fixture.ps1'
                $otherDirectory = Join-Path $testRoot 'unrelated'
                $null = New-Item -ItemType Directory -Path $otherDirectory
                $otherPath = Join-Path $otherDirectory 'legacy-fixture.ps1'
                Set-Content -LiteralPath $identity, $otherPath -Value 'Start-Sleep -Seconds 60'
                $previous = Start-Process pwsh -ArgumentList @('-NoProfile', '-File', "`"$identity`"") -PassThru
                $other = Start-Process pwsh -ArgumentList @('-NoProfile', '-File', "`"$otherPath`"") -PassThru
                $persistent = Start-Process pwsh -ArgumentList @('-NoProfile', '-NoExit', '-File', "`"$identity`"") -PassThru
                $registration = $null
                $statePath = $null
                try {
                    $registration = Enter-ValidationInstance -ScriptPath $identity
                    $statePath = $registration.Name
                    Check ($previous.WaitForExit(5000)) 'An older -File invocation was not stopped.'
                    Check (-not $other.HasExited) 'A different script path was terminated.'
                    Check (-not $persistent.HasExited) 'An unregistered persistent terminal was terminated.'
                }
                finally {
                    if ($registration) { $registration.Dispose() }
                    foreach ($process in @($previous, $other, $persistent)) {
                        if (-not $process.HasExited) {
                            Stop-Process -Id $process.Id -Force
                            $null = $process.WaitForExit(5000)
                        }
                        $process.Dispose()
                    }
                    if ($statePath -and (Test-Path -LiteralPath $statePath)) { Remove-Item -LiteralPath $statePath -Force }
                }
            }
            if ($statePath -and (Test-Path -LiteralPath $statePath)) { Remove-Item -LiteralPath $statePath -Force }
        }
    }
    Test-Local 'native heartbeat describes the actual wait' {
        $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes('Start-Sleep -Seconds 3'))
        $script:LastConsoleUtc = [DateTime]::UtcNow
        $captured = & {
            $null = Invoke-Native pwsh @('-NoProfile', '-EncodedCommand', $encoded) `
                'waiting for a simulated VM deployment' -HeartbeatSeconds 1
        } 6>&1 | Out-String
        Check ($captured -match 'Still waiting for a simulated VM deployment; waiting for pwsh to finish') 'No descriptive heartbeat was emitted.'
    }
    Test-Local 'native timeout is bounded and fails explicitly' {
        $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes('Start-Sleep -Seconds 30'))
        $timer = [Diagnostics.Stopwatch]::StartNew()
        Expect-Error {
            Invoke-Native pwsh @('-NoProfile', '-EncodedCommand', $encoded) 'simulated hung deployment' -TimeoutSeconds 2
        } 'Timed out.*simulated hung deployment'
        Check ($timer.Elapsed.TotalSeconds -lt 15) 'Timeout did not stop the native process promptly.'
    }
    Test-Local 'native timeout terminates an external child process as well as its PowerShell host' {
        $pidPath = Join-Path $testRoot 'timeout-child.pid'
        $escaped = $pidPath.Replace("'", "''")
        $child = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes("`$PID | Set-Content -LiteralPath '$escaped'; Start-Sleep -Seconds 60"))
        $parent = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes("& pwsh -NoProfile -EncodedCommand '$child'"))
        Expect-Error { Invoke-Native pwsh @('-NoProfile', '-EncodedCommand', $parent) 'testing process-tree timeout' -TimeoutSeconds 4 } 'Timed out'
        Check (Test-Path -LiteralPath $pidPath) 'The child fixture did not start.'
        $childPid = [int](Get-Content -LiteralPath $pidPath -Raw)
        Check ($null -eq (Get-Process -Id $childPid -ErrorAction SilentlyContinue)) 'The timed-out external child was left running.'
    }
    Test-Local 'sensitive errors are redacted' {
        $script:Secrets.Add('actual-password')
        $text = Protect-Text 'actual-password https://example.invalid/blob?sv=1&sig=abc%2F123&sp=r'
        Check (-not $text.Contains('actual-password') -and -not $text.Contains('abc%2F123')) 'Sensitive data was not redacted.'
    }
    Test-Local 'both modes use direct PUT, operationId polling, and final instance view' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-Az { throw 'RunCommand must not use an ARM deployment or CLI status poll.' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body, $TimeoutSeconds, [switch]$RequireInstanceView)
            Check ($Token -eq 'fixture-token') 'Existing ARM credentials were not used.'
            if ($Method -eq 'PUT') {
                Check ($Uri -eq "$($script:VmId)/runCommands/case?api-version=2024-07-01") 'Incorrect resource PUT URL.'
                Check ($Body.location -eq $script:Location) 'PUT omitted location.'
                if ($script:DirectMode) { Check ($Body.properties.runImmediately -eq $true) 'Immediate PUT omitted runImmediately.' }
                else { Check (-not $Body.properties.ContainsKey('runImmediately')) 'Traditional PUT was marked immediate.' }
                return @{ StatusCode = 201; Headers = @{
                    'Azure-AsyncOperation' = 'https://management.azure.com/locations/test/operations/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee?api-version=2024-07-01'
                    'Location' = 'https://management.azure.com/other/operations/wrong-operation'
                }; Body = @{} }
            }
            if ($Uri -like '*operations*') {
                Check ($Uri -like '*operations/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee?*') 'The wrong operation URL was polled.'
                $script:DirectPolls++
                return @{ StatusCode = 200; Headers = @{}; Body = @{
                    status = if ($script:DirectPolls -eq 1) { 'InProgress' } else { 'Succeeded' }
                } }
            }
            Check ($Uri -like '*$expand=instanceView') 'Final GET omitted instance view.'
            Check ([bool]$RequireInstanceView) 'Final GET did not enable the HTTP-boundary expansion check.'
            return @{ StatusCode = 200; Headers = @{}; Body = (Get-TestResource $script:DirectMode) }
        }
        function Wait-Condition {
            param($Activity, $Probe, $TimeoutSeconds)
            if ($Activity -like '*CRP operation*') { Check (-not (& $Probe)) 'In-progress operation was counted as complete.' }
            Check ([bool](& $Probe)) 'A completed operation or instance view was not detected.'
        }
        foreach ($mode in @($false, $true)) {
            $script:DirectMode = $mode
            $script:DirectPolls = 0
            $captured = & { $script:DirectResult = Invoke-RunCommand 'case' @{ source = @{ script = 'echo ok' } } $mode } 6>&1 | Out-String
            Check ([regex]::Matches($captured, 'CRP operationId: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee').Count -eq 1) 'OperationId was not traced exactly once.'
            Assert-Execution $script:DirectResult
            Check ($script:DirectResult.OperationId -eq 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee') 'Polling operationId was not returned.'
        }
    }
    Test-Local 'only immediate results get a 30-second deadline after CRP completion' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            if ($Method -eq 'PUT') {
                $script:OriginalResultDeadline = $script:RunCommandDeadlineUtc
                if ($script:ResultSynchronous) { return @{ StatusCode = 200; Headers = @{}; Body = @{} } }
                return @{ StatusCode = 202; Headers = @{ 'Azure-AsyncOperation' = '/operations/result-deadline' }; Body = @{} }
            }
            if ($Uri -like '*operations*') {
                Check ($script:RunCommandDeadlineUtc -eq $script:OriginalResultDeadline) 'CRP polling deadline was shortened.'
                return @{ StatusCode = 200; Headers = @{}; Body = @{ status = 'Succeeded' } }
            }
            $remaining = ($script:RunCommandDeadlineUtc - [DateTime]::UtcNow).TotalSeconds
            if ($script:ResultImmediate -and $script:ResultBudget -gt 30) {
                Check ($remaining -gt 28 -and $remaining -le 30) 'Immediate result GET is not capped at 30 seconds.'
            }
            else {
                Check ($script:RunCommandDeadlineUtc -eq $script:OriginalResultDeadline) 'Original total deadline was changed or extended.'
            }
            @{ StatusCode = 200; Headers = @{}; Body = (Get-TestResource $script:ResultImmediate) }
        }
        function Wait-Condition {
            param($Activity, $Probe, $TimeoutSeconds)
            $expectedTimeout = if ($Activity -like '*CRP operation*' -or -not $script:ResultImmediate) {
                $script:ResultBudget
            } else { 30 }
            Check ($TimeoutSeconds -eq $expectedTimeout) 'Incorrect phase-specific polling timeout.'
            Check ([bool](& $Probe)) 'Completed operation or result was not detected.'
        }
        foreach ($mode in @($false, $true)) {
            foreach ($synchronous in @($false, $true)) {
                foreach ($budget in @(600, 1)) {
                    $script:ResultImmediate = $mode
                    $script:ResultSynchronous = $synchronous
                    $script:ResultBudget = $budget
                    $result = Invoke-RunCommand 'result-deadline' @{} $mode -CompletionTimeoutSeconds $budget
                    Assert-Execution $result
                    Check ($null -eq $script:RunCommandDeadlineUtc) 'Result deadline leaked after completion.'
                }
            }
        }
    }
    Test-Local 'instance-view polls log only allowed fields and retain the last values on every timeout path' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            if ($Method -eq 'PUT') { return @{ StatusCode = 200; Headers = @{}; Body = @{} } }
            Check ($Uri -like '*?api-version=2024-07-01&$expand=instanceView') 'GET API version or expansion changed.'
            $script:ViewPolls++
            if ($script:ViewPolls -eq 1) {
                return @{ StatusCode = 200; Headers = @{}; Body = @{ properties = @{
                    provisioningState = 'Succeeded'; runImmediately = $false
                } } }
            }
            if ($script:ViewPolls -eq 2) {
                return @{ StatusCode = 200; Headers = @{}; Body = @{ properties = @{
                    provisioningState = 'Succeeded'; runImmediately = $true
                    instanceView = @{ executionState = 'Pending'; output = 'PRIVATE-OUTPUT'; error = 'PRIVATE-ERROR' }
                } } }
            }
            throw [TimeoutException]::new('Deadline exceeded while reading instance view')
        }
        function Wait-Condition {
            param($Activity, $Probe, $TimeoutSeconds)
            Check (-not (& $Probe)) 'Missing instance view was counted as terminal.'
            Check (-not (& $Probe)) 'Instance view without an exit code was counted as complete.'
            if ($script:ViewTimeoutMode -eq 'request') { $null = & $Probe }
            throw [TimeoutException]::new('Timed out waiting for instance-view exit code')
        }
        foreach ($mode in @('request', 'condition')) {
            $script:ViewTimeoutMode = $mode
            $script:ViewPolls = 0
            $script:ViewTimeout = $null
            $captured = & {
                try { Invoke-RunCommand 'view-debug' @{} $true }
                catch [TimeoutException] { $script:ViewTimeout = $_.Exception }
            } 6>&1 | Out-String
            Check ($null -ne $script:ViewTimeout) 'Timeout type was not preserved.'
            $lines = @($captured -split '\r?\n' | Where-Object { $_ -match 'instance-view GET: ' })
            Check ($lines.Count -eq 2) 'Each completed GET must produce exactly one summary.'
            $first = ($lines[0] -split 'instance-view GET: ', 2)[1] | ConvertFrom-Json -AsHashtable
            $lastJson = ($lines[1] -split 'instance-view GET: ', 2)[1]
            $last = $lastJson | ConvertFrom-Json -AsHashtable
            $keys = @('api-version', 'httpStatus', 'provisioningState', 'runImmediately',
                'instanceView.executionState', 'instanceView.exitCode', 'outputPresent', 'errorPresent',
                'expectedOutputMatched', 'expectedErrorMatched', 'unmetConditions')
            Check ((($last.Keys | Sort-Object) -join ',') -eq (($keys | Sort-Object) -join ',')) 'Unexpected diagnostic fields.'
            Check ($first.runImmediately -eq $false -and $null -eq $first['instanceView.executionState']) 'Missing view or false value was not preserved.'
            Check ($last['api-version'] -eq '2024-07-01' -and $last.httpStatus -eq 200 -and
                $last.provisioningState -eq 'Succeeded' -and $last.runImmediately -eq $true -and
                $last['instanceView.executionState'] -eq 'Pending') 'Incorrect poll values.'
            Check ($last.outputPresent -and $last.errorPresent -and
                $last.expectedOutputMatched -and $last.expectedErrorMatched) 'Presence or optional-marker diagnostics are incorrect.'
            Check ($last.unmetConditions -contains 'Exit code missing') 'Timeout did not identify the missing exit code.'
            Check ($script:ViewTimeout.Message.Contains($lastJson)) 'Timeout omitted the last completed GET values.'
            Check ($null -ne $script:ViewTimeout.InnerException) 'Original timeout was not retained.'
            Check (($captured + $script:ViewTimeout.Message) -notmatch 'PRIVATE-OUTPUT|PRIVATE-ERROR') 'Command output leaked into diagnostics.'
            Check ($null -eq $script:RunCommandDeadlineUtc) 'Timeout deadline was not cleared.'
        }
        $script:PendingRunCommand = $null
    }
    Test-Local 'instance-view GET errors log status without dumping the response body' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            if ($Method -eq 'PUT') { return @{ StatusCode = 200; Headers = @{}; Body = @{} } }
            @{ StatusCode = 500; Headers = @{}; Body = @{ error = @{ message = 'PRIVATE-ERROR' } } }
        }
        $captured = & {
            Expect-Error { Invoke-RunCommand 'view-error' @{} $true } 'failed:.*"httpStatus":500'
        } 6>&1 | Out-String
        Check ($captured -match '"httpStatus":500') 'Failed GET status was not logged.'
        Check ($captured -notmatch 'PRIVATE-ERROR') 'Failed GET dumped its response body.'
        $script:PendingRunCommand = $null
    }
    Test-Local 'instance-view timeout before the first response reports null response fields' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            if ($Method -eq 'PUT') { return @{ StatusCode = 200; Headers = @{}; Body = @{} } }
            throw [TimeoutException]::new('Deadline exceeded before sending GET')
        }
        Expect-Error { Invoke-RunCommand 'view-no-response' @{} $true } 'Deadline exceeded.*Last RunCommand.*"api-version":"2024-07-01","httpStatus":null.*"instanceView.executionState":null'
        $script:PendingRunCommand = $null
    }
    Test-Local 'exit code completes polling even with Pending or missing execution state' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            if ($Method -eq 'PUT') { return @{ StatusCode = 200; Headers = @{}; Body = @{} } }
            $script:ExitCodePolls++
            $resource = Get-TestResource
            if ($script:ExitCodePolls -eq 1) {
                $resource.properties.instanceView.Remove('exitCode')
            }
            elseif ($script:ExitCodePolls -eq 2) {
                $resource.properties.instanceView.exitCode = $null
            }
            else {
                $resource.properties.instanceView.exitCode = $script:ExpectedExitCode
                $resource.properties.instanceView.executionState = 'Pending'
                if ($script:MissingExecutionState) { $resource.properties.instanceView.Remove('executionState') }
            }
            @{ StatusCode = 200; Headers = @{}; Body = $resource }
        }
        function Wait-Condition {
            param($Activity, $Probe, $TimeoutSeconds)
            Check (-not (& $Probe)) 'Terminal executionState without an exit code completed polling.'
            Check (-not (& $Probe)) 'Null exit code completed polling.'
            Check ([bool](& $Probe)) 'An exit code with Pending or missing executionState did not complete polling.'
        }
        foreach ($missing in @($false, $true)) {
            foreach ($code in @(0, 1, -1)) {
                $script:MissingExecutionState = $missing
                $script:ExpectedExitCode = $code
                $script:ExitCodePolls = 0
                $result = Invoke-RunCommand 'exit-code' @{} $true -ExpectedExitCode $code
                Assert-Execution $result $code
                Check ($script:ExitCodePolls -eq 3) 'Incorrect number of instance-view polls.'
            }
        }
    }
    Test-Local 'polling waits for expected output, error, exit code, and provisioning convergence' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            if ($Method -eq 'PUT') {
                return @{ StatusCode = 202; Headers = @{ 'Azure-AsyncOperation' = '/operations/result-convergence' }; Body = @{} }
            }
            if ($Uri -like '*operations*') {
                return @{ StatusCode = 200; Headers = @{}; Body = @{ status = $script:ConvergenceOperation } }
            }
            $script:ConvergencePolls++
            $view = @{ executionState = 'Pending'; exitCode = 0 }
            $properties = @{ provisioningState = 'Succeeded'; runImmediately = $true; instanceView = $view }
            if ($script:ConvergencePolls -ge 2) { $view.output = '' }
            if ($script:ConvergencePolls -ge 3) { $view.output = "expected`r`nstdout" }
            if ($script:ConvergencePolls -ge 4) { $view.error = '' }
            if ($script:ConvergencePolls -ge 5) {
                $view.error = 'expected stderr'
                $view.exitCode = if ($script:ConvergenceExitCode -eq 0) { 1 } else { 0 }
            }
            if ($script:ConvergencePolls -ge 6) { $view.exitCode = $script:ConvergenceExitCode }
            if ($script:ConvergencePolls -eq 6) { $properties.provisioningState = 'Updating' }
            elseif ($script:ConvergencePolls -ge 7) { $properties.provisioningState = $script:ConvergenceOperation }
            @{ StatusCode = 200; Headers = @{}; Body = @{ properties = $properties } }
        }
        function Wait-Condition {
            param($Activity, $Probe, $TimeoutSeconds)
            if ($Activity -like '*CRP operation*') {
                Check ([bool](& $Probe)) 'Operation did not finish.'
                return
            }
            foreach ($poll in 1..6) {
                Check (-not (& $Probe)) "Incomplete result on poll $poll was counted as complete."
            }
            Check ([bool](& $Probe)) 'Complete Pending result was not detected.'
        }
        foreach ($code in @(0, 1, -1)) {
            $script:ConvergenceExitCode = $code
            $script:ConvergenceOperation = if ($code -eq 1) { 'Failed' } else { 'Succeeded' }
            $script:ConvergencePolls = 0
            $expectations = @{ ExpectedOutput = "expected`nstdout"; ExpectedError = 'expected stderr' }
            if ($code -eq -1) { $expectations.ExpectNonzeroExitCode = $true }
            else { $expectations.ExpectedExitCode = $code }
            $captured = & { $script:ConvergenceResult = Invoke-RunCommand 'convergence' @{} $true @expectations } 6>&1 | Out-String
            Assert-Execution $script:ConvergenceResult $code $script:ConvergenceOperation
            Check ($script:ConvergencePolls -eq 7) 'Provisioning convergence was not awaited.'
            Check ($captured -notmatch 'expected stdout|expected stderr') 'Expected output or error leaked into the trace.'
            $summaries = @($captured -split '\r?\n' | Where-Object { $_ -match 'instance-view GET: ' } |
                ForEach-Object { ($_ -split 'instance-view GET: ', 2)[1] | ConvertFrom-Json -AsHashtable })
            Check ($summaries.Count -eq 7) 'Missing poll diagnostics.'
            Check ($summaries[0]['instanceView.exitCode'] -eq 0 -and
                -not $summaries[0].outputPresent -and -not $summaries[0].errorPresent) 'Initial default-zero result was not diagnosed.'
            Check ($summaries[1].outputPresent -and -not $summaries[1].expectedOutputMatched) 'Empty output should be present but unmatched.'
            Check ($summaries[2].expectedOutputMatched -and -not $summaries[2].expectedErrorMatched -and
                $summaries[2].unmetConditions -contains 'Expected error not matched') 'Missing stderr did not identify its blocking condition.'
            Check ($summaries[3].errorPresent -and -not $summaries[3].expectedErrorMatched) 'Empty stderr should be present but unmatched.'
            Check ($summaries[4].unmetConditions.Count -eq 1 -and
                $summaries[4].unmetConditions[0] -match 'exit code|Exit code') 'Wrong exit code did not identify its blocking condition.'
            Check ($summaries[5].unmetConditions -contains 'Terminal provisioning state required') 'Updating provisioning state was not diagnosed.'
            Check ($summaries[6].unmetConditions.Count -eq 0 -and
                $summaries[6]['instanceView.executionState'] -eq 'Pending') 'Pending executionState incorrectly blocks completion.'
        }
    }
    Test-Local 'success with default zero exit code waits for output even after provisioning succeeds' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            if ($Method -eq 'PUT') { return @{ StatusCode = 200; Headers = @{}; Body = @{} } }
            $script:OutputPolls++
            $resource = Get-TestResource
            $view = $resource.properties.instanceView
            $view.executionState = 'Pending'
            if ($script:OutputPolls -eq 1) { $view.Remove('output') }
            elseif ($script:OutputPolls -eq 2) { $view.output = '' }
            elseif ($script:OutputPolls -eq 3) { $view.output = 'partial' }
            else { $view.output = 'expected output' }
            @{ StatusCode = 200; Headers = @{}; Body = $resource }
        }
        function Wait-Condition {
            param($Activity, $Probe, $TimeoutSeconds)
            foreach ($poll in 1..3) { Check (-not (& $Probe)) "Premature completion on poll $poll." }
            Check ([bool](& $Probe)) 'Expected output did not complete polling.'
        }
        $script:OutputPolls = 0
        $result = Invoke-RunCommand 'output-convergence' @{} $true -ExpectedOutput 'expected output'
        Assert-Execution $result
        Assert-Contains $result.View.output 'expected output' 'Converged output'
    }
    Test-Local 'immediate execution cannot silently fall back to traditional' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            return @{ StatusCode = 200; Headers = @{}; Body = (Get-TestResource $false) }
        }
        Expect-Error { Invoke-RunCommand 'case' @{ source = @{ script = 'echo ok' } } $true } 'did not confirm runImmediately'
        $script:PendingRunCommand = $null
    }
    Test-Local 'Location operation-results polling and synchronous PUT responses are supported' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            if ($Method -eq 'PUT') {
                if ($script:UseSynchronousPut) { return @{ StatusCode = 200; Headers = @{}; Body = (Get-TestResource $false) } }
                return @{ StatusCode = 202; Headers = @{ Location = '/locations/test/operationResults/location-operation?api-version=2024-07-01' }; Body = @{} }
            }
            if ($Uri -like '*operationResults*') {
                $script:LocationReads++
                return @{ StatusCode = if ($script:LocationReads -eq 1) { 202 } else { 204 }; Headers = @{}; Body = @{} }
            }
            return @{ StatusCode = 200; Headers = @{}; Body = (Get-TestResource $false) }
        }
        function Wait-Condition {
            param($Activity, $Probe, $TimeoutSeconds)
            if ($Activity -like '*CRP operation*') { Check (-not (& $Probe)) 'HTTP 202 was considered complete.' }
            Check ([bool](& $Probe)) 'Completion was not detected.'
        }
        foreach ($synchronous in @($false, $true)) {
            $script:UseSynchronousPut = $synchronous
            $script:LocationReads = 0
            $result = Invoke-RunCommand 'case' @{ source = @{ script = 'echo ok' } } $false
            Assert-Execution $result
            if ($synchronous) { Check ($result.OperationId -eq '') 'A synchronous response invented an operationId.' }
            else { Check ($result.OperationId -eq 'location-operation') 'Location operationId was lost.' }
        }
    }
    Test-Local 'accepted PUT without polling URL fails and untrusted operation URLs are rejected' {
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            return @{ StatusCode = 202; Headers = @{}; Body = @{} }
        }
        Expect-Error { Invoke-RunCommand 'case' @{} $true } 'without returning an operation polling URL'
        $script:PendingRunCommand = $null
        foreach ($uri in @('https://other.example/operations/id', 'http://management.azure.com/operations/id',
            'https://user@management.azure.com/operations/id')) {
            Expect-Error { Resolve-ArmUri $uri } 'outside the configured ARM endpoint'
        }
    }
    Test-Local 'Retry-After is respected without extending the total deadline' {
        $oldDeadline = $script:RunCommandDeadlineUtc
        try {
            $script:RunCommandDeadlineUtc = [DateTime]::UtcNow.AddSeconds(0.5)
            $timer = [Diagnostics.Stopwatch]::StartNew()
            Wait-RunCommandRetryAfter @{ 'Retry-After' = '30' }
            Check ([DateTime]::UtcNow -ge $script:RunCommandDeadlineUtc -and $timer.Elapsed.TotalSeconds -lt 2) 'Retry-After ignored the remaining deadline.'
            Expect-Error { Wait-RunCommandRetryAfter @{ 'Retry-After' = '-1' } } 'negative Retry-After'
            $script:RunCommandDeadlineUtc = [DateTime]::UtcNow.AddSeconds(3)
            Wait-RunCommandRetryAfter @{ 'Retry-After' = [DateTimeOffset]::UtcNow.AddSeconds(-10).ToString('R', [Globalization.CultureInfo]::InvariantCulture) }
        }
        finally { $script:RunCommandDeadlineUtc = $oldDeadline }
    }
    Test-Local 'direct HTTP requests are canceled at the remaining total deadline' {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
        $listener.Start()
        $port = $listener.LocalEndpoint.Port
        $oldEndpoint = $script:ArmEndpoint
        $oldDeadline = $script:RunCommandDeadlineUtc
        $server = $null
        try {
            $script:ArmEndpoint = [uri]"http://127.0.0.1:$port/"
            $server = Start-ThreadJob -ArgumentList $listener -ScriptBlock {
                param($socket)
                $client = $socket.AcceptTcpClient()
                try { Start-Sleep -Seconds 30 }
                finally { $client.Dispose() }
            }
            $script:RunCommandDeadlineUtc = [DateTime]::UtcNow.AddSeconds(1)
            $timer = [Diagnostics.Stopwatch]::StartNew()
            Expect-Error {
                Invoke-ArmRequest GET '/operations/hung' 'fixture-token' 'waiting for a simulated CRP response' -TimeoutSeconds 420
            } 'Timed out'
            Check ($timer.Elapsed.TotalSeconds -lt 4) 'HTTP cancellation did not honor the remaining total budget.'
        }
        finally {
            $script:ArmEndpoint = $oldEndpoint
            $script:RunCommandDeadlineUtc = $oldDeadline
            $listener.Stop()
            if ($server) {
                if ($server.State -in @('Running', 'NotStarted')) { Stop-Job -Job $server }
                Remove-Job -Job $server -Force
            }
        }
    }
    Test-Local 'instance-view HTTP boundary rejects missing or misplaced expansion before sending' {
        foreach ($query in @('?api-version=2024-07-01',
            '?api-version=2024-07-01&expand=instanceView',
            '?api-version=2024-07-01&$expand=other',
            '?api-version=2024-07-01#$expand=instanceView',
            '?api-version=2024-07-01&other=$expand=instanceView')) {
            Expect-Error {
                Invoke-ArmRequest GET "/subscriptions/test/runCommands/test$query" 'fixture-token' 'testing expansion guard' -RequireInstanceView
            } 'Refusing to send an instance-view request'
        }
        Expect-Error {
            Invoke-ArmRequest PUT '/subscriptions/test/runCommands/test?api-version=2024-07-01&$expand=instanceView' `
                'fixture-token' 'testing method guard' -RequireInstanceView
        } 'Refusing to send an instance-view request'
    }
    Test-Local 'direct HTTP PUT polls its actual operationId and verifies expected provisioning failure' {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
        $listener.Start()
        $port = $listener.LocalEndpoint.Port
        $oldEndpoint = $script:ArmEndpoint
        $server = $null
        function Get-RunCommandArmToken { 'fixture-token-not-a-real-credential' }
        try {
            $script:ArmEndpoint = [uri]"http://127.0.0.1:$port/"
            $server = Start-ThreadJob -ArgumentList $listener, $port -ScriptBlock {
                param($socket, $port)
                for ($index = 0; $index -lt 3; $index++) {
                    $client = $socket.AcceptTcpClient()
                    try {
                        $stream = $client.GetStream()
                        $reader = [IO.StreamReader]::new($stream, [Text.Encoding]::UTF8, $false, 1024, $true)
                        try {
                            $line = $reader.ReadLine()
                            $length = 0
                            $authorization = ''
                            $clientRequestId = ''
                            while ($header = $reader.ReadLine()) {
                                if ($header -match '^Content-Length: (\d+)') { $length = [int]$Matches[1] }
                                if ($header -like 'Authorization:*') { $authorization = $header }
                                if ($header -match '^x-ms-client-request-id: (.+)$') { $clientRequestId = $Matches[1] }
                            }
                            if ($authorization -ne 'Authorization: Bearer fixture-token-not-a-real-credential') { throw 'Authorization header was lost.' }
                            $text = ''
                            if ($length) {
                                $chars = [char[]]::new($length)
                                $read = 0
                                while ($read -lt $length) {
                                    $count = $reader.Read($chars, $read, $length - $read)
                                    if ($count -eq 0) { throw 'Request body ended early.' }
                                    $read += $count
                                }
                                $text = [string]::new($chars)
                            }
                            switch ($index) {
                                0 {
                                    if ($line -notlike 'PUT */runCommands/real-http?api-version=2024-07-01 HTTP/*') { throw "Wrong PUT target: $line" }
                                    $request = ConvertFrom-Json $text -AsHashtable
                                    if ($request.properties.runImmediately -ne $true -or $request.properties.treatFailureAsDeploymentFailure -ne $true) {
                                        throw 'Direct request properties were lost.'
                                    }
                                    $status = '201 Created'
                                    $extra = "Azure-AsyncOperation: http://127.0.0.1:$port/operations/real-operation-id?api-version=2024-07-01`r`n"
                                    $body = '{}'
                                }
                                1 {
                                    if ($line -notlike 'GET /operations/real-operation-id?* HTTP/*') { throw "Wrong operation target: $line" }
                                    $status = '200 OK'; $extra = ''; $body = '{"status":"Failed","error":{"code":"VmExtensionError","message":"expected script failure"}}'
                                }
                                2 {
                                    if ($line -notlike '*$expand=instanceView HTTP/*') { throw "Missing instance view: $line" }
                                    $parsedId = [guid]::Empty
                                    if (-not [guid]::TryParse($clientRequestId, [ref]$parsedId)) { throw 'Instance-view GET did not send a valid client request ID.' }
                                    $status = '200 OK'
                                    $extra = "x-ms-request-id: fixture-resource-request`r`nx-ms-correlation-request-id: fixture-resource-correlation`r`n"
                                    $body = '{"properties":{"runImmediately":true,"provisioningState":"Failed","instanceView":{"executionState":"Failed","exitCode":1,"output":"negative test","error":"expected error"}}}'
                                }
                            }
                            $bytes = [Text.Encoding]::UTF8.GetBytes($body)
                            $headers = [Text.Encoding]::ASCII.GetBytes("HTTP/1.1 $status`r`n${extra}Content-Type: application/json`r`nContent-Length: $($bytes.Length)`r`nConnection: close`r`n`r`n")
                            $stream.Write($headers, 0, $headers.Length)
                            $stream.Write($bytes, 0, $bytes.Length)
                            $stream.Flush()
                        }
                        finally { $reader.Dispose() }
                    }
                    finally { $client.Dispose() }
                }
            }
            $trace = & {
                $script:HttpTraceResult = Invoke-RunCommand 'real-http' @{ source = @{ script = 'exit 1' }; treatFailureAsDeploymentFailure = $true } $true -ExpectedExitCode 1
            } 6>&1 | Out-String
            $result = $script:HttpTraceResult
            $requestLine = @($trace -split '\r?\n' | Where-Object { $_ -match 'ARM instance-view request: ' })
            $responseLine = @($trace -split '\r?\n' | Where-Object { $_ -match 'ARM instance-view response: ' })
            Check ($requestLine.Count -eq 1 -and $responseLine.Count -eq 1) 'Instance-view request/response traces were not emitted exactly once.'
            $requestTrace = ($requestLine[0] -split 'ARM instance-view request: ', 2)[1] | ConvertFrom-Json -AsHashtable
            $responseTrace = ($responseLine[0] -split 'ARM instance-view response: ', 2)[1] | ConvertFrom-Json -AsHashtable
            Check ($requestTrace.uri -eq "http://127.0.0.1:$port$($script:VmId)/runCommands/real-http?api-version=2024-07-01&`$expand=instanceView") 'Trace did not capture the final request URI.'
            Check ($requestTrace.method -eq 'GET' -and $requestTrace.httpVersion -eq '1.1') 'Trace did not capture HTTP request properties.'
            Check ($requestTrace['x-ms-client-request-id'] -eq $responseTrace['x-ms-client-request-id']) 'Client request IDs did not correlate.'
            Check ($responseTrace.httpStatus -eq 200 -and $responseTrace['x-ms-request-id'] -eq 'fixture-resource-request' -and
                $responseTrace['x-ms-correlation-request-id'] -eq 'fixture-resource-correlation') 'Response trace omitted server correlation IDs.'
            $rawBody = '{"properties":{"runImmediately":true,"provisioningState":"Failed","instanceView":{"executionState":"Failed","exitCode":1,"output":"negative test","error":"expected error"}}}'
            Check ($trace.Contains("ARM instance-view raw response body (x-ms-client-request-id=$($requestTrace['x-ms-client-request-id'])):")) 'Raw response was not correlated with its request.'
            Check ($trace.Contains($rawBody)) 'Raw instance-view response body was not logged verbatim.'
            Check ($trace -notmatch 'fixture-token-not-a-real-credential|Authorization:|expected script failure') 'Credentials or a non-instance-view response leaked into HTTP tracing.'
            Assert-Execution $result 1 'Failed'
            Check ($result.OperationId -eq 'real-operation-id') 'The real polling operationId was not captured.'
            Check ($result.OperationError.code -eq 'VmExtensionError') 'CRP operation error details were discarded.'
            $null = Wait-Job -Job $server -Timeout 5
            $null = Receive-Job -Job $server -ErrorAction Stop
            Check ($server.State -eq 'Completed') 'The HTTP fixture did not finish.'
        }
        finally {
            $script:ArmEndpoint = $oldEndpoint
            $script:PendingRunCommand = $null
            $listener.Stop()
            if ($server) {
                if ($server.State -in @('Running', 'NotStarted')) { Stop-Job -Job $server }
                Remove-Job -Job $server -Force
            }
        }
    }
    Test-Local 'raw instance-view response is logged before malformed JSON parsing fails' {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
        $listener.Start()
        $port = $listener.LocalEndpoint.Port
        $oldEndpoint = $script:ArmEndpoint
        $server = $null
        $rawBody = "{`n  `"properties`": INVALID-JSON`n}"
        try {
            $script:ArmEndpoint = [uri]"http://127.0.0.1:$port/"
            $server = Start-ThreadJob -ArgumentList $listener, $rawBody -ScriptBlock {
                param($socket, $body)
                $client = $socket.AcceptTcpClient()
                try {
                    $stream = $client.GetStream()
                    $reader = [IO.StreamReader]::new($stream, [Text.Encoding]::UTF8, $false, 1024, $true)
                    try {
                        while ($reader.ReadLine()) {}
                        $bytes = [Text.Encoding]::UTF8.GetBytes($body)
                        $headers = [Text.Encoding]::ASCII.GetBytes("HTTP/1.1 200 OK`r`nContent-Type: application/json`r`nContent-Length: $($bytes.Length)`r`nConnection: close`r`n`r`n")
                        $stream.Write($headers, 0, $headers.Length)
                        $stream.Write($bytes, 0, $bytes.Length)
                        $stream.Flush()
                    }
                    finally { $reader.Dispose() }
                }
                finally { $client.Dispose() }
            }
            $script:RawResponseParseError = $null
            $trace = & {
                try {
                    $null = Invoke-ArmRequest GET '/runCommands/raw?api-version=2024-07-01&$expand=instanceView' `
                        'fixture-token-not-a-real-credential' 'testing raw response tracing' -RequireInstanceView
                }
                catch { $script:RawResponseParseError = $_ }
            } 6>&1 | Out-String
            Check ($null -ne $script:RawResponseParseError -and
                $script:RawResponseParseError.FullyQualifiedErrorId -match 'ConvertFromJson') 'Malformed JSON did not retain the parse failure.'
            Check (($trace -replace "`r", '').Contains($rawBody)) 'Malformed response body or its original whitespace was lost before logging.'
            Check ($trace -match 'ARM instance-view raw response body \(x-ms-client-request-id=') 'Malformed response was not labeled and correlated.'
            Check ($trace -notmatch 'fixture-token-not-a-real-credential|Authorization:') 'Raw response tracing exposed request credentials.'
            $null = Wait-Job -Job $server -Timeout 5
            $null = Receive-Job -Job $server -ErrorAction Stop
            Check ($server.State -eq 'Completed') 'Malformed JSON HTTP fixture did not finish.'
        }
        finally {
            $script:ArmEndpoint = $oldEndpoint
            $listener.Stop()
            if ($server) {
                if ($server.State -in @('Running', 'NotStarted')) { Stop-Job -Job $server }
                Remove-Job -Job $server -Force
            }
        }
    }
    Test-Local 'a missing deployment correlationId is explicitly reported' {
        $captured = & { Write-DeploymentCorrelationId 'fixture-deployment' '' } 3>&1 | Out-String
        Check ($captured -match 'fixture-deployment did not return properties.correlationId') 'Missing deployment metadata was not reported.'
    }
    Test-Local 'a nonterminal RunCommand hits one total deadline and aborts after diagnostics and cleanup' {
        $script:Results.Clear()
        $script:TimeoutActions = [Collections.Generic.List[string]]::new()
        function Get-RunCommandArmToken { 'fixture-token' }
        function Invoke-ArmRequest {
            param($Method, $Uri, $Token, $Activity, $Body)
            if ($Method -eq 'PUT') {
                return @{ StatusCode = 202; Headers = @{ 'Azure-AsyncOperation' = '/operations/timeout-fixture' }; Body = @{} }
            }
            return @{ StatusCode = 200; Headers = @{}; Body = @{ status = 'InProgress' } }
        }
        function Invoke-Az {
            param($Arguments, $Activity, $Output, $TimeoutSeconds)
            if ($Arguments[0] -eq 'vm') {
                Check ($TimeoutSeconds -eq 60) 'Incomplete-command deletion was not bounded.'
                $script:TimeoutActions.Add('delete')
                return
            }
            throw 'Unexpected request in timeout fixture.'
        }
        function Save-RunCommandLogs { param($TestName) $script:TimeoutActions.Add('logs') }
        $timer = [Diagnostics.Stopwatch]::StartNew()
        Expect-Error {
            Invoke-TestCase 'immediate-hung' {
                Invoke-RunCommand 'immediate-hung' @{ source = @{ script = 'echo ok' } } $true -CompletionTimeoutSeconds 1
            }
        } 'Timed out'
        Check ($timer.Elapsed.TotalSeconds -lt 5) 'The total RunCommand deadline was not enforced.'
        Check (($script:TimeoutActions -join ',') -eq 'logs,delete') 'Diagnostics and incomplete-command cleanup ran in the wrong order.'
        Check ($script:Results.Count -eq 1 -and -not $script:Results[0].Passed) 'Timeout did not remain a recorded test failure.'
        Check ($null -eq $script:RunCommandDeadlineUtc -and $null -eq $script:PendingRunCommand) 'Timeout state leaked into later operations.'
    }
    Test-Local 'negative script execution is distinct from deployment failure' {
        $result = @{
            OperationState = 'Succeeded'; ProvisioningState = 'Succeeded'
            View = @{ executionState = 'Pending'; exitCode = 1 }
        }
        Assert-Execution $result 1
        Expect-Error { Assert-Execution $result } 'exit code'
        $result.OperationState = 'Failed'
        $result.ProvisioningState = 'Failed'
        Assert-Execution $result 1 'Failed'
        $result.View.exitCode = $null
        Expect-Error { Assert-Execution $result 1 'Failed' } 'no exit code'
    }
    Test-Local 'failed tests collect diagnostics and stay failed' {
        $script:Results.Clear()
        $script:CollectedTest = ''
        function Save-RunCommandLogs { param($TestName) $script:CollectedTest = $TestName }
        Invoke-TestCase 'expected-failure' { throw 'simulated RunCommand failure' }
        Check ($script:CollectedTest -eq 'expected-failure') 'Failure did not trigger log collection.'
        Check (-not $script:Results[0].Passed) 'A failed test was reported as passing.'
        Check ($script:Results[0].Error -match 'simulated RunCommand failure') 'Original failure was lost.'
    }
    Test-Local 'a log-collection failure preserves the original test failure' {
        $script:Results.Clear()
        function Save-RunCommandLogs { param($TestName) throw 'collection unavailable' }
        Invoke-TestCase 'expected-failure' { throw 'original failure' }
        Check ($script:Results[0].Error -eq 'original failure') 'Collection masked the original failure.'
        Check ($script:Results[0].LogCollectionError -eq 'collection unavailable') 'Collection failure was hidden.'
    }
    Test-Local 'resource-group deletion waits for all resources to disappear' {
        $script:DeletionRequested = $false
        function Invoke-Az {
            param($Arguments, $Activity, $Output, $TimeoutSeconds)
            if ($Arguments[1] -eq 'delete') {
                Check ($Arguments -contains '--yes' -and $Arguments -contains '--no-wait') 'Deletion is not noninteractive.'
                $script:DeletionRequested = $true
                return
            }
            if ($Arguments[1] -eq 'exists') { return 'false' }
            throw 'Unexpected deletion call.'
        }
        Remove-TestResourceGroup
        Check $script:DeletionRequested 'Resource-group deletion was not requested.'
    }
    Test-Local 'both complete suites exercise all expected assertions' {
        $script:Results.Clear()
        $script:SourceScriptUri = 'https://example.invalid/source'
        $script:ErrorScriptUri = 'https://example.invalid/error'
        $script:RunAsPassword = 'offline-password'
        $script:SuiteCalls = [Collections.Generic.List[bool]]::new()
        $script:SuiteVerifications = [Collections.Generic.List[bool]]::new()
        $script:FailedCommandPendingDeletion = $null
        $script:DeletedFailedCommands = [Collections.Generic.List[string]]::new()
        $script:ForceNegativeAssertionFailure = $false
        $script:ForceNegativeCleanupFailure = $false
        $script:CollectedNegativeTest = ''
        $hashes = @{ 'run-command-handler' = 'handler-hash'; 'immediate-run-command-handler' = 'service-hash' }
        function Send-Blob {
            param($Path, $Name, $Type)
            Check ($script:Results[-1].Name -like '*-buddy-build-preflight' -and $script:Results[-1].Passed) `
                'Suite setup ran before buddy-build verification succeeded.'
        }
        function New-BlobSas { param($Name, $Permissions) "https://example.invalid/$Name" }
        function Test-BlobOutput { param($Name, $Expected) }
        function Save-RunCommandLogs { param($TestName) $script:CollectedNegativeTest = $TestName }
        function Invoke-CustomScript {
            param($Action, $Configuration)
            Check ($Action -eq 'verify') 'Unexpected guest action.'
            Check ($Configuration.hashes['run-command-handler'] -eq 'handler-hash' -and
                $Configuration.hashes['immediate-run-command-handler'] -eq 'service-hash') `
                'Guest verification did not receive both locally built binary hashes.'
            $script:SuiteVerifications.Add([bool]$Configuration.requireService)
        }
        function Invoke-Az {
            param($Arguments, $Activity, $Output)
            Check (($Arguments[0..2] -join ' ') -eq 'vm run-command delete') 'Unexpected suite Azure call.'
            Check ($Arguments -contains '--yes' -and $Arguments -notcontains '--no-wait') `
                'Negative-test deletion must be noninteractive and wait for completion.'
            $index = [array]::IndexOf($Arguments, '--name')
            Check ($Arguments[$index + 1] -eq $script:FailedCommandPendingDeletion) 'Wrong negative-test command deleted.'
            if ($script:ForceNegativeAssertionFailure) {
                Check ($script:CollectedNegativeTest -like '*-deployment-failure-true') 'Cleanup ran before failed-test logs were collected.'
            }
            if ($script:ForceNegativeCleanupFailure) { throw 'simulated negative-test cleanup failure' }
            $script:DeletedFailedCommands.Add($script:FailedCommandPendingDeletion)
            $script:FailedCommandPendingDeletion = $null
        }
        function Invoke-RunCommand {
            param($Name, $Properties, [bool]$Immediate, [int]$ExpectedExitCode = 0,
                [switch]$ExpectNonzeroExitCode, [string]$ExpectedOutput = '', [string]$ExpectedError = '')
            Check ($null -eq $script:FailedCommandPendingDeletion) 'A failed command was left behind before the next test.'
            $script:SuiteCalls.Add($Immediate)
            $view = @{ executionState = 'Pending'; exitCode = 0; output = ''; error = '' }
            $result = @{ OperationState = 'Succeeded'; ProvisioningState = 'Succeeded'; View = $view }
            if ($Name -like '*inline') { $view.output = 'HelloWorldCurrentUser:root' }
            elseif ($Name -like '*parameters') {
                Check ($Properties.protectedParameters[2].name -eq '') 'Unnamed protected parameter is missing.'
                Check ($Properties.parameters.Count -eq 2) 'Named public parameters are missing.'
                $view.output = "CurrentUser:root`nLinuxScript`nChipmunk:Ralph`nSquirrel:Iggy`nSource:NewYorkCity`nDestination:Seattle`nNamelessArgument:MilkyWay"
            }
            elseif ($Name -like '*error' -or $Name -like '*failure-*') {
                $view.exitCode = 1
                $view.output = 'linux error script'
                $view.error = 'there is an angry chipmunk'
                if ($Name -like '*failure-true') {
                    $script:FailedCommandPendingDeletion = $Name
                    $result.OperationState = 'Failed'
                    $result.ProvisioningState = 'Failed'
                    if ($script:ForceNegativeAssertionFailure) { $view.exitCode = 2 }
                }
            }
            elseif ($Name -like '*run-as-user') {
                Check ($Properties.runAsUser -eq 'rcvalidation') 'Wrong RunAs account.'
                $view.output = 'HelloWorldCurrentUser:rcvalidation'
            }
            elseif ($Name -like '*timeout') {
                Check ($Properties.timeoutInSeconds -eq 5) 'Timeout test duration changed.'
                $view.exitCode = -1
                $view.output = 'HelloWorld1'
            }
            elseif ($Name -notlike '*command-id') { throw "Unexpected test $Name." }
            if ($Name -like '*error' -or $Name -like '*failure-*') {
                Check ($ExpectedExitCode -eq 1 -and $ExpectedOutput -eq 'linux error script' -and
                    $ExpectedError -eq 'there is an angry chipmunk') 'Negative test did not pass its readiness expectations.'
            }
            elseif ($Name -like '*timeout') {
                Check ($ExpectNonzeroExitCode -and $ExpectedOutput -eq 'HelloWorld1') 'Timeout test did not pass its readiness expectations.'
            }
            elseif ($Name -notlike '*command-id') {
                Check ($ExpectedOutput -eq $view.output -and $ExpectedExitCode -eq 0) 'Output-producing test did not pass its readiness expectations.'
            }
            return $result
        }
        Invoke-TestSuite $false $hashes
        Invoke-TestSuite $true $hashes
        Check ($script:Results.Count -eq 20) 'The complete suites and preflight checks did not run.'
        Check (@($script:Results | Where-Object { -not $_.Passed }).Count -eq 0) 'A modeled synthetic case failed.'
        Check (@($script:SuiteCalls | Where-Object { $_ }).Count -eq 8) 'Immediate test submissions were not marked immediate.'
        Check (@($script:SuiteCalls | Where-Object { -not $_ }).Count -eq 8) 'Traditional test submissions were missing.'
        Check ($script:SuiteVerifications.Count -eq 4 -and
            @($script:SuiteVerifications | Where-Object { $_ }).Count -eq 2) 'Preflight and final checks did not verify both modes.'
        Check (($script:DeletedFailedCommands -join ',') -eq 'traditional-failure-true,immediate-failure-true') `
            'The intentional failed RunCommands were not both removed.'
        $script:Results.Clear()
        $script:ForceNegativeAssertionFailure = $true
        Invoke-TestSuite $false $hashes
        Check (@($script:Results | Where-Object { -not $_.Passed }).Count -eq 1) 'The negative-test assertion failure was hidden.'
        Check ($null -eq $script:FailedCommandPendingDeletion) 'Cleanup was skipped after a failed assertion.'
        $script:ForceNegativeAssertionFailure = $false
        $script:ForceNegativeCleanupFailure = $true
        $script:Results.Clear()
        Expect-Error { Invoke-TestSuite $true $hashes } 'negative-test cleanup failure'
        Check ($script:Results[-1].Name -eq 'immediate-deployment-failure-true') 'The suite continued after cleanup failed.'
    }
    Test-Local 'failed binary preflight collects logs and prevents either suite from starting' {
        function Send-Blob { throw 'Suite setup must not run after failed preflight.' }
        function Invoke-RunCommand { throw 'RunCommand must not run after failed preflight.' }
        function Invoke-CustomScript {
            param($Action, $Configuration)
            Check ($Action -eq 'verify') 'Preflight must verify, not reinstall the binaries.'
            Check ($Configuration.hashes.Count -eq 2) 'Preflight omitted expected local hashes.'
            throw 'installed binary checksum mismatch'
        }
        function Save-RunCommandLogs { param($TestName) $script:CollectedPreflight = $TestName }
        foreach ($immediate in @($false, $true)) {
            $script:Results.Clear()
            $script:CollectedPreflight = ''
            $hashes = @{ 'run-command-handler' = 'handler-hash'; 'immediate-run-command-handler' = 'service-hash' }
            Expect-Error { Invoke-TestSuite $immediate $hashes } 'preflight failed; refusing'
            Check ($script:Results.Count -eq 1 -and -not $script:Results[0].Passed) 'Failed preflight was not recorded.'
            Check ($script:Results[0].Error -match 'checksum mismatch') 'Preflight error was lost.'
            Check ($script:CollectedPreflight -eq $script:Results[0].Name) 'Failed preflight did not collect diagnostic logs.'
        }
    }
    Test-Local 'orchestration tears down on success/failure, honors retention, and protects unrelated groups' {
        function Enter-ValidationInstance { [IO.MemoryStream]::new() }
        function Build-RunCommandBinaries {
            param($RepositoryRoot)
            foreach ($name in @('run-command-handler', 'immediate-run-command-handler')) {
                Set-Content -LiteralPath (Join-Path $script:OutputDirectory $name) -Value 'offline-binary'
            }
        }
        function Send-Blob { param($Path, $Name, $Type) }
        function New-BlobSas { param($Name, $Permissions) "https://example.invalid/$Name" }
        function Invoke-CustomScript {
            param($Action, $Configuration)
            Check $script:LifecycleVmReady 'Guest setup ran before VM creation completed.'
            if ($Action -eq 'wait-service') {
                Check ($script:LifecycleGuestActions.Count -eq 0) 'Service readiness was not the first guest setup action.'
            }
            elseif ($Action -eq 'install') {
                Check ($Configuration.requireService -eq $true -and $script:LifecycleGuestActions[-1] -eq 'wait-service') `
                    'Immediate binaries were not installed after service readiness.'
            }
            else { throw "Unexpected orchestration guest action: $Action" }
            $script:LifecycleGuestActions.Add($Action)
        }
        function Invoke-RunCommand {
            param($Name, $Properties, $Immediate)
            @{
                OperationState = 'Succeeded'; ProvisioningState = 'Succeeded'
                View = @{ executionState = 'Succeeded'; exitCode = 0; output = 'inet test'; error = '' }
            }
        }
        function Invoke-TestSuite {
            param([bool]$Immediate, [hashtable]$Hashes)
            Check ($Hashes.Count -eq 2 -and $Hashes.ContainsKey('run-command-handler') -and
                $Hashes.ContainsKey('immediate-run-command-handler')) 'Orchestration did not supply both local build hashes.'
            Check (($script:LifecycleGuestActions -join ',') -eq 'wait-service,install') `
                'Suites ran before service readiness and local binary installation completed.'
            $script:LifecycleModes.Add($Immediate)
            $script:Results.Add(@{ Name = 'mock'; Passed = ($script:LifecycleScenario -ne 'test-failure') })
        }
        function Remove-TestResourceGroup {
            $script:LifecycleDeletes++
            if ($script:LifecycleScenario -eq 'cleanup-failure') { throw 'simulated teardown failure' }
        }
        function Invoke-Az {
            param($Arguments, $Activity, $Output, $TimeoutSeconds, [switch]$AllowFailure,
                [switch]$WithoutSubscription, [switch]$RetryStorageAuthorization)
            $command = $Arguments[0..1] -join ' '
            switch ($command) {
                'account show' {
                    if ($script:LifecycleScenario -eq 'not-logged-in') {
                        return @{ ExitCode = 1; StdOut = ''; StdErr = 'No cached account.' }
                    }
                    return @{
                        ExitCode = 0
                        StdOut = '{"id":"test-subscription","name":"offline","tenantId":"22222222-2222-2222-2222-222222222222","user":{"name":"offline@example.invalid","type":"user"}}'
                        StdErr = ''
                    }
                }
                'account get-access-token' {
                    if ($script:LifecycleScenario -eq 'expired-login') {
                        return @{ ExitCode = 1; StdOut = ''; StdErr = 'Credentials expired.' }
                    }
                    return @{ ExitCode = 0; StdOut = (New-TestArmToken); StdErr = '' }
                }
                'group exists' {
                    if ($script:LifecycleScenario -in @('existing', 'foreign')) { return 'true' }
                    return 'false'
                }
                'group show' {
                    if ($script:LifecycleScenario -eq 'foreign') { return @{ tags = @{} } }
                    return @{ tags = @{ RunCommandValidationOwner = $owner } }
                }
                'group create' { return }
                'deployment group' {
                    Check ($Arguments -contains 'properties.correlationId' -and $Output -eq 'tsv') `
                        'Setup deployments did not request their own correlationId.'
                    if ($Arguments -contains 'validation-network') {
                        $templateIndex = [array]::IndexOf($Arguments, '--template-file')
                        $network = Get-Content -LiteralPath $Arguments[$templateIndex + 1] -Raw | ConvertFrom-Json -AsHashtable
                        $resource = $network.resources[0]
                        Check ($resource.type -eq 'Microsoft.Network/virtualNetworks') 'Network deployment did not create a VNet.'
                        Check ($resource.name -eq 'runcommand-validation-vnet') 'Unexpected VNet name.'
                        Check ($resource.properties.subnets.Count -eq 1) 'Unexpected subnet count.'
                        $subnet = $resource.properties.subnets[0]
                        Check ($subnet.name -eq 'validation') 'Unexpected subnet name.'
                        Check ($subnet.properties.ContainsKey('defaultOutboundAccess') -and
                            $subnet.properties.defaultOutboundAccess -ceq $false) 'Private subnet policy requires explicit defaultOutboundAccess=false.'
                        if ($script:LifecycleScenario -eq 'network-failure') { throw 'simulated network deployment failure' }
                        $publicIp = @($network.resources | Where-Object { $_.type -eq 'Microsoft.Network/publicIPAddresses' })
                        $nsg = @($network.resources | Where-Object { $_.type -eq 'Microsoft.Network/networkSecurityGroups' })
                        $nic = @($network.resources | Where-Object { $_.type -eq 'Microsoft.Network/networkInterfaces' })
                        Check ($publicIp.Count -eq 1 -and $publicIp[0].name -eq 'runcommand-validation-ip' -and
                            $publicIp[0].sku.name -eq 'Standard' -and $publicIp[0].properties.publicIPAllocationMethod -eq 'Static') 'Explicit outbound public IP was not preserved.'
                        Check ($nsg.Count -eq 1 -and $nsg[0].properties.securityRules.Count -eq 0) 'Network setup would open inbound ports.'
                        Check ($nic.Count -eq 1 -and $nic[0].properties.ipConfigurations[0].properties.subnet.id -eq
                            "[resourceId('Microsoft.Network/virtualNetworks/subnets', 'runcommand-validation-vnet', 'validation')]") 'NIC does not use the private subnet.'
                        Check ($nic[0].dependsOn.Count -eq 3 -and $nic[0].properties.networkSecurityGroup.id -eq
                            "[resourceId('Microsoft.Network/networkSecurityGroups', 'runcommand-validation-nsg')]") 'NIC dependencies or NSG association are missing.'
                        $script:PrivateNetworkReady = $true
                    }
                    elseif ($Arguments -contains 'validation-vm') {
                        Check $script:PrivateNetworkReady 'VM creation ran before the private network was deployed.'
                        Check ($TimeoutSeconds -eq 2400) 'VM creation timeout was changed.'
                        $templateIndex = [array]::IndexOf($Arguments, '--template-file')
                        $template = Get-Content -LiteralPath $Arguments[$templateIndex + 1] -Raw | ConvertFrom-Json -AsHashtable
                        $vm = $template.resources[0]
                        Check ($vm.type -eq 'Microsoft.Compute/virtualMachines' -and $vm.apiVersion -eq '2024-07-01' -and
                            $vm.name -eq 'runcommand-validation') 'Incorrect VM creation contract.'
                        Check ($vm.properties.installRunCommandAsService -ceq $true) 'Immediate RunCommand was not enabled in the initial VM creation request.'
                        Check ($vm.properties.networkProfile.networkInterfaces[0].id -eq
                            "/subscriptions/test-subscription/resourceGroups/$($script:ResourceGroup)/providers/Microsoft.Network/networkInterfaces/runcommand-validation-nic") 'VM is not attached to its precreated NIC.'
                        Check ($vm.properties.storageProfile.imageReference.sku -eq '22_04-lts-gen2' -and
                            $vm.properties.securityProfile.securityType -eq 'Standard' -and
                            $vm.properties.osProfile.linuxConfiguration.provisionVMAgent -ceq $true) 'VM image, security type, or agent provisioning changed.'
                        Check ($template.parameters.adminPassword.type -eq 'secureString' -and
                            $vm.properties.osProfile.adminPassword -eq "[parameters('adminPassword')]") 'Admin password was embedded in the VM template.'
                        $parametersIndex = [array]::IndexOf($Arguments, '--parameters')
                        Check ($parametersIndex -ge 0 -and $Arguments[$parametersIndex + 1].StartsWith('@')) 'Admin password was passed on the command line.'
                        $parameters = Get-Content -LiteralPath $Arguments[$parametersIndex + 1].Substring(1) -Raw | ConvertFrom-Json -AsHashtable
                        Check (-not [string]::IsNullOrWhiteSpace($parameters.adminPassword.value)) 'VM password parameter was not supplied.'
                        $script:LifecycleVmReady = $true
                    }
                    return 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'
                }
                'role assignment' {
                    $idIndex = [array]::IndexOf($Arguments, '--assignee-object-id')
                    Check ($Arguments[$idIndex + 1] -eq '11111111-1111-1111-1111-111111111111') 'Storage RBAC did not use the ARM token object ID.'
                    return
                }
                'storage container' {
                    Check ([bool]$RetryStorageAuthorization) 'Private container creation did not enable bounded authorization retries.'
                    return
                }
                'vm create' { throw 'VM creation must enable immediate RunCommand in the initial ARM template.' }
                'vm extension' {
                    if ($script:LifecycleScenario -eq 'setup-failure') { throw 'simulated VMAccess setup failure' }
                    return
                }
                'resource update' { throw 'Immediate RunCommand must not be enabled by a later VM update.' }
                default { throw "Unexpected Azure call in offline orchestration: $command" }
            }
        }
        $previousKeep = $script:KeepResources
        try {
            foreach ($scenario in @('success', 'not-logged-in', 'expired-login', 'network-failure', 'setup-failure', 'test-failure', 'keep', 'existing', 'foreign', 'cleanup-failure')) {
                $script:LifecycleScenario = $scenario
                $script:LifecycleDeletes = 0
                $script:LifecycleModes = [Collections.Generic.List[bool]]::new()
                $script:PrivateNetworkReady = $false
                $script:LifecycleVmReady = $false
                $script:LifecycleGuestActions = [Collections.Generic.List[string]]::new()
                $script:KeepResources = $scenario -eq 'keep'
                try {
                    if ($scenario -in @('success', 'keep', 'existing')) { Start-RunCommandValidation }
                    else {
                        $pattern = switch ($scenario) {
                            'setup-failure' { 'VMAccess setup failure' }
                            'test-failure' { 'functional test\(s\) failed' }
                            'foreign' { 'ownership tag does not match' }
                            'cleanup-failure' { 'teardown failure' }
                            'not-logged-in' { 'Run "az login"' }
                            'expired-login' { 'Run "az login"' }
                            'network-failure' { 'network deployment failure' }
                        }
                        Expect-Error { Start-RunCommandValidation } $pattern
                    }
                    $expectedDeletes = if ($scenario -eq 'existing') { 2 } elseif ($scenario -in @('keep', 'foreign', 'not-logged-in', 'expired-login')) { 0 } else { 1 }
                    Check ($script:LifecycleDeletes -eq $expectedDeletes) "Incorrect resource cleanup in scenario $scenario."
                    if ($scenario -in @('success', 'keep', 'existing', 'test-failure', 'cleanup-failure')) {
                        Check ($script:LifecycleModes.Count -eq 2 -and -not $script:LifecycleModes[0] -and $script:LifecycleModes[1]) `
                            "Traditional and immediate suites should each run once, in that order, in scenario $scenario."
                        Check (($script:LifecycleGuestActions -join ',') -eq 'wait-service,install') `
                            "Immediate service readiness and binary installation did not run exactly once in scenario $scenario."
                    }
                }
                finally {
                    Remove-Item -LiteralPath $script:OutputDirectory -Recurse -Force
                    $script:OutputDirectory = $testRoot
                    $script:PrivateDirectory = $testRoot
                }
            }
        }
        finally {
            $script:KeepResources = $previousKeep
            $script:Secrets = [Collections.Generic.List[string]]::new()
            $ownerBytes = [Text.Encoding]::UTF8.GetBytes("offline@example.invalid|$([Environment]::MachineName)")
            $ownerHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($ownerBytes)).Substring(0, 12).ToLowerInvariant()
            $lockPath = Join-Path ([IO.Path]::GetTempPath()) "test-subscription-rg-runcommand-local-validation-$ownerHash.lock"
            if (Test-Path -LiteralPath $lockPath) { Remove-Item -LiteralPath $lockPath -Force }
        }
    }
    Test-Local 'build failure restores environment and never logs into Azure' {
        $script:AzureCalled = $false
        $old = @($env:GOOS, $env:GOARCH, $env:CGO_ENABLED)
        function Invoke-Native {
            param($Executable, $Arguments, $Activity)
            if ($Executable -ne 'go') { $script:AzureCalled = $true }
            throw 'simulated build failure'
        }
        Expect-Error { Build-RunCommandBinaries (Split-Path -Parent $PSScriptRoot) } 'simulated build failure'
        Check (-not $script:AzureCalled) 'Azure was invoked after a failed build.'
        Check ($env:GOOS -ceq $old[0] -and $env:GOARCH -ceq $old[1] -and $env:CGO_ENABLED -ceq $old[2]) 'Build environment was not restored.'
    }
    if (-not $SkipBuild) {
        Test-Local 'actual Windows-to-Linux builds produce both amd64 ELF binaries' {
            $old = @($env:GOOS, $env:GOARCH, $env:CGO_ENABLED)
            Push-Location $testRoot
            try { Build-RunCommandBinaries (Split-Path -Parent $PSScriptRoot) }
            finally { Pop-Location }
            foreach ($name in @('run-command-handler', 'immediate-run-command-handler')) {
                $bytes = [IO.File]::ReadAllBytes((Join-Path $testRoot $name))
                Check ([Convert]::ToHexString($bytes[0..3]) -eq '7F454C46') "$name is not an ELF binary."
                Check ($bytes[4] -eq 2 -and $bytes[18] -eq 62) "$name is not Linux/amd64."
            }
            Check ($env:GOOS -ceq $old[0] -and $env:GOARCH -ceq $old[1] -and $env:CGO_ENABLED -ceq $old[2]) 'Build environment was not restored.'
        }
    }
    Write-Host "$passed offline checks passed. No Azure resources were created." -ForegroundColor Green
}
finally {
    Remove-Item -LiteralPath $testRoot -Recurse -Force
}
