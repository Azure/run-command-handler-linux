package commands

import (
	"bytes"
	"compress/gzip"
	"container/list"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/Azure/azure-extension-platform/pkg/extensionevents"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/run-command-handler-linux/internal/extensionpolicysettingsrc"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type mockCommandFile struct {
	bytes.Buffer
	entries []os.DirEntry
	dirErr  error
}

func (f *mockCommandFile) Close() error                       { return nil }
func (f *mockCommandFile) ReadDir(int) ([]os.DirEntry, error) { return f.entries, f.dirErr }

type mockCommandEntry string

func (e mockCommandEntry) Name() string               { return string(e) }
func (e mockCommandEntry) IsDir() bool                { return false }
func (e mockCommandEntry) Type() os.FileMode          { return 0 }
func (e mockCommandEntry) Info() (os.FileInfo, error) { return nil, nil }

func mockCommandDependencies(t *testing.T) {
	t.Helper()
	mkdirAll, mkdir, remove, removeAll := commandMkdirAll, commandMkdir, commandRemove, commandRemoveAll
	read, write, stat, copy, open, create := commandReadFile, commandWriteFile, commandStat, commandCopy, commandOpen, commandCreate
	install, uninstall, enable, settings := immediateInstall, immediateUninstall, immediateEnable, readCommandSettings
	readOutput, process, validate := readOutputFromPosition, processDownloadedScript, validateDownloadedScript
	credential, client, createBlob, appendSAS, appendManaged := newManagedIdentityCredential, newAppendBlobClient, createManagedAppendBlob, appendSASBlock, appendManagedBlock
	t.Cleanup(func() {
		commandMkdirAll, commandMkdir, commandRemove, commandRemoveAll = mkdirAll, mkdir, remove, removeAll
		commandReadFile, commandWriteFile, commandStat, commandCopy, commandOpen, commandCreate = read, write, stat, copy, open, create
		immediateInstall, immediateUninstall, immediateEnable, readCommandSettings = install, uninstall, enable, settings
		readOutputFromPosition, processDownloadedScript, validateDownloadedScript = readOutput, process, validate
		newManagedIdentityCredential, newAppendBlobClient, createManagedAppendBlob, appendSASBlock, appendManagedBlock = credential, client, createBlob, appendSAS, appendManaged
	})
	commandMkdirAll = func(string, os.FileMode) error { return nil }
	commandMkdir = func(string, os.FileMode) error { return nil }
	commandRemove = func(string) error { return nil }
	commandRemoveAll = func(string) error { return nil }
	commandReadFile = func(string) ([]byte, error) { return []byte("0"), nil }
	commandWriteFile = func(string, []byte, os.FileMode) error { return nil }
	commandStat = func(string) (os.FileInfo, error) { return nil, nil }
	commandCopy = func(io.Writer, io.Reader) (int64, error) { return 0, nil }
	commandOpen = func(string) (commandFile, error) { return &mockCommandFile{}, nil }
	commandCreate = func(string) (commandFile, error) { return &mockCommandFile{}, nil }
	immediateInstall = func() (int, error) { return 0, nil }
	immediateUninstall = func(*log.Context, types.HandlerEnvironment, string, int, *extensionevents.ExtensionEventManager) (int, error) {
		return 0, nil
	}
	immediateEnable = func(*log.Context, types.HandlerEnvironment, string, int, handlersettings.HandlerSettings, *extensionevents.ExtensionEventManager) (int, error) {
		return 0, nil
	}
	readCommandSettings = func(string, string, int, *log.Context) (handlersettings.HandlerSettings, error) {
		return handlersettings.HandlerSettings{}, nil
	}
	readOutputFromPosition = func(string, int64) ([]byte, error) { return []byte("output"), nil }
	processDownloadedScript = func(*log.Context, string, string, *handlersettings.HandlerSettings) (string, error) {
		return "/mock/script.sh", nil
	}
	validateDownloadedScript = func(string, *extensionpolicysettingsrc.RCv2ExtensionPolicySettings) error { return nil }
	newManagedIdentityCredential = func(*azidentity.ManagedIdentityCredentialOptions) (*azidentity.ManagedIdentityCredential, error) {
		return &azidentity.ManagedIdentityCredential{}, nil
	}
	newAppendBlobClient = func(string, azcore.TokenCredential, *appendblob.ClientOptions) (*appendblob.Client, error) {
		return &appendblob.Client{}, nil
	}
	createManagedAppendBlob = func(*appendblob.Client) error { return nil }
	appendSASBlock = func(*storage.Blob, []byte) error { return nil }
	appendManagedBlock = func(*appendblob.Client, []byte) error { return nil }
}

func requireCommandCode(t *testing.T, expected int, err error) {
	t.Helper()
	require.Error(t, err)
	actual, ok := errorutil.Code(err)
	require.True(t, ok, "unclassified error: %v", err)
	require.Equal(t, expected, actual)
}

func requireReportedClarification(t *testing.T, expected int, err error, report types.StatusReport) {
	t.Helper()
	requireCommandCode(t, expected, err)
	requireStatusClarification(t, expected, report)
}

func requireStatusClarification(t *testing.T, expected int, report types.StatusReport) {
	t.Helper()
	require.Positive(t, expected, "policy violations are customer errors")
	require.Len(t, report, 1)
	require.Len(t, report[0].Status.Substatuses, 1)
	require.Equal(t, "ErrorClarification", report[0].Status.Substatuses[0].Name)
	require.Equal(t, string(types.StatusError), report[0].Status.Substatuses[0].Status)
	require.Equal(t, expected, report[0].Status.Substatuses[0].Code)
}

func TestLifecycleClarifications(t *testing.T) {
	for _, code := range []int{errorutil.Os_FailedToCreateDataDir, errorutil.Os_FailedToDeleteDataDir, errorutil.Os_FailedToStatRuntimePolicy, errorutil.Storage_failedToAppendOutputWithSas, errorutil.Storage_failedToAppendOutputWithManagedIdentity, errorutil.Os_FailedToResetSequenceNumber, errorutil.FileDownload_unableToCreateDownloadDirectory, errorutil.Os_FailedToDeleteBlockedDownloadedScript, errorutil.ExtensionPolicySettings_downloadedScriptNotAllowed} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			mockCommandDependencies(t)
			failure := errors.New("injected machine/network failure")
			ctx := log.NewContext(log.NewNopLogger())
			var err error
			switch code {
			case errorutil.Os_FailedToCreateDataDir:
				commandMkdirAll = func(string, os.FileMode) error { return failure }
				_, _, err, _ = install(ctx, types.HandlerEnvironment{}, nil, types.RCMetadata{}, types.CmdInstallTemplate)
			case errorutil.Os_FailedToDeleteDataDir:
				commandRemoveAll = func(string) error { return failure }
				_, _, err, _ = uninstall(ctx, types.HandlerEnvironment{}, nil, types.RCMetadata{}, types.CmdUninstallTemplate)
			case errorutil.Os_FailedToStatRuntimePolicy:
				commandStat = func(string) (os.FileInfo, error) { return nil, failure }
				_, _, err, _ = enable(ctx, types.HandlerEnvironment{}, nil, types.RCMetadata{}, types.CmdEnableTemplate)
			case errorutil.Storage_failedToAppendOutputWithSas:
				appendSASBlock = func(*storage.Blob, []byte) error { return failure }
				_, err = appendToBlob("mock", &storage.Blob{}, nil, 0, ctx)
			case errorutil.Storage_failedToAppendOutputWithManagedIdentity:
				appendManagedBlock = func(*appendblob.Client, []byte) error { return failure }
				_, err = appendToBlob("mock", nil, &appendblob.Client{}, 0, ctx)
			case errorutil.Os_FailedToResetSequenceNumber:
				var output bytes.Buffer
				ctx = log.NewContext(log.NewLogfmtLogger(&output))
				commandRemove = func(string) error { return failure }
				resetSeqNum(ctx, "/mock/mrseq", createExtensionEventManager(ctx, types.HandlerEnvironment{}))
				require.Contains(t, output.String(), "clarificationCode="+strconv.Itoa(errorutil.Os_FailedToResetSequenceNumber))
				return
			case errorutil.FileDownload_unableToCreateDownloadDirectory:
				commandMkdirAll = func(string, os.FileMode) error { return failure }
				_, err = downloadScript(ctx, "/mock", &handlersettings.HandlerSettings{}, nil)
			case errorutil.Os_FailedToDeleteBlockedDownloadedScript, errorutil.ExtensionPolicySettings_downloadedScriptNotAllowed:
				cfg := handlersettings.HandlerSettings{}
				cfg.PublicSettings.Source = &handlersettings.ScriptSource{ScriptURI: "https://mock/script.sh"}
				validateDownloadedScript = func(string, *extensionpolicysettingsrc.RCv2ExtensionPolicySettings) error { return failure }
				var output bytes.Buffer
				ctx = log.NewContext(log.NewLogfmtLogger(&output))
				if code == errorutil.Os_FailedToDeleteBlockedDownloadedScript {
					commandRemove = func(string) error { return failure }
				}
				_, err = downloadScript(ctx, "/mock", &cfg, &extensionpolicysettingsrc.RCv2ExtensionPolicySettings{})
				if code == errorutil.Os_FailedToDeleteBlockedDownloadedScript {
					require.Contains(t, output.String(), "clarificationCode="+strconv.Itoa(errorutil.Os_FailedToDeleteBlockedDownloadedScript))
					requireCommandCode(t, errorutil.ExtensionPolicySettings_downloadedScriptNotAllowed, err)
					return
				}
			}
			requireCommandCode(t, code, err)
		})
	}
}

