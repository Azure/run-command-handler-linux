package extensionpolicysettingsrc

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/azure-extension-platform/pkg/extensionpolicysettings"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/stretchr/testify/require"
)

func makeSettings(scriptType handlersettings.ScriptType, commandID string, runAsUser string, outputBlobURI string) *handlersettings.HandlerSettings {
	return &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{
			Source: &handlersettings.ScriptSource{
				ScriptType: scriptType,
				CommandId:  commandID,
			},
			RunAsUser:     runAsUser,
			OutputBlobURI: outputBlobURI,
		},
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	_ = r.Close()

	return string(out)
}

func TestInitializeExtensionPolicySettings_InvalidPath_ReturnsError(t *testing.T) {
	var mgr *extensionpolicysettings.ExtensionPolicySettingsManager[RCv2ExtensionPolicySettings]
	out := &RCv2ExtensionPolicySettings{}

	err := InitializeExtensionPolicySettings(mgr, "/definitely/not/found/policy.json", out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to")
}

func TestInitializeExtensionPolicySettings_ValidFile_ReturnsNil(t *testing.T) {
	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "policy.json")

	// Minimal valid payload for current ValidateFormat behavior.
	err := os.WriteFile(policyPath, []byte("{}"), 0600)
	require.NoError(t, err)

	var mgr *extensionpolicysettings.ExtensionPolicySettingsManager[RCv2ExtensionPolicySettings]
	out := &RCv2ExtensionPolicySettings{}

	err = InitializeExtensionPolicySettings(mgr, policyPath, out)
	require.NoError(t, err)
}

func TestInitializeExtensionPolicySettings_CurrentBehavior_DoesNotPopulateOutputStruct(t *testing.T) {
	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "policy.json")

	payload := `{"limitScripts":"inline","runAsUser":"alice"}`
	err := os.WriteFile(policyPath, []byte(payload), 0600)
	require.NoError(t, err)

	var mgr *extensionpolicysettings.ExtensionPolicySettingsManager[RCv2ExtensionPolicySettings]
	out := &RCv2ExtensionPolicySettings{}

	err = InitializeExtensionPolicySettings(mgr, policyPath, out)
	require.NoError(t, err)

	// Documents current implementation behavior (pointer reassignment inside function).
	require.Equal(t, "", out.LimitScripts)
	require.Equal(t, "", out.RunAsUser)
}

func TestInitialValidateHandlerSettingsAgainstPolicy(t *testing.T) {
	t.Run("nil policy", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "", "")
		err := InitialValidateHandlerSettingsAgainstPolicy(settings, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no policy provided")
	})

	t.Run("script type blocked by policy", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "", "")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts: "gallery",
		}

		err := InitialValidateHandlerSettingsAgainstPolicy(settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), "script type inline is not allowed by policy")
	})

	t.Run("command id not in allowlist", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "restartVM", "", "")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts:       "allowedcommandid",
			CommandIdAllowlist: []string{"safeCommand"},
		}

		err := InitialValidateHandlerSettingsAgainstPolicy(settings, policy)
		require.Error(t, err)
	})

	t.Run("runAs mismatch", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "bob", "")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts: "inline",
			RunAsUser:    "alice",
		}

		err := InitialValidateHandlerSettingsAgainstPolicy(settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not match")
	})

	t.Run("all checks pass", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "safeCommand", " Alice ", "https://example/blob")
		policy := &RCv2ExtensionPolicySettings{
			LimitScripts:       "allowall",
			CommandIdAllowlist: []string{"safeCommand"},
			RunAsUser:          "alice",
			DisableOutputBlobs: true,
		}

		err := InitialValidateHandlerSettingsAgainstPolicy(settings, policy)
		require.NoError(t, err)
	})
}

func TestValidateScriptTypeAgainstPolicy(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		err := ValidateScriptTypeAgainstPolicy(handlersettings.InlineScript, "inline")
		require.NoError(t, err)
	})

	t.Run("blocked", func(t *testing.T) {
		err := ValidateScriptTypeAgainstPolicy(handlersettings.GalleryScript, "inline")
		require.Error(t, err)
		require.Contains(t, err.Error(), "script type gallery is not allowed by policy")
	})

	t.Run("invalid policy token currently treated as blocked", func(t *testing.T) {
		err := ValidateScriptTypeAgainstPolicy(handlersettings.InlineScript, "notARealScriptType")
		require.Error(t, err)
		require.Contains(t, err.Error(), "script type inline is not allowed by policy")
	})
}

func TestValidateCommandId(t *testing.T) {
	t.Run("empty allowlist allows all", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "anything", "", "")
		policy := &RCv2ExtensionPolicySettings{
			CommandIdAllowlist: nil,
		}
		err := ValidateCommandId(settings, policy)
		require.NoError(t, err)
	})

	t.Run("value present in allowlist", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "safeCommand", "", "")
		policy := &RCv2ExtensionPolicySettings{
			CommandIdAllowlist: []string{"safeCommand", "other"},
		}
		err := ValidateCommandId(settings, policy)
		require.NoError(t, err)
	})

	t.Run("value missing from allowlist", func(t *testing.T) {
		settings := makeSettings(handlersettings.CommandIdScript, "restartVM", "", "")
		policy := &RCv2ExtensionPolicySettings{
			CommandIdAllowlist: []string{"safeCommand", "other"},
		}
		err := ValidateCommandId(settings, policy)
		require.Error(t, err)
	})
}

func TestValidateRunAsUser(t *testing.T) {
	t.Run("match with whitespace and case differences", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", " Alice ", "")
		policy := &RCv2ExtensionPolicySettings{
			RunAsUser: "alice",
		}
		err := ValidateRunAsUser(settings, policy)
		require.NoError(t, err)
	})

	t.Run("mismatch", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "bob", "")
		policy := &RCv2ExtensionPolicySettings{
			RunAsUser: "alice",
		}
		err := ValidateRunAsUser(settings, policy)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not match")
	})
}

func TestValidateOutputBlob(t *testing.T) {
	t.Run("policy does not disable output blobs prints nothing", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "", "https://example/blob")
		policy := &RCv2ExtensionPolicySettings{
			DisableOutputBlobs: false,
		}

		out := captureStdout(t, func() {
			ValidateOutputBlob(settings, policy)
		})
		require.Equal(t, "", out)
	})

	t.Run("disabled with output blob uri prints ignore warning", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "", "https://example/blob")
		policy := &RCv2ExtensionPolicySettings{
			DisableOutputBlobs: true,
		}

		out := captureStdout(t, func() {
			ValidateOutputBlob(settings, policy)
		})
		require.Contains(t, out, "Output blobs are disabled by policy")
		require.Contains(t, out, "provided output blob URI will be ignored")
	})

	t.Run("disabled without output blob uri prints no blob warning", func(t *testing.T) {
		settings := makeSettings(handlersettings.InlineScript, "", "", "")
		policy := &RCv2ExtensionPolicySettings{
			DisableOutputBlobs: true,
		}

		out := captureStdout(t, func() {
			ValidateOutputBlob(settings, policy)
		})
		require.Contains(t, out, "Output blobs are disabled by policy")
		require.Contains(t, out, "No output blobs will be created")
	})
}
