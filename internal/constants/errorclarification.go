package constants

const (
	FileDownload_BadRequest                  = -41
	FileDownload_UnknownError                = -40
	FileDownload_StorageError                = -42
	FileDownload_UnhandledError              = -43
	FileDownload_StorageClientInitialization = -44

	Internal_CouldNotFindCertificate                      = -20
	Internal_CouldNotDecrypt                              = -22
	Internal_ArtifactCountMismatch                        = -23
	Internal_ArtifactDoesNotExist                         = -24
	Internal_IncorrectRunAsScriptPath                     = -25
	Internal_RunAsOpenSourceScriptFileFailed              = -26
	Internal_RunAsCreateRunAsScriptFileFailed             = -27
	Internal_RunAsCopySourceScriptToRunAsScriptFileFailed = -28
	Internal_RunAsLookupUserUidFailed                     = -29
	Internal_RunAsScriptFileChangeOwnerFailed             = -30
	Internal_RunAsScriptFileChangePermissionsFailed       = -31

	Internal_CouldNotParseSettings             = -32
	Internal_InvalidHandlerSettingsJson        = -33
	Internal_InvalidHandlerSettingsCount       = -34
	Internal_NoHandlerSettingsThumbprint       = -35
	Internal_HandlerSettingsFailedToDecode     = -36
	Internal_DecryptingProtectedSettingsFailed = -37
	Internal_UnmarshalProtectedSettingsFailed  = -38
	Internal_UnmarshalSettingsFailed           = -39

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
	CustomerInput_NoScriptSpecified              = 29

	FileDownload_AccessDenied                 = 52
	FileDownload_DoesNotExist                 = 53
	FileDownload_NetworkingError              = 54
	FileDownload_GenericError                 = 55
	FileDownload_UnableToWriteFile            = 57
	ArtifactDownload_GenericError             = 58
	FileDownload_UnableToParseFileName        = 59
	FileDownload_CannotExtractFileNameFromUrl = 60
	FileDownload_InvalidFileName              = 61
	FileDownload_FailedStatusCode             = 62
	FileDownload_CannotParseUrl               = 63
	FileDownload_CannotGenerateSasKey         = 64
	FileDownload_Empty                        = 65

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
	ImmediateRC_CommandSkipped          = 105

	FileSystem_CreateDataDirectoryFailed = 110
	FileSystem_RemoveDataDirectoryFailed = 121
	FileSystem_OpenStandardOutFailed     = 122
	FileSystem_OpenStandardErrorFailed   = 123
)