func TestRehydrationClarifications(t *testing.T) {
	for _, code := range []int{errorutil.Os_FailedToOpenRehydrationStatusDirectory, errorutil.Os_FailedToReadRehydrationStatusDirectory, errorutil.Internal_invalidStatusFileSequenceNumber, errorutil.Os_FailedToRehydrateSequenceNumber, errorutil.Os_FailedToStatRehydrationSequenceFile, errorutil.Os_FailedToReadRehydrationSequenceFile, errorutil.Internal_invalidMrseqFileSequenceNumber, errorutil.Os_FailedToUpdateRehydratedSequenceFile} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			mockCommandDependencies(t)
			failure := errors.New("injected failure")
			directory := &mockCommandFile{entries: []os.DirEntry{mockCommandEntry("command.5.status")}}
			commandOpen = func(string) (commandFile, error) { return directory, nil }
			switch code {
			case errorutil.Os_FailedToOpenRehydrationStatusDirectory:
				commandOpen = func(string) (commandFile, error) { return nil, failure }
			case errorutil.Os_FailedToReadRehydrationStatusDirectory:
				directory.dirErr = failure
			case errorutil.Internal_invalidStatusFileSequenceNumber:
				directory.entries = []os.DirEntry{mockCommandEntry("command.invalid.status")}
			case errorutil.Os_FailedToRehydrateSequenceNumber:
				commandStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
				commandWriteFile = func(string, []byte, os.FileMode) error { return failure }
			case errorutil.Os_FailedToStatRehydrationSequenceFile:
				commandStat = func(string) (os.FileInfo, error) { return nil, failure }
			case errorutil.Os_FailedToReadRehydrationSequenceFile:
				commandReadFile = func(string) ([]byte, error) { return nil, failure }
			case errorutil.Internal_invalidMrseqFileSequenceNumber:
				commandReadFile = func(string) ([]byte, error) { return []byte("invalid"), nil }
			case errorutil.Os_FailedToUpdateRehydratedSequenceFile:
				commandWriteFile = func(string, []byte, os.FileMode) error { return failure }
			}
			ctx := log.NewContext(log.NewNopLogger())
			err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, "/mock/old", "/mock/new", createExtensionEventManager(ctx, types.HandlerEnvironment{}))
			requireCommandCode(t, code, err)
		})
	}
}

