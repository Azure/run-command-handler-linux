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
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/run-command-handler-linux/internal/cleanup"
	"github.com/Azure/run-command-handler-linux/internal/commandProcessor"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/exec"
	"github.com/Azure/run-command-handler-linux/internal/files"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/immediatecmds"
	"github.com/Azure/run-command-handler-linux/internal/instanceview"
	"github.com/Azure/run-command-handler-linux/internal/pid"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/telemetry"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/download"
	seqnum "github.com/Azure/run-command-handler-linux/pkg/seqnumutil"
	"github.com/Azure/run-command-handler-linux/pkg/versionutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	maxScriptSize         = 256 * 1024
	updateStatusInSeconds = 30
)

const (
	fullName                = "Microsoft.Compute.CPlat.Core.RunCommandLinux"
	maxTailLen              = 4 * 1024 // length of max stdout/stderr to be transmitted in .status file
	maxTelemetryTailLen int = 1800
)

var (
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
)

func update(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	exitCode, err := immediatecmds.Update(ctx, h, metadata.ExtName, metadata.SeqNum)
	if err != nil {
		return "", "", err, exitCode
	}

	// Copy any .mrseq or .status files -Most Recently executed Sequence number files and status files for Run Commands from old version to new version.
	// This is necessary to prevent rerunning of already executed Run Commands after upgrade of extension version, and also return their statuses.
	copyError := CopyStateForUpdate(ctx)
	if copyError != nil {
		return "", "", errors.Wrap(copyError, "Migrating *.mrseq or .status files failed during update."), constants.ExitCode_CopyStateForUpdateFailed
	}

	ctx.Log("event", "update")
	return "", "", nil, constants.ExitCode_Okay
}

func disable(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	extensionHandlerName := commandProcessor.GetExtensionName(ctx)
	ctx.Log("event", "disable", "extensionHandlerName", extensionHandlerName)
	ctx.Log("message", fmt.Sprintf("disable called for extension %s", extensionHandlerName))
	return "", "", nil, constants.ExitCode_Okay

	if extensionHandlerName == "" {
		exitCode, err := immediatecmds.Disable(ctx, h, metadata.ExtName, metadata.SeqNum)
		if err != nil {
			return "", "", err, exitCode
		}
	}

	ctx.Log("event", "disable")
	pid.KillPreviousExtension(ctx, metadata.PidFilePath)
	return "", "", nil, constants.ExitCode_Okay
}

func install(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	exitCode, err := immediatecmds.Install()
	if err != nil {
		return "", "", err, exitCode
	}

	if err := os.MkdirAll(constants.DataDir, 0755); err != nil {
		return "", "", errors.Wrap(err, "failed to create data dir"), constants.ExitCode_CreateDataDirectoryFailed
	}

	ctx.Log("event", "created data dir", "path", constants.DataDir)
	ctx.Log("event", "installed")
	return "", "", nil, constants.ExitCode_Okay
}

func uninstall(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	exitCode, err := immediatecmds.Uninstall(ctx, h, metadata.ExtName, metadata.SeqNum)
	if err != nil {
		return "", "", err, exitCode
	}

	{ // a new context scope with path
		ctx = ctx.With("path", constants.DataDir)
		ctx.Log("event", "removing data dir", "path", constants.DataDir)
		if err := os.RemoveAll(constants.DataDir); err != nil {
			return "", "", errors.Wrap(err, "failed to delete data directory"), constants.ExitCode_RemoveDataDirectoryFailed
		}
		ctx.Log("event", "removed data dir")
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
		c.Functions.Cleanup(ctx, metadata, h, "")
		return errors.New("the script configuration has already been processed, will not run again")
	}

	return nil
}

