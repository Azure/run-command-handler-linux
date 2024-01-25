package cleanup

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/Azure/azure-extension-platform/pkg/utils"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/linuxutils"
	"github.com/go-kit/kit/log"
)

func ImmediateRunCommandCleanup(ctx *log.Context, metadata types.RCMetadata, h types.HandlerEnvironment, runAsUser string) {
	deleteAllScriptsAndSettings(ctx, metadata, h, runAsUser)
}

func RunCommandCleanup(ctx *log.Context, metadata types.RCMetadata, h types.HandlerEnvironment, runAsUser string) {
	deleteScriptsAndSettingsExceptMostRecent(ctx, metadata, h, runAsUser)
}

func deleteAllScriptsAndSettings(ctx *log.Context, metadata types.RCMetadata, h types.HandlerEnvironment, runAsUser string) {
	downloadParent := filepath.Join(constants.DataDir, metadata.DownloadDir)
	runtimeSettingsRegexFormat := metadata.ExtName + ".\\d+.settings"

	ctx.Log("message", "removing settings and script files")
	err := linuxutils.TryClearExtensionScriptsDirectoriesAndSettingsFiles(ctx, downloadParent, h.HandlerEnvironment.ConfigFolder, "", runtimeSettingsRegexFormat)
	if err != nil {
		ctx.Log("warning", "failed to remove both. See error for more details", "error", err)
	}

	if runAsUser != "" {
		runAsDownloadParent := filepath.Join(fmt.Sprintf(constants.RunAsDir, runAsUser), constants.DataDir)
		ctx.Log("message", "removing all files from the download 'runas' directory "+runAsDownloadParent)
		err = linuxutils.TryDeleteDirectories(ctx, runAsDownloadParent)
		if err != nil {
			ctx.Log("event", "could not clear runas script")
		}
	}
}

func deleteScriptsAndSettingsExceptMostRecent(ctx *log.Context, metadata types.RCMetadata, h types.HandlerEnvironment, runAsUser string) {
	downloadParent := filepath.Join(constants.DataDir, metadata.DownloadDir)
	runtimeSettingsRegexFormat := metadata.ExtName + ".\\d+.settings"
	runtimeSettingsLastSeqNumFormat := metadata.ExtName + ".%d.settings"

	ctx.Log("event", "clearing settings and script files except most recent seq num")
	err := utils.TryClearExtensionScriptsDirectoriesAndSettingsFilesExceptMostRecent(downloadParent, h.HandlerEnvironment.ConfigFolder, "",
		uint64(metadata.SeqNum), runtimeSettingsRegexFormat, runtimeSettingsLastSeqNumFormat)
	if err != nil {
		ctx.Log("event", "could not clear settings and script files")
	}

	if runAsUser != "" {
		runAsDownloadParent := filepath.Join(fmt.Sprintf(constants.RunAsDir, runAsUser), constants.DataDir)
		seqNumString := strconv.Itoa(metadata.SeqNum)
		ctx.Log("message", "removing all files from the download 'runas' directory "+runAsDownloadParent)
		err = utils.TryDeleteDirectoriesExcept(runAsDownloadParent, seqNumString)
		if err != nil {
			ctx.Log("event", "could not clear runas script")
		}
	}
}
