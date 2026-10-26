package cleanup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/Azure/run-command-handler-linux/pkg/linuxutils"
	"github.com/go-kit/kit/log"
)

var (
	openDownloadDirectory   = func(name string) (io.Closer, error) { return os.Open(name) }
	clearExceptMostRecent   = linuxutils.TryClearExtensionScriptsDirectoriesAndSettingsFilesExceptMostRecent
	deleteDirectoriesExcept = linuxutils.TryDeleteDirectoriesExcept
)

func ImmediateRunCommandCleanup(ctx *log.Context, metadata types.RCMetadata, h types.HandlerEnvironment, runAsUser string) {
	deleteAllScriptsAndSettings(ctx, metadata, h, runAsUser)
}

func RunCommandCleanup(ctx *log.Context, metadata types.RCMetadata, h types.HandlerEnvironment, runAsUser string) {
	deleteScriptsAndSettingsExceptMostRecent(ctx, metadata, h, runAsUser)
}

func deleteAllScriptsAndSettings(ctx *log.Context, metadata types.RCMetadata, h types.HandlerEnvironment, runAsUser string) {
	runtimeSettingsRegexFormat := metadata.ExtName + ".\\d+.settings"

	ctx.Log("message", "removing settings and script files")
	err := linuxutils.TryClearExtensionScriptsDirectoriesAndSettingsFiles(ctx, metadata.DownloadPath, h.HandlerEnvironment.ConfigFolder, "", runtimeSettingsRegexFormat)
	if err != nil {
		ctx.Log("warning", "failed to remove both. See error for more details", "error", err)
	}

	if runAsUser != "" {
		runAsDownloadParent := filepath.Join(fmt.Sprintf(constants.RunAsDir, runAsUser), metadata.DownloadDir)
		ctx.Log("message", "removing all files from the download 'runas' directory "+runAsDownloadParent)
		err = linuxutils.TryDeleteDirectories(ctx, runAsDownloadParent)
		if err != nil {
			ctx.Log("event", "could not clear runas script", "error", err)
		}
	}
}

func deleteScriptsAndSettingsExceptMostRecent(ctx *log.Context, metadata types.RCMetadata, h types.HandlerEnvironment, runAsUser string) {
	runtimeSettingsRegexFormat := metadata.ExtName + ".\\d+.settings"
	runtimeSettingsLastSeqNumFormat := metadata.ExtName + ".%d.settings"

	// check if directory exists
	dir, err := openDownloadDirectory(metadata.DownloadPath)
	if err == nil {
		dir.Close()
		err := clearExceptMostRecent(ctx, metadata.DownloadPath, h.HandlerEnvironment.ConfigFolder, "",
			uint64(metadata.SeqNum), runtimeSettingsRegexFormat, runtimeSettingsLastSeqNumFormat)
		if err != nil {
			ctx.Log("event", "could not clear settings and script files", "error", errorutil.WithCode(errorutil.Os_FailedToCleanOldScriptsAndSettings, err))
		}
	} else if !os.IsNotExist(err) {
		ctx.Log("event", "could not open download directory", "error", errorutil.WithCode(errorutil.Os_FailedToOpenDownloadDirectory, err))
	} else {
		ctx.Log("message", "directory does not exist. Skipping cleanup")
	}

	if runAsUser != "" {
		runAsDownloadParent := filepath.Join(fmt.Sprintf(constants.RunAsDir, runAsUser), metadata.DownloadDir)
		seqNumString := strconv.Itoa(metadata.SeqNum)
		ctx.Log("message", "removing all files from the download 'runas' directory "+runAsDownloadParent)
		err = deleteDirectoriesExcept(ctx, runAsDownloadParent, seqNumString)
		if err != nil {
			ctx.Log("event", "could not clear runas script", "error", errorutil.WithCode(errorutil.Os_FailedToCleanOldRunAsDirectories, err))
		}
	}
}
