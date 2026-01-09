package constants

const (
	// Exit codes
	ExitCode_Okay = 0

	// Service Errors (-200s):
	ExitCode_UpgradeInstalledServiceFailed   = -216
	ExitCode_InstallServiceFailed            = -217
	ExitCode_UninstallInstalledServiceFailed = -218
	ExitCode_DisableInstalledServiceFailed   = -219
	ExitCode_CopyStateForUpdateFailed        = -220
	ExitCode_CouldNotRehydrateMrSeq          = -224
)
