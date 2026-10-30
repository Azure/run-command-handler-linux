package commands

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"container/list"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-extension-platform/pkg/extensionerrors"
	"github.com/Azure/azure-extension-platform/pkg/extensionevents"
	"github.com/Azure/azure-extension-platform/pkg/extensionpolicysettings"
	"github.com/Azure/azure-extension-platform/pkg/handlerenv"
	"github.com/Azure/azure-extension-platform/pkg/hashutils"
	"github.com/Azure/azure-extension-platform/pkg/logging"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/run-command-handler-linux/internal/cleanup"
	"github.com/Azure/run-command-handler-linux/internal/commandProcessor"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/exec"
	"github.com/Azure/run-command-handler-linux/internal/extensionpolicysettingsrc"
	"github.com/Azure/run-command-handler-linux/internal/files"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/immediatecmds"
	"github.com/Azure/run-command-handler-linux/internal/instanceview"
	"github.com/Azure/run-command-handler-linux/internal/pid"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/telemetry"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/download"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	seqnum "github.com/Azure/run-command-handler-linux/pkg/seqnumutil"
	"github.com/Azure/run-command-handler-linux/pkg/versionutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	maxScriptSize         = 256 * 1024
	updateStatusInSeconds = 15
)

const (
	fullName                = "Microsoft.Compute.CPlat.Core.RunCommandLinux"
	maxTailLen              = 4 * 1024 // length of max stdout/stderr to be transmitted in .status file
	maxTelemetryTailLen int = 1800
)

var (
	immediateInstall         = immediatecmds.Install
	immediateUninstall       = immediatecmds.Uninstall
	immediateEnable          = immediatecmds.Enable
	readCommandSettings      = handlersettings.GetHandlerSettings
	readOutputFromPosition   = files.GetFileFromPosition
	processDownloadedScript  = files.DownloadAndProcessScript
	validateDownloadedScript = func(path string, policy *extensionpolicysettingsrc.RCv2ExtensionPolicySettings) error {
		return extensionpolicysettings.ValidateFileHashInAllowlist(path, policy.DownloadedScriptsAllowlist, hashutils.HashTypeSHA256)
	}
	commandMkdirAll              = os.MkdirAll
	commandMkdir                 = os.Mkdir
	commandRemove                = os.Remove
	commandRemoveAll             = os.RemoveAll
	commandReadFile              = os.ReadFile
	commandWriteFile             = os.WriteFile
	commandStat                  = os.Stat
	commandCopy                  = io.Copy
	commandOpen                  = func(path string) (commandFile, error) { return os.Open(path) }
	commandCreate                = func(path string) (commandFile, error) { return os.Create(path) }
	newManagedIdentityCredential = azidentity.NewManagedIdentityCredential
	newAppendBlobClient          = appendblob.NewClient
	createManagedAppendBlob      = func(client *appendblob.Client) error {
		_, err := client.Create(context.Background(), nil)
		return err
	}
	appendSASBlock     = func(blob *storage.Blob, data []byte) error { return blob.AppendBlock(data, nil) }
	appendManagedBlock = func(client *appendblob.Client, data []byte) error {
		_, err := client.AppendBlock(context.Background(), streaming.NopCloser(bytes.NewReader(data)), nil)
		return err
	}
	cmdDefaultReportStatusFunc = status.ReportStatusToLocalFile
	cmdDefaultCleanupFunc      = cleanup.RunCommandCleanup
	telemetryResult            = telemetry.SendTelemetry(telemetry.NewTelemetryEventSender(), fullName, versionutil.Version)

	CmdInstall   = types.CmdInstallTemplate.InitializeFunctions(types.CmdFunctions{Invoke: install, Pre: nil, ReportStatus: cmdDefaultReportStatusFunc, Cleanup: cmdDefaultCleanupFunc})
	CmdEnable    = types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{Invoke: enable, Pre: enablePre, ReportStatus: cmdDefaultReportStatusFunc, Cleanup: cmdDefaultCleanupFunc})
	CmdDisable   = types.CmdDisableTemplate.InitializeFunctions(types.CmdFunctions{Invoke: disable, Pre: nil, ReportStatus: cmdDefaultReportStatusFunc, Cleanup: cmdDefaultCleanupFunc})
	CmdUpdate    = types.CmdUpdateTemplate.InitializeFunctions(types.CmdFunctions{Invoke: update, Pre: nil, ReportStatus: cmdDefaultReportStatusFunc, Cleanup: cmdDefaultCleanupFunc})
	CmdUninstall = types.CmdUninstallTemplate.InitializeFunctions(types.CmdFunctions{Invoke: uninstall, Pre: nil, ReportStatus: cmdDefaultReportStatusFunc, Cleanup: cmdDefaultCleanupFunc})

	Cmds = map[string]types.Cmd{
		"install":   CmdInstall,
		"enable":    CmdEnable,
		"disable":   CmdDisable,
		"update":    CmdUpdate,
		"uninstall": CmdUninstall,
	}

	RunCmd  = runCmd
	DataDir = constants.DataDir

	// Used by unit tests to mock out executing the command
	ExecCmdInDir = exec.ExecCmdInDir

	// Used by unit tests to mock out the immediate run command update step
	immediateUpdate = immediatecmds.Update

	ErrAlreadyProcessed = types.ErrAlreadyProcessed
)

type commandFile interface {
	io.ReadWriteCloser
	ReadDir(int) ([]os.DirEntry, error)
}

func update(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	extensionEvents := createExtensionEventManager(ctx, h)

	// Figure out the directories from which and to where we're upgrading. We cannot entirely rely on the environment variables from the Guest Agent
	upgradeFromVersionDirectory, upgradeToVersionDirectory, upgradeFromVersion := determineUpgradeVersionDirectories(ctx, extensionEvents)

	if compareVersions(constants.FirstVersionNoRehydration, upgradeFromVersion) > 0 {
		// Rehydrate any mrseq files from the corresponding status file.
		err := rehydrateMrSeqFilesForProblematicUpgrades(ctx, upgradeFromVersionDirectory, upgradeToVersionDirectory, extensionEvents)
		if err != nil {
			// If we fail on update, then there's a risk we could re-execute the customer's script. Don't take that chance.
			// By failing Update, the extension goal state will fail. WALA will try us again on the next goal state.
			ctx.Log("event", "Unable to rehydrate mrseq files")
			return "", "", err, constants.ExitCode_CouldNotRehydrateMrSeq
		}
	}

	// Copy any .mrseq or .status files -Most Recently executed Sequence number files and status files for Run Commands from old version to new version.
	// This is necessary to prevent rerunning of already executed Run Commands after upgrade of extension version, and also return their statuses.
	copyError := CopyStateForUpdate(ctx, upgradeFromVersionDirectory, upgradeToVersionDirectory, extensionEvents)
	if copyError != nil {
		return "", "", errors.Wrap(copyError, "Migrating *.mrseq or .status files failed during update."), constants.ExitCode_CopyStateForUpdateFailed
	}

	exitCode, err := immediateUpdate(ctx, h, metadata.ExtName, metadata.SeqNum, extensionEvents)
	if err != nil {
		return "", "", err, exitCode
	}

	ctx.Log("event", "update")
	return "", "", nil, constants.ExitCode_Okay
}

