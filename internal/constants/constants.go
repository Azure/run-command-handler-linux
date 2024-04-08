package constants

const (
	// dataDir is where we store the downloaded files, logs and state for
	// the extension handler
	DataDir = "/var/lib/waagent/run-command-handler"

	// Directory used for copying the Run Command script file to be able to RunAs a different user.
	// It needs to copied because of permission restrictions. RunAsUser does not have permission to execute under /var/lib/waagent and its subdirectories.
	// %s needs to be replaced by '<RunAsUser>' (RunAs username)
	RunAsDir = "/home/%s/waagent/run-command-handler-runas"

	// ConfigSequenceNumberEnvName environment variable should be set by VMAgent to sequence number
	ConfigSequenceNumberEnvName = "ConfigSequenceNumber"

	// ConfigExtensionNameEnvName environment variable should be set by VMAgent to extension name
	ConfigExtensionNameEnvName = "ConfigExtensionName"

	ConfigFileExtension = ".settings"

	// General failed exit code when extension provisioning fails due to service errors.
	FailedExitCodeGeneral = -1

	// The output directory for logs of immediate run command
	ImmediateRCOutputDirectory = "/var/log/azure/run-command-handler/ImmediateRunCommandService.log"

	// Download folder to use for standard managed run command
	DownloadFolder = "download/"

	// Download folder to use for immediate run command
	ImmediateDownloadFolder = "immediateDownload/"

	// Name of the run command extension
	RunCommandExtensionName = "Microsoft.CPlat.Core.RunCommandHandlerLinux"
)
