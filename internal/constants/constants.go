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
)