func disable(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	extensionEvents := createExtensionEventManager(ctx, h)
	extensionHandlerName := commandProcessor.GetExtensionName(ctx)
	if extensionHandlerName == constants.ImmediateRunCommandHandlerName {
		exitCode, err := immediatecmds.Disable(ctx, h, metadata.ExtName, metadata.SeqNum, extensionEvents)
		if err != nil {
			// Remove the mrseq file for the extension. For RunCommands that are called from the Guest Agent, it will delete these files for us
			// if the extension is actually being deleted, and keep them for an update. However, in IRC we're not called by Guest Agent, so we
			// need to delete them ourself.
			resetSeqNum(ctx, metadata.MostRecentSequence, extensionEvents)
			return "", "", err, exitCode
		}
	}

	ctx.Log("event", "disable")
	pid.KillPreviousExtension(ctx, metadata.PidFilePath)
	return "", "", nil, constants.ExitCode_Okay
}

func install(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	extensionEvents := createExtensionEventManager(ctx, h)
	exitCode, err := immediateInstall()
	if err != nil {
		return "", "", err, exitCode
	}

	if err := commandMkdirAll(DataDir, 0755); err != nil {
		errMessage := fmt.Sprintf("Failed to create data dir: %v due to: %v", DataDir, err)
		extensionEvents.LogErrorEvent("install", errMessage)
		return "", "", errorutil.WithCode(errorutil.Os_FailedToCreateDataDir, errors.Wrap(err, errMessage)), constants.ExitCode_CreateDataDirectoryFailed
	}

	ctx.Log("event", "created data dir", "path", DataDir)
	ctx.Log("event", "installed")
	extensionEvents.LogInformationalEvent("uninstall", fmt.Sprintf("created data dir: %v", DataDir))
	return "", "", nil, constants.ExitCode_Okay
}

func uninstall(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	extensionEvents := createExtensionEventManager(ctx, h)
	exitCode, err := immediateUninstall(ctx, h, metadata.ExtName, metadata.SeqNum, extensionEvents)
	if err != nil {
		return "", "", err, exitCode
	}

	{ // a new context scope with path
		ctx = ctx.With("path", DataDir)
		ctx.Log("event", "removing data dir", "path", DataDir)
		if err := commandRemoveAll(DataDir); err != nil {
			errMessage := fmt.Sprintf("Failed to delete data directory: %v due to: %v", DataDir, err)
			extensionEvents.LogErrorEvent("uninstall", errMessage)
			return "", "", errorutil.WithCode(errorutil.Os_FailedToDeleteDataDir, errors.Wrap(err, errMessage)), constants.ExitCode_RemoveDataDirectoryFailed
		}
		ctx.Log("event", "removed data dir")
		extensionEvents.LogInformationalEvent("uninstall", fmt.Sprintf("removed data dir %v", DataDir))
	}
	ctx.Log("event", "uninstalled")
	return "", "", nil, constants.ExitCode_Okay
}

func enablePre(ctx *log.Context, h types.HandlerEnvironment, metadata types.RCMetadata, c types.Cmd) error {
	// exit if this sequence number (a snapshot of the configuration) is already
	// processed. if not, save this sequence number before proceeding.
	if shouldExit, err := checkAndSaveSeqNum(ctx, metadata.SeqNum, metadata.MostRecentSequence); err != nil {
		return errors.Wrap(err, "failed to process sequence number")
	} else if shouldExit {
		ctx.Log("event", "exit", "message", "the script configuration has already been processed, will not run again")

		if c.Functions.Cleanup != nil {
			c.Functions.Cleanup(ctx, metadata, h, "")
		}

		return ErrAlreadyProcessed
	}

	return nil
}

