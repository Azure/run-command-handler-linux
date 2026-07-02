package extensionpolicysettingsrc

import (
	"os"
	"path/filepath"
	"testing"

	"fmt"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func makeSettings(scriptType handlersettings.ScriptType, commandID string, runAsUser string, outputBlobURI string, errorBlobURI string) *handlersettings.HandlerSettings {
	return &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{
			Source: &handlersettings.ScriptSource{
				ScriptType: scriptType,
				CommandId:  commandID,
			},
			RunAsUser:     runAsUser,
			OutputBlobURI: outputBlobURI,
			ErrorBlobURI:  errorBlobURI,
		},
	}
}

func TestInitializeExtensionPolicySettings_EmptyPath_ReturnsError(t *testing.T) {
	_, _, err, exitCode := InitializeExtensionPolicySettings(nopCtx(), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "policy path to initialize extension policy settings is empty")
	require.Equal(t, constants.ExitCode_InitializeCalledWithNoPolicyPath, exitCode)
}
func TestInitializeExtensionPolicySettings_InvalidPath_ReturnsError(t *testing.T) {
	_, _, err, exitCode := InitializeExtensionPolicySettings(nopCtx(), "/definitely/not/found/policy.json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to load extension policy settings from file. Ensure the policy format is valid and the file is accessible")
	require.Equal(t, constants.ExitCode_LoadExtensionPolicySettingsFailed, exitCode)
}

func TestInitializeExtensionPolicySettings_InvalidPolicyFails(t *testing.T) {
	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "policy.json")

	payload := `{"blah blah"}`
	err := os.WriteFile(policyPath, []byte(payload), 0600)
	require.NoError(t, err)

	_, _, err, exitCode := InitializeExtensionPolicySettings(nopCtx(), policyPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to load extension policy settings from file. Ensure the policy format is valid and the file is accessible")
	require.Equal(t, constants.ExitCode_LoadExtensionPolicySettingsFailed, exitCode)
}

func TestInitializeExtensionPolicySettings_ValidFile_ReturnsNil(t *testing.T) {
	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "policy.json")

	// Minimal valid payload for current ValidateFormat behavior.
	err := os.WriteFile(policyPath, []byte("{}"), 0600)
	require.NoError(t, err)

	_, _, err, exitCode := InitializeExtensionPolicySettings(nopCtx(), policyPath)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
}

func TestInitializeExtensionPolicySettings_PopulatesOutputStruct(t *testing.T) {
	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "policy.json")

	payload := `{"limitScripts":"inline","runAsUser":"alice"}`
	err := os.WriteFile(policyPath, []byte(payload), 0600)
	require.NoError(t, err)

	out := &RCv2ExtensionPolicySettings{}

	_, out, err, exitCode := InitializeExtensionPolicySettings(nopCtx(), policyPath)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	require.Equal(t, "inline", out.LimitScripts)
	require.Equal(t, "alice", out.RunAsUser)
}

// Test that validation passes and fails as expected.
func TestValidateHandlerSettingsAgainstPolicy(t *testing.T) {
	t.Run("nil policy", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "", "", "")
		err, exitCode := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no policy provided to validate handler settings")
		require.Equal(t, constants.ExitCode_ValidateCalledWithNilPolicy, exitCode)
	})

	// This test mimicks running an inline script, but policy only allows gallery scripts.
	// Validation fails.
	t.Run("script type blocked by policy", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "", "", "")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts: "gallery",
		}

		err, exitCode := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), "script type inline is not allowed by policy")
		require.Equal(t, constants.ExitCode_ScriptTypeNotAllowedByExtensionPolicy, exitCode)
	})

	// This test mimicks running a commandId that is not in the allowlist.
	// Additionally, only commandId types are allowed.
	t.Run("command ID not in allowlist", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "restartVM", "", "", "")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts:       "allowedcommandid",
			CommandIdAllowlist: []string{"safeCommand"},
		}

		err, exitCode := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, policy)
		require.Error(t, err)
		require.Equal(t, constants.ExitCode_CommandIdNotAllowedByExtensionPolicy, exitCode)
	})

	t.Run("runAs mismatch", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "bob", "", "")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts: "inline",
			RunAsUser:    "alice",
		}

		err, exitCode := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not match")
		require.Equal(t, constants.ExitCode_RunAsUserNotAllowedByExtensionPolicy, exitCode)
	})

	t.Run("enforce limitScripts must be set. If not set, all commands fail", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "safeCommand", " Alice ", "https://example/blob", "https://example/errorBlob")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts:       "",
			CommandIdAllowlist: []string{"safeCommand"},
			RunAsUser:          "Alice",
			DisableOutputBlobs: true,
		}

		err, exitCode := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, policy)
		require.Contains(t, err.Error(), "script type commandId is not allowed by policy")
		require.Equal(t, constants.ExitCode_ScriptTypeNotAllowedByExtensionPolicy, exitCode)
	})

	t.Run("output blobs disabled, output blob provided", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", " Alice ", "https://example/blob", "")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts:       "allowall",
			CommandIdAllowlist: []string{"safeCommand"},
			RunAsUser:          "alice",
			DisableOutputBlobs: true,
		}

		err, exitCode := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("output blobs are disabled in policy, but settings specify outputBlobURI '%s' or errorBlobURI '%s'", settings.OutputBlobURI, settings.ErrorBlobURI))
		require.Equal(t, constants.ExitCode_OutputBlobSpecifiedButNotAllowedByExtensionPolicy, exitCode)
	})

	t.Run("all checks pass commandId", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "safeCommand", " Alice ", "https://example/blob", "https://example/errorBlob")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts:       "allowall",
			CommandIdAllowlist: []string{"safeCommand"},
			RunAsUser:          "alice",
			DisableOutputBlobs: false,
		}

		err, exitCode := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, policy)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode)
	})

	t.Run("all checks pass downloadedScript", func(t *testing.T) {
		settings := makeSettings(handlersettings.DownloadedScript, "safeCommand", " Alice ", "https://example/blob", "https://example/errorBlob")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts:       "alloweddownloaded",
			CommandIdAllowlist: []string{"safeCommand"},
			RunAsUser:          "alice",
			DisableOutputBlobs: false,
		}

		err, exitCode := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, policy)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode)
	})

	t.Run("all checks pass disable output blobs", func(t *testing.T) {
		settings := makeSettings(handlersettings.DownloadedScript, "safeCommand", " Alice ", "", "")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts:       "alloweddownloaded",
			CommandIdAllowlist: []string{"safeCommand"},
			RunAsUser:          "alice",
			DisableOutputBlobs: true,
		}

		err, exitCode := ValidateHandlerSettingsAgainstPolicy(nopCtx(), settings, policy)
		require.NoError(t, err)
		require.Equal(t, 0, exitCode)
	})

}