func enable(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
	// parse the extension handler settings (not available prior to 'enable')
	cfg, err1 := handlersettings.GetHandlerSettings(h.HandlerEnvironment.ConfigFolder, metadata.ExtName, metadata.SeqNum, ctx)
	if err1 != nil {
		return "", "", errors.Wrap(err1, "failed to get configuration"), constants.ExitCode_GetHandlerSettingsFailed
	}

	exitCode, err := immediatecmds.Enable(ctx, h, metadata.ExtName, metadata.SeqNum, cfg)

	// If there is an error or the customer requested to install the script as a service, return the error and exit code immediately.
	if err != nil || cfg.InstallAsService() {
		return "", "", err, exitCode
	}

	dir := filepath.Join(metadata.DownloadPath, fmt.Sprintf("%d", metadata.SeqNum))
	scriptFilePath, err := downloadScript(ctx, dir, &cfg)
	if err != nil {
		return "",
			"",
			errors.Wrap(err, fmt.Sprintf("File downloads failed. Use either a public script URI that points to .sh file, Azure storage blob SAS URI or storage blob accessible by a managed identity and retry. If managed identity is used, make sure it has been given access to container of storage blob '%s' with 'Storage Blob Data Reader' role assignment. In case of user-assigned identity, make sure you add it under VM's identity. For more info, refer https://aka.ms/RunCommandManagedLinux", download.GetUriForLogging(cfg.ScriptURI()))),
			constants.ExitCode_ScriptBlobDownloadFailed
	}

	err = downloadArtifacts(ctx, dir, &cfg)
	if err != nil {
		return "", "",
			errors.Wrap(err, "Artifact downloads failed. Use either a public artifact URI that points to .sh file, Azure storage blob SAS URI, or storage blob accessible by a managed identity and retry."),
			constants.ExitCode_DownloadArtifactFailed
	}

	blobCreateOrReplaceError := "Error creating AppendBlob '%s' using SAS token or Managed identity. Please use a valid blob SAS URI with [read, append, create, write] permissions OR managed identity. If managed identity is used, make sure Azure blob and identity exist, and identity has been given access to storage blob's container with 'Storage Blob Data Contributor' role assignment. In case of user-assigned identity, make sure you add it under VM's identity and provide outputBlobUri / errorBlobUri and corresponding clientId in outputBlobManagedIdentity / errorBlobManagedIdentity parameter(s). In case of system-assigned identity, do not use outputBlobManagedIdentity / errorBlobManagedIdentity parameter(s). For more info, refer https://aka.ms/RunCommandManagedLinux"

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
	runErr, exitCode := runCmd(ctx, dir, scriptFilePath, &cfg, metadata)

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
	errorFilePosition, err = appendToBlob(stderrF, errorBlobSASRef, errorBlobAppendClient, errorFilePosition, ctx)

	c.Functions.Cleanup(ctx, metadata, h, cfg.PublicSettings.RunAsUser)
	return stdoutTail, stderrTail, runErr, exitCode
}