func enable(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	extensionEvents := createExtensionEventManager(ctx, h)

	// parse the extension handler settings (not available prior to 'enable')
	cfg, err1 := readCommandSettings(h.HandlerEnvironment.ConfigFolder, metadata.ExtName, metadata.SeqNum, ctx)
	if err1 != nil {
		errMessage := fmt.Sprintf("Failed to get configuration: %v", err1)
		extensionEvents.LogErrorEvent("enable", errMessage)
		return "", "", errors.Wrap(err1, "failed to get configuration"), constants.ExitCode_GetHandlerSettingsFailed
	}

	exitCode, err := immediateEnable(ctx, h, metadata.ExtName, metadata.SeqNum, cfg, extensionEvents)

	// If there is an error or the customer requested to install the script as a service, return the error and exit code immediately.
	if err != nil || cfg.InstallAsService() {
		return "", "", err, exitCode
	}

	// Load extension policy settings.
	// If policy file exists, load the policy. If not, then don't load.
	var extensionPolicyManagerPtr *extensionpolicysettings.ExtensionPolicySettingsManager[extensionpolicysettingsrc.RCv2ExtensionPolicySettings]
	policyPath := filepath.Join(h.HandlerEnvironment.ConfigFolder, constants.PolicyFileName)
	var rceps *extensionpolicysettingsrc.RCv2ExtensionPolicySettings

	if _, err := commandStat(policyPath); err == nil {
		extensionPolicyManagerPtr, rceps, err, exitCode = extensionpolicysettingsrc.InitializeExtensionPolicySettings(ctx, policyPath)
		if err != nil {
			// TODO: In the future, fail the command if the policy file was invalid and return the exitCode.
			// For now, log the error and continue with the command execution.
			ctx.Log("error", "failed to initialize extension policy settings. Executing command with no policy applied for now.", "error", err, "errorCode", exitCode)
		} else {
			rcepsToLoggable := fmt.Sprintf("%+v", rceps)
			ctx.Log("message", "successfully initialized extension policy settings: ", "policy", rcepsToLoggable)
		}
	} else if os.IsNotExist(err) {
		ctx.Log("message", "extension policy settings file does not exist. No policy applied.", "error", err)
		extensionPolicyManagerPtr = nil
	} else {
		return "", "", errorutil.WithCode(errorutil.Os_FailedToStatRuntimePolicy, errors.Wrap(err, "failed to stat extension policy settings file")), constants.ExitCode_LoadExtensionPolicySettingsFailed
	}

	// Validate handler settings against policy settings.
	if extensionPolicyManagerPtr != nil && rceps != nil {
		if err, exitCode = extensionpolicysettingsrc.ValidateHandlerSettingsAgainstPolicy(ctx, &cfg, rceps); err != nil {
			return "", "", err, exitCode
		}
	}

	dir := filepath.Join(metadata.DownloadPath, fmt.Sprintf("%d", metadata.SeqNum))
	scriptFilePath, err := downloadScript(ctx, dir, &cfg, rceps)
	if err != nil && errors.Is(err, extensionerrors.ErrItemNotInAllowlist) {
		return "", "", errors.Wrap(err, "downloaded script file is not in the allowlist."), constants.ExitCode_DownloadedScriptBlockedByExtensionPolicy
	}

	if err != nil {
		errMessage := fmt.Sprintf("Failed to download script: %v due to: %v", download.GetUriForLogging(cfg.ScriptURI()), err)
		extensionEvents.LogErrorEvent("enable", errMessage)
		return "",
			"",
			errors.Wrap(err, fmt.Sprintf("File downloads failed. Use either a public script URI that points to .sh file, Azure storage blob SAS URI or storage blob accessible by a managed identity and retry. If managed identity is used, make sure it has been given access to container of storage blob '%s' with 'Storage Blob Data Reader' role assignment. In case of user-assigned identity, make sure you add it under VM's identity. For more info, refer https://aka.ms/RunCommandManagedLinux", download.GetUriForLogging(cfg.ScriptURI()))),
			constants.ExitCode_ScriptBlobDownloadFailed
	}

	err = downloadArtifacts(ctx, dir, &cfg)
	if err != nil {
		errMessage := fmt.Sprintf("Failed to download artifacts: %v", err)
		extensionEvents.LogErrorEvent("enable", errMessage)
		return "", "",
			errors.Wrap(err, "Artifact downloads failed. Use either a public artifact URI that points to .sh file, Azure storage blob SAS URI, or storage blob accessible by a managed identity and retry."),
			constants.ExitCode_DownloadArtifactFailed
	}

	blobCreateOrReplaceError := "Error creating AppendBlob '%s' using SAS token or Managed identity. Please use a valid blob SAS URI with [read, append, create, write] permissions OR managed identity. If managed identity is used, make sure Azure blob and identity exist, and identity has been given access to storage blob's container with 'Storage Blob Data Contributor' role assignment. In case of user-assigned identity, make sure you add it under VM's identity and provide outputBlobUri / errorBlobUri and corresponding clientId in outputBlobManagedIdentity / errorBlobManagedIdentity parameter(s). In case of system-assigned identity, do not use outputBlobManagedIdentity / errorBlobManagedIdentity parameter(s). For more info, refer https://aka.ms/RunCommandManagedLinux"

	// DisableOutputBlobs (policy settings) should have already been validated in ValidateHandlerSettingsAgainstPolicy,
	// so we don't need to check it again. If a blob URI was passed in, we assume disableOutputBlobs == false.
	var outputBlobSASRef *storage.Blob
	var outputBlobAppendClient *appendblob.Client
	var outputBlobAppendCreateOrReplaceError error
	outputFilePosition := int64(0)

	// Create or Replace outputBlobURI if provided. Fail the command if create or replace fails.
	if cfg.OutputBlobURI != "" {
		outputBlobSASRef, outputBlobAppendClient, outputBlobAppendCreateOrReplaceError = createOrReplaceAppendBlob(cfg.OutputBlobURI,
			cfg.ProtectedSettings.OutputBlobSASToken, cfg.ProtectedSettings.OutputBlobManagedIdentity, ctx)

		if outputBlobAppendCreateOrReplaceError != nil {
			return "",
				"",
				errors.Wrap(outputBlobAppendCreateOrReplaceError, fmt.Sprintf(blobCreateOrReplaceError, cfg.OutputBlobURI)),
				constants.ExitCode_BlobCreateOrReplaceFailed
		}
	}

	var errorBlobSASRef *storage.Blob
	var errorBlobAppendClient *appendblob.Client
	var errorBlobAppendCreateOrReplaceError error
	errorFilePosition := int64(0)

	// Create or Replace errorBlobURI if provided. Fail the command if create or replace fails.
	if cfg.ErrorBlobURI != "" {
		errorBlobSASRef, errorBlobAppendClient, errorBlobAppendCreateOrReplaceError = createOrReplaceAppendBlob(cfg.ErrorBlobURI,
			cfg.ProtectedSettings.ErrorBlobSASToken, cfg.ProtectedSettings.ErrorBlobManagedIdentity, ctx)

		if errorBlobAppendCreateOrReplaceError != nil {
			return "",
				"",
				errors.Wrap(errorBlobAppendCreateOrReplaceError, fmt.Sprintf(blobCreateOrReplaceError, cfg.ErrorBlobURI)),
				constants.ExitCode_BlobCreateOrReplaceFailed
		}
	}

	// AsyncExecution requested by customer means the extension should report successful extension deployment to complete the provisioning state
	// Later the full extension output will be reported
	statusToReport := types.StatusTransitioning
	if cfg.AsyncExecution {
		ctx.Log("message", "asycExecution is true - report success")
		statusToReport = types.StatusSuccess
		instanceview.ReportInstanceView(ctx, h, metadata, statusToReport, c, report)
	}

	stdoutF, stderrF := exec.LogPaths(dir)

	// Implement ticker to update extension status periodically
	ticker := time.NewTicker(updateStatusInSeconds * time.Second)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				ctx.Log("event", "report partial status")
				stdoutTail, stderrTail := getOutput(ctx, stdoutF, stderrF)
				report.Output = stdoutTail
				report.Error = stderrTail
				instanceview.ReportInstanceView(ctx, h, metadata, statusToReport, c, report)
				outputFilePosition, err = appendToBlob(stdoutF, outputBlobSASRef, outputBlobAppendClient, outputFilePosition, ctx)
				errorFilePosition, err = appendToBlob(stderrF, errorBlobSASRef, errorBlobAppendClient, errorFilePosition, ctx)
			}
		}
	}()

	// execute the command, save its error
	runErr, exitCode := RunCmd(ctx, dir, scriptFilePath, &cfg, metadata)

	ticker.Stop()
	done <- true

	// collect the logs if available
	stdoutTail, stderrTail := getOutput(ctx, stdoutF, stderrF)

	isSuccess := runErr == nil
	telemetryResult("Output", "-- stdout/stderr omitted from telemetry pipeline --", isSuccess, 0)

	if isSuccess {
		ctx.Log("event", "enabled")
	} else {
		ctx.Log("event", "enable script failed")
	}

	// Report the output streams to blobs
	outputFilePosition, err = appendToBlob(stdoutF, outputBlobSASRef, outputBlobAppendClient, outputFilePosition, ctx)
	if err != nil && runErr == nil {
		runErr, exitCode = err, constants.ExitCode_BlobCreateOrReplaceFailed
	}
	errorFilePosition, err = appendToBlob(stderrF, errorBlobSASRef, errorBlobAppendClient, errorFilePosition, ctx)
	if err != nil && runErr == nil {
		runErr, exitCode = err, constants.ExitCode_BlobCreateOrReplaceFailed
	}

	if c.Functions.Cleanup != nil {
		c.Functions.Cleanup(ctx, metadata, h, cfg.PublicSettings.RunAsUser)
	}

	return stdoutTail, stderrTail, runErr, exitCode
}

func createExtensionEventManager(ctx *log.Context, hEnv types.HandlerEnvironment) *extensionevents.ExtensionEventManager {
	el := logging.New(nil)
	platformHandlerEnv := convertToPlatformHandlerEnv((hEnv))
	extensionEvents := extensionevents.New(el, platformHandlerEnv)
	return extensionEvents
}

// Converts from the local HandlerEnvironment to the azure-extension-platform HandlerEnvironment
func convertToPlatformHandlerEnv(myEnv types.HandlerEnvironment) *handlerenv.HandlerEnvironment {
	env := myEnv.HandlerEnvironment
	return &handlerenv.HandlerEnvironment{
		HeartbeatFile:       env.HeartbeatFile,
		StatusFolder:        env.StatusFolder,
		ConfigFolder:        env.ConfigFolder,
		LogFolder:           env.LogFolder,
		EventsFolder:        env.EventsFolder,
		DeploymentID:        env.DeploymentID,
		RoleName:            env.RoleName,
		Instance:            env.Instance,
		HostResolverAddress: env.HostResolverAddress,
	}
}

