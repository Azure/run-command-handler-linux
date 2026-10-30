package extensionpolicysettingsrc

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

func requirePolicyCode(t *testing.T, err error, want int) {
	t.Helper()
	require.Error(t, err)
	code, ok := errorutil.Code(err)
	require.True(t, ok, "missing clarification: %v", err)
	require.Equal(t, want, code)
}

func mockPolicyBoundaries(t *testing.T) {
	t.Helper()
	newManager, load, get, allow := newPolicyManager, loadPolicySettings, getPolicySettings, validatePolicyAllowlist
	t.Cleanup(func() {
		newPolicyManager, loadPolicySettings, getPolicySettings, validatePolicyAllowlist = newManager, load, get, allow
	})
	newPolicyManager = func(string) (*policyManager, error) { return new(policyManager), nil }
	loadPolicySettings = func(*policyManager) error { return nil }
	getPolicySettings = func(*policyManager) (*RCv2ExtensionPolicySettings, error) {
		return &RCv2ExtensionPolicySettings{}, nil
	}
}

func TestPolicyFormatClarifications(t *testing.T) {
	t.Run("invalid script type", func(t *testing.T) {
		_, err := StringToAllowedScriptTypeFlag("unknown")
		requirePolicyCode(t, err, errorutil.ExtensionPolicySettings_invalidPolicyFileFormat)
		requirePolicyCode(t, (RCv2ExtensionPolicySettings{LimitScripts: "unknown"}).ValidateFormat(), errorutil.ExtensionPolicySettings_invalidPolicyFileFormat)
		requirePolicyCode(t, ValidateScriptTypeAgainstPolicy(nopCtx(), handlersettings.InlineScript, "unknown"), errorutil.ExtensionPolicySettings_invalidPolicyFileFormat)
	})
	t.Run("download allowlist 2501", func(t *testing.T) {
		err := (RCv2ExtensionPolicySettings{LimitScripts: "inline", DownloadedScriptsAllowlist: []string{"hash"}}).ValidateFormat()
		requirePolicyCode(t, err, errorutil.ExtensionPolicySettings_inconsistentDownloadedScriptAllowlist)
	})
	t.Run("command allowlist 2502", func(t *testing.T) {
		err := (RCv2ExtensionPolicySettings{LimitScripts: "inline", CommandIdAllowlist: []string{"command"}}).ValidateFormat()
		requirePolicyCode(t, err, errorutil.ExtensionPolicySettings_inconsistentCommandIdAllowlist)
	})
}

func TestPolicyScriptClarifications(t *testing.T) {
	for _, tc := range []struct {
		script handlersettings.ScriptType
		code   int
	}{
		{handlersettings.InlineScript, errorutil.ExtensionPolicySettings_scriptNotAllowed},
		{handlersettings.DownloadedScript, errorutil.ExtensionPolicySettings_downloadedScriptTypeNotAllowed},
		{handlersettings.GalleryScript, errorutil.ExtensionPolicySettings_galleryScriptNotAllowed},
		{handlersettings.DiagnosticScript, errorutil.ExtensionPolicySettings_diagnosticScriptNotAllowed},
		{handlersettings.CommandIdScript, errorutil.ExtensionPolicySettings_commandIdScriptNotAllowed},
		{handlersettings.ScriptType("unknown"), errorutil.ExtensionPolicySettings_unknownScriptType},
	} {
		t.Run(string(tc.script), func(t *testing.T) {
			requirePolicyCode(t, CompareScriptTypeToAllowedScriptType(tc.script, AllowedScriptNone), tc.code)
			err := ValidateScriptTypeAgainstPolicy(nopCtx(), tc.script, "")
			requirePolicyCode(t, err, tc.code)
			err, exit := ValidateHandlerSettingsAgainstPolicy(nopCtx(), makeSettings(tc.script, "", "", "", ""), &RCv2ExtensionPolicySettings{})
			requirePolicyCode(t, err, tc.code)
			require.Equal(t, constants.ExitCode_ScriptTypeNotAllowedByExtensionPolicy, exit)
		})
	}
}

func TestPolicyInitializationClarifications(t *testing.T) {
	t.Run("empty path -2510", func(t *testing.T) {
		mockPolicyBoundaries(t)
		_, _, err, exit := InitializeExtensionPolicySettings(nopCtx(), "")
		requirePolicyCode(t, err, errorutil.Internal_emptyPolicyPath)
		require.Equal(t, constants.ExitCode_InitializeCalledWithNoPolicyPath, exit)
	})
	for _, tc := range []struct {
		name string
		code int
		exit int
	}{
		{"create", errorutil.Internal_failedToCreatePolicyManager, constants.ExitCode_FailedToCreateExtensionPolicySettingsManager},
		{"load", errorutil.ExtensionPolicySettings_policyLoadFailed, constants.ExitCode_LoadExtensionPolicySettingsFailed},
		{"get", errorutil.Internal_failedToGetPolicySettings, constants.ExitCode_FailedToGetExtensionPolicySettings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockPolicyBoundaries(t)
			cause := errors.New("boundary failed")
			switch tc.name {
			case "create":
				newPolicyManager = func(string) (*policyManager, error) { return nil, cause }
			case "load":
				loadPolicySettings = func(*policyManager) error { return cause }
			case "get":
				getPolicySettings = func(*policyManager) (*RCv2ExtensionPolicySettings, error) { return nil, cause }
			}
			manager, policy, err, exit := InitializeExtensionPolicySettings(nopCtx(), "policy.json")
			require.Nil(t, manager)
			require.Nil(t, policy)
			requirePolicyCode(t, err, tc.code)
			require.ErrorIs(t, err, cause)
			require.Equal(t, tc.exit, exit)
		})
	}
	t.Run("success", func(t *testing.T) {
		mockPolicyBoundaries(t)
		manager, policy, err, exit := InitializeExtensionPolicySettings(nopCtx(), "policy.json")
		require.NotNil(t, manager)
		require.NotNil(t, policy)
		require.NoError(t, err)
		require.Zero(t, exit)
	})
}