// appendToBlob saves a file (from seeking position to the end of the file) to AppendBlob. Returns the new position (end of the file)
func appendToBlob(sourceFilePath string, appendBlobRef *storage.Blob, appendBlobClient *appendblob.Client, outputFilePosition int64, ctx *log.Context) (int64, error) {
	var err error
	var newOutput []byte
	if appendBlobRef != nil || appendBlobClient != nil {
		// Save to blob
		newOutput, err = files.GetFileFromPosition(sourceFilePath, outputFilePosition)
		if err == nil {
			newOutputSize := len(newOutput)
			if newOutputSize > 0 {
				if appendBlobRef != nil {
					err = appendBlobRef.AppendBlock(newOutput, nil)
				} else if appendBlobClient != nil {
					ctx.Log("message", fmt.Sprintf("inside appendBlobClient. Output is '%s'", newOutput))
					_, err = appendBlobClient.AppendBlock(context.Background(), streaming.NopCloser(bytes.NewReader(newOutput)), nil)
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

// Copy state of the extension from old version to new version during update (.mrseq files, .status files)
func CopyStateForUpdate(ctx log.Logger) error {
	// Copy .mrseq files (Most Recently executed Sequence number) that helps determine whether a sequence number of Run Command has been previously executed or not.
	mrseqFilesNameList, mrseqFileCopyErr := copyFiles(ctx, ".mrseq", "")
	if mrseqFileCopyErr != nil {
		return mrseqFileCopyErr
	}

	// Copy .status files of already executed sequence numbers
	_, statusFileCopyErr := copyFiles(ctx, ".status", constants.StatusFileDirectory)
	if statusFileCopyErr != nil {
		return statusFileCopyErr
	}

	// If status file corresponding to a .mrseq file does not exist, create a dummy status file to prevent poll status timeouts for already executed Run Commands after upgrade.
	if mrseqFilesNameList != nil && mrseqFilesNameList.Len() > 0 {
		// This is best effort - Do not return error if any case of failures.
		// Worst case that could happen is poll status timeouts for those few cases where creating dummy status file failed for some reason.
		createDummyStatusFilesIfNeeded(ctx, mrseqFilesNameList)
	}

	return nil
}

// Copy files like *.mrseq (Most Recently executed Sequence number), .status files from old extension version to new extension version during update.
func copyFiles(ctx log.Logger, fileExtensionSuffix string, extensionSubdirectory string) (*list.List, error) {

	newExtensionVersion := os.Getenv(constants.ExtensionVersionEnvName)
	oldExtensionVersion := os.Getenv(constants.ExtensionVersionUpdatingFromEnvName)

	ctx.Log("message", fmt.Sprintf("Migrating '%s' files from extension version '%s' to '%s'", fileExtensionSuffix, oldExtensionVersion, newExtensionVersion))

	newExtensionDirectory := os.Getenv(constants.ExtensionPathEnvName)
	oldExtensionDirectory := strings.ReplaceAll(newExtensionDirectory, newExtensionVersion, oldExtensionVersion)

	// Append subdirectory like "status" under extension folder if provided.
	if extensionSubdirectory != "" {
		newExtensionDirectory = filepath.Join(newExtensionDirectory, extensionSubdirectory)
		oldExtensionDirectory = filepath.Join(oldExtensionDirectory, extensionSubdirectory)

		// Create subdirectory like "status" directory if it does not exist
		_, err := os.Open(newExtensionDirectory)
		if err != nil {
			errr := os.Mkdir(newExtensionDirectory, 0700)
			if errr != nil {
				return nil, errors.Wrap(errr, fmt.Sprintf("Failed to create directory '%s'", newExtensionDirectory))
			}
		}
	}

	if oldExtensionDirectory == "" || newExtensionDirectory == "" {
		return nil, errors.New("oldExtesionDirectory or newExtensionDirectory is empty")
	}

	// Check if the directory exists
	sourceDirectoryFDRef, err := os.Open(oldExtensionDirectory)
	if err != nil {
		errMessage := fmt.Sprintf("could not open sourceDirectory %s", oldExtensionDirectory)
		ctx.Log("message", errMessage)
		return nil, errors.Wrap(err, errMessage)
	}

	directoryEntries, err := sourceDirectoryFDRef.ReadDir(0)
	if err != nil {
		errMessage := fmt.Sprintf("could not read directory entries from sourceDirectory %s", oldExtensionDirectory)
		ctx.Log("message", errMessage)
		return nil, errors.Wrap(err, errMessage)
	}

	numberOfFilesMigrated := 0
	fileNamesMigrated := list.New()

	for _, dirEntry := range directoryEntries {
		fileName := dirEntry.Name()

		if strings.HasSuffix(fileName, fileExtensionSuffix) {
			sourceFileFullPath := filepath.Join(oldExtensionDirectory, fileName)
			destinationFileFullPath := filepath.Join(newExtensionDirectory, fileName)

			sourceFile, sourceFileOpenError := os.Open(sourceFileFullPath)
			if sourceFileOpenError != nil {
				errMessage := "Failed to open '%s' file '%s' for reading. Contact ICM team AzureRT\\Extensions for this service error."
				ctx.Log("message", fmt.Sprintf(errMessage, fileExtensionSuffix, sourceFileFullPath))
				return fileNamesMigrated, errors.Wrapf(sourceFileOpenError, errMessage)
			}
			defer sourceFile.Close()

			destFile, destFileCreateError := os.Create(destinationFileFullPath)
			if destFileCreateError != nil {
				errMessage := "Failed to create '%s' file '%s'. Contact ICM team AzureRT\\Extensions for this service error."
				ctx.Log("message", fmt.Sprintf(errMessage, fileExtensionSuffix, destinationFileFullPath))
				return fileNamesMigrated, errors.Wrapf(destFileCreateError, errMessage)
			}
			defer destFile.Close()

			_, copyError := io.Copy(destFile, sourceFile)
			if copyError != nil {
				errMessage := fmt.Sprintf("Failed to copy '%s' file '%s' to path '%s'. Contact ICM team AzureRT\\Extensions for this service error.",
					fileExtensionSuffix, sourceFileFullPath, destinationFileFullPath)
				ctx.Log("message", errMessage)
				return fileNamesMigrated, errors.Wrapf(copyError, errMessage)
			} else {
				ctx.Log("message", fmt.Sprintf("File '%s' was copied successfully to '%s'", sourceFileFullPath, destinationFileFullPath))
				numberOfFilesMigrated++
				fileNamesMigrated.PushBack(fileName)
			}
		}
	}

	ctx.Log("message", fmt.Sprintf("Migrated %d '%s' files from extension version '%s' to '%s'", numberOfFilesMigrated, fileExtensionSuffix, oldExtensionVersion, newExtensionVersion))

	return fileNamesMigrated, nil
}

// This need to be only executed by Update operation
func createDummyStatusFilesIfNeeded(ctx log.Logger, mrseqFilesNameList *list.List) error {
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
	var allErr error = errors.New("Refer to all error messages above.")

	for mreSeqFileNameElement := mrseqFilesNameList.Front(); mreSeqFileNameElement != nil; mreSeqFileNameElement = mreSeqFileNameElement.Next() {
		mrSeqFileName = (mreSeqFileNameElement.Value).(string)

		// Read the most recently executed sequence number from the .mrseq file
		mrSeqFileFullPath = filepath.Join(newExtensionDirectory, mrSeqFileName)
		content, err = os.ReadFile(mrSeqFileFullPath)
		if err != nil {
			errorMessage = fmt.Sprintf("Reading mrseq (Most Recently executed Sequence number) from file '%s' failed with error '%s'", mrSeqFileFullPath, err.Error())
			ctx.Log("error", errorMessage)
			allErr = errors.Wrap(allErr, errorMessage)
			continue
		}

		var mrseqNumber int
		if content != nil {
			mrseqNumberString := string(content)
			mrseqNumber, err = strconv.Atoi(mrseqNumberString)
			if err != nil {
				errorMessage = fmt.Sprintf("mrseqNumberString to mrseqNumber conversion (string to int) of '%s' failed with error '%s'", mrseqNumberString, err.Error())
				ctx.Log("error", errorMessage)
				allErr = errors.Wrap(allErr, errorMessage)
				continue
			}
		} else {
			errorMessage = fmt.Sprintf("Empty .mrseq file content. No sequence number was found inside file  '%s' ", mrSeqFileFullPath)
			ctx.Log("error", errorMessage)
			allErr = errors.Wrap(allErr, errorMessage)
			continue
		}

		// Find extension name from the .mrseq file
		mrSeqFileExtensionIndex = strings.Index(mrSeqFileName, constants.MrSeqFileExtension)
		if mrSeqFileExtensionIndex == -1 {
			errorMessage = fmt.Sprintf("Invalid mrseq file '%s'", mrSeqFileName)
			ctx.Log("error", errorMessage)
			allErr = errors.Wrap(allErr, errorMessage)
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
				allErr = errors.Wrap(allErr, errorMessage)
				continue
			}

			statusReport := types.NewStatusReport(types.StatusSuccess, "Enable", instanceViewMessage, extensionName)
			rootStatusJson, err = status.MarshalStatusReportIntoJson(statusReport, true)
			if err != nil {
				errorMessage = fmt.Sprintf("failed to marshal status report into json for status file '%s' with error '%s'", statusFilePath, err.Error())
				allErr = errors.Wrap(allErr, errorMessage)
				continue
			}

			err = status.SaveStatusReport(statusFileDirectoryPath, extensionName, mrseqNumber, rootStatusJson)
			if err != nil {
				errorMessage = fmt.Sprintf("Failed to create a dummy status file '%s' as it was not existing for .mrseq file '%s' with error '%s'", statusFilePath, mrSeqFileFullPath, err.Error())
				allErr = errors.Wrap(allErr, errorMessage)
				continue
			}
		}
	}
	return allErr
}

// downloadScript downloads the script file specified in cfg into dir (creates if does
// not exist) and takes storage credentials specified in cfg into account.
func downloadScript(ctx *log.Context, dir string, cfg *handlersettings.HandlerSettings) (string, error) {
	// - prepare the output directory for files and the command output
	// - create the directory if missing
	ctx.Log("event", "creating output directory", "path", dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", errors.Wrap(err, "failed to prepare output directory")
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
		file, err := files.DownloadAndProcessScript(ctx, scriptURI, dir, cfg)
		if err != nil {
			ctx.Log("event", "download failed", "error", err)
			return "", errors.Wrapf(err, "failed to download file %s. ", scriptURI)
		}
		scriptFilePath = file
		ctx.Log("event", "download complete", "output", dir)
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
	err, exitCode = exec.ExecCmdInDir(ctx, scriptFilePath, dir, cfg)
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
		return "", "", errors.Wrap(err, "failed to decode script")
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
		return "", "", errors.Wrap(err, "failed to decompress script")
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
			return nil, errors.New("Managed identity's ObjectId is not supported. Use ClientId instead")
		}
	}

	if ID != "" { // Use user-assigned identity if clientId is provided
		miCredentialOptions := azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(ID)}
		miCred, miCredError = azidentity.NewManagedIdentityCredential(&miCredentialOptions)
	} else { // Use system-assigned identity if clientId not provided
		miCred, miCredError = azidentity.NewManagedIdentityCredential(nil)
	}

	var appendBlobClient *appendblob.Client
	var appendBlobNewClientError error
	if miCredError == nil {
		appendBlobClient, appendBlobNewClientError = appendblob.NewClient(blobUri, miCred, nil)
		if appendBlobNewClientError != nil {
			return nil, errors.Wrap(appendBlobNewClientError, fmt.Sprintf("Error Creating client to Append Blob '%s'. Make sure you are using Append blob. Other types of blob such as PageBlob, BlockBlob are not supported types.", download.GetUriForLogging(blobUri)))
		} else {
			// Create or Replace Append blob. If AppendBlob already exists, blob gets cleared.
			_, createAppendBlobError := appendBlobClient.Create(context.Background(), nil)
			if createAppendBlobError != nil {
				return nil, errors.Wrap(createAppendBlobError, fmt.Sprintf("Error creating or replacing the Append blob '%s'. Make sure you are using Append blob. Other types of blob such as PageBlob, BlockBlob are not supported types.", download.GetUriForLogging(blobUri)))
			}
		}
	} else {
		return nil, errors.Wrap(miCredError, "Error while retrieving managed identity credential")
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
