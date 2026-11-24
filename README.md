# Azure RunCommand Extension Handler for Linux (1.3.0) 

RunCommand extension handler runs scripts on VMs send by deploying a RunCommand resource to a Linux VM.
These scripts can be used to bootstrap/install software, run administrative tasks, or run
automation tasks. RunCommand can run an inline script you specify or
download a script file from the Internet or Azure Storage.

## Immediate status reporting

The immediate service uses each goal state's top-level `name` as the handler
identity in `aggregateHandlerImmediateStatus`. Production and EDP test handler
results are grouped separately, including progress, completion, failure, and
skipped-command reports.

Immediate commands defer requested cleanup until final status reporting has been
attempted. This keeps command settings available when evaluating
`treatFailureAsDeploymentFailure`. Traditional-command cleanup ordering is unchanged.

The handler identity is retained in the local terminal-status cache for reporting
after a service restart. Legacy cache entries without a handler identity are
ignored with a warning rather than reported under an assumed handler name.

## Error clarification

RunCommand V2 reports classified failures using the Azure extension platform's
`ErrorWithClarification`. Positive clarification codes indicate customer input,
permissions, connectivity, policy, or VM problems. Negative codes indicate handler
or Azure infrastructure failures. Clarification codes are separate from script
exit codes and handler process exit codes.

Classification follows the failing operation's ownership, not just whether it
uses an OS API or reads JSON:

- Handler-owned filesystem, process records, sequence numbers, terminal-status
  caches, telemetry, and service lifecycle operations are system errors.
- Guest-agent/platform settings envelopes, certificate metadata, protected-settings
  encoding, and generated script types are system errors. Invalid customer-supplied
  settings field types, scripts, parameters, URLs, identity permissions, and policy
  violations remain user errors.
- An unsupported VM capability (such as missing systemd) is a user error.
  Shared CustomScript classifications retain their published signs and values,
  including `FileDownload_unableToCreateDownloadDirectory` (`50`).

The ownership correction changes formerly positive RunCommand-specific system
codes to negative values while preserving their absolute numbers (for example,
data-directory creation `5400` becomes `-5400`, and invalid terminal-state JSON
`5208` becomes `-5208`). The retired positive values must not be reused.

Output-blob creation or replacement failures return exit code `-108`
(`ExitCode_OutputBlobCreateOrReplaceFailed`); error-blob creation or replacement
failures return `-109` (`ExitCode_ErrorBlobCreateOrReplaceFailed`). Once blobs
are created, periodic and final appends are best effort: failures do not change
the command result or exit code. Only the first append failure per command is
logged and written as an extension error event, across both output and error
blobs. Subsequent appends are still attempted.

Managed and immediate status reports include a `substatus` entry with
`name: "ErrorClarification"`, `status: "error"`, and the numeric `code`. The
existing instance-view message, output, script exit code, and
`treatFailureAsDeploymentFailure` behavior are preserved. A failed script can
therefore have a successful top-level deployment status and still carry its
error clarification.

Immediate commands retain the script's exit code and full terminal instance view.
When execution or reporting produces an `ImmediateCommandExecutionError`, the task
returns `ExitCode_ImmediateTaskFailed`; this does not replace the script's exit code
in the retained instance view.
An execution result already reported to the observer is not replaced by a fallback
status. If terminal reporting fails, the fallback retries the original instance
view and top-level status. Script failures carry the positive
`CommandExecution_failureExitCode` clarification, not a system-error code.

Codes are declared centrally as named `ErrorClarificationCode` constants in
[errorclarificationcodes.go](pkg/errorutil/errorclarificationcodes.go), grouped
into system and user errors like CustomScript. Constant names describe the failure
or subsystem, not its user/system classification; classification is carried by
the numeric sign. Shared classifications retain CustomScript's numeric values,
but use classification-neutral names. Additional RunCommand failure sites have
distinct constants to preserve source-location uniqueness.

Each code is allocated to exactly one production classification site. Apart from
the ownership correction above, codes must not be reused or renumbered.
Higher layers add context without replacing a code
from a lower layer. The shared [error helper](pkg/errorutil/errors.go) preserves
both pointer and value platform errors, wrapped causes, and joined errors. Tests
check constant-value uniqueness, named usage at every classification site, test
references or unreachable-path explanations, and CustomScript compatibility.

The allocation ranges are:

| Absolute code range | Area |
| --- | --- |
| CustomScript values | Shared execution, input, download, identity, policy, and OS classifications |
| 1000-1999 | Download, blob/URL parsing, and preprocessing |
| 2000-2999 | Handler settings and extension policy |
| 3000-4999 | Lifecycle support, services, machine operations, and communication |
| 5000-5099 | Execution and RunAs |
| 5100-5199 | Instance-view serialization |
| 5200-5299 | Local and immediate status |
| 5300-5399 | Command processing |
| 5400-5499 | Command orchestration, upgrades, and output blobs |

Error-path tests inject filesystem, process, identity, and HTTP failures instead
of depending on the host configuration or Azure endpoints. Defensive paths that
cannot fail with the current concrete types are documented by code in the tests.
Tests expecting a production error must assert its exact named clarification code,
not only that an error occurred or that its message matches. Propagation tests
must verify the original lower-level code. Control-flow sentinels are explicitly
tested as unclassified rather than treated as failures.
Tests expecting a failed execution in status must also check the serialized
`ErrorClarification` substatus and its exact code, including when
`treatFailureAsDeploymentFailure` leaves the top-level status successful.

### Running tests on Windows

The handler runtime and full integration suite require Linux. Mocked clarification
tests can also run natively on Windows, including from VS Code:

```powershell
go test .\cmd\main -run TestCommandLineClarifications
```

Cleanup uses shared Go filesystem helpers on Linux and Windows; its error-path
tests inject filesystem failures. Only OS-specific primitives such as Linux
process-group signaling have separate implementations. The Windows signaling
default explicitly returns an unsupported-operation error; tests inject mocks.
Immediate-status tests that exercise persistence use a separate `t.TempDir()`
and `t.Setenv()` extension path per test, so existing local status files cannot
affect their results.

## Contributing

This project welcomes contributions and suggestions.  Most contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit https://cla.opensource.microsoft.com.

When you submit a pull request, a CLA bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., status check, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

## Trademarks

This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft 
trademarks or logos is subject to and must follow 
[Microsoft's Trademark & Brand Guidelines](https://www.microsoft.com/en-us/legal/intellectualproperty/trademarks/usage/general).
Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship.
Any use of third-party trademarks or logos are subject to those third-party's policies.