// appendToBlob saves a file (from seeking position to the end of the file) to AppendBlob. Returns the new position (end of the file)
func appendToBlob(sourceFilePath string, appendBlobRef *storage.Blob, appendBlobClient *appendblob.Client, outputFilePosition int64, ctx *log.Context) (int64, error) {
	var err error
	var newOutput []byte
	if appendBlobRef != nil || appendBlobClient != nil {
		// Save to blob
		newOutput, err = readOutputFromPosition(sourceFilePath, outputFilePosition)
		if err == nil {
			newOutputSize := len(newOutput)
			if newOutputSize > 0 {
				if appendBlobRef != nil {
					err = errorutil.WithCode(errorutil.Storage_failedToAppendOutputWithSas, appendSASBlock(appendBlobRef, newOutput))
				} else if appendBlobClient != nil {
					ctx.Log("message", fmt.Sprintf("inside appendBlobClient. Output is '%s'", newOutput))
					err = errorutil.WithCode(errorutil.Storage_failedToAppendOutputWithManagedIdentity, appendManagedBlock(appendBlobClient, newOutput))
				}

				if err == nil {
					outputFilePosition += int64(newOutputSize)
				} else {
					ctx.Log("message", "AppendToBlob failed", "error", err)
				}
			}
		} else {
			ctx.Log("message", "AppendToBlob - GetFileFromPosition failed.", "error", err)
		}
	}

	return outputFilePosition, err
}

func getOutput(ctx *log.Context, stdoutFileName string, stderrFileName string) (string, string) {
	// collect the logs if available
	stdoutTail, err := files.TailFile(stdoutFileName, maxTailLen)
	if err != nil {
		ctx.Log("message", "error tailing stdout logs", "error", err)
	}
	stderrTail, err := files.TailFile(stderrFileName, maxTailLen)
	if err != nil {
		ctx.Log("message", "error tailing stderr logs", "error", err)
	}
	return string(stdoutTail), string(stderrTail)
}

// checkAndSaveSeqNum checks if the given seqNum is already processed
// according to the specified seqNumFile and if so, returns true,
// otherwise saves the given seqNum into seqNumFile returns false.
func checkAndSaveSeqNum(ctx log.Logger, seq int, mrseqPath string) (shouldExit bool, _ error) {
	ctx.Log("event", "comparing seqnum", "path", mrseqPath)
	smaller, err := seqnum.IsSmallerThan(mrseqPath, seq)
	if err != nil {
		return false, errors.Wrap(err, "failed to check sequence number")
	}

	if !smaller {
		// stored sequence number is equals or greater than the current
		// sequence number.
		return true, nil
	}

	if err := seqnum.SaveSeqNum(mrseqPath, seq); err != nil {
		return false, errors.Wrap(err, "failed to save sequence number")
	}
	ctx.Log("event", "seqnum saved", "path", mrseqPath)
	return false, nil
}

// resetSeqNum deletes the seqNum file to reset the sequence number
func resetSeqNum(ctx log.Logger, mrseqPath string, extensionEvents *extensionevents.ExtensionEventManager) {
	message := fmt.Sprintf("Resetting seqnum by deleting file '%s'", mrseqPath)
	ctx.Log("message", message)
	extensionEvents.LogInformationalEvent("resetseqnum", message)
	if err := commandRemove(mrseqPath); err != nil && !os.IsNotExist(err) {
		ctx.Log("error", errorutil.WithCode(errorutil.Os_FailedToResetSequenceNumber, errors.Wrap(err, "failed to reset sequence number")), "clarificationCode", errorutil.Os_FailedToResetSequenceNumber)
	}
}

// Copy state of the extension from old version to new version during update (.mrseq files, .status files)
func CopyStateForUpdate(ctx log.Logger, upgradeFromVersionDirectory string, upgradeToVersionDirectory string, extensionEvents *extensionevents.ExtensionEventManager) error {
	// Copy .mrseq files (Most Recently executed Sequence number) that helps determine whether a sequence number of Run Command has been previously executed or not.
	mrseqFilesNameList, mrseqFileCopyErr := copyFiles(ctx, constants.MrSeqFileExtension, "", upgradeFromVersionDirectory, upgradeToVersionDirectory, extensionEvents)
	if mrseqFileCopyErr != nil {
		return mrseqFileCopyErr
	}

	// Copy .status files of already executed sequence numbers
	_, statusFileCopyErr := copyFiles(ctx, ".status", constants.StatusFileDirectory, upgradeFromVersionDirectory, upgradeToVersionDirectory, extensionEvents)
	if statusFileCopyErr != nil {
		return statusFileCopyErr
	}

	// If status file corresponding to a .mrseq file does not exist, create a dummy status file to prevent poll status timeouts for already executed Run Commands after upgrade.
	if mrseqFilesNameList != nil && mrseqFilesNameList.Len() > 0 {
		// This is best effort - Do not return error if any case of failures.
		// Worst case that could happen is poll status timeouts for those few cases where creating dummy status file failed for some reason.
		createDummyStatusFilesIfNeeded(ctx, mrseqFilesNameList, extensionEvents)
	}

	return nil
}

func determineUpgradeVersionDirectories(ctx *log.Context, extensionEvents *extensionevents.ExtensionEventManager) (upgradeFromVersionDirectory string, upgradeToVersionDirectory string, upgradeFromVersion string) {
	// These two environment variables will tell us the extension versions involved, but won't actually tell us
	// the from/to versions
	upgradeToVersion := os.Getenv(constants.VersionEnvName)
	extensionVersionValue := os.Getenv(constants.ExtensionVersionEnvName)
	updatingFromVersionValue := os.Getenv(constants.ExtensionVersionUpdatingFromEnvName)

	// In some WALA versions, there is a bug where on downgrade it will send the same value for upgradeToVersion and upgradeFromVersion
	// Newer versions will send the correct value for upgradeToVersion
	// Therefore:
	// Action    | Old WALA                               | New WALA
	// ---------------------------------------------------| -------------------------------------
	// Downgrade | upgradeToVersion == upgradeFromVersion | upgradeToVersion < upgradeFromVersion
	// ------------------------------------------------------------------------------------------
	// Upgrade   | upgradeToVersion > upgradeFromVersion  | upgradeToVersion > upgradeFromVersion
	// ------------------------------------------------------------------------------------------
	if upgradeToVersion == updatingFromVersionValue {
		// This is a downgrade. We therefore need to use the extension version
		upgradeFromVersion = extensionVersionValue
	} else {
		// This is an upgrade on the old WALA or an upgrade or downgrade on the new WALA
		upgradeFromVersion = updatingFromVersionValue
	}

	// Determine the corresponding extension directories
	extensionDirectory := os.Getenv(constants.ExtensionPathEnvName)
	if strings.Contains(extensionDirectory, upgradeToVersion) {
		upgradeToVersionDirectory = extensionDirectory
		upgradeFromVersionDirectory = strings.ReplaceAll(extensionDirectory, upgradeToVersion, upgradeFromVersion)
	} else {
		upgradeFromVersionDirectory = extensionDirectory
		upgradeToVersionDirectory = strings.ReplaceAll(extensionDirectory, upgradeFromVersion, upgradeToVersion)
	}

	msg := fmt.Sprintf("determineUpgradeVersionDirectories: move from='%s' to='%s'", upgradeFromVersionDirectory, upgradeToVersionDirectory)
	ctx.Log("message", msg)
	extensionEvents.LogInformationalEvent("determineUpgradeVersions", msg)

	return upgradeFromVersionDirectory, upgradeToVersionDirectory, upgradeFromVersion
}