func TestPolicySettingsClarifications(t *testing.T) {
	t.Run("nil policy -2514", func(t *testing.T) {
		err, exit := ValidateHandlerSettingsAgainstPolicy(nopCtx(), makeSettings(handlersettings.InlineScript, "", "", "", ""), nil)
		requirePolicyCode(t, err, errorutil.Internal_nilPolicySettings)
		require.Equal(t, constants.ExitCode_ValidateCalledWithNilPolicy, exit)
	})
	t.Run("command 2515", func(t *testing.T) {
		mockPolicyBoundaries(t)
		cause := errors.New("not in allowlist")
		validatePolicyAllowlist = func(string, []string) error { return cause }
		policy := &RCv2ExtensionPolicySettings{LimitScripts: "allowall", CommandIdAllowlist: []string{"safe"}}
		err, exit := ValidateHandlerSettingsAgainstPolicy(nopCtx(), makeSettings(handlersettings.CommandIdScript, "unsafe", "", "", ""), policy)
		requirePolicyCode(t, err, errorutil.ExtensionPolicySettings_commandToExecuteNotAllowed)
		require.ErrorIs(t, err, cause)
		require.Equal(t, constants.ExitCode_CommandIdNotAllowedByExtensionPolicy, exit)
	})
	t.Run("user 2516", func(t *testing.T) {
		policy := &RCv2ExtensionPolicySettings{LimitScripts: "allowall", RunAsUser: "alice"}
		err, exit := ValidateHandlerSettingsAgainstPolicy(nopCtx(), makeSettings(handlersettings.InlineScript, "", "bob", "", ""), policy)
		requirePolicyCode(t, err, errorutil.ExtensionPolicySettings_runAsUserNotAllowed)
		require.Equal(t, constants.ExitCode_RunAsUserNotAllowedByExtensionPolicy, exit)
	})
	for _, output := range []bool{true, false} {
		t.Run(fmt.Sprintf("output blobs 2517 output=%t", output), func(t *testing.T) {
			policy := &RCv2ExtensionPolicySettings{LimitScripts: "allowall", DisableOutputBlobs: true}
			settings := makeSettings(handlersettings.InlineScript, "", "", "", "")
			if output {
				settings.OutputBlobURI = "https://example.test/blob?sig=secret"
			} else {
				settings.ErrorBlobURI = "https://example.test/blob?sig=secret"
			}
			err, exit := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, policy)
			requirePolicyCode(t, err, errorutil.ExtensionPolicySettings_outputBlobsNotAllowed)
			require.NotContains(t, err.Error(), "secret")
			require.Equal(t, constants.ExitCode_OutputBlobSpecifiedButNotAllowedByExtensionPolicy, exit)
		})
	}
}

func TestPolicyPreservesLowerClarifications(t *testing.T) {
	for _, lower := range []error{
		vmextension.NewErrorWithClarificationPtr(errorutil.CustomerInput_invalidSettingsJson, errors.New("pointer")),
		vmextension.NewErrorWithClarification(errorutil.CustomerInput_invalidSettingsJson, errors.New("value")),
	} {
		for _, boundary := range []string{"create", "load", "get", "allowlist"} {
			t.Run(fmt.Sprintf("%s/%v", boundary, lower), func(t *testing.T) {
				mockPolicyBoundaries(t)
				wrapped := fmt.Errorf("boundary: %w", lower)
				switch boundary {
				case "create":
					newPolicyManager = func(string) (*policyManager, error) { return nil, wrapped }
				case "load":
					loadPolicySettings = func(*policyManager) error { return wrapped }
				case "get":
					getPolicySettings = func(*policyManager) (*RCv2ExtensionPolicySettings, error) { return nil, wrapped }
				case "allowlist":
					validatePolicyAllowlist = func(string, []string) error { return wrapped }
				}
				var err error
				if boundary == "allowlist" {
					err = ValidateCommandId(nopCtx(), makeSettings(handlersettings.CommandIdScript, "command", "", "", ""), &RCv2ExtensionPolicySettings{CommandIdAllowlist: []string{"safe"}})
				} else {
					_, _, err, _ = InitializeExtensionPolicySettings(nopCtx(), "policy.json")
				}
				requirePolicyCode(t, err, errorutil.CustomerInput_invalidSettingsJson)
				require.ErrorIs(t, err, lower)
			})
		}
	}
}
