package constants

const (
	FileDownload_BadRequest     = -41
	FileDownload_UnknownError   = -40
	FileDownload_StorageError   = -42
	FileDownload_UnhandledError = -43

	Internal_CouldNotFindCertificate = -20
	Internal_CouldNotDecrypt         = -22
	Internal_ArtifactCountMismatch   = -23
	Internal_ArtifactDoesNotExist    = -24

	SystemError = -1 // CRP will interpret anything > 0 as a user error

	// User errors
	CommandExecution_BadConfig                = 1
	CommandExecution_FailureExitCode          = 2
	CommandExecution_TimedOut                 = 4
	CommandExecution_RunAsCreateProcessFailed = 5
	CommandExecution_RunAsUserLogonFailed     = 6

	CustomerInput_StorageCredsAndMIBothSpecified = 26
	CustomerInput_ClientIdObjectIdBothSpecified  = 27
	CustomerInput_ErrorAndOutputBlobsSame        = 28

	FileDownload_AccessDenied      = 52
	FileDownload_DoesNotExist      = 53
	FileDownload_NetworkingError   = 54
	FileDownload_GenericError      = 55
	FileDownload_UnableToWriteFile = 57

	Msi_NotFound                    = 70
	Msi_DoesNotHaveRightPermissions = 71
	Msi_GenericRetrievalError       = 72

	AppendBlobCreation_DoesNotExist     = 90
	AppendBlobCreation_PermissionsIssue = 91
	AppendBlobCreation_Other            = 92
	AppendBlobCreation_InvalidUri       = 93
	AppendBlobCreation_InvalidMsi       = 94

	ImmediateRC_ExceededConcurrentLimit = 100
	ImmediateRC_TaskCanceled            = 101
	ImmediateRC_TaskTimeout             = 102
	ImmediateRC_UnknownFailure          = 103
	ImmediateRC_UnhandledException      = 104
)

func TranslateExitCodeToErrorClarification(exitCode int) int {
	switch exitCode {
	case ExitCode_Okay:
		return 0 // Success, no error clarification needed

	// User errors (-100s) -> map to positive user error codes
	case ExitCode_ScriptBlobDownloadFailed:
		return FileDownload_GenericError
	case ExitCode_BlobCreateOrReplaceFailed:
		return AppendBlobCreation_Other
	case ExitCode_RunAsLookupUserFailed:
		return CommandExecution_RunAsUserLogonFailed

	// Service errors (-200s) -> map based on specific failure type
	case ExitCode_CreateDataDirectoryFailed, ExitCode_RemoveDataDirectoryFailed:
		return SystemError // File system operation failures
	case ExitCode_GetHandlerSettingsFailed:
		return CommandExecution_BadConfig
	case ExitCode_SaveScriptFailed:
		return FileDownload_UnableToWriteFile
	case ExitCode_CommandExecutionFailed:
		return CommandExecution_FailureExitCode
	case ExitCode_OpenStdOutFileFailed, ExitCode_OpenStdErrFileFailed:
		return SystemError // I/O failures
	case ExitCode_IncorrectRunAsScriptPath, ExitCode_RunAsIncorrectScriptPath:
		return CommandExecution_BadConfig
	case ExitCode_RunAsOpenSourceScriptFileFailed:
		return FileDownload_DoesNotExist
	case ExitCode_RunAsCreateRunAsScriptFileFailed, ExitCode_RunAsCopySourceScriptToRunAsScriptFileFailed:
		return FileDownload_UnableToWriteFile
	case ExitCode_RunAsLookupUserUidFailed:
		return CommandExecution_RunAsUserLogonFailed
	case ExitCode_RunAsScriptFileChangeOwnerFailed, ExitCode_RunAsScriptFileChangePermissionsFailed:
		return SystemError // Permission/ownership failures
	case ExitCode_DownloadArtifactFailed:
		return FileDownload_GenericError
	case ExitCode_UpgradeInstalledServiceFailed, ExitCode_InstallServiceFailed,
		ExitCode_UninstallInstalledServiceFailed, ExitCode_DisableInstalledServiceFailed:
		return SystemError // Service management failures
	case ExitCode_CopyStateForUpdateFailed:
		return FileDownload_UnableToWriteFile
	case ExitCode_SkippedImmediateGoalState:
		return ImmediateRC_TaskCanceled
	case ExitCode_ImmediateTaskTimeout:
		return ImmediateRC_TaskTimeout
	case ExitCode_ImmediateTaskFailed:
		return ImmediateRC_UnknownFailure

	// Handle standard Linux exit codes
	default:
		switch {
		case exitCode == 0:
			return 0 // Success
		case exitCode > 0 && exitCode < 128:
			// Standard program exit codes (1-127)
			if exitCode == 1 {
				return CommandExecution_FailureExitCode
			} else if exitCode == 126 {
				return CommandExecution_RunAsCreateProcessFailed // Command not executable
			} else if exitCode == 127 {
				return FileDownload_DoesNotExist // Command not found
			} else {
				return CommandExecution_FailureExitCode
			}
		case exitCode >= 128 && exitCode <= 255:
			// Signal-terminated processes (128 + signal number)
			if exitCode == 130 { // SIGINT (Ctrl+C)
				return ImmediateRC_TaskCanceled
			} else if exitCode == 137 { // SIGKILL
				return ImmediateRC_TaskTimeout
			} else if exitCode == 143 { // SIGTERM
				return ImmediateRC_TaskCanceled
			} else {
				return ImmediateRC_UnhandledException
			}
		case exitCode < 0:
			// Negative exit codes - internal errors
			return SystemError
		default:
			// Unknown exit codes
			return ImmediateRC_UnknownFailure
		}
	}
}