// compareVersions compares two dotted version strings (e.g., "2.1", "2.1.0", "2.1.0.3").
// Returns: +1 if a>b, -1 if a<b, 0 if equal.
func compareVersions(a, b string) int {
	aParts := splitVersion(a)
	bParts := splitVersion(b)

	// Normalize lengths to the same number of components (4 segments max is common for extensions)
	const maxSeg = 4
	aParts = padTo(aParts, maxSeg)
	bParts = padTo(bParts, maxSeg)

	for i := 0; i < maxSeg; i++ {
		if aParts[i] > bParts[i] {
			return 1
		}
		if aParts[i] < bParts[i] {
			return -1
		}
	}
	return 0
}

// splitVersion converts "x.y.z.t" → []int{ x, y, z, t } (non-numeric parts treated as 0).
func splitVersion(v string) []int {
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

func padTo(in []int, size int) []int {

	if len(in) >= size {
		return in[:size]
	}
	n := size - len(in)

	// Ensure capacity for the extra n elements without reallocating.
	out := slices.Grow(in, n)

	// Extend length to size by appending n zero-values.
	out = append(out, make([]int, n)...)
	return out

}

func rehydrateMrSeqFilesForProblematicUpgrades(ctx *log.Context, updateFromVersionDirectory string, updateToVersionDirectory string, extensionEvents *extensionevents.ExtensionEventManager) error {
	oldExtensionStatusDirectory := filepath.Join(updateFromVersionDirectory, constants.StatusFileDirectory)

	extensionStatusDirectoryFDRef, err := commandOpen(oldExtensionStatusDirectory)
	if err != nil {
		errMessage := fmt.Sprintf("Failed to open status directory '%s' due to '%v'", oldExtensionStatusDirectory, err)
		extensionEvents.LogErrorEvent("rehydratemrseq", errMessage)
		return errorutil.WithCode(errorutil.Os_FailedToOpenRehydrationStatusDirectory, errors.Wrap(err, errMessage))
	}
	defer extensionStatusDirectoryFDRef.Close()

	// Iterate through the status directory, looking for files of the format: {extension name}.{seqNo}.status
	// For each of these, look for the corresponding file {extension name}.mrseq under the extension directory
	// If we find any status files missing their corresponding mrseq, then rehydrate it by taking the seqNo from the status file name
	statusFiles, err := extensionStatusDirectoryFDRef.ReadDir(0)
	if err != nil {
		errMessage := fmt.Sprintf("could not read directory entries from status directory %s", updateFromVersionDirectory)
		ctx.Log("message", errMessage)
		extensionEvents.LogErrorEvent("rehydratemrseq", errMessage)
		return errorutil.WithCode(errorutil.Os_FailedToReadRehydrationStatusDirectory, errors.Wrap(err, errMessage))
	}

	for _, statusFile := range statusFiles {
		statusFileName := statusFile.Name()

		if strings.HasSuffix(statusFileName, constants.StatusFileExtension) {
			parts := strings.Split(statusFileName, ".")
			if len(parts) != 3 {
				ctx.Log("message", fmt.Sprintf("Invalid status file '%s'", statusFileName))
			} else {
				extensionName := parts[0]
				seqNo := parts[1]
				seqNoAsInt, parseErr := strconv.Atoi(seqNo)
				if parseErr != nil {
					return errorutil.WithCode(errorutil.Internal_invalidStatusFileSequenceNumber, errors.Wrap(parseErr, "invalid sequence number in status filename"))
				}
				mrSeqFileName := extensionName + constants.MrSeqFileExtension
				mrSeqFilePath := filepath.Join(updateToVersionDirectory, mrSeqFileName)

				_, err = commandStat(mrSeqFilePath)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						message := fmt.Sprintf("Rehydrating mrseq file for '%s' because it was mistakenly deleted during disable", extensionName)
						ctx.Log("message", message)
						extensionEvents.LogInformationalEvent("rehydratemrseq", message)
						err = commandWriteFile(mrSeqFilePath, []byte(seqNo), os.FileMode(0600))
						if err != nil {
							errMessage := fmt.Sprintf("Could not write file '%s'", mrSeqFilePath)
							ctx.Log("message", errMessage)
							extensionEvents.LogErrorEvent("rehydratemrseq", errMessage)
							return errorutil.WithCode(errorutil.Os_FailedToRehydrateSequenceNumber, errors.Wrap(err, errMessage))
						}
						message = fmt.Sprintf("Successfully rehydrated mrseq file for '%s' with seqNo '%s'. File location '%s'", extensionName, seqNo, mrSeqFilePath)
						ctx.Log("message", message)
						extensionEvents.LogInformationalEvent("rehydratemrseq", message)
					} else {
						errMessage := fmt.Sprintf("Could not access file '%s' even though it exists", mrSeqFilePath)
						ctx.Log("message", errMessage)
						extensionEvents.LogErrorEvent("rehydratemrseq", errMessage)
						return errorutil.WithCode(errorutil.Os_FailedToStatRehydrationSequenceFile, errors.Wrap(err, errMessage))
					}
				} else {
					// Overwrite mrseq this seqNo is greater than the one it contains (simple solution for multiple status files)
					contents, err := commandReadFile(mrSeqFilePath)
					if err != nil {
						errMessage := fmt.Sprintf("Could not read file '%s'", mrSeqFilePath)
						ctx.Log("message", errMessage)
						extensionEvents.LogErrorEvent("rehydratemrseq", errMessage)
						return errorutil.WithCode(errorutil.Os_FailedToReadRehydrationSequenceFile, errors.Wrap(err, errMessage))
					}

					currentSeqNo, parseErr := strconv.Atoi(string(contents))
					if parseErr != nil {
						return errorutil.WithCode(errorutil.Internal_invalidMrseqFileSequenceNumber, errors.Wrap(parseErr, "invalid sequence number in mrseq file"))
					}
					if seqNoAsInt > currentSeqNo {
						err = commandWriteFile(mrSeqFilePath, []byte(seqNo), os.FileMode(0600))
						if err != nil {
							errMessage := fmt.Sprintf("Could not write file '%s'", mrSeqFilePath)
							ctx.Log("message", errMessage)
							extensionEvents.LogErrorEvent("rehydratemrseq", errMessage)
							return errorutil.WithCode(errorutil.Os_FailedToUpdateRehydratedSequenceFile, errors.Wrap(err, errMessage))
						}
						message := fmt.Sprintf("Updated mrseq file for '%s' with seqNo '%s'. File location '%s'", extensionName, seqNo, mrSeqFilePath)
						ctx.Log("message", message)
						extensionEvents.LogInformationalEvent("rehydratemrseq", message)
					}
				}
			}
		}
	}

	return nil
}

