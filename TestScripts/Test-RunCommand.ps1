#requires -Version 7.3
<#
.SYNOPSIS
Builds and validates the local Linux RunCommand binaries on a disposable Azure VM.
.DESCRIPTION
Requires an existing az login session and uses its default subscription. Deletes the
previous resource group owned by this harness before provisioning a fresh one.
Requires permission to create resources and assign Storage Blob Data Contributor.
.EXAMPLE
.\TestSripts\Test-RunCommand.ps1
.EXAMPLE
.\TestSripts\Test-RunCommand.ps1 -Region eastus2euap -KeepResources
#>
[CmdletBinding()]
param(
    [ValidateNotNullOrEmpty()]
    [string] $Region = 'CentralUSEUAP',
    [ValidateNotNullOrEmpty()]
    [string] $VmSize = 'Standard_D2s_v5',
    [switch] $KeepResources
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$script:LastConsoleUtc = [DateTime]::UtcNow
$script:HarnessPath = $PSCommandPath
$script:RunCommandDeadlineUtc = $null
$script:PendingRunCommand = $null
$script:OperationDeadlineUtc = $null
$script:ArmEndpoint = [uri]'https://management.azure.com/'

function Write-Step {
    param([string] $Message, [ConsoleColor] $Color = [ConsoleColor]::Cyan)
    Write-Host "[$(Get-Date -Format 'HH:mm:ss')] $Message" -ForegroundColor $Color
    $script:LastConsoleUtc = [DateTime]::UtcNow
}

function Protect-Text {
    param([AllowEmptyString()][string] $Text)
    foreach ($secret in $script:Secrets) {
        if ($secret) { $Text = $Text.Replace($secret, '<redacted>') }
    }
    $Text -replace '(?i)([?&](?:sig|skoid|sktid|skt|ske|sks|skv|se|sp|sv|sr)=)[^&\s"''<>]+', '$1<redacted>'
}

function Invoke-Native {
    param(
        [string] $Executable,
        [string[]] $Arguments,
        [string] $Activity,
        [int] $TimeoutSeconds = 1800,
        [int] $HeartbeatSeconds = 60,
        [switch] $AllowFailure
    )
    $invocationPath = Join-Path $script:PrivateDirectory "$([guid]::NewGuid()).native.json"
    $process = $null
    $timer = [Diagnostics.Stopwatch]::StartNew()
    try {
        Write-JsonFile $invocationPath @{ executable = $Executable; arguments = $Arguments }
        $escapedPath = $invocationPath.Replace("'", "''")
        $command = @"
`$ErrorActionPreference = 'Stop'
`$PSNativeCommandUseErrorActionPreference = `$false
`$document = [Text.Json.JsonDocument]::Parse([IO.File]::ReadAllText('$escapedPath'))
try {
    `$tool = `$document.RootElement.GetProperty('executable').GetString()
    `$toolArguments = @(`$document.RootElement.GetProperty('arguments').EnumerateArray() | ForEach-Object { `$_.GetString() })
}
finally { `$document.Dispose() }
& `$tool @toolArguments
exit `$LASTEXITCODE
"@
        $start = [Diagnostics.ProcessStartInfo]::new((Join-Path $PSHOME 'pwsh.exe'))
        $start.WorkingDirectory = $PWD.ProviderPath
        $start.UseShellExecute = $false
        $start.RedirectStandardOutput = $true
        $start.RedirectStandardError = $true
        foreach ($argument in @('-NoProfile', '-NonInteractive', '-EncodedCommand',
            [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command)))) {
            $start.ArgumentList.Add($argument)
        }
        $process = [Diagnostics.Process]::Start($start)
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()
        while (-not $process.WaitForExit(1000)) {
            if ($timer.Elapsed.TotalSeconds -ge $TimeoutSeconds) {
                throw [TimeoutException]::new("Timed out after $TimeoutSeconds seconds: $Activity")
            }
            if (([DateTime]::UtcNow - $script:LastConsoleUtc).TotalSeconds -ge $HeartbeatSeconds) {
                Write-Step "Still $Activity; waiting for $Executable to finish ($([int]$timer.Elapsed.TotalSeconds)s elapsed)." DarkGray
            }
        }
        $outputWait = [math]::Max(1, [math]::Min(5000, ($TimeoutSeconds - $timer.Elapsed.TotalSeconds) * 1000))
        if (-not [Threading.Tasks.Task]::WhenAll([Threading.Tasks.Task[]]@($stdoutTask, $stderrTask)).Wait([int]$outputWait)) {
            throw [TimeoutException]::new("Timed out waiting for native output streams to close: $Activity")
        }
        $result = @{
            ExitCode = $process.ExitCode
            StdOut = $stdoutTask.GetAwaiter().GetResult()
            StdErr = $stderrTask.GetAwaiter().GetResult()
        }
        if ($result.ExitCode -ne 0 -and -not $AllowFailure) {
            throw "$Activity failed (exit $($result.ExitCode)). $(Protect-Text ($result.StdErr + $result.StdOut))"
        }
        return $result
    }
    finally {
        if ($process) {
            if (-not $process.HasExited) {
                $process.Kill($true)
                if (-not $process.WaitForExit(5000)) { Write-Warning "Native process $($process.Id) did not exit after termination." }
            }
            $process.Dispose()
        }
        Remove-Item -LiteralPath $invocationPath -Force -ErrorAction SilentlyContinue
    }
}

function Invoke-Az {
    param(
        [string[]] $Arguments,
        [string] $Activity,
        [ValidateSet('json', 'tsv', 'none')][string] $Output = 'json',
        [int] $TimeoutSeconds = 1800,
        [switch] $AllowFailure,
        [switch] $WithoutSubscription,
        [switch] $RetryStorageAuthorization,
        [ValidateRange(1, 3600)][int] $AuthorizationTimeoutSeconds = 600
    )
    if ($RetryStorageAuthorization -and $Arguments[0] -ne 'storage') {
        throw 'Storage authorization retries can only be used with Azure CLI storage commands.'
    }
    $cliArguments = @($Arguments) + @('--output', $Output, '--only-show-errors')
    if (-not $WithoutSubscription) {
        $cliArguments += @('--subscription', $script:SubscriptionId)
    }
    $authorizationWait = $null
    while ($true) {
        $attemptTimeout = $TimeoutSeconds
        if ($script:RunCommandDeadlineUtc) {
            $caseRemaining = ($script:RunCommandDeadlineUtc - [DateTime]::UtcNow).TotalSeconds
            if ($caseRemaining -le 0) { throw [TimeoutException]::new("RunCommand exceeded its total execution deadline while $Activity") }
            $attemptTimeout = [math]::Min($attemptTimeout, [math]::Max(1, [math]::Ceiling($caseRemaining)))
        }
        if ($script:OperationDeadlineUtc) {
            $operationRemaining = ($script:OperationDeadlineUtc - [DateTime]::UtcNow).TotalSeconds
            if ($operationRemaining -le 0) { throw [TimeoutException]::new("Operation exceeded its total deadline while $Activity") }
            $attemptTimeout = [math]::Min($attemptTimeout, [math]::Max(1, [math]::Ceiling($operationRemaining)))
        }
        if ($authorizationWait) {
            $remaining = $AuthorizationTimeoutSeconds - $authorizationWait.Elapsed.TotalSeconds
            if ($remaining -le 0) {
                $result.StdErr = "Storage authorization did not recover within $AuthorizationTimeoutSeconds seconds. Verify the storage-scoped role assignment and any applicable access policies. Last Azure CLI error: $($result.StdErr)"
                break
            }
            $attemptTimeout = [math]::Min($attemptTimeout, [math]::Max(1, [math]::Ceiling($remaining)))
        }
        $result = Invoke-Native -Executable 'az' -Arguments $cliArguments -Activity $Activity `
            -TimeoutSeconds $attemptTimeout -AllowFailure
        if (-not $RetryStorageAuthorization -or $result.ExitCode -eq 0) { break }
        $authorizationError = $result.StdErr -match 'AuthorizationPermissionMismatch|AuthorizationFailure|\bForbidden\b|\b403\b|You do not have the required permissions needed to perform this operation'
        if (-not $authorizationError -or $result.StdErr -match 'AADSTS|AuthenticationFailed|InvalidAuthenticationInfo') { break }
        if (-not $authorizationWait) {
            $authorizationWait = [Diagnostics.Stopwatch]::StartNew()
            Write-Step "Storage authorization rejected $Activity. Waiting up to $AuthorizationTimeoutSeconds seconds for access to become available; individual retries are suppressed." Yellow
        }
        if (([DateTime]::UtcNow - $script:LastConsoleUtc).TotalSeconds -ge 60) {
            Write-Step "Still $Activity; waiting for storage authorization ($([int]$authorizationWait.Elapsed.TotalSeconds)s elapsed)." DarkGray
        }
        $remaining = $AuthorizationTimeoutSeconds - $authorizationWait.Elapsed.TotalSeconds
        if ($remaining -gt 0) { Start-Sleep -Seconds ([math]::Min(10, [math]::Ceiling($remaining))) }
    }
    if ($AllowFailure) { return $result }
    if ($result.ExitCode -ne 0) {
        throw "$Activity failed (exit $($result.ExitCode)). $(Protect-Text ($result.StdErr + $result.StdOut))"
    }
    if ($Output -eq 'json' -and $result.StdOut.Trim()) {
        return ConvertFrom-Json -InputObject $result.StdOut -AsHashtable
    }
    if ($Output -eq 'tsv') { return $result.StdOut.Trim() }
}

function Get-LoggedInAzureAccount {
    Write-Step 'Checking the existing Azure CLI session and default subscription.'
    $account = Invoke-Az @('account', 'show') 'checking the existing Azure CLI account' `
        -WithoutSubscription -AllowFailure -TimeoutSeconds 120
    if ($account.ExitCode -ne 0) {
        throw "An existing Azure CLI login is required. Run `"az login`" before running this script. $(Protect-Text $account.StdErr)"
    }
    $accountDetails = ConvertFrom-Json -InputObject $account.StdOut -AsHashtable
    if ($accountDetails['user']['type'] -ne 'user') {
        throw 'This harness requires an Azure CLI user session. Run "az login" with your user account before running this script.'
    }
    $token = Invoke-Az @('account', 'get-access-token', '--resource-type', 'arm', '--query', 'accessToken') `
        'validating the existing Azure CLI credentials' -WithoutSubscription -AllowFailure -Output tsv -TimeoutSeconds 120
    if ($token.StdOut.Trim()) { $script:Secrets.Add($token.StdOut.Trim()) }
    if ($token.ExitCode -ne 0) {
        throw "The existing Azure CLI session cannot authenticate. Run `"az login`" before running this script. $(Protect-Text $token.StdErr)"
    }
    $accountDetails.userObjectId = Get-ArmTokenObjectId -AccessToken $token.StdOut.Trim() -TenantId $accountDetails.tenantId
    Write-Step 'Validated the signed-in user identity using ARM credentials; no Microsoft Graph access is required.'
    return $accountDetails
}

function Get-ArmTokenObjectId {
    param([string] $AccessToken, [string] $TenantId)
    # Read identity metadata only; Azure, not this decoder, authorizes subsequent requests.
    $parts = $AccessToken.Split('.')
    if ($parts.Count -ne 3) { throw 'The ARM access token is not a JWT; cannot determine the signed-in user object ID.' }
    $payload = $parts[1].Replace('-', '+').Replace('_', '/')
    $payload = $payload.PadRight($payload.Length + ((4 - $payload.Length % 4) % 4), '=')
    try { $json = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($payload)) }
    catch [FormatException] { throw 'The ARM access token payload is not valid base64; cannot determine the signed-in user object ID.' }
    try { $claims = ConvertFrom-Json -InputObject $json -AsHashtable -ErrorAction Stop }
    catch [ArgumentException] { throw 'The ARM access token payload is not valid JSON; cannot determine the signed-in user object ID.' }
    $objectId = [guid]::Empty
    if ($claims -isnot [hashtable] -or -not [guid]::TryParse([string]$claims['oid'], [ref]$objectId) -or $objectId -eq [guid]::Empty) {
        throw 'The ARM access token does not contain a valid user object ID (oid).'
    }
    if (-not $TenantId -or $claims['tid'] -ine $TenantId) {
        throw 'The ARM access token tenant does not match the selected subscription tenant.'
    }
    return $objectId.ToString()
}

function Stop-ValidationProcess {
    param([int] $ProcessId, [datetime] $StartedUtc, [string] $ExpectedStartTicks)
    $snapshot = @(Get-CimInstance Win32_Process)
    $ancestor = $PID
    while ($ancestor) {
        if ($ancestor -eq $ProcessId) {
            throw "Refusing to terminate the current PowerShell process or its ancestor (PID $ProcessId)."
        }
        $parent = @($snapshot | Where-Object { $_.ProcessId -eq $ancestor })
        $ancestor = if ($parent.Count) { [int]$parent[0].ParentProcessId } else { 0 }
    }
    $ids = [Collections.Generic.List[int]]::new()
    $ids.Add($ProcessId)
    for ($index = 0; $index -lt $ids.Count; $index++) {
        foreach ($child in $snapshot) {
            if ($child.ParentProcessId -eq $ids[$index] -and
                $child.CreationDate.ToUniversalTime() -ge $StartedUtc -and
                -not $ids.Contains([int]$child.ProcessId)) {
                $ids.Add([int]$child.ProcessId)
            }
        }
    }
    $processes = [Collections.Generic.List[Diagnostics.Process]]::new()
    try {
        foreach ($id in $ids) {
            $process = Get-Process -Id $id -ErrorAction SilentlyContinue
            if ($process) {
                # Retain the process handle so PID reuse cannot target a different process.
                $null = $process.Handle
                $processes.Add($process)
                if ($id -eq $ProcessId -and
                    [string]$process.StartTime.ToUniversalTime().Ticks -ne $ExpectedStartTicks) {
                    throw "Previous validation PID $ProcessId was reused; refusing to terminate it."
                }
                if ($id -ne $ProcessId) {
                    $recorded = $snapshot | Where-Object { $_.ProcessId -eq $id } | Select-Object -First 1
                    if ([math]::Abs(($process.StartTime.ToUniversalTime() - $recorded.CreationDate.ToUniversalTime()).TotalMilliseconds) -gt 1) {
                        throw "Previous validation child PID $id was reused; refusing to terminate it."
                    }
                }
            }
        }
        foreach ($process in $processes) {
            if (-not $process.HasExited) {
                Write-Step "Stopping previous validation process PID $($process.Id)." Yellow
                Stop-Process -Id $process.Id -Force -ErrorAction Stop
                if (-not $process.WaitForExit(30000)) {
                    throw "Timed out waiting for previous validation process PID $($process.Id) to exit."
                }
            }
        }
    }
    finally {
        foreach ($process in $processes) { $process.Dispose() }
    }
}

function Enter-ValidationInstance {
    param([string] $ScriptPath = $script:HarnessPath)
    if (-not $IsWindows) { throw 'Validation process management currently requires Windows.' }
    $fullPath = [IO.Path]::GetFullPath($ScriptPath)
    $key = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData(
        [Text.Encoding]::UTF8.GetBytes($fullPath.ToLowerInvariant())
    )).Substring(0, 24)
    $statePath = Join-Path ([IO.Path]::GetTempPath()) "runcommand-validation-$key.instance.json"
    $mutex = [Threading.Mutex]::new($false, "Local\RunCommandValidation-$key")
    $acquired = $false
    $registration = $null
    try {
        Write-Step 'Checking for previous instances of this validation script.'
        try { $acquired = $mutex.WaitOne([TimeSpan]::FromSeconds(60)) }
        catch [Threading.AbandonedMutexException] {
            $acquired = $true
            Write-Step 'Recovered the startup lock from an interrupted validation run.' Yellow
        }
        if (-not $acquired) { throw 'Timed out waiting for another validation instance to finish startup.' }
        if (Test-Path -LiteralPath $statePath) {
            $active = $false
            try {
                $probe = [IO.File]::Open($statePath, 'Open', 'ReadWrite', 'None')
                $probe.Dispose()
            }
            catch [IO.IOException] {
                if (($_.Exception.HResult -band 0xffff) -notin @(32, 33)) { throw }
                $active = $true
            }
            if ($active) {
                $stream = [IO.File]::Open($statePath, 'Open', 'Read', 'ReadWrite')
                $reader = [IO.StreamReader]::new($stream)
                try { $previous = $reader.ReadToEnd() | ConvertFrom-Json -AsHashtable }
                finally { $reader.Dispose() }
                $process = Get-Process -Id $previous.ProcessId -ErrorAction SilentlyContinue
                try {
                    if (-not $process -or $previous.ScriptPath -ine $fullPath -or
                        [string]$process.StartTime.ToUniversalTime().Ticks -ne $previous.ProcessStartTicks) {
                        throw 'The active validation registration does not match its process; refusing to terminate an unverified process.'
                    }
                    Stop-ValidationProcess -ProcessId $process.Id -StartedUtc ([datetime]$previous.StartedUtc) `
                        -ExpectedStartTicks $previous.ProcessStartTicks
                }
                finally { if ($process) { $process.Dispose() } }
            }
        }
        # Also recognize older versions launched with an explicit, absolute -File path.
        foreach ($candidate in Get-CimInstance Win32_Process -Filter "Name = 'pwsh.exe' OR Name = 'powershell.exe'") {
            if ($candidate.ProcessId -eq $PID -or -not $candidate.CommandLine) { continue }
            if ($candidate.CommandLine -match '(?i)\s-NoE\w*(?:\s|$)') { continue }
            $match = [regex]::Match($candidate.CommandLine, '(?i)\s-(?:File|f)\s+(?:"([^"]+)"|''([^'']+)''|(\S+))')
            if (-not $match.Success) { continue }
            $path = ($match.Groups[1..3] | Where-Object { $_.Success } | Select-Object -First 1).Value
            if ([IO.Path]::IsPathFullyQualified($path) -and [IO.Path]::GetFullPath($path) -ieq $fullPath) {
                $process = Get-Process -Id $candidate.ProcessId -ErrorAction SilentlyContinue
                if (-not $process) { continue }
                try {
                    $null = $process.Handle
                    $started = $process.StartTime.ToUniversalTime()
                    if ([math]::Abs(($started - $candidate.CreationDate.ToUniversalTime()).TotalMilliseconds) -gt 1) {
                        throw "Previous validation PID $($candidate.ProcessId) was reused; refusing to terminate it."
                    }
                    Stop-ValidationProcess -ProcessId $candidate.ProcessId -StartedUtc $started `
                        -ExpectedStartTicks ([string]$started.Ticks)
                }
                finally { $process.Dispose() }
            }
        }
        $registration = [IO.File]::Open($statePath, 'OpenOrCreate', 'ReadWrite', 'Read')
        $self = Get-Process -Id $PID
        try {
            $metadata = @{
                ProcessId = $PID
                ProcessStartTicks = [string]$self.StartTime.ToUniversalTime().Ticks
                StartedUtc = [DateTime]::UtcNow.ToString('o', [Globalization.CultureInfo]::InvariantCulture)
                ScriptPath = $fullPath
            } | ConvertTo-Json -Compress
        }
        finally { $self.Dispose() }
        $bytes = [Text.Encoding]::UTF8.GetBytes($metadata)
        $registration.SetLength(0)
        $registration.Write($bytes, 0, $bytes.Length)
        $registration.Flush()
        return $registration
    }
    catch {
        if ($registration) { $registration.Dispose() }
        throw
    }
    finally {
        if ($acquired) { $mutex.ReleaseMutex() }
        $mutex.Dispose()
    }
}

function Write-JsonFile {
    param([string] $Path, [object] $Value)
    $Value | ConvertTo-Json -Depth 40 | Set-Content -LiteralPath $Path -Encoding utf8NoBOM
}

function Wait-Condition {
    param(
        [string] $Activity,
        [scriptblock] $Probe,
        [int] $TimeoutSeconds = 1200,
        [int] $IntervalSeconds = 10
    )
    $timer = [Diagnostics.Stopwatch]::StartNew()
    while ($true) {
        if (& $Probe) { return }
        if ($timer.Elapsed.TotalSeconds -ge $TimeoutSeconds) {
            throw [TimeoutException]::new("Timed out after $TimeoutSeconds seconds: $Activity")
        }
        if (([DateTime]::UtcNow - $script:LastConsoleUtc).TotalSeconds -ge 60) {
            Write-Step "Still $Activity ($([int]$timer.Elapsed.TotalSeconds)s elapsed)." DarkGray
        }
        Start-Sleep -Seconds ([math]::Min($IntervalSeconds, [math]::Max(0, $TimeoutSeconds - $timer.Elapsed.TotalSeconds)))
    }
}

function New-RunCommandRequest {
    param([hashtable] $Properties, [bool] $Immediate)
    $copy = @{} + $Properties
    if ($Immediate) { $copy.runImmediately = $true }
    return @{ location = $script:Location; properties = $copy }
}

function Resolve-ArmUri {
    param([string] $Uri)
    $resolved = [uri]::new($script:ArmEndpoint, $Uri)
    if ($resolved.Scheme -ne $script:ArmEndpoint.Scheme -or $resolved.Authority -ne $script:ArmEndpoint.Authority -or $resolved.UserInfo) {
        throw 'Refusing to send ARM credentials to an operation URL outside the configured ARM endpoint.'
    }
    return $resolved
}

function Get-RunCommandArmToken {
    $token = Invoke-Az @('account', 'get-access-token', '--resource-type', 'arm', '--query', 'accessToken') `
        'obtaining existing ARM credentials for RunCommand REST requests' -WithoutSubscription -Output tsv -TimeoutSeconds 120
    if ([string]::IsNullOrWhiteSpace($token)) { throw 'ARM access token is empty. Run "az login" before validation.' }
    $script:Secrets.Add($token)
    return $token
}

function Invoke-ArmRequest {
    param([string] $Method, [string] $Uri, [string] $Token, [string] $Activity,
        [hashtable] $Body, [int] $TimeoutSeconds = 420, [switch] $RequireInstanceView)
    $resolved = Resolve-ArmUri $Uri
    foreach ($deadline in @($script:RunCommandDeadlineUtc, $script:OperationDeadlineUtc)) {
        if ($deadline) {
            $remaining = ($deadline - [DateTime]::UtcNow).TotalSeconds
            if ($remaining -le 0) { throw [TimeoutException]::new("Deadline exceeded while $Activity") }
            $TimeoutSeconds = [math]::Min($TimeoutSeconds, [math]::Max(1, [math]::Ceiling($remaining)))
        }
    }
    $handler = [Net.Http.HttpClientHandler]::new()
    $handler.AllowAutoRedirect = $false
    $client = [Net.Http.HttpClient]::new($handler)
    $client.Timeout = [Threading.Timeout]::InfiniteTimeSpan
    $request = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::new($Method), $resolved)
    $request.Headers.Authorization = [Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $Token)
    if ($Body) {
        $request.Content = [Net.Http.StringContent]::new(($Body | ConvertTo-Json -Depth 40 -Compress),
            [Text.Encoding]::UTF8, 'application/json')
    }
    $cancellation = [Threading.CancellationTokenSource]::new()
    $cancellation.CancelAfter([TimeSpan]::FromSeconds($TimeoutSeconds))
    $response = $null
    $httpDeadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    try {
        if ($RequireInstanceView) {
            if ($request.Method -ne [Net.Http.HttpMethod]::Get -or
                $request.RequestUri.Query -cnotmatch '(?:^\?|&)\$expand=instanceView(?:&|$)') {
                throw 'Refusing to send an instance-view request without GET and the literal $expand=instanceView query parameter.'
            }
            $clientRequestId = [guid]::NewGuid().ToString()
            $request.Headers.Add('x-ms-client-request-id', $clientRequestId)
            $requestTrace = [ordered]@{
                method = $request.Method.Method
                uri = $request.RequestUri.AbsoluteUri
                httpVersion = $request.Version.ToString()
                versionPolicy = $request.VersionPolicy.ToString()
                'x-ms-client-request-id' = $clientRequestId
            } | ConvertTo-Json -Compress
            Write-Step "ARM instance-view request: $requestTrace" DarkGray
        }
        $task = $client.SendAsync($request, $cancellation.Token)
        while (-not $task.IsCompleted) {
            if ([DateTime]::UtcNow -ge $httpDeadline) {
                $cancellation.Cancel()
                throw [TimeoutException]::new("Timed out after $TimeoutSeconds seconds: $Activity")
            }
            Start-Sleep -Milliseconds 200
            if (([DateTime]::UtcNow - $script:LastConsoleUtc).TotalSeconds -ge 60) {
                Write-Step "Still $Activity; waiting for the CRP HTTP response." DarkGray
            }
        }
        $response = $task.GetAwaiter().GetResult()
        $headers = @{}
        foreach ($header in $response.Headers) { $headers[$header.Key] = $header.Value -join ',' }
        if ($RequireInstanceView) {
            $responseTrace = [ordered]@{
                'x-ms-client-request-id' = $clientRequestId
                httpStatus = [int]$response.StatusCode
                'x-ms-request-id' = $headers['x-ms-request-id']
                'x-ms-correlation-request-id' = $headers['x-ms-correlation-request-id']
            } | ConvertTo-Json -Compress
            Write-Step "ARM instance-view response: $responseTrace" DarkGray
        }
        $text = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
        if ($RequireInstanceView) {
            Write-Step "ARM instance-view raw response body (x-ms-client-request-id=$clientRequestId):`n$text" DarkGray
        }
        return @{
            StatusCode = [int]$response.StatusCode
            Headers = $headers
            Body = if ($text.Trim()) { ConvertFrom-Json $text -AsHashtable } else { @{} }
        }
    }
    catch {
        if ($cancellation.IsCancellationRequested) {
            throw [TimeoutException]::new("Timed out after $TimeoutSeconds seconds: $Activity")
        }
        throw
    }
    finally {
        if ($response) { $response.Dispose() }
        $request.Dispose()
        $client.Dispose()
        $cancellation.Dispose()
    }
}

function Assert-ArmResponse {
    param([hashtable] $Response, [string] $Activity)
    if ($Response.StatusCode -notin @(200, 201, 202, 204)) {
        throw "$Activity failed (HTTP $($Response.StatusCode)). $(Protect-Text ($Response.Body | ConvertTo-Json -Depth 40 -Compress))"
    }
}

function Wait-RunCommandRetryAfter {
    param([hashtable] $Headers)
    $delay = 0
    if ($Headers['Retry-After']) {
        $value = $Headers['Retry-After']
        if (-not [int]::TryParse($value, [ref]$delay)) {
            $date = [DateTimeOffset]::Parse($value, [Globalization.CultureInfo]::InvariantCulture)
            $delay = [math]::Max(0, [math]::Ceiling(($date - [DateTimeOffset]::UtcNow).TotalSeconds))
        }
    }
    if ($delay -lt 0) { throw 'CRP returned a negative Retry-After interval.' }
    if ($delay -gt 0) {
        $remaining = ($script:RunCommandDeadlineUtc - [DateTime]::UtcNow).TotalSeconds
        $until = [DateTime]::UtcNow.AddSeconds([math]::Min([double]$delay, [math]::Max(0.0, $remaining)))
        while ([DateTime]::UtcNow -lt $until) {
            if (([DateTime]::UtcNow - $script:LastConsoleUtc).TotalSeconds -ge 60) {
                Write-Step "Still waiting for CRP's Retry-After interval before polling the RunCommand operation." DarkGray
            }
            Start-Sleep -Milliseconds 200
        }
    }
}

function Write-DeploymentCorrelationId {
    param([string] $Name, [AllowEmptyString()][string] $CorrelationId)
    if ([string]::IsNullOrWhiteSpace($CorrelationId)) {
        Write-Warning "ARM deployment $Name did not return properties.correlationId."
    }
    else {
        Write-Step "ARM deployment $Name correlationId: $CorrelationId" DarkGray
    }
}

function Invoke-RunCommand {
    param([string] $Name, [hashtable] $Properties, [bool] $Immediate,
        [ValidateRange(1, 1200)][int] $CompletionTimeoutSeconds = 600,
        [int] $ExpectedExitCode = 0, [switch] $ExpectNonzeroExitCode,
        [string] $ExpectedOutput = '', [string] $ExpectedError = '')
    $script:RunCommandDeadlineUtc = [DateTime]::UtcNow.AddSeconds($CompletionTimeoutSeconds)
    $script:PendingRunCommand = @{ Name = $Name; OperationId = '' }
    try {
        $token = Get-RunCommandArmToken
        $apiVersion = '2024-07-01'
        $url = "$($script:VmId)/runCommands/$([uri]::EscapeDataString($Name))?api-version=$apiVersion"
        $response = Invoke-ArmRequest PUT $url $token "submitting RunCommand $Name directly to CRP" `
            -Body (New-RunCommandRequest $Properties $Immediate)
        Assert-ArmResponse $response "submitting RunCommand $Name"
        $operationUrl = $response.Headers['Azure-AsyncOperation']
        $statusEndpoint = -not [string]::IsNullOrWhiteSpace($operationUrl)
        if (-not $operationUrl) { $operationUrl = $response.Headers['Location'] }
        $state = @{ Operation = ''; OperationError = $null; Resource = $null; Headers = $response.Headers }
        if ($operationUrl) {
            $operationUri = Resolve-ArmUri $operationUrl
            $operationId = [uri]::UnescapeDataString($operationUri.AbsolutePath.TrimEnd('/').Split('/')[-1])
            $script:PendingRunCommand.OperationId = $operationId
            Write-Step "RunCommand $Name CRP operationId: $operationId" Magenta
            Wait-Condition -Activity "waiting for CRP operation $operationId for RunCommand $Name" `
                -TimeoutSeconds $CompletionTimeoutSeconds -Probe {
                Wait-RunCommandRetryAfter $state.Headers
                $poll = Invoke-ArmRequest GET $operationUri.AbsoluteUri $token "polling CRP operation $operationId for $Name"
                $state.Headers = $poll.Headers
                $state.OperationError = $poll.Body['error']
                if ($statusEndpoint) {
                    if ($poll.StatusCode -ne 200) { Assert-ArmResponse $poll "polling CRP operation $operationId"; return $false }
                    if (-not $poll.Body['status']) { throw "CRP operation $operationId returned no operation status." }
                    $state.Operation = $poll.Body.status
                }
                else {
                    if ($poll.StatusCode -eq 202) { return $false }
                    if ($poll.StatusCode -in @(400, 409, 500) -and $poll.Body['error']) {
                        $state.Operation = 'Failed'
                        return $true
                    }
                    Assert-ArmResponse $poll "polling CRP operation $operationId"
                    $state.Operation = if ($poll.Body['status']) { $poll.Body.status } elseif ($poll.Body['properties']) {
                        $poll.Body.properties.provisioningState
                    } else { 'Succeeded' }
                }
                $state.Operation -in @('Succeeded', 'Failed', 'Canceled')
            }
        }
        else {
            if ($response.StatusCode -eq 202) { throw 'CRP accepted RunCommand without returning an operation polling URL.' }
            Write-Step "RunCommand $Name returned synchronously; no operation polling URL or operationId was supplied." DarkGray
        }
        $resultTimeoutSeconds = $CompletionTimeoutSeconds
        if ($Immediate) {
            $resultTimeoutSeconds = 30
            $resultDeadlineUtc = [DateTime]::UtcNow.AddSeconds($resultTimeoutSeconds)
            if ($resultDeadlineUtc -lt $script:RunCommandDeadlineUtc) {
                $script:RunCommandDeadlineUtc = $resultDeadlineUtc
            }
        }
        $pollValues = [ordered]@{
            'api-version' = $apiVersion
            httpStatus = $null
            provisioningState = $null
            runImmediately = $null
            'instanceView.executionState' = $null
            'instanceView.exitCode' = $null
            outputPresent = $null
            errorPresent = $null
            expectedOutputMatched = $null
            expectedErrorMatched = $null
            unmetConditions = @('No completed instance-view GET')
        }
        $state.LastInstanceViewPoll = $pollValues | ConvertTo-Json -Compress
        try {
            Wait-Condition -Activity "waiting for RunCommand $Name expected instance-view result and terminal provisioning state" -TimeoutSeconds $resultTimeoutSeconds -Probe {
                $resource = Invoke-ArmRequest GET "$url&`$expand=instanceView" $token "reading RunCommand instance view for $Name" -RequireInstanceView
                $properties = $resource.Body['properties']
                $view = if ($properties) { $properties['instanceView'] } else { $null }
                $pollValues.httpStatus = $resource.StatusCode
                $pollValues.provisioningState = if ($properties) { $properties['provisioningState'] } else { $null }
                $pollValues.runImmediately = if ($properties) { $properties['runImmediately'] } else { $null }
                $pollValues['instanceView.executionState'] = if ($view) { $view['executionState'] } else { $null }
                $pollValues['instanceView.exitCode'] = if ($view) { $view['exitCode'] } else { $null }
                $pollValues.outputPresent = [bool]($view -and $null -ne $view['output'])
                $pollValues.errorPresent = [bool]($view -and $null -ne $view['error'])
                $pollValues.expectedOutputMatched = [bool](-not $ExpectedOutput -or
                    ($pollValues.outputPresent -and ($view['output'] -replace "`r", '').Contains($ExpectedOutput)))
                $pollValues.expectedErrorMatched = [bool](-not $ExpectedError -or
                    ($pollValues.errorPresent -and $view['error'].Contains($ExpectedError)))
                $unmet = [Collections.Generic.List[string]]::new()
                if (-not $view) { $unmet.Add('Instance view missing') }
                if ($null -eq $pollValues['instanceView.exitCode']) { $unmet.Add('Exit code missing') }
                elseif ($ExpectNonzeroExitCode) {
                    if ([int]$pollValues['instanceView.exitCode'] -eq 0) { $unmet.Add('Nonzero exit code required') }
                }
                elseif ([int]$pollValues['instanceView.exitCode'] -ne $ExpectedExitCode) {
                    $unmet.Add("Exit code must equal $ExpectedExitCode")
                }
                if ($pollValues.provisioningState -notin @('Succeeded', 'Failed', 'Canceled')) {
                    $unmet.Add('Terminal provisioning state required')
                }
                if (-not $pollValues.expectedOutputMatched) { $unmet.Add('Expected output not matched') }
                if (-not $pollValues.expectedErrorMatched) { $unmet.Add('Expected error not matched') }
                $pollValues.unmetConditions = @($unmet.ToArray())
                $state.LastInstanceViewPoll = $pollValues | ConvertTo-Json -Compress
                Write-Step "RunCommand $Name instance-view GET: $($state.LastInstanceViewPoll)" DarkGray
                if ($resource.StatusCode -notin @(200, 201, 202, 204)) {
                    throw "reading RunCommand instance view for $Name failed: $($state.LastInstanceViewPoll)"
                }
                $state.Resource = $resource.Body
                return $unmet.Count -eq 0
            }
        }
        catch [TimeoutException] {
            throw [TimeoutException]::new(
                "$($_.Exception.Message) Last RunCommand instance-view GET values (null means not returned): $($state.LastInstanceViewPoll)",
                $_.Exception)
        }
        if ($Immediate -and $state.Resource.properties['runImmediately'] -ne $true) {
            throw 'The resource did not confirm runImmediately=true; refusing to count a traditional execution as immediate coverage.'
        }
        if (-not $state.Operation) { $state.Operation = $state.Resource.properties.provisioningState }
        $operationId = $script:PendingRunCommand.OperationId
        $script:PendingRunCommand = $null
        return @{
            OperationState = $state.Operation
            OperationId = $operationId
            OperationError = $state.OperationError
            ProvisioningState = $state.Resource.properties.provisioningState
            View = $state.Resource.properties.instanceView
        }
    }
    finally {
        $script:RunCommandDeadlineUtc = $null
    }
}

function New-BlobSas {
    param([string] $Name, [string] $Permissions = 'r')
    Invoke-Az -Arguments @(
        'storage', 'blob', 'generate-sas', '--account-name', $script:StorageName,
        '--container-name', 'validation', '--name', $Name, '--permissions', $Permissions,
        '--expiry', [DateTime]::UtcNow.AddHours(4).ToString("yyyy-MM-dd'T'HH:mm:ss'Z'", [Globalization.CultureInfo]::InvariantCulture),
        '--auth-mode', 'login', '--as-user', '--https-only', '--full-uri'
    ) -Activity "issuing a user-delegation SAS for $Name" -Output tsv -RetryStorageAuthorization
}

function Send-Blob {
    param([string] $Path, [string] $Name, [string] $Type = 'block')
    $null = Invoke-Az -Arguments @(
        'storage', 'blob', 'upload', '--account-name', $script:StorageName,
        '--container-name', 'validation', '--name', $Name, '--file', $Path,
        '--type', $Type, '--overwrite', 'true', '--auth-mode', 'login', '--no-progress'
    ) -Activity "uploading $Name using Entra authentication" -Output none -RetryStorageAuthorization
}

function Receive-Blob {
    param([string] $Name, [string] $Path)
    $null = Invoke-Az -Arguments @(
        'storage', 'blob', 'download', '--account-name', $script:StorageName,
        '--container-name', 'validation', '--name', $Name, '--file', $Path,
        '--overwrite', 'true', '--auth-mode', 'login', '--no-progress'
    ) -Activity "downloading $Name" -Output none -RetryStorageAuthorization
}

function Invoke-CustomScript {
    param([string] $Action, [hashtable] $Configuration = @{})
    Write-Step "CustomScript: $Action."
    $configName = "guest-$([guid]::NewGuid().ToString('N')).json"
    $configPath = Join-Path $script:PrivateDirectory $configName
    $protectedPath = Join-Path $script:PrivateDirectory "cse-$configName"
    $configurationUploaded = $false
    try {
        Write-JsonFile $configPath $Configuration
        Send-Blob $configPath $configName
        $configurationUploaded = $true
        $uris = @($script:GuestScriptUri, (New-BlobSas $configName))
        if ($Action -eq 'install') { $uris += $script:BinaryUris }
        Write-JsonFile $protectedPath @{
            fileUris = $uris
            commandToExecute = "bash Invoke-RunCommandGuest.sh $Action $configName"
        }
        $collect = $Action -eq 'collect'
        $arguments = @(
            'vm', 'extension', 'set', '-g', $script:ResourceGroup, '--vm-name', $script:VmName,
            '--name', 'CustomScript', '--publisher', 'Microsoft.Azure.Extensions', '--version', '2.1',
            '--extension-instance-name', 'RunCommandValidationCSE', '--force-update',
            '--protected-settings', "@$protectedPath"
        )
        if ($collect) { $arguments += '--no-wait' }
        $null = Invoke-Az -Arguments $arguments `
            -Activity "running CustomScript '$Action' on the VM" -Output none -TimeoutSeconds 300
        if ($collect) {
            Wait-Condition -Activity 'waiting for CSE to upload the RunCommand diagnostic archive (independent of VM update status)' `
                -TimeoutSeconds 240 -Probe {
                    (Invoke-Az @('storage', 'blob', 'exists', '--account-name', $script:StorageName,
                        '--container-name', 'validation', '--name', $Configuration.blobName, '--auth-mode', 'login',
                        '--query', 'exists') 'checking whether CSE uploaded diagnostic logs' -Output tsv -TimeoutSeconds 30) -eq 'true'
                } | Out-Null
        }
    }
    finally {
        Remove-Item -LiteralPath $configPath, $protectedPath -Force -ErrorAction SilentlyContinue
        # A collection configuration contains a log-upload SAS; remove it from storage, too.
        if ($configurationUploaded) {
            try {
                $deleted = Invoke-Az -Arguments @(
                    'storage', 'blob', 'delete', '--account-name', $script:StorageName,
                    '--container-name', 'validation', '--name', $configName, '--auth-mode', 'login'
                ) -Activity 'removing the temporary guest configuration' -Output none -AllowFailure `
                    -RetryStorageAuthorization -AuthorizationTimeoutSeconds 120
                if ($deleted.ExitCode -ne 0) {
                    Write-Warning "Could not remove temporary guest configuration: $(Protect-Text $deleted.StdErr)"
                }
            }
            catch {
                Write-Warning "Temporary guest configuration cleanup failed: $(Protect-Text $_.Exception.Message)"
            }
        }
    }
}

function Save-RunCommandLogs {
    param([string] $TestName)
    $blobName = "logs-$TestName-$([DateTime]::UtcNow.ToString('yyyyMMddTHHmmss', [Globalization.CultureInfo]::InvariantCulture)).tar.gz"
    $path = Join-Path $script:OutputDirectory $blobName
    $previousDeadline = $script:OperationDeadlineUtc
    $script:OperationDeadlineUtc = [DateTime]::UtcNow.AddSeconds(600)
    try {
        Invoke-CustomScript -Action collect -Configuration @{ uploadUri = (New-BlobSas $blobName 'cw'); blobName = $blobName }
        Receive-Blob $blobName $path
        Write-Step "RunCommand diagnostic logs saved to: $path" Yellow
    }
    finally { $script:OperationDeadlineUtc = $previousDeadline }
}

function Assert-Equal {
    param($Actual, $Expected, [string] $Description)
    if ($Actual -cne $Expected) {
        throw "$Description`: expected '$Expected', received '$Actual'."
    }
}

function Assert-Contains {
    param([AllowNull()][string] $Actual, [string] $Expected, [string] $Description)
    if ($null -eq $Actual -or -not $Actual.Contains($Expected)) {
        throw "$Description`: expected output containing '$Expected'. Received: $Actual"
    }
}

function Assert-Execution {
    param([hashtable] $Result, [int] $ExitCode = 0, [string] $OperationState = 'Succeeded')
    Assert-Equal $Result.OperationState $OperationState 'CRP operation state'
    Assert-Equal $Result.ProvisioningState $OperationState 'RunCommand provisioning state'
    if ($null -eq $Result.View.exitCode) { throw 'RunCommand instance view has no exit code.' }
    Assert-Equal ([int]$Result.View.exitCode) $ExitCode 'Script exit code'
}

function Test-BlobOutput {
    param([string] $Name, [string] $Expected)
    $path = Join-Path $script:PrivateDirectory "$Name.txt"
    try {
        Wait-Condition -Activity "waiting for the expected RunCommand output in append blob $Name" -TimeoutSeconds 90 -Probe {
            Receive-Blob $Name $path
            ([IO.File]::ReadAllText($path)).Contains($Expected)
        }
    }
    finally {
        Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue
    }
}

function Invoke-TestCase {
    param([string] $Name, [scriptblock] $Test)
    Write-Step "TEST $Name"
    $timer = [Diagnostics.Stopwatch]::StartNew()
    $result = @{ Name = $Name; Passed = $false; Error = $null; Seconds = 0 }
    $timeoutFailure = $null
    try {
        & $Test
        $result.Passed = $true
        Write-Step "PASS $Name" Green
    }
    catch {
        if ($_.Exception -is [TimeoutException]) { $timeoutFailure = $_ }
        $result.Error = Protect-Text $_.Exception.Message
        Write-Step "FAIL $Name`: $($result.Error)" Red
        try { Save-RunCommandLogs $Name }
        catch {
            Write-Warning "CustomScript log collection also failed: $(Protect-Text $_.Exception.Message)"
            $result.LogCollectionError = Protect-Text $_.Exception.Message
        }
        if ($script:PendingRunCommand) {
            try {
                $null = Invoke-Az @('vm', 'run-command', 'delete', '-g', $script:ResourceGroup,
                    '--vm-name', $script:VmName, '--name', $script:PendingRunCommand.Name, '--yes') `
                    'deleting the incomplete RunCommand after log collection' -Output none -TimeoutSeconds 60
            }
            catch { Write-Warning "Incomplete RunCommand cleanup failed: $(Protect-Text $_.Exception.Message)" }
            finally { $script:PendingRunCommand = $null }
            if (-not $timeoutFailure) {
                $timeoutFailure = [InvalidOperationException]::new('An incomplete RunCommand failed; aborting further tests to avoid queued VM operations.')
            }
        }
    }
    finally {
        $result.Seconds = [math]::Round($timer.Elapsed.TotalSeconds, 1)
        $script:Results.Add($result)
        Write-JsonFile (Join-Path $script:OutputDirectory 'results.json') @($script:Results.ToArray())
    }
    if ($timeoutFailure) { throw $timeoutFailure }
}

function Invoke-TestSuite {
    param([bool] $Immediate, [hashtable] $Hashes)
    $mode = if ($Immediate) { 'immediate' } else { 'traditional' }
    Write-Step "Verifying both installed $mode RunCommand binaries against the local build before starting the suite."
    Invoke-TestCase "$mode-buddy-build-preflight" {
        Invoke-CustomScript verify @{ hashes = $Hashes; requireService = $Immediate }
    }
    if (-not $script:Results[-1].Passed) {
        throw "The $mode buddy-build preflight failed; refusing to run the suite against unverified binaries."
    }
    Write-Step "Running the $mode RunCommand suite."
    $outputName = "$mode-output.txt"
    $errorName = "$mode-error.txt"
    $emptyFile = Join-Path $script:PrivateDirectory 'empty'
    [IO.File]::WriteAllBytes($emptyFile, [byte[]]@())
    Send-Blob $emptyFile $outputName append
    Send-Blob $emptyFile $errorName append
    $outputUri = New-BlobSas $outputName 'racw'
    $errorUri = New-BlobSas $errorName 'racw'

    Invoke-TestCase "$mode-inline" {
        $result = Invoke-RunCommand "$mode-inline" @{
            source = @{ script = 'printf "HelloWorldCurrentUser:%s\n" "$(id -un)"' }
            timeoutInSeconds = 30
        } $Immediate -ExpectedOutput 'HelloWorldCurrentUser:root'
        Assert-Execution $result
        Assert-Contains $result.View.output 'HelloWorldCurrentUser:root' 'Inline script output'
    }
    Invoke-TestCase "$mode-script-uri-parameters" {
        $expected = "CurrentUser:root`nLinuxScript`nChipmunk:Ralph`nSquirrel:Iggy`nSource:NewYorkCity`nDestination:Seattle`nNamelessArgument:MilkyWay"
        $result = Invoke-RunCommand "$mode-parameters" @{
            source = @{ scriptUri = $script:SourceScriptUri }
            parameters = @(@{ name = 'CHIPMUNK'; value = 'Ralph' }, @{ name = 'SQUIRREL'; value = 'Iggy' })
            protectedParameters = @(
                @{ name = 'Source'; value = 'NewYorkCity' }, @{ name = 'Destination'; value = 'Seattle' },
                @{ name = ''; value = 'MilkyWay' }
            )
            outputBlobUri = $outputUri
            timeoutInSeconds = 30
        } $Immediate -ExpectedOutput $expected
        Assert-Execution $result
        Assert-Contains ($result.View.output -replace "`r", '') $expected 'Parameterized script output'
        Test-BlobOutput $outputName $expected
    }
    Invoke-TestCase "$mode-error-blob" {
        $result = Invoke-RunCommand "$mode-error" @{
            source = @{ scriptUri = $script:ErrorScriptUri }
            errorBlobUri = $errorUri
            timeoutInSeconds = 30
        } $Immediate -ExpectedExitCode 1 -ExpectedOutput 'linux error script' -ExpectedError 'there is an angry chipmunk'
        Assert-Execution $result 1
        Assert-Contains $result.View.output 'linux error script' 'Error script stdout'
        Assert-Contains $result.View.error 'there is an angry chipmunk' 'Error script stderr'
        Test-BlobOutput $errorName 'there is an angry chipmunk'
    }
    Invoke-TestCase "$mode-deployment-failure-false" {
        $result = Invoke-RunCommand "$mode-failure-false" @{
            source = @{ scriptUri = $script:ErrorScriptUri }
            treatFailureAsDeploymentFailure = $false
            timeoutInSeconds = 30
        } $Immediate -ExpectedExitCode 1 -ExpectedOutput 'linux error script' -ExpectedError 'there is an angry chipmunk'
        Assert-Execution $result 1
    }
    Invoke-TestCase "$mode-deployment-failure-true" {
        $result = Invoke-RunCommand "$mode-failure-true" @{
            source = @{ scriptUri = $script:ErrorScriptUri }
            treatFailureAsDeploymentFailure = $true
            timeoutInSeconds = 30
        } $Immediate -ExpectedExitCode 1 -ExpectedOutput 'linux error script' -ExpectedError 'there is an angry chipmunk'
        Assert-Execution $result 1 'Failed'
    }
    Write-Step "Removing the intentional deployment-failure RunCommand $mode-failure-true before further VM operations."
    $null = Invoke-Az @('vm', 'run-command', 'delete', '-g', $script:ResourceGroup,
        '--vm-name', $script:VmName, '--name', "$mode-failure-true", '--yes') `
        "waiting for RunCommand $mode-failure-true deletion and guest cleanup" -Output none
    Invoke-TestCase "$mode-command-id" {
        $result = Invoke-RunCommand "$mode-command-id" @{
            source = @{ commandId = 'RunShellScript' }
            timeoutInSeconds = 30
        } $Immediate
        Assert-Execution $result
    }
    Invoke-TestCase "$mode-run-as-user" {
        $result = Invoke-RunCommand "$mode-run-as-user" @{
            source = @{ script = 'printf "HelloWorldCurrentUser:%s\n" "$(id -un)"' }
            runAsUser = 'rcvalidation'
            runAsPassword = $script:RunAsPassword
            timeoutInSeconds = 30
        } $Immediate -ExpectedOutput 'HelloWorldCurrentUser:rcvalidation'
        Assert-Execution $result
        Assert-Contains $result.View.output 'HelloWorldCurrentUser:rcvalidation' 'RunAs user identity'
    }
    Invoke-TestCase "$mode-timeout" {
        $result = Invoke-RunCommand "$mode-timeout" @{
            source = @{ script = 'echo HelloWorld1; sleep 10; echo HelloWorld2' }
            timeoutInSeconds = 5
        } $Immediate -ExpectNonzeroExitCode -ExpectedOutput 'HelloWorld1'
        Assert-Equal $result.OperationState 'Succeeded' 'Timeout CRP operation state'
        Assert-Equal $result.ProvisioningState 'Succeeded' 'Timeout provisioning state'
        if ($null -eq $result.View.exitCode -or [int]$result.View.exitCode -eq 0) {
            throw "Expected a timed-out/failed execution with a nonzero exit code: $($result.View | ConvertTo-Json -Compress)"
        }
        Assert-Contains $result.View.output 'HelloWorld1' 'Output before timeout'
        if ($result.View.output.Contains('HelloWorld2')) { throw 'The script continued beyond its timeout.' }
    }
    Invoke-TestCase "$mode-buddy-build-integrity" {
        Invoke-CustomScript verify @{ hashes = $Hashes; requireService = $Immediate }
    }
}

function Remove-TestResourceGroup {
    Write-Step "Deleting resource group $($script:ResourceGroup) and all its resources."
    $null = Invoke-Az -Arguments @('group', 'delete', '--name', $script:ResourceGroup, '--yes', '--no-wait') `
        -Activity 'requesting test resource-group deletion' -Output none
    Wait-Condition -Activity "waiting for resource group $($script:ResourceGroup) and all contained resources to be deleted" `
        -TimeoutSeconds 2400 -IntervalSeconds 15 -Probe {
            (Invoke-Az -Arguments @('group', 'exists', '--name', $script:ResourceGroup) `
                -Activity 'checking resource-group deletion' -Output tsv -TimeoutSeconds 420) -eq 'false'
        }
}

function Build-RunCommandBinaries {
    param([string] $RepositoryRoot)
    Write-Step "Building local Linux/amd64 binaries. Results directory: $($script:OutputDirectory)"
    $oldEnvironment = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
    try {
        $env:GOOS = 'linux'
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '0'
        [xml]$manifest = Get-Content -LiteralPath (Join-Path $RepositoryRoot 'misc\manifest.xml') -Raw
        $version = [string]$manifest.ExtensionImage.Version
        Push-Location $RepositoryRoot
        try {
            foreach ($build in @(
                @('run-command-handler', '.\cmd\main'),
                @('immediate-run-command-handler', '.\cmd\immediateruncommandservice')
            )) {
                $null = Invoke-Native go @(
                    'build', '-tags', 'netgo,osusergo', '-ldflags', "-X main.Version=$version",
                    '-o', (Join-Path $script:OutputDirectory $build[0]), $build[1]
                ) "building Linux $($build[0])"
                Write-Step "Built $($build[0])." Green
            }
        }
        finally { Pop-Location }
    }
    finally {
        foreach ($key in $oldEnvironment.Keys) {
            if ($null -eq $oldEnvironment[$key]) {
                Remove-Item -LiteralPath "Env:\$key" -ErrorAction SilentlyContinue
            }
            else {
                [Environment]::SetEnvironmentVariable($key, $oldEnvironment[$key], 'Process')
            }
        }
    }
}

function Start-RunCommandValidation {
    $script:Secrets = [Collections.Generic.List[string]]::new()
    $script:Results = [Collections.Generic.List[hashtable]]::new()
    $script:SubscriptionId = ''
    $repo = Split-Path -Parent $PSScriptRoot
    $script:OutputDirectory = Join-Path $repo "bin\functional-validation\$([DateTime]::UtcNow.ToString('yyyyMMddTHHmmss', [Globalization.CultureInfo]::InvariantCulture))-$([guid]::NewGuid().ToString('N').Substring(0, 6))"
    $script:PrivateDirectory = Join-Path ([IO.Path]::GetTempPath()) "runcommand-validation-$([guid]::NewGuid().ToString('N'))"
    $null = New-Item -ItemType Directory -Path $script:OutputDirectory, $script:PrivateDirectory
    $ownsGroup = $false
    $lock = $null
    $instance = $null
    $failure = $null
    try {
        foreach ($tool in @('go', 'az', 'Start-ThreadJob')) {
            $null = Get-Command $tool -ErrorAction Stop
        }
        $instance = Enter-ValidationInstance
        Build-RunCommandBinaries $repo
        $account = Get-LoggedInAzureAccount
        $script:SubscriptionId = $account.id
        $ownerBytes = [Text.Encoding]::UTF8.GetBytes("$($account.user.name)|$([Environment]::MachineName)")
        $owner = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($ownerBytes)).Substring(0, 12).ToLowerInvariant()
        $script:ResourceGroup = "rg-runcommand-local-validation-$owner"
        $script:VmName = 'runcommand-validation'
        $script:Location = $Region.ToLowerInvariant()
        $script:VmId = "/subscriptions/$($script:SubscriptionId)/resourceGroups/$($script:ResourceGroup)/providers/Microsoft.Compute/virtualMachines/$($script:VmName)"
        Write-Step "Validation target: subscriptionId=$($script:SubscriptionId) ($($account.name)); resourceGroup=$($script:ResourceGroup); vmName=$($script:VmName); region=$Region." Magenta
        Write-Step 'This run creates billable Azure resources and deletes any previous resources owned by this harness.' Yellow
        $lockPath = Join-Path ([IO.Path]::GetTempPath()) "$($script:SubscriptionId)-$($script:ResourceGroup).lock"
        $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        $exists = Invoke-Az @('group', 'exists', '--name', $script:ResourceGroup) 'checking for the previous test resource group' -Output tsv
        if ($exists -eq 'true') {
            $previous = Invoke-Az @('group', 'show', '--name', $script:ResourceGroup) 'checking previous resource-group ownership'
            if (-not $previous['tags'] -or $previous['tags']['RunCommandValidationOwner'] -ne $owner) {
                throw "Refusing to delete resource group $($script:ResourceGroup): its harness ownership tag does not match."
            }
            Remove-TestResourceGroup
        }
        Write-Step "Creating temporary resource group in $Region."
        # Set before the request so an interrupted/partially successful create is cleaned up.
        $ownsGroup = $true
        $null = Invoke-Az @('group', 'create', '--name', $script:ResourceGroup, '--location', $script:Location,
            '--tags', "RunCommandValidationOwner=$owner", 'Purpose=RunCommandLocalValidation') 'creating the test resource group'
        $script:StorageName = "rcval$owner$([guid]::NewGuid().ToString('N').Substring(0, 7))"
        Write-Step 'Creating storage with anonymous blob access and shared-key authentication disabled.'
        $storageTemplate = @{
            '$schema' = 'https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#'
            contentVersion = '1.0.0.0'
            resources = @(@{
                type = 'Microsoft.Storage/storageAccounts'; apiVersion = '2023-01-01'
                name = $script:StorageName; location = $script:Location
                sku = @{ name = 'Standard_LRS' }; kind = 'StorageV2'
                properties = @{
                    allowSharedKeyAccess = $false; allowBlobPublicAccess = $false
                    defaultToOAuthAuthentication = $true; supportsHttpsTrafficOnly = $true
                    minimumTlsVersion = 'TLS1_2'
                }
            })
        }
        $storagePath = Join-Path $script:PrivateDirectory 'storage.json'
        Write-JsonFile $storagePath $storageTemplate
        $storageCorrelationId = Invoke-Az @('deployment', 'group', 'create', '-g', $script:ResourceGroup, '-n', 'validation-storage',
            '--template-file', $storagePath, '--query', 'properties.correlationId') 'deploying the private-content storage account' -Output tsv
        Write-DeploymentCorrelationId 'validation-storage' $storageCorrelationId
        $objectId = $account.userObjectId
        $storageId = "/subscriptions/$($script:SubscriptionId)/resourceGroups/$($script:ResourceGroup)/providers/Microsoft.Storage/storageAccounts/$($script:StorageName)"
        $null = Invoke-Az @('role', 'assignment', 'create', '--assignee-object-id', $objectId,
            '--assignee-principal-type', 'User', '--role', 'ba92f5b4-2d11-453d-a403-e96b0029c9fe',
            '--scope', $storageId) 'granting the signed-in user Storage Blob Data Contributor' -Output none
        Write-Step 'Waiting for storage RBAC propagation; authorization retries are suppressed.'
        $null = Invoke-Az @('storage', 'container', 'create', '--account-name', $script:StorageName,
            '--name', 'validation', '--public-access', 'off', '--auth-mode', 'login') `
            'creating the private container while waiting for storage authorization' -Output none `
            -RetryStorageAuthorization -TimeoutSeconds 120
        $script:BinaryUris = @()
        $hashes = @{}
        foreach ($name in @('run-command-handler', 'immediate-run-command-handler')) {
            $path = Join-Path $script:OutputDirectory $name
            $hashes[$name] = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
            Send-Blob $path $name
            $script:BinaryUris += New-BlobSas $name
        }
        $guestPath = Join-Path $script:PrivateDirectory 'Invoke-RunCommandGuest.sh'
        $guestText = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'Invoke-RunCommandGuest.sh'))
        [IO.File]::WriteAllText($guestPath, ($guestText -replace "`r", ''))
        Send-Blob $guestPath 'Invoke-RunCommandGuest.sh'
        $script:GuestScriptUri = New-BlobSas 'Invoke-RunCommandGuest.sh'
        $source = @'
#!/bin/sh
set -eu
printf 'CurrentUser:%s\nLinuxScript\n' "$(id -un)"
printf 'Chipmunk:%s\nSquirrel:%s\nSource:%s\nDestination:%s\nNamelessArgument:%s\n' "$CHIPMUNK" "$SQUIRREL" "$Source" "$Destination" "$1"
'@
        $sourcePath = Join-Path $script:PrivateDirectory 'LinuxScript.sh'
        [IO.File]::WriteAllText($sourcePath, ($source -replace "`r", '') + "`n")
        Send-Blob $sourcePath 'LinuxScript.sh'
        $script:SourceScriptUri = New-BlobSas 'LinuxScript.sh'
        $errorPath = Join-Path $script:PrivateDirectory 'LinuxErrorScript.sh'
        [IO.File]::WriteAllText($errorPath, "#!/bin/sh`necho 'linux error script'`necho 'there is an angry chipmunk' >&2`nexit 1`n")
        Send-Blob $errorPath 'LinuxErrorScript.sh'
        $script:ErrorScriptUri = New-BlobSas 'LinuxErrorScript.sh'
        $script:RunAsPassword = 'Rc!' + [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(20)) + 'a7'
        $adminPassword = 'Rc!' + [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(20)) + 'a7'
        $script:Secrets.Add($script:RunAsPassword)
        $script:Secrets.Add($adminPassword)
        Write-Step 'Creating a private subnet with default outbound access explicitly disabled.'
        $vnetName = "$($script:VmName)-vnet"
        $subnetName = 'validation'
        $nicName = "$($script:VmName)-nic"
        $nsgName = "$($script:VmName)-nsg"
        $publicIpName = 'runcommand-validation-ip'
        $networkTemplate = @{
            '$schema' = 'https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#'
            contentVersion = '1.0.0.0'
            resources = @(@{
                type = 'Microsoft.Network/virtualNetworks'; apiVersion = '2024-05-01'
                name = $vnetName; location = $script:Location
                properties = @{
                    addressSpace = @{ addressPrefixes = @('10.77.0.0/16') }
                    subnets = @(@{
                        name = $subnetName
                        properties = @{
                            addressPrefix = '10.77.0.0/24'
                            defaultOutboundAccess = $false
                        }
                    })
                }
            }, @{
                type = 'Microsoft.Network/publicIPAddresses'; apiVersion = '2024-05-01'
                name = $publicIpName; location = $script:Location
                sku = @{ name = 'Standard' }
                properties = @{ publicIPAllocationMethod = 'Static' }
            }, @{
                type = 'Microsoft.Network/networkSecurityGroups'; apiVersion = '2024-05-01'
                name = $nsgName; location = $script:Location
                properties = @{ securityRules = @() }
            }, @{
                type = 'Microsoft.Network/networkInterfaces'; apiVersion = '2024-05-01'
                name = $nicName; location = $script:Location
                dependsOn = @(
                    "[resourceId('Microsoft.Network/virtualNetworks', '$vnetName')]"
                    "[resourceId('Microsoft.Network/publicIPAddresses', '$publicIpName')]"
                    "[resourceId('Microsoft.Network/networkSecurityGroups', '$nsgName')]"
                )
                properties = @{
                    networkSecurityGroup = @{ id = "[resourceId('Microsoft.Network/networkSecurityGroups', '$nsgName')]" }
                    ipConfigurations = @(@{
                        name = 'primary'
                        properties = @{
                            privateIPAllocationMethod = 'Dynamic'
                            subnet = @{ id = "[resourceId('Microsoft.Network/virtualNetworks/subnets', '$vnetName', '$subnetName')]" }
                            publicIPAddress = @{ id = "[resourceId('Microsoft.Network/publicIPAddresses', '$publicIpName')]" }
                        }
                    })
                }
            })
        }
        $networkPath = Join-Path $script:PrivateDirectory 'network.json'
        Write-JsonFile $networkPath $networkTemplate
        $networkCorrelationId = Invoke-Az @('deployment', 'group', 'create', '-g', $script:ResourceGroup, '-n', 'validation-network',
            '--template-file', $networkPath, '--query', 'properties.correlationId') 'deploying the policy-compliant virtual network and private subnet' -Output tsv
        Write-DeploymentCorrelationId 'validation-network' $networkCorrelationId
        $vmTemplate = @{
            '$schema' = 'https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#'
            contentVersion = '1.0.0.0'
            parameters = @{ adminPassword = @{ type = 'secureString' } }
            resources = @(@{
                type = 'Microsoft.Compute/virtualMachines'; apiVersion = '2024-07-01'
                name = $script:VmName; location = $script:Location
                properties = @{
                    installRunCommandAsService = $true
                    hardwareProfile = @{ vmSize = $VmSize }
                    osProfile = @{
                        computerName = $script:VmName; adminUsername = 'rcadmin'
                        adminPassword = "[parameters('adminPassword')]"
                        linuxConfiguration = @{ disablePasswordAuthentication = $false; provisionVMAgent = $true }
                    }
                    storageProfile = @{
                        imageReference = @{
                            publisher = 'Canonical'; offer = '0001-com-ubuntu-server-jammy'
                            sku = '22_04-lts-gen2'; version = 'latest'
                        }
                        osDisk = @{ createOption = 'FromImage'; caching = 'ReadWrite'; managedDisk = @{ storageAccountType = 'Premium_LRS' } }
                    }
                    networkProfile = @{
                        networkInterfaces = @(@{
                            id = "/subscriptions/$($script:SubscriptionId)/resourceGroups/$($script:ResourceGroup)/providers/Microsoft.Network/networkInterfaces/$nicName"
                        })
                    }
                    securityProfile = @{ securityType = 'Standard' }
                }
            })
        }
        $vmPath = Join-Path $script:PrivateDirectory 'vm.json'
        $vmParametersPath = Join-Path $script:PrivateDirectory 'vm.parameters.json'
        Write-JsonFile $vmPath $vmTemplate
        Write-JsonFile $vmParametersPath @{ adminPassword = @{ value = $adminPassword } }
        Write-Step "Creating Ubuntu 22.04 Linux/amd64 VM ($VmSize) with immediate RunCommand support enabled. No inbound NSG ports will be opened."
        try {
            $vmCorrelationId = Invoke-Az @('deployment', 'group', 'create', '-g', $script:ResourceGroup, '-n', 'validation-vm',
                '--template-file', $vmPath, '--parameters', "@$vmParametersPath", '--query', 'properties.correlationId') `
                'provisioning the Linux VM with immediate RunCommand support and waiting for Azure VM creation' -Output tsv -TimeoutSeconds 2400
            Write-DeploymentCorrelationId 'validation-vm' $vmCorrelationId
        }
        finally { Remove-Item -LiteralPath $vmParametersPath -Force }
        Write-Step 'Bootstrapping managed RunCommand with ip addr (the Linux equivalent of ipconfig).'
        $bootstrap = Invoke-RunCommand 'bootstrap-network' @{ source = @{ script = 'ip addr' }; timeoutInSeconds = 30 } $false -ExpectedOutput 'inet'
        Assert-Execution $bootstrap
        Assert-Contains $bootstrap.View.output 'inet' 'Bootstrap network output'
        Write-Step 'Using VMAccessForLinux to add the RunAs test user rcvalidation.'
        $accessPath = Join-Path $script:PrivateDirectory 'vmaccess.json'
        Write-JsonFile $accessPath @{ username = 'rcvalidation'; password = $script:RunAsPassword }
        try {
            $null = Invoke-Az @('vm', 'extension', 'set', '-g', $script:ResourceGroup, '--vm-name', $script:VmName,
                '--name', 'VMAccessForLinux', '--publisher', 'Microsoft.OSTCExtensions', '--version', '1.5',
                '--protected-settings', "@$accessPath") 'waiting for VMAccess to create the RunAs user' -Output none
        }
        finally { Remove-Item -LiteralPath $accessPath -Force }
        Invoke-CustomScript wait-service
        Invoke-CustomScript install @{ hashes = $hashes; requireService = $true }
        Invoke-TestSuite $false $hashes
        Invoke-TestSuite $true $hashes
        $failed = @($script:Results | Where-Object { -not $_.Passed })
        if ($failed.Count -gt 0) { throw "$($failed.Count) RunCommand functional test(s) failed." }
        Write-Step "All $($script:Results.Count) functional tests passed." Green
    }
    catch {
        $failure = $_
        Write-Step (Protect-Text $_.Exception.Message) Red
    }
    finally {
        if ($ownsGroup) {
            if ($KeepResources) {
                Write-Step "Keeping resource group $($script:ResourceGroup) for debugging. Charges continue; the next run will delete it." Yellow
            }
            else {
                try { Remove-TestResourceGroup }
                catch {
                    Write-Step "RESOURCE CLEANUP FAILED: $(Protect-Text $_.Exception.Message). Delete $($script:ResourceGroup) manually." Red
                    if (-not $failure) { $failure = $_ }
                }
            }
        }
        if ($lock) { $lock.Dispose() }
        try {
            Write-JsonFile (Join-Path $script:OutputDirectory 'results.json') @($script:Results.ToArray())
            Write-Step "Results and built binaries: $($script:OutputDirectory)"
            $script:Secrets.Clear()
            if (Test-Path -LiteralPath $script:PrivateDirectory) {
                Remove-Item -LiteralPath $script:PrivateDirectory -Recurse -Force
            }
        }
        finally {
            if ($instance) { $instance.Dispose() }
        }
    }
    if ($failure) { throw $failure }
}

if ($MyInvocation.InvocationName -ne '.') {
    Start-RunCommandValidation
}