func TestStateCopyClarifications(t *testing.T) {
	for _, code := range []int{errorutil.Os_FailedToCreateUpgradeStatusDirectory, errorutil.Internal_emptyUpgradeDirectories, errorutil.Os_FailedToOpenUpgradeSourceDirectory, errorutil.Os_FailedToReadUpgradeSourceDirectory, errorutil.Os_FailedToOpenUpgradeSourceFile, errorutil.Os_FailedToCreateUpgradeDestinationFile, errorutil.Os_FailedToCopyUpgradeStateFile} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			mockCommandDependencies(t)
			failure := errors.New("injected failure")
			subdir, source := "", "/mock/source"
			directory := &mockCommandFile{entries: []os.DirEntry{mockCommandEntry("command.mrseq")}}
			commandOpen = func(string) (commandFile, error) { return directory, nil }
			switch code {
			case errorutil.Os_FailedToCreateUpgradeStatusDirectory:
				subdir = "status"
				commandOpen = func(string) (commandFile, error) { return nil, failure }
				commandMkdir = func(string, os.FileMode) error { return failure }
			case errorutil.Internal_emptyUpgradeDirectories:
				source = ""
			case errorutil.Os_FailedToOpenUpgradeSourceDirectory:
				commandOpen = func(string) (commandFile, error) { return nil, failure }
			case errorutil.Os_FailedToReadUpgradeSourceDirectory:
				directory.dirErr = failure
			case errorutil.Os_FailedToOpenUpgradeSourceFile:
				calls := 0
				commandOpen = func(string) (commandFile, error) {
					calls++
					if calls == 2 {
						return nil, failure
					}
					return directory, nil
				}
			case errorutil.Os_FailedToCreateUpgradeDestinationFile:
				commandCreate = func(string) (commandFile, error) { return nil, failure }
			case errorutil.Os_FailedToCopyUpgradeStateFile:
				commandCopy = func(io.Writer, io.Reader) (int64, error) { return 0, failure }
			}
			ctx := log.NewContext(log.NewNopLogger())
			_, err := copyFiles(ctx, ".mrseq", subdir, source, "/mock/destination", createExtensionEventManager(ctx, types.HandlerEnvironment{}))
			requireCommandCode(t, code, err)
		})
	}
}