// Copy files like *.mrseq (Most Recently executed Sequence number), .status files from old extension version to new extension version during update.
func copyFiles(ctx log.Logger, fileExtensionSuffix string, extensionSubdirectory string, upgradeFromVersionDirectory string, upgradeToVersionDirectory string, extensionEvents *extensionevents.ExtensionEventManager) (*list.List, error) {

	message := fmt.Sprintf("Migrating '%s' files from '%s' to '%s'", fileExtensionSuffix, upgradeFromVersionDirectory, upgradeToVersionDirectory)
	ctx.Log("message", message)
	extensionEvents.LogInformationalEvent("copyfiles", message)

	// Append subdirectory like "status" under extension folder if provided.
	if extensionSubdirectory != "" {
		upgradeToVersionDirectory = filepath.Join(upgradeToVersionDirectory, extensionSubdirectory)
		upgradeFromVersionDirectory = filepath.Join(upgradeFromVersionDirectory, extensionSubdirectory)

		// Create subdirectory like "status" directory if it does not exist
		directory, err := commandOpen(upgradeToVersionDirectory)
		if err != nil {
			errr := commandMkdir(upgradeToVersionDirectory, 0700)
			if errr != nil {
				errMessage := fmt.Sprintf("Failed to create directory '%s'", upgradeToVersionDirectory)
				extensionEvents.LogErrorEvent("copyfiles", errMessage)
				return nil, errorutil.WithCode(errorutil.Os_FailedToCreateUpgradeStatusDirectory, errors.Wrap(errr, errMessage))
			}
		} else {
			directory.Close()
		}
	}

	if upgradeFromVersionDirectory == "" || upgradeToVersionDirectory == "" {
		errMessage := "oldExtesionDirectory or newExtensionDirectory is empty"
		extensionEvents.LogErrorEvent("copyfiles", errMessage)
		return nil, errorutil.WithCode(errorutil.Internal_emptyUpgradeDirectories, errors.New(errMessage))
	}

	// Check if the directory exists
	sourceDirectoryFDRef, err := commandOpen(upgradeFromVersionDirectory)
	if err != nil {
		errMessage := fmt.Sprintf("could not open sourceDirectory %s", upgradeFromVersionDirectory)
		ctx.Log("message", errMessage)
		extensionEvents.LogErrorEvent("copyfiles", errMessage)
		return nil, errorutil.WithCode(errorutil.Os_FailedToOpenUpgradeSourceDirectory, errors.Wrap(err, errMessage))
	}
	defer sourceDirectoryFDRef.Close()

	directoryEntries, err := sourceDirectoryFDRef.ReadDir(0)
	if err != nil {
		errMessage := fmt.Sprintf("could not read directory entries from sourceDirectory %s", upgradeFromVersionDirectory)
		ctx.Log("message", errMessage)
		extensionEvents.LogErrorEvent("copyfiles", errMessage)
		return nil, errorutil.WithCode(errorutil.Os_FailedToReadUpgradeSourceDirectory, errors.Wrap(err, errMessage))
	}

	numberOfFilesMigrated := 0
	fileNamesMigrated := list.New()

	for _, dirEntry := range directoryEntries {
		fileName := dirEntry.Name()

		if strings.HasSuffix(fileName, fileExtensionSuffix) {
			sourceFileFullPath := filepath.Join(upgradeFromVersionDirectory, fileName)
			destinationFileFullPath := filepath.Join(upgradeToVersionDirectory, fileName)

			sourceFile, sourceFileOpenError := commandOpen(sourceFileFullPath)
			if sourceFileOpenError != nil {
				errMessage := "Failed to open '%s' file '%s' for reading. Contact ICM team AzureRT\\Extensions for this service error."
				ctx.Log("message", fmt.Sprintf(errMessage, fileExtensionSuffix, sourceFileFullPath))
				extensionEvents.LogErrorEvent("copyfiles", errMessage)
				return fileNamesMigrated, errorutil.WithCode(errorutil.Os_FailedToOpenUpgradeSourceFile, errors.Wrap(sourceFileOpenError, errMessage))
			}
			defer sourceFile.Close()

			destFile, destFileCreateError := commandCreate(destinationFileFullPath)
			if destFileCreateError != nil {
				errMessage := "Failed to create '%s' file '%s'. Contact ICM team AzureRT\\Extensions for this service error."
				ctx.Log("message", fmt.Sprintf(errMessage, fileExtensionSuffix, destinationFileFullPath))
				extensionEvents.LogErrorEvent("copyfiles", errMessage)
				return fileNamesMigrated, errorutil.WithCode(errorutil.Os_FailedToCreateUpgradeDestinationFile, errors.Wrap(destFileCreateError, errMessage))
			}
			defer destFile.Close()

			_, copyError := commandCopy(destFile, sourceFile)
			if copyError != nil {
				errMessage := fmt.Sprintf("Failed to copy '%s' file '%s' to path '%s'. Contact ICM team AzureRT\\Extensions for this service error.",
					fileExtensionSuffix, sourceFileFullPath, destinationFileFullPath)
				ctx.Log("message", errMessage)
				extensionEvents.LogErrorEvent("copyfiles", errMessage)
				return fileNamesMigrated, errorutil.WithCode(errorutil.Os_FailedToCopyUpgradeStateFile, errors.Wrap(copyError, errMessage))
			} else {
				message := fmt.Sprintf("File '%s' was copied successfully to '%s'", sourceFileFullPath, destinationFileFullPath)
				ctx.Log("message", message)
				extensionEvents.LogInformationalEvent("copyfiles", message)
				numberOfFilesMigrated++
				fileNamesMigrated.PushBack(fileName)
			}
		}
	}

	message = fmt.Sprintf("Migrated %d '%s' files from extension version '%s' to '%s'", numberOfFilesMigrated, fileExtensionSuffix, upgradeFromVersionDirectory, upgradeToVersionDirectory)
	ctx.Log("message", message)
	extensionEvents.LogInformationalEvent("copyfiles", message)

	return fileNamesMigrated, nil
}

