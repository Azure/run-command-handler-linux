# Local Linux VM functional validation

Run from the repository root in **PowerShell 7.3 or later**:

```powershell
az login
.\TestSripts\Test-RunCommand.ps1
.\TestSripts\Test-RunCommand.ps1 -Region eastus2euap -KeepResources
```

The default region is `CentralUSEUAP`, VM size is `Standard_D2s_v5`, and image is
Ubuntu 22.04 x64. `-VmSize` can select another **x64** size. The script builds both
Linux binaries on your machine and fails immediately on a build error. You must
already be signed in using `az login`; the script checks the cached account and
its ability to obtain an ARM token. If authentication is unavailable or expired,
it fails with an instruction to run `az login`. It never signs you in or switches
your default subscription.
After authentication, the subscription ID, resource group, VM name, and region
are printed together once in magenta so the validation target is easy to find.

Builds run from the repository root regardless of the caller's working directory.
The native launcher explicitly inherits PowerShell's current filesystem location,
including the build helper's `Push-Location`, rather than the host process's
original working directory.
Native arguments are read as JSON strings without automatic date conversion.
SAS expiry and machine-readable UTC timestamps use invariant Gregorian formatting,
so local language, calendar, and date/time separator settings do not change them.

## Requirements and resource ownership

- Windows, Go matching [go.mod](../go.mod), Azure CLI, and PowerShell 7.3 or later
  must be installed. Offline tests also use `Start-ThreadJob` for local fixtures.
- Your signed-in **user** must be allowed to provision Compute, Network, Storage,
  and ARM deployment resources and assign **Storage Blob Data Contributor** on
  the created storage account. Resource providers and regional VM quota must
  already be available. Ordinary Contributor alone cannot create role assignments.
- Immediate RunCommand must be supported for your subscription and region.
  The initial VM ARM deployment sets `installRunCommandAsService: true` using API
  `2024-07-01`, then uses direct RunCommand resource PUTs with
  `runImmediately: true`.
- Azure endpoints must be reachable from your machine and the VM.

**This script creates billable resources.** It uses a predictable resource group
named `rg-runcommand-local-validation-<user-and-machine-hash>` in the selected
subscription. Each run deletes the previous group before provisioning. An
ownership tag prevents deletion of an unrelated group with the same name.
Do not put unrelated resources in this group.

At startup the script terminates a previous registered run of this same script
and its native child processes, then takes over. **This terminates the PowerShell
host running that previous invocation**, including an interactive terminal host.
Process IDs and process start times are verified, and the current host and its
ancestors are protected. Other PowerShell sessions are not terminated. A held
registration file and startup mutex coordinate concurrent invocations, including
scripts launched from an interactive prompt; completed/stale registrations are
reusable without killing the old host. Older runs started using an absolute
`pwsh -File` path without `-NoExit` are also recognized. Pre-registration versions
launched inside an interactive/persistent shell cannot be identified safely and
must be stopped manually.

By default, the entire resource group is deleted on success or failure.
`-KeepResources` retains everything for debugging, including on failure; charges
continue until you delete it. **The next run still deletes the retained group.**
A forced PowerShell termination, machine shutdown, or Azure outage can prevent
cleanup; the next run removes that predictable group. Cleanup errors are
explicitly reported and make the script fail.

The script first deploys a VNet and a private subnet with
`defaultOutboundAccess: false`, complying with the **Subnets should be private**
policy instead of relying on automatically generated networking. The network
ARM deployment also creates the NIC, NSG, and public IP. A separate VM ARM
deployment references that NIC and enables immediate RunCommand at creation;
there is no later VM update to enable the service. The VM has a
Standard public IP for **explicit** outbound access; it does not rely on default
outbound connectivity. No inbound NSG ports are opened. Debugging can use Azure
tooling/CustomScript; SSH would require separately configuring credentials and a
narrowly scoped inbound rule. The VNet, subnet, NIC, NSG, and public IP are all
deleted with the test resource group.

## Storage and binary replacement