func TestValidateScriptTypeAgainstPolicy(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		err := ValidateScriptTypeAgainstPolicy(nopCtx(), handlersettings.InlineScript, "inline")
		require.NoError(t, err)
	})

	t.Run("blocked", func(t *testing.T) {
		err := ValidateScriptTypeAgainstPolicy(nopCtx(), handlersettings.GalleryScript, "inline")
		require.Error(t, err)
		require.Contains(t, err.Error(), "script type gallery is not allowed by policy")
	})

	// This tests edge case where policy has an invalid script type token.
	t.Run("invalid policy token is treated as blocked", func(t *testing.T) {
		err := ValidateScriptTypeAgainstPolicy(nopCtx(), handlersettings.InlineScript, "notARealScriptType")
		require.Error(t, err)
		require.Contains(t, err.Error(), "script type inline is not allowed by policy")
	})
}

func TestValidateCommandId(t *testing.T) {
	t.Run("empty allowlist allows all", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "anything", "", "", "")
		policy := &RCv2ExtensionPolicySettings{
			CommandIdAllowlist: nil,
		}
		err := ValidateCommandId(nopCtx(), settings, policy)
		require.NoError(t, err)
	})

	t.Run("value present in allowlist", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "safeCommand", "", "", "")
		policy := &RCv2ExtensionPolicySettings{
			CommandIdAllowlist: []string{"safeCommand", "other"},
		}
		err := ValidateCommandId(nopCtx(), settings, policy)
		require.NoError(t, err)
	})

	t.Run("value missing from allowlist", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "restartVM", "", "", "")
		policy := &RCv2ExtensionPolicySettings{
			CommandIdAllowlist: []string{"safeCommand", "other"},
		}
		err := ValidateCommandId(nopCtx(), settings, policy)
		require.Contains(t, err.Error(), "command ID restartVM is not allowed by policy")
		require.Contains(t, err.Error(), "item is not in the allowlist")
	})
}

func TestValidateRunAsUser(t *testing.T) {
	t.Run("match with whitespace and case differences", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", " Alice ", "", "")
		policy := &RCv2ExtensionPolicySettings{
			RunAsUser: "alice",
		}
		err := ValidateRunAsUser(nopCtx(), settings, policy)
		require.NoError(t, err)
	})

	t.Run("mismatch", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "bob", "", "")
		policy := &RCv2ExtensionPolicySettings{
			RunAsUser: "alice",
		}
		err := ValidateRunAsUser(nopCtx(), settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), "runAsUser 'bob' in settings does not match runAsUser 'alice' in policy")
	})
}

func TestValidateDisableOutputBlobs(t *testing.T) {
	t.Run("output blobs disabled in policy, outputblob provided", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", " Alice ", "https://example/blob", "")
		policy := &RCv2ExtensionPolicySettings{
			DisableOutputBlobs: true,
		}
		err := ValidateDisableOutputBlobs(nopCtx(), settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("output blobs are disabled in policy, but settings specify outputBlobURI '%s' or errorBlobURI '%s'", settings.OutputBlobURI, settings.ErrorBlobURI))
	})

	t.Run("output blobs disabled in policy, error blob provided", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", " Alice ", "", "https://example/errorBlob")
		policy := &RCv2ExtensionPolicySettings{
			DisableOutputBlobs: true,
		}
		err := ValidateDisableOutputBlobs(nopCtx(), settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("output blobs are disabled in policy, but settings specify outputBlobURI '%s' or errorBlobURI '%s'", settings.OutputBlobURI, settings.ErrorBlobURI))
	})

	t.Run("output blobs disabled in policy, both output and error blobs provided", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", " Alice ", "https://example/blob", "https://example/errorBlob")
		policy := &RCv2ExtensionPolicySettings{
			DisableOutputBlobs: true,
		}
		err := ValidateDisableOutputBlobs(nopCtx(), settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("output blobs are disabled in policy, but settings specify outputBlobURI '%s' or errorBlobURI '%s'", settings.OutputBlobURI, settings.ErrorBlobURI))
	})

	t.Run("output blobs disabled in policy, pass", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", " Alice ", "", "")
		policy := &RCv2ExtensionPolicySettings{
			DisableOutputBlobs: true,
		}
		err := ValidateDisableOutputBlobs(nopCtx(), settings, policy)
		require.NoError(t, err)
	})
}

func nopCtx() *log.Context {
	return log.NewContext(log.NewNopLogger())
}