// This need to be only executed by Update operation
func createDummyStatusFilesIfNeeded(ctx log.Logger, mrseqFilesNameList *list.List, extensionEvents *extensionevents.ExtensionEventManager) error {
	if mrseqFilesNameList == nil || mrseqFilesNameList.Len() <= 0 {
		return nil
	}

	// Create dummy status file for .mrseq file if status file is not available.
	newExtensionDirectory := os.Getenv(constants.ExtensionPathEnvName)
	statusFileDirectoryPath := filepath.Join(newExtensionDirectory, constants.StatusFileDirectory)

	var mrSeqFileName string
	var mrSeqFileFullPath string
	var extensionName string
	var mrSeqFileExtensionIndex int
	var statusFileName string
	var statusFilePath string
	var errorMessage string
	var instanceViewMessage string
	var err error
	var content []byte
	var allErr error

	for mreSeqFileNameElement := mrseqFilesNameList.Front(); mreSeqFileNameElement != nil; mreSeqFileNameElement = mreSeqFileNameElement.Next() {
		mrSeqFileName = (mreSeqFileNameElement.Value).(string)

		// Read the most recently executed sequence number from the .mrseq file
		mrSeqFileFullPath = filepath.Join(newExtensionDirectory, mrSeqFileName)
		content, err = commandReadFile(mrSeqFileFullPath)
		if err != nil {
			errorMessage = fmt.Sprintf("Reading mrseq (Most Recently executed Sequence number) from file '%s' failed with error '%s'", mrSeqFileFullPath, err.Error())
			ctx.Log("error", errorMessage)
			extensionEvents.LogErrorEvent("createdummystatusfiles", errorMessage)
			if allErr == nil {
				allErr = errorutil.WithCode(errorutil.Os_FailedToReadDummyStatusSequenceFile, errors.Wrap(err, errorMessage))
			}
			continue
		}

		var mrseqNumber int
		if content != nil {
			mrseqNumberString := string(content)
			mrseqNumber, err = strconv.Atoi(mrseqNumberString)
			if err != nil {
				errorMessage = fmt.Sprintf("mrseqNumberString to mrseqNumber conversion (string to int) of '%s' failed with error '%s'", mrseqNumberString, err.Error())
				ctx.Log("error", errorMessage)
				extensionEvents.LogErrorEvent("createdummystatusfiles", errorMessage)
				if allErr == nil {
					allErr = errorutil.WithCode(errorutil.Internal_invalidDummyStatusSequenceNumber, errors.Wrap(err, errorMessage))
				}
				continue
			}
		} else {
			errorMessage = fmt.Sprintf("Empty .mrseq file content. No sequence number was found inside file  '%s' ", mrSeqFileFullPath)
			ctx.Log("error", errorMessage)
			extensionEvents.LogErrorEvent("createdummystatusfiles", errorMessage)
			if allErr == nil {
				allErr = errorutil.WithCode(errorutil.Internal_emptyDummyStatusSequenceFile, errors.New(errorMessage))
			}
			continue
		}

		// Find extension name from the .mrseq file
		mrSeqFileExtensionIndex = strings.Index(mrSeqFileName, constants.MrSeqFileExtension)
		if mrSeqFileExtensionIndex == -1 {
			errorMessage = fmt.Sprintf("Invalid mrseq file '%s'", mrSeqFileName)
			ctx.Log("error", errorMessage)
			extensionEvents.LogErrorEvent("createdummystatusfiles", errorMessage)
			if allErr == nil {
				allErr = errorutil.WithCode(errorutil.Internal_invalidDummyStatusSequenceFileName, errors.New(errorMessage))
			}
			continue
		}
		extensionName = mrSeqFileName[0:mrSeqFileExtensionIndex]

		// Determine status file name and status file path
		statusFileName = fmt.Sprintf("%s.%d.status", extensionName, mrseqNumber)
		statusFilePath = filepath.Join(statusFileDirectoryPath, statusFileName)

		var rootStatusJson []byte
		// If status file path does not exist, create a dummy status file to prevent poll status timeouts for already executed Run Commands after upgrade.
		if !handlersettings.DoesFileExist(statusFilePath) {

			timeNow := time.Now().UTC().Format(time.RFC3339)
			instanceView := types.RunCommandInstanceView{
				ExecutionState:   types.Unknown, // InstanceView executionState is Unknown because we do not know whether previously executed Run command's state for sure.
				ExecutionMessage: "The script has been executed. However, the instanceView's execution state, output, error are unknown.",
				ExitCode:         0,
				Output:           "Unknown",
				Error:            "Unknown",
				StartTime:        timeNow,
				EndTime:          timeNow,
			}
			instanceViewMessage, err = instanceview.SerializeInstanceView(&instanceView)
			if err != nil {
				errorMessage = fmt.Sprintf("Failed to serialize unknown instanceView, error is '%s'", err.Error())
				extensionEvents.LogErrorEvent("createdummystatusfiles", errorMessage)
				if allErr == nil {
					allErr = errors.Wrap(err, errorMessage)
				}
				continue
			}

			statusReport := types.NewStatusReport(types.StatusSuccess, "Enable", instanceViewMessage, extensionName)
			rootStatusJson, err = status.MarshalStatusReportIntoJson(statusReport, true)
			if err != nil {
				errorMessage = fmt.Sprintf("failed to marshal status report into json for status file '%s' with error '%s'", statusFilePath, err.Error())
				extensionEvents.LogErrorEvent("createdummystatusfiles", errorMessage)
				if allErr == nil {
					allErr = errors.Wrap(err, errorMessage)
				}
				continue
			}

			err = status.SaveStatusReport(statusFileDirectoryPath, extensionName, mrseqNumber, rootStatusJson)
			if err != nil {
				errorMessage = fmt.Sprintf("Failed to create a dummy status file '%s' as it was not existing for .mrseq file '%s' with error '%s'", statusFilePath, mrSeqFileFullPath, err.Error())
				extensionEvents.LogErrorEvent("createdummystatusfiles", errorMessage)
				if allErr == nil {
					allErr = errors.Wrap(err, errorMessage)
				}
				continue
			}
		}
	}
	return allErr
}

// downloadScript downloads the script file specified in cfg into dir (creates if does
// not exist) and takes storage credentials specified in cfg into account.
func downloadScript(ctx *log.Context, dir string, cfg *handlersettings.HandlerSettings, rceps *extensionpolicysettingsrc.RCv2ExtensionPolicySettings) (string, error) {
	// - prepare the output directory for files and the command output
	// - create the directory if missing
	ctx.Log("event", "creating output directory", "path", dir)
	if err := commandMkdirAll(dir, 0700); err != nil {
		return "", errorutil.WithCode(errorutil.FileDownload_unableToCreateDownloadDirectory, errors.Wrap(err, "failed to prepare output directory"))
	}
	ctx.Log("event", "created output directory")

	dos2unix := 1

	// - download scriptURI
	scriptFilePath := ""
	scriptURI := cfg.ScriptURI()
	ctx.Log("scriptUri", scriptURI)
	if scriptURI != "" {
		telemetryResult("scenario", fmt.Sprintf("source.scriptUri;dos2unix=%d", dos2unix), true, 0*time.Millisecond)
		ctx.Log("event", "download start")
		file, err := processDownloadedScript(ctx, scriptURI, dir, cfg)
		if err != nil {
			ctx.Log("event", "download failed", "error", err)
			return "", errors.Wrapf(err, "failed to download file %s. ", scriptURI)
		}
		scriptFilePath = file
		ctx.Log("event", "download complete", "output", dir)

		if rceps != nil && cfg.ScriptType() != handlersettings.GalleryScript {
			// Assume the downloaded script TYPE is already allowed, since this was already validated earlier in enable().
			err = validateDownloadedScript(scriptFilePath, rceps)
			if err != nil {
				ctx.Log("message", "downloaded script file is not in the allowlist, attempting to delete", "scriptFilePath", scriptFilePath)
				if delErr := commandRemove(scriptFilePath); delErr != nil {
					ctx.Log("message", "failed to delete downloaded script file", "scriptFilePath", scriptFilePath, "error", errorutil.WithCode(errorutil.Os_FailedToDeleteBlockedDownloadedScript, delErr), "clarificationCode", errorutil.Os_FailedToDeleteBlockedDownloadedScript)
				} else {
					ctx.Log("message", "successfully deleted downloaded script file", "scriptFilePath", scriptFilePath)
				}
				return scriptFilePath, errorutil.WithCode(errorutil.ExtensionPolicySettings_downloadedScriptNotAllowed, errors.Wrapf(err, "file %s blocked by policy", scriptFilePath))
			}
		}
	}
	return scriptFilePath, nil
}