The account disables anonymous blob access **and shared-key authentication** and
defaults to OAuth. Its container is private. All local uploads/downloads use
`--auth-mode login`; the signed-in user receives Storage Blob Data Contributor
at account scope. Container creation, uploads, downloads, and user-delegation SAS
generation all tolerate intermittent storage authorization failures for up to
ten minutes per operation, rather than assuming successful container creation
guarantees every subsequent blob operation is authorized. The script prints one
notice and ongoing 60-second heartbeats, not each retry. Persistent failures
retain the Azure CLI error and fail explicitly; authentication/conditional-access
errors and unrelated failures are not retried. Temporary configuration deletion
has a shorter two-minute authorization window and is attempted only after a
successful upload. Shared-key authentication is never enabled as a fallback.
This matches the supplied storage authentication instructions.
No Microsoft Graph lookup or Graph token is required. The signed-in user's
object ID is read from the existing ARM access token's `oid` claim, with the
token tenant checked against the selected subscription tenant. This metadata
is used for the storage role assignment; Azure still authorizes each request.
The token is not printed or returned in account metadata, and temporary native
command output is removed. Resource operations remain pinned to the selected
subscription. Organizational authentication policies for ARM and Storage still
apply.

This is **authenticated storage over the public HTTPS endpoint**, not a
private-endpoint/network-isolated account. No anonymous access is enabled.
CustomScript receives four-hour, blob-scoped, HTTPS-only **user-delegation SAS**
URLs in protected settings. Source/binary downloads are read-only; command
output uses append blobs; diagnostic uploads get a separate create/write SAS.
RunCommand properties are sent in an authenticated HTTPS request body, without
creating ARM deployment templates or writing local request-body files.
Passwords/SAS URLs are not printed. The existing Azure CLI login supplies ARM
credentials; operation polling URLs must remain on the configured ARM endpoint,
and HTTP redirects are not automatically followed.

Bootstrap uses `ip addr`, the Linux equivalent of `ipconfig`. CustomScript
recognizes both handler names supported by this repository:
`Microsoft.CPlat.Core.RunCommandHandlerLinux` and
`Microsoft.Azure.Extensions.Edp.RunCommandHandlerLinuxTest`. It identifies the
active installed handler using the VM agent's
`config/HandlerState` (`Enabled`), so retained older versions and absent/stale XML
do not prevent replacement. If no enabled version is recorded, it uses the newest
cached XML configuration or a single binary directory without recorded state.
Known inactive handlers and multiple enabled versions are rejected; selection
errors list discovered directories, states, binary presence, searched handler
names, and any unrecognized RunCommand directory names for diagnosis. Unrecognized
names are reported, never selected for replacement.
CustomScript verifies
SHA-256 hashes, replaces both binaries using same-directory atomic renames,
and restarts the service if necessary. After waiting for the service enabled
at VM creation to be ready, it installs into the active directory and checks
the running process's hash.
Each suite verifies that the installed buddy-build hashes have not changed.
Before either suite starts, a required buddy-build preflight independently
compares both installed binaries with the SHA-256 hashes of the current local
build, rather than trusting only the guest installation marker. Immediate-mode
preflight also checks the running service executable. Failure records a failed
test, collects diagnostic logs, and stops before submitting suite commands.
An unsupported/unconfirmed immediate property is a failure, not a silent
fallback to traditional execution.

## Coverage and diagnostics

Live validation runs the traditional suite first, followed by the immediate suite
on the same VM. Immediate support is enabled at VM creation, and the local binaries
are installed before either suite runs. The traditional `ip addr` bootstrap remains
part of setup.

The same suite uses direct RunCommand resource PUTs for traditional and immediate
RunCommand, including bootstrap. Both call the Compute resource API through
`management.azure.com` with API version `2024-07-01`; there is no parent ARM
deployment to wait for. ARM templates remain in use for storage, network, and VM
setup. The VM admin password uses a `secureString` parameter supplied through a
temporary private parameter file, removed immediately after VM deployment.

