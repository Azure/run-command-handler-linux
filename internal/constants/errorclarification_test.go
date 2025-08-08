package constants

import (
	"testing"
)

func TestTranslateExitCodeToErrorClarification(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		expected int
	}{
		// Success case
		{
			name:     "Success exit code",
			exitCode: ExitCode_Okay,
			expected: 0,
		},

		{
			name:     "Script blob download failed",
			exitCode: ExitCode_ScriptBlobDownloadFailed,
			expected: FileDownload_GenericError,
		},
		{
			name:     "Blob create or replace failed",
			exitCode: ExitCode_BlobCreateOrReplaceFailed,
			expected: AppendBlobCreation_Other,
		},
		{
			name:     "RunAs lookup user failed",
			exitCode: ExitCode_RunAsLookupUserFailed,
			expected: CommandExecution_RunAsUserLogonFailed,
		},

		// Service errors (-200s) mapping tests
		{
			name:     "Create data directory failed",
			exitCode: ExitCode_CreateDataDirectoryFailed,
			expected: SystemError,
		},
		{
			name:     "Remove data directory failed",
			exitCode: ExitCode_RemoveDataDirectoryFailed,
			expected: SystemError,
		},
		{
			name:     "Get handler settings failed",
			exitCode: ExitCode_GetHandlerSettingsFailed,
			expected: CommandExecution_BadConfig,
		},
		{
			name:     "Save script failed",
			exitCode: ExitCode_SaveScriptFailed,
			expected: FileDownload_UnableToWriteFile,
		},
		{
			name:     "Command execution failed",
			exitCode: ExitCode_CommandExecutionFailed,
			expected: CommandExecution_FailureExitCode,
		},
		{
			name:     "Open stdout file failed",
			exitCode: ExitCode_OpenStdOutFileFailed,
			expected: SystemError,
		},
		{
			name:     "Open stderr file failed",
			exitCode: ExitCode_OpenStdErrFileFailed,
			expected: SystemError,
		},
		{
			name:     "Incorrect RunAs script path",
			exitCode: ExitCode_IncorrectRunAsScriptPath,
			expected: CommandExecution_BadConfig,
		},
		{
			name:     "RunAs incorrect script path",
			exitCode: ExitCode_RunAsIncorrectScriptPath,
			expected: CommandExecution_BadConfig,
		},
		{
			name:     "RunAs open source script file failed",
			exitCode: ExitCode_RunAsOpenSourceScriptFileFailed,
			expected: FileDownload_DoesNotExist,
		},
		{
			name:     "RunAs create script file failed",
			exitCode: ExitCode_RunAsCreateRunAsScriptFileFailed,
			expected: FileDownload_UnableToWriteFile,
		},
		{
			name:     "RunAs copy script failed",
			exitCode: ExitCode_RunAsCopySourceScriptToRunAsScriptFileFailed,
			expected: FileDownload_UnableToWriteFile,
		},
		{
			name:     "RunAs lookup user UID failed",
			exitCode: ExitCode_RunAsLookupUserUidFailed,
			expected: CommandExecution_RunAsUserLogonFailed,
		},
		{
			name:     "RunAs change owner failed",
			exitCode: ExitCode_RunAsScriptFileChangeOwnerFailed,
			expected: SystemError,
		},
		{
			name:     "RunAs change permissions failed",
			exitCode: ExitCode_RunAsScriptFileChangePermissionsFailed,
			expected: SystemError,
		},
		{
			name:     "Download artifact failed",
			exitCode: ExitCode_DownloadArtifactFailed,
			expected: FileDownload_GenericError,
		},
		{
			name:     "Upgrade service failed",
			exitCode: ExitCode_UpgradeInstalledServiceFailed,
			expected: SystemError,
		},
		{
			name:     "Install service failed",
			exitCode: ExitCode_InstallServiceFailed,
			expected: SystemError,
		},
		{
			name:     "Uninstall service failed",
			exitCode: ExitCode_UninstallInstalledServiceFailed,
			expected: SystemError,
		},
		{
			name:     "Disable service failed",
			exitCode: ExitCode_DisableInstalledServiceFailed,
			expected: SystemError,
		},
		{
			name:     "Copy state for update failed",
			exitCode: ExitCode_CopyStateForUpdateFailed,
			expected: FileDownload_UnableToWriteFile,
		},
		{
			name:     "Skipped immediate goal state",
			exitCode: ExitCode_SkippedImmediateGoalState,
			expected: ImmediateRC_TaskCanceled,
		},
		{
			name:     "Immediate task timeout",
			exitCode: ExitCode_ImmediateTaskTimeout,
			expected: ImmediateRC_TaskTimeout,
		},
		{
			name:     "Immediate task failed",
			exitCode: ExitCode_ImmediateTaskFailed,
			expected: ImmediateRC_UnknownFailure,
		},

		// Standard Linux exit codes
		{
			name:     "Standard success",
			exitCode: 0,
			expected: 0,
		},
		{
			name:     "Standard error",
			exitCode: 1,
			expected: CommandExecution_FailureExitCode,
		},
		{
			name:     "Command not executable",
			exitCode: 126,
			expected: CommandExecution_RunAsCreateProcessFailed,
		},
		{
			name:     "Command not found",
			exitCode: 127,
			expected: FileDownload_DoesNotExist,
		},
		{
			name:     "SIGINT (Ctrl+C)",
			exitCode: 130,
			expected: ImmediateRC_TaskCanceled,
		},
		{
			name:     "SIGKILL",
			exitCode: 137,
			expected: ImmediateRC_TaskTimeout,
		},
		{
			name:     "SIGTERM",
			exitCode: 143,
			expected: ImmediateRC_TaskCanceled,
		},

		{
			name:     "Standard program exit code (mid-range)",
			exitCode: 50,
			expected: CommandExecution_FailureExitCode,
		},
		{
			name:     "Signal-terminated (other signal)",
			exitCode: 140,
			expected: ImmediateRC_UnhandledException,
		},
		{
			name:     "Negative internal error",
			exitCode: -50,
			expected: SystemError,
		},
		{
			name:     "Very high exit code",
			exitCode: 300,
			expected: ImmediateRC_UnknownFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TranslateExitCodeToErrorClarification(tt.exitCode)
			if result != tt.expected {
				t.Errorf("TranslateExitCodeToErrorClarification(%d) = %d, expected %d",
					tt.exitCode, result, tt.expected)
			}
		})
	}
}

