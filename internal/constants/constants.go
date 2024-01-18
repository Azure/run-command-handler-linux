package constants

var (
	// dataDir is where we store the downloaded files, logs and state for
	// the extension handler
	DataDir = "/var/lib/waagent/run-command-handler"

	// Directory used for copying the Run Command script file to be able to RunAs a different user.
	// It needs to copied because of permission restrictions. RunAsUser does not have permission to execute under /var/lib/waagent and its subdirectories.
	// %s needs to be replaced by '<RunAsUser>' (RunAs username)
	RunAsDir = "/home/%s/waagent/run-command-handler-runas"

	// seqNumFile holds the processed highest sequence number to make
	// sure we do not run the command more than once for the same sequence
	// number. Stored under dataDir.
	//seqNumFile = "seqnum"

	// most recent sequence, which was previously traced by seqNumFile. This was
	// incorrect. The correct way is mrseq.  This file is auto-preserved by the agent.
	MostRecentSequence = "mrseq"

	// Filename where active process keeps track of process id and process start time
	PidFilePath = "pidstart"

	// downloadDir is where we store the downloaded files in the "{downloadDir}/{seqnum}/file"
	// format and the logs as "{downloadDir}/{seqnum}/std(out|err)". Stored under dataDir
	// multiconfig support - when extName is set we use {downloadDir}/{extName}/...
	DownloadDir = "download"

	// ConfigSequenceNumber environment variable should be set by VMAgent to sequence number
	ConfigSequenceNumber = "ConfigSequenceNumber"

	// ConfigExtensionName environment variable should be set by VMAgent to extension name
	ConfigExtensionName = "ConfigExtensionName"

	ConfigFileExtension = ".settings"

	// General failed exit code when extension provisioning fails due to service errors.
	FailedExitCodeGeneral = -1
)