- Inline script, successful execution, and root identity.
- Private script URI; named public/protected and unnamed parameters; stdout
  append-blob contents.
- Script exit code 1, stderr, and error append-blob contents.
- `treatFailureAsDeploymentFailure` false and true, checking deployment,
  provisioning, and execution results separately.
- Built-in `RunShellScript` command ID.
- A user created using **VMAccessForLinux**, with actual `id -un` RunAs validation.
- Five-second timeout of a ten-second script, nonzero exit, and no post-timeout
  output.
- Installed buddy-build integrity, including the running immediate service.

After asserting `treatFailureAsDeploymentFailure: true`, each suite deletes that
intentionally failed RunCommand and waits for deletion to finish. Leaving the
failed command installed can cause later VM updates, including service
enablement, to fail with its expected error. If assertions fail, diagnostic logs
are collected before deletion; a deletion failure aborts further VM operations
and is reported rather than ignored. Test results remain in `results.json`.

These are the applicable single-Linux-VM scenarios modeled on the supplied
synthetic. Windows, VMSS, and the separate Fleet Diagnostics resource/API are
not included.

Ordinary assertion failures do not stop the remaining cases. A RunCommand timeout
or incomplete deployment aborts the suite after bounded diagnostics and cleanup,
rather than queuing more commands against an unhealthy VM. Each failure triggers
CustomScript to collect handler/VM-agent logs, handler status, and the service
journal and upload a compressed archive. Handler-specific logs and status are
collected for both production and EDP test handlers. The script downloads it and prints its
local path **in yellow**, before resource teardown. Collection failure is also
reported without masking the original failure. Archives can contain sensitive
script output; keep them private.

Immediate status notifications also write a best-effort debug copy using the
traditional status-file format and location from `HandlerEnvironment.json`:
`<statusFolder>/<command-name>.<sequence-number>.status`. Progress and terminal
notifications update the same file; empty initial notifications do not overwrite
it. Failures to load the handler environment or write the copy are logged but do
not prevent HTTP status reporting or fail the goal state. HTTP reporting remains
authoritative. These per-command files and the existing immediate aggregate
terminal-status file are included in the collected status directories.