func TestDummyStatusClarifications(t *testing.T) {
	for _, code := range []int{errorutil.Os_FailedToReadDummyStatusSequenceFile, errorutil.Internal_invalidDummyStatusSequenceNumber, errorutil.Internal_emptyDummyStatusSequenceFile, errorutil.Internal_invalidDummyStatusSequenceFileName} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			mockCommandDependencies(t)
			filename := "command.mrseq"
			switch code {
			case errorutil.Os_FailedToReadDummyStatusSequenceFile:
				commandReadFile = func(string) ([]byte, error) { return nil, errors.New("read failed") }
			case errorutil.Internal_invalidDummyStatusSequenceNumber:
				commandReadFile = func(string) ([]byte, error) { return []byte("invalid"), nil }
			case errorutil.Internal_emptyDummyStatusSequenceFile:
				commandReadFile = func(string) ([]byte, error) { return nil, nil }
			case errorutil.Internal_invalidDummyStatusSequenceFileName:
				filename = "invalid"
			}
			names := list.New()
			names.PushBack(filename)
			ctx := log.NewContext(log.NewNopLogger())
			err := createDummyStatusFilesIfNeeded(ctx, names, createExtensionEventManager(ctx, types.HandlerEnvironment{}))
			requireCommandCode(t, code, err)
		})
	}
}

func TestScriptDecodeClarifications(t *testing.T) {
	_, _, err := decodeScript("not base64!")
	requireCommandCode(t, errorutil.CustomerInput_invalidEmbeddedScriptBase64, err)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err = writer.Write([]byte("script"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	truncated := compressed.Bytes()[:compressed.Len()-8]
	_, _, err = decodeScript(base64.StdEncoding.EncodeToString(truncated))
	requireCommandCode(t, errorutil.CustomerInput_invalidCompressedScript, err)
}

func TestManagedBlobClarifications(t *testing.T) {
	for _, code := range []int{errorutil.CustomerInput_outputIdentityObjectIdNotSupported, errorutil.Storage_failedToCreateManagedAppendBlobClient, errorutil.Storage_failedToCreateManagedAppendBlob, errorutil.Imds_failedToCreateManagedIdentityCredential} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			mockCommandDependencies(t)
			failure := errors.New("injected Azure failure")
			var identity *handlersettings.RunCommandManagedIdentity
			switch code {
			case errorutil.CustomerInput_outputIdentityObjectIdNotSupported:
				identity = &handlersettings.RunCommandManagedIdentity{ObjectId: "unsupported"}
			case errorutil.Storage_failedToCreateManagedAppendBlobClient:
				newAppendBlobClient = func(string, azcore.TokenCredential, *appendblob.ClientOptions) (*appendblob.Client, error) {
					return nil, failure
				}
			case errorutil.Storage_failedToCreateManagedAppendBlob:
				createManagedAppendBlob = func(*appendblob.Client) error { return failure }
			case errorutil.Imds_failedToCreateManagedIdentityCredential:
				newManagedIdentityCredential = func(*azidentity.ManagedIdentityCredentialOptions) (*azidentity.ManagedIdentityCredential, error) {
					return nil, failure
				}
			}
			_, err := createOrReplaceAppendBlobUsingManagedIdentity("https://mock/container/blob", identity)
			requireCommandCode(t, code, err)
		})
	}
}
