package constants

const (
	FileDownload_BadRequest                  = -41
	FileDownload_UnknownError                = -40
	FileDownload_StorageError                = -42
	FileDownload_UnhandledError              = -43
	FileDownload_StorageClientInitialization = -44
	FileDownload_CreateDirectoryFailure      = -45
	FileDownload_OpenFileForWriteFailure     = -46
	FileDownload_CouldNotCreateRequest       = -47
	FileDownload_InternalServerError         = -48
	FileDownload_WriteFileError              = -49

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
	Internal_UnmarshalPublicSettingsFailed     = -40
	Internal_InvalidArtifactSpecification      = -41

	Internal_CouldNotCreateStatusDirectory = -60
	Internal_ExtensionDirectoryNameEmpty   = -61
	Internal_CouldNotOpenSubdirectory      = -62
	Internal_CouldNotReadDirectoryEntries  = -63
	Internal_FailedToOpenFileForReading    = -64
	Internal_FailedToCreateFile            = -65
	Internal_FailedToCopyFile              = -66
	Internal_FailedToReadFile              = -67
	Internal_CouldNotOpenFileForWriting    = -68

	Immediate_CouldNotDetermineServiceInstalled  = -70
	Immediate_CouldNotDetermineInstalledVersion  = -71
	Immediate_CouldNotMarkBinaryAsExecutable     = -72
	Immediate_CouldNotRemoveOldUnitConfigFile    = -73
	Immediate_ErrorCreatingUnitConfig            = -74
	Immediate_ErrorReloadingDaemonWorker         = -75
	Immediate_ErrorEnablingUnit                  = -76
	Immediate_CouldNotStartService               = -77
	Immediate_CouldNotCheckServiceAlreadyEnabled = -78
	Immediate_EnableServiceFailed                = -79

	Script_FailedToDecode     = -101
	Script_FailedToDecompress = -102

	Hgap_FailedCreateRequest             = -120
	Hgap_CertificateMissingFromGoalState = -121
	Hgap_NoCertThumbprint                = -122
	Hgap_FailedToCreateRequestFactory    = -123
	Hgap_FailedToParseAddress            = -124
	Hgap_EtagNotFound                    = -125
	Hgap_FailedToParseImmediateSettings  = -126
	Hgap_CouldNotCreateRequestManager    = -127
	Hgap_InternalArgumentError           = -128

	HandlerEnv_CouldNotFindBaseDirectory = -140
	HandlerEnv_HandlingError             = -141
	HandlerEnv_NotFound                  = -142
	HandlerEnv_UnmarshalFailed           = -143
	HandlerEnv_InvalidConfigCount        = -144

	Msi_CouldNotDeserializeResponse = -90

	Internal_UnknownError = -200

	SystemError = -1 // CRP will interpret anything > 0 as a user error

	// User errors
	CommandExecution_BadConfig                = 1
	CommandExecution_FailureExitCode          = 2
	CommandExecution_TimedOut                 = 4
	CommandExecution_RunAsCreateProcessFailed = 5
	CommandExecution_RunAsUserLogonFailed     = 6
	CommandExecution_CouldNotStart            = 7

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

	AppendBlobCreation_DoesNotExist         = 90
	AppendBlobCreation_PermissionsIssue     = 91
	AppendBlobCreation_Other                = 92
	AppendBlobCreation_InvalidUri           = 93
	AppendBlobCreation_InvalidMsi           = 94
	AppendBlobCreation_ObjectIdNotSupported = 95
	AppendBlobCreation_ClientError          = 96

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

	Immediate_Systemd_NotSupported = 140

	Http_RequestFailure   = 150
	Http_FailedStatusCode = 151
)
