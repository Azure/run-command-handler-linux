package constants

const (
	// Exit codes
	ExitCode_Okay = 0

	// User errors (-100s):
	ExitCode_ScriptBlobDownloadFailed  = -100
	ExitCode_BlobCreateOrReplaceFailed = -101
	ExitCode_RunAsLookupUserFailed     = -102

	// Service Errors (-200s):
	ExitCode_CreateDataDirectoryFailed                    = -200
	ExitCode_RemoveDataDirectoryFailed                    = -201
	ExitCode_GetHandlerSettingsFailed                     = -202
	ExitCode_SaveScriptFailed                             = -203
	ExitCode_CommandExecutionFailed                       = -204
	ExitCode_OpenStdOutFileFailed                         = -205
	ExitCode_OpenStdErrFileFailed                         = -206
	ExitCode_IncorrectRunAsScriptPath                     = -207
	ExitCode_RunAsIncorrectScriptPath                     = -208
	ExitCode_RunAsOpenSourceScriptFileFailed              = -209
	ExitCode_RunAsCreateRunAsScriptFileFailed             = -210
	ExitCode_RunAsCopySourceScriptToRunAsScriptFileFailed = -211
	ExitCode_RunAsLookupUserUidFailed                     = -212
	ExitCode_RunAsScriptFileChangeOwnerFailed             = -213
	ExitCode_RunAsScriptFileChangePermissionsFailed       = -214
	ExitCode_DownloadArtifactFailed                       = -215
	ExitCode_UpgradeInstalledServiceFailed                = -216
	ExitCode_InstallServiceFailed                         = -217
	ExitCode_UninstallInstalledServiceFailed              = -218
	ExitCode_DisableInstalledServiceFailed                = -219
	ExitCode_CopyStateForUpdateFailed                     = -220

	// Unknown errors (-300s):
)