func downloadArtifacts(ctx *log.Context, dir string, cfg *handlersettings.HandlerSettings) error {
	artifacts, err := cfg.ReadArtifacts()
	if err != nil {
		return err
	}

	if artifacts == nil {
		return nil
	}

	ctx.Log("event", "Downloading artifacts")
	for i := 0; i < len(artifacts); i++ {
		// Download the artifact
		filePath, err := files.DownloadAndProcessArtifact(ctx, dir, &artifacts[i])
		if err != nil {
			ctx.Log("events", "Failed to download artifact", err, "artifact", artifacts[i].ArtifactUri)
			return errors.Wrapf(err, "failed to download artifact %s", artifacts[i].ArtifactUri)
		}

		ctx.Log("event", "Downloaded artifact complete", "file", filePath)
	}

	return nil
}

// runCmd runs the command (extracted from cfg) in the given dir (assumed to exist).
func runCmd(ctx *log.Context, dir string, scriptFilePath string, cfg *handlersettings.HandlerSettings, metadata types.RCMetadata) (err error, exitCode int) {
	ctx.Log("event", "executing command", "output", dir)
	var scenario string

	// If script is specified - use it directly for command
	if cfg.Script() != "" {
		scenario = "embedded-script"
		// Save the script to a file
		scriptFilePath = filepath.Join(dir, "script.sh")
		err := files.SaveScriptFile(scriptFilePath, cfg.Script())
		if err != nil {
			ctx.Log("event", "failed to save script to file", "error", err, "file", scriptFilePath)
			return errors.Wrap(err, "failed to save script to file"), constants.ExitCode_SaveScriptFailed
		}
	} else if cfg.ScriptURI() != "" {
		// If scriptUri is specified then cmd should start it
		scenario = "public-scriptUri"
	}

	ctx.Log("event", "prepare command", "scriptFile", scriptFilePath)

	// We need to kill previous extension process if exists before starting a new one.
	pid.KillPreviousExtension(ctx, metadata.PidFilePath)

	// Store the active process id and start time in case its a long running process that needs to be killed later
	// If process exited successfully the pid file is deleted
	pid.SaveCurrentPidAndStartTime(metadata.PidFilePath)
	defer pid.DeleteCurrentPidAndStartTime(metadata.PidFilePath)

	begin := time.Now()
	err, exitCode = ExecCmdInDir(ctx, scriptFilePath, dir, cfg)
	elapsed := time.Since(begin)
	isSuccess := err == nil

	telemetryResult("scenario", scenario, isSuccess, elapsed)

	if err != nil {
		ctx.Log("event", "failed to execute command", "error", err, "output", dir)
		return errors.Wrap(err, "failed to execute command"), exitCode
	}
	ctx.Log("event", "executed command", "output", dir)
	return nil, constants.ExitCode_Okay
}

// base64 decode and optionally GZip decompress a script
func decodeScript(script string) (string, string, error) {
	// scripts must be base64 encoded
	s, err := base64.StdEncoding.DecodeString(script)
	if err != nil {
		return "", "", errorutil.WithCode(errorutil.CustomerInput_invalidEmbeddedScriptBase64, errors.Wrap(err, "failed to decode script"))
	}

	// scripts may be gzip'ed
	r, err := gzip.NewReader(bytes.NewReader(s))
	if err != nil {
		return string(s), fmt.Sprintf("%d;%d;gzip=0", len(script), len(s)), nil
	}

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	n, err := io.Copy(w, r)
	if err != nil {
		return "", "", errorutil.WithCode(errorutil.CustomerInput_invalidCompressedScript, errors.Wrap(err, "failed to decompress script"))
	}

	w.Flush()
	return buf.String(), fmt.Sprintf("%d;%d;gzip=1", len(script), n), nil
}

func createOrReplaceAppendBlobUsingManagedIdentity(blobUri string, managedIdentity *handlersettings.RunCommandManagedIdentity) (*appendblob.Client, error) {
	var ID string = ""
	var miCred *azidentity.ManagedIdentityCredential = nil
	var miCredError error = nil

	if managedIdentity != nil {
		if managedIdentity.ClientId != "" {
			ID = managedIdentity.ClientId
		} else if managedIdentity.ObjectId != "" { //ObjectId is not supported by azidentity.NewManagedIdentityCredential
			return nil, errorutil.WithCode(errorutil.CustomerInput_outputIdentityObjectIdNotSupported, errors.New("Managed identity's ObjectId is not supported. Use ClientId instead"))
		}
	}

	if ID != "" { // Use user-assigned identity if clientId is provided
		miCredentialOptions := azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(ID)}
		miCred, miCredError = newManagedIdentityCredential(&miCredentialOptions)
	} else { // Use system-assigned identity if clientId not provided
		miCred, miCredError = newManagedIdentityCredential(nil)
	}

	var appendBlobClient *appendblob.Client
	var appendBlobNewClientError error
	if miCredError == nil {
		appendBlobClient, appendBlobNewClientError = newAppendBlobClient(blobUri, miCred, nil)
		if appendBlobNewClientError != nil {
			return nil, errorutil.WithCode(errorutil.Storage_failedToCreateManagedAppendBlobClient, errors.Wrap(appendBlobNewClientError, fmt.Sprintf("Error Creating client to Append Blob '%s'. Make sure you are using Append blob. Other types of blob such as PageBlob, BlockBlob are not supported types.", download.GetUriForLogging(blobUri))))
		} else {
			// Create or Replace Append blob. If AppendBlob already exists, blob gets cleared.
			createAppendBlobError := createManagedAppendBlob(appendBlobClient)
			if createAppendBlobError != nil {
				return nil, errorutil.WithCode(errorutil.Storage_failedToCreateManagedAppendBlob, errors.Wrap(createAppendBlobError, fmt.Sprintf("Error creating or replacing the Append blob '%s'. Make sure you are using Append blob. Other types of blob such as PageBlob, BlockBlob are not supported types.", download.GetUriForLogging(blobUri))))
			}
		}
	} else {
		return nil, errorutil.WithCode(errorutil.Imds_failedToCreateManagedIdentityCredential, errors.Wrap(miCredError, "Error while retrieving managed identity credential"))
	}

	return appendBlobClient, nil
}

func createOrReplaceAppendBlob(blobUri string, sasToken string, managedIdentity *handlersettings.RunCommandManagedIdentity, ctx *log.Context) (*storage.Blob, *appendblob.Client, error) {
	var blobSASRef *storage.Blob
	var blobSASTokenError error
	var blobAppendClient *appendblob.Client
	var blobAppendClientError error

	// Validate blob can be created or replaced.
	if blobUri != "" {
		if sasToken != "" {
			blobSASRef, blobSASTokenError = download.CreateOrReplaceAppendBlob(blobUri, sasToken)

			if blobSASTokenError != nil {
				ctx.Log("message", fmt.Sprintf("Error creating blob '%s' using SAS token. Retrying with system-assigned managed identity if available..", download.GetUriForLogging(blobUri)), "error", blobSASTokenError)
			}
		}

		// Try to create or replace output blob using managed identity.
		if sasToken == "" || blobSASTokenError != nil {

			blobAppendClient, blobAppendClientError = createOrReplaceAppendBlobUsingManagedIdentity(blobUri, managedIdentity)
		}

		if (sasToken == "" && blobAppendClientError != nil) ||
			(blobSASTokenError != nil && blobAppendClientError != nil) {

			var er error
			if blobSASTokenError != nil {
				er = blobSASTokenError
			} else {
				er = blobAppendClientError
			}
			return nil, nil, errors.Wrap(er, "Creating or Replacing append blob failed.")
		}
	}
	return blobSASRef, blobAppendClient, nil
}
