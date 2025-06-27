package constants

const (
	// The directory of the Microsoft Azure Linux VM Agent (waagent).
	WaAgentDirectory = "/var/lib/waagent"

	// dataDir is where we store the downloaded files, logs and state for
	// the extension handler
	DataDir = WaAgentDirectory + "/run-command-handler"

	// Directory used for copying the Run Command script file to be able to RunAs a different user.
	// It needs to copied because of permission restrictions. RunAsUser does not have permission to execute under /var/lib/waagent and its subdirectories.
	// %s needs to be replaced by '<RunAsUser>' (RunAs username)
	RunAsDir = "/home/%s/waagent/run-command-handler-runas"

	// ConfigSequenceNumberEnvName environment variable should be set by VMAgent to sequence number
	ConfigSequenceNumberEnvName = "ConfigSequenceNumber"

	// ConfigExtensionNameEnvName environment variable should be set by VMAgent to extension name
	ConfigExtensionNameEnvName = "ConfigExtensionName"

	ConfigFileExtension = ".settings"

	MrSeqFileExtension = ".mrseq"

	StatusFileDirectory = "status"

	StatusFileExtension = ".status"

	// The directory where the immediate run command status that have reached the terminal status are stored.
	ImmediateStatusFileDirectory = "status"

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

	// The current version of the extension. This value is provided by the agent for all commands.
	// See more in: https://github.com/Azure/azure-vmextension-publishing/wiki/2.0-Partner-Guide-Handler-Design-Details#236-summary
	ExtensionVersionEnvName = "AZURE_GUEST_AGENT_EXTENSION_VERSION"

	// This is the version the extension is updating from
	// See more in: https://github.com/Azure/azure-vmextension-publishing/wiki/2.0-Partner-Guide-Handler-Design-Details#236-summary
	ExtensionVersionUpdatingFromEnvName = "AZURE_GUEST_AGENT_UPDATING_FROM_VERSION"

	// The path of the extension in the VM with full name. This value is provided by the agent for all commands.
	// See more in: https://github.com/Azure/azure-vmextension-publishing/wiki/2.0-Partner-Guide-Handler-Design-Details#236-summary
	ExtensionPathEnvName = "AZURE_GUEST_AGENT_EXTENSION_PATH"

	// The name of the immediate run command service
	ImmediateRunCommandHandlerName = "runCommandService"

	// The time to wait between each poll of the goal states
	PolingIntervalInSeconds = 1

	// The name of the file that contains the immediate goal states that reached the terminal status
	ImmediateGoalStatesInTerminalStatusFileName = "immediateGoalStatesInTerminalStatusFile.status"
)