The collector includes `CommandExecution*.log` under both versioned and
unversioned production/EDP handler directories. The VM agent currently uses
`/var/log/azure/<handler-full-name>/CommandExecution.log`, normally without a
version suffix, and `CommandExecution_<command-name>.log` for multiconfig
execution (see [VM agent source](https://github.com/Azure/WALinuxAgent/blob/master/azurelinuxagent/ga/exthandlers.py)).
The handler also writes `handler.log` and `ImmediateRunCommandService.log`
under `/var/log/azure/run-command-handler`. Collection fails explicitly if no
execution logs exist; it does not report an empty diagnostic archive as success.
CSE collection is submitted with `--no-wait`, and the harness polls the private
diagnostic blob instead of waiting for a VM-wide update that can be held up by a
failed RunCommand. Collection submission has a five-minute cap, archive polling
has a four-minute cap, and the complete diagnostic operation (including storage)
has a ten-minute deadline.

Each RunCommand has one ten-minute completion budget spanning credential
retrieval, the PUT, CRP operation polling, and instance-view result polling.
Immediate RunCommands additionally have a 30-second result-polling deadline
starting when CRP operation polling completes (or when a synchronous PUT returns).
This also caps in-flight result GETs and never extends the original total
deadline. Traditional RunCommand timeouts and CRP operation-polling timeouts
are unchanged.
Individual HTTP requests are capped at seven minutes and at the remaining total
budget. The harness prefers `Azure-AsyncOperation`, otherwise uses `Location`,
honors `Retry-After`, and checks the final RunCommand instance view and provisioning
state even after operation completion. Instance-view polling waits for a terminal
provisioning state, the expected exit code (any nonzero code for the timeout case),
and each test's expected stdout/stderr markers where applicable. A default zero
exit code without the expected output is not enough. Tests still assert the
returned exit code, output/error, and CRP/provisioning states. A failed CRP operation
does not bypass polling while its resource result is still converging.
`executionState` is traced but does not
determine completion or pass/fail, so a value reset to `Pending` does not block
validation. Expected negative tests assert CRP
operation failure rather than ARM deployment failure. Each instance-view GET poll
logs its effective API version, HTTP status, provisioning state,
`runImmediately`, `instanceView.executionState`, and `instanceView.exitCode`,
with missing values shown as JSON `null`. It also logs output/error presence
(empty strings count as present), expected-output/error match booleans (true
when no marker is required), and an `unmetConditions` list identifying exactly
what is blocking completion. Neither actual nor expected output/error text is
logged in these summaries. Instance-view polling timeouts include
the last values (or null response fields if no GET completed). These summaries
do not include command output or the full instance view. A timeout cancels the
in-flight HTTP request, attempts CSE diagnostics, and deletes the incomplete
RunCommand (one-minute cap) before normal resource-group teardown. There is no
parent deployment to cancel. `-KeepResources` still retains the VM
and resource group. Host sleep/offline time cannot produce console heartbeats;
on resume, expired deadlines fail rather than restarting their budget.

For each RunCommand, the harness prints the CRP `operationId` from the returned
polling URL once, alongside the command name, in magenta. It uses that exact URL
for operation polling and returns the ID with the command result. Synchronous
responses with no polling URL explicitly report that no operationId was supplied;
an HTTP 202 without a polling URL is an error. Storage and network ARM deployments
still trace their own `properties.correlationId`. HTTP response `x-ms-request-id`
and `x-ms-correlation-request-id` headers are traced only for instance-view GETs.
Immediately before sending each instance-view GET, the harness validates that
the final `HttpRequestMessage.RequestUri` contains the literal
`$expand=instanceView` query parameter and fails explicitly if it does not.
It logs that exact absolute URI, requested HTTP version/policy, and a fresh
`x-ms-client-request-id` that is also sent as a request header. The response
trace includes the same client request ID, HTTP status, and the two server
request IDs (JSON `null` when absent). These traces enable correlation with
CRP logs without printing authorization headers or request bodies.
For temporary debugging, each instance-view GET also prints the complete response
body immediately after reading it and before any JSON parsing or reserialization,
tagged with the same client request ID. This includes malformed JSON responses.
**The raw response can contain stdout, stderr, and sensitive command output;
keep these console logs private.** Other ARM responses are not dumped.
These traces do not prove how downstream services forward or interpret the
query string.

Builds, `results.json`, and diagnostic archives are retained under the ignored
`bin\functional-validation\<run-id>` directory. Temporary local parameter and
protected-settings files are removed. A failed test or setup/cleanup error
terminates the script with an error.

Long-running native commands and polling waits report the exact current activity
at least every 60 seconds, including credential checks, builds, provisioning, extension
execution, tests, and deletion. Apart from the instance-view GET summaries and
their correlated request/response traces,
normal polling attempts are not printed.

## Offline validation

```powershell
.\TestSripts\Test-RunCommand.Offline.Tests.ps1
.\TestSripts\Test-RunCommand.Offline.Tests.ps1 -SkipBuild
```

These checks do **not** log into Azure or provision resources. They validate
native-command handling/heartbeats, real Azure CLI REST query forwarding to a
local loopback HTTP server without authentication, existing-login enforcement, safe takeover of
isolated test processes, ARM request shapes and secret handling, failure
assertions, diagnostic triggering, teardown requests, and actual Linux
cross-compilation unless skipped. They do not substitute for a live VM run.

Guest handler-selection and hashing regressions can also run locally with
Python 3.10 or later (no Azure access, VM agent, or service changes):

```powershell
python .\TestSripts\Test-RunCommandGuest.py
```

These execute the functions extracted from the actual embedded guest script,
including multiple installed versions, missing/stale XML, inactive and ambiguous
handlers, missing binaries, production/EDP names and diagnostic paths, and Python
3.10 syntax compatibility. Supported names are checked against the repository's
Go constants.
