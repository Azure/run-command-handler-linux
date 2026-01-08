package cleanup

import (
	"fmt"
	"path/filepath"

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
			ctx.Log("event", "could not clear runas script")
		}
	}
}

func deleteScriptsAndSettingsExceptMostRecent(ctx *log.Context, metadata types.RCMetadata, h types.HandlerEnvironment, runAsUser string) {
	// runtimeSettingsRegexFormat := metadata.ExtName + ".\\d+.settings"
	// runtimeSettingsLastSeqNumFormat := metadata.ExtName + ".%d.settings"

	// // check if directory exists
	// _, err := os.Open(metadata.DownloadPath)
	// if err == nil {
	// 	err := utils.TryClearExtensionScriptsDirectoriesAndSettingsFilesExceptMostRecent(metadata.DownloadPath, h.HandlerEnvironment.ConfigFolder, "",
	// 		uint64(metadata.SeqNum), runtimeSettingsRegexFormat, runtimeSettingsLastSeqNumFormat)
	// 	if err != nil {
	// 		ctx.Log("event", "could not clear settings and script files", "error", err)
	// 	}
	// } else {
	// 	ctx.Log("message", "directory does not exist. Skipping cleanup")
	// }

	// if runAsUser != "" {
	// 	runAsDownloadParent := filepath.Join(fmt.Sprintf(constants.RunAsDir, runAsUser), metadata.DownloadDir)
	// 	seqNumString := strconv.Itoa(metadata.SeqNum)
	// 	ctx.Log("message", "removing all files from the download 'runas' directory "+runAsDownloadParent)
	// 	err = utils.TryDeleteDirectoriesExcept(runAsDownloadParent, seqNumString)
	// 	if err != nil {
	// 		ctx.Log("event", "could not clear runas script")
	// 	}
	// }
}