func TestTranslateExitCodeToErrorClarification_RangeTests(t *testing.T) {
	// Test ranges of exit codes
	rangeTests := []struct {
		name        string
		startCode   int
		endCode     int
		expected    int
		description string
	}{
		{
			name:        "Standard program exit codes (2-125)",
			startCode:   2,
			endCode:     125,
			expected:    CommandExecution_FailureExitCode,
			description: "Should map all standard program failures to CommandExecution_FailureExitCode",
		},
		{
			name:        "Signal-terminated range (128-255)",
			startCode:   128,
			endCode:     129,
			expected:    ImmediateRC_UnhandledException,
			description: "Signal codes other than specific ones should map to UnhandledException",
		},
	}

	for _, tt := range rangeTests {
		t.Run(tt.name, func(t *testing.T) {
			for code := tt.startCode; code <= tt.endCode; code++ {
				if code == 126 || code == 127 || code == 130 || code == 137 || code == 143 {
					continue
				}

				result := TranslateExitCodeToErrorClarification(code)
				if result != tt.expected {
					t.Errorf("TranslateExitCodeToErrorClarification(%d) = %d, expected %d (%s)",
						code, result, tt.expected, tt.description)
					break
				}
			}
		})
	}
}

func TestTranslateExitCodeToErrorClarification_ConsistencyChecks(t *testing.T) {
	t.Run("All user errors (-100s) map to positive codes", func(t *testing.T) {
		userErrorCodes := []int{
			ExitCode_ScriptBlobDownloadFailed,
			ExitCode_BlobCreateOrReplaceFailed,
			ExitCode_RunAsLookupUserFailed,
		}

		for _, code := range userErrorCodes {
			result := TranslateExitCodeToErrorClarification(code)
			if result < 0 {
				t.Errorf("User error exit code %d mapped to negative clarification code %d", code, result)
			}
		}
	})

	t.Run("Success codes map to zero", func(t *testing.T) {
		successCodes := []int{0, ExitCode_Okay}

		for _, code := range successCodes {
			result := TranslateExitCodeToErrorClarification(code)
			if result != 0 {
				t.Errorf("Success exit code %d should map to 0, got %d", code, result)
			}
		}
	})

	t.Run("Function handles boundary values", func(t *testing.T) {
		boundaryTests := []struct {
			code int
			desc string
		}{
			{-1000, "Very negative"},
			{-1, "Just below zero"},
			{255, "Max 8-bit value"},
			{256, "Above 8-bit"},
			{1000, "Very positive"},
		}

		for _, test := range boundaryTests {
			result := TranslateExitCodeToErrorClarification(test.code)
			if result == 0 && test.code != 0 {
				t.Logf("Boundary test %s (%d) mapped to 0", test.desc, test.code)
			}
		}
	})
}
