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
