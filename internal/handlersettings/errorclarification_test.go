package handlersettings

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func requireSettingsCode(t *testing.T, err error, want int) {
	t.Helper()
	require.Error(t, err)
	code, ok := errorutil.Code(err)
	require.True(t, ok, "missing clarification: %v", err)
	require.Equal(t, want, code)
}

func mockSettingsMachine(t *testing.T) {
	t.Helper()
	read, abs, decrypt := readSettingsFile, settingsAbsPath, decryptSettings
	t.Cleanup(func() {
		readSettingsFile, settingsAbsPath, decryptSettings = read, abs, decrypt
	})
	settingsAbsPath = func(string) (string, error) { return filepath.Join("extension", "bin", "handler"), nil }
	readSettingsFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	decryptSettings = func(string, string, string, []byte) ([]byte, string, error) {
		t.Fatal("unexpected external process")
		return nil, "", nil
	}
}

func TestHandlerEnvironmentClarifications(t *testing.T) {
	t.Run("absolute path 2400", func(t *testing.T) {
		mockSettingsMachine(t)
		cause := errors.New("working directory unavailable")
		settingsAbsPath = func(string) (string, error) { return "", cause }
		_, err := GetHandlerEnv()
		requireSettingsCode(t, err, errorutil.Os_FailedToGetWorkingDirectory)
		require.ErrorIs(t, err, cause)
	})
	t.Run("read 2401", func(t *testing.T) {
		mockSettingsMachine(t)
		readSettingsFile = func(string) ([]byte, error) { return nil, os.ErrPermission }
		_, err := GetHandlerEnv()
		requireSettingsCode(t, err, errorutil.Os_FailedToExamineHandlerEnvironment)
		require.ErrorIs(t, err, os.ErrPermission)
	})
	t.Run("missing 2402", func(t *testing.T) {
		mockSettingsMachine(t)
		_, err := GetHandlerEnv()
		requireSettingsCode(t, err, errorutil.Os_CouldNotFindHandlerEnvironment)
	})
	t.Run("json 2403", func(t *testing.T) {
		_, err := ParseHandlerEnv([]byte("{"))
		requireSettingsCode(t, err, errorutil.Os_InvalidHandlerEnvironmentJson)
		var syntax *json.SyntaxError
		require.ErrorAs(t, err, &syntax)
	})
	t.Run("count 2404", func(t *testing.T) {
		_, err := ParseHandlerEnv([]byte("[]"))
		requireSettingsCode(t, err, errorutil.Os_UnexpectedHandlerEnvironmentCount)
	})
	t.Run("parent path and success", func(t *testing.T) {
		mockSettingsMachine(t)
		calls := 0
		readSettingsFile = func(string) ([]byte, error) {
			calls++
			if calls == 1 {
				return nil, os.ErrNotExist
			}
			return []byte("[{}]"), nil
		}
		_, err := GetHandlerEnv()
		require.NoError(t, err)
		require.Equal(t, 2, calls)
	})
}

func TestSettingsJSONClarifications(t *testing.T) {
	t.Run("marshal -2410", func(t *testing.T) {
		err := unmarshalSettings(make(chan int), new(interface{}))
		requireSettingsCode(t, err, errorutil.Internal_failedToMarshalHandlerSettings)
		var unsupported *json.UnsupportedTypeError
		require.ErrorAs(t, err, &unsupported)
	})
	t.Run("unmarshal 2411", func(t *testing.T) {
		err := UnmarshalHandlerSettings(map[string]interface{}{"timeoutInSeconds": "bad"}, nil, new(PublicSettings), new(ProtectedSettings))
		requireSettingsCode(t, err, errorutil.CustomerInput_invalidHandlerSettingsJson)
		var mismatch *json.UnmarshalTypeError
		require.ErrorAs(t, err, &mismatch)
		err = UnmarshalHandlerSettings(nil, map[string]interface{}{"runAsPassword": 12}, new(PublicSettings), new(ProtectedSettings))
		requireSettingsCode(t, err, errorutil.CustomerInput_invalidHandlerSettingsJson)
	})
	for _, tc := range []struct {
		name    string
		data    string
		readErr error
		code    int
	}{
		{"read 2412", "", os.ErrPermission, errorutil.Os_FailedToReadHandlerSettings},
		{"parse config", "{", nil, errorutil.Internal_badConfig},
		{"count 2414", "{}", nil, errorutil.CustomerInput_unexpectedRuntimeSettingsCount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockSettingsMachine(t)
			readSettingsFile = func(string) ([]byte, error) { return []byte(tc.data), tc.readErr }
			_, _, err := ReadSettings("config.settings")
			requireSettingsCode(t, err, tc.code)
			if tc.readErr != nil {
				require.ErrorIs(t, err, tc.readErr)
			}
		})
	}
	t.Run("source 2420 through validation", func(t *testing.T) {
		mockSettingsMachine(t)
		readSettingsFile = func(string) ([]byte, error) {
			return []byte(`{"runtimeSettings":[{"handlerSettings":{}}]}`), nil
		}
		_, err := ParseAndValidateSettings(log.NewContext(log.NewNopLogger()), "config.settings")
		requireSettingsCode(t, err, errorutil.CustomerInput_commandToExecuteAndScriptNotSpecified)
		require.ErrorIs(t, err, errSourceNotSpecified)
		requireSettingsCode(t, HandlerSettings{}.validate(), errorutil.CustomerInput_commandToExecuteAndScriptNotSpecified)
		require.NoError(t, (HandlerSettings{PublicSettings: PublicSettings{InstallAsService: true}}).validate())
	})
}

func TestProtectedSettingsClarifications(t *testing.T) {
	encrypted := settings.SettingsCommon{
		ProtectedSettingsBase64: base64.StdEncoding.EncodeToString([]byte("encrypted")),
		SettingsCertThumbprint:  "thumb",
	}
	t.Run("thumbprint 2415", func(t *testing.T) {
		hs := encrypted
		hs.SettingsCertThumbprint = ""
		requireSettingsCode(t, unmarshalProtectedSettings("config", hs, new(interface{})), errorutil.CustomerInput_missingCertificateThumbprint)
	})
	t.Run("base64 2416", func(t *testing.T) {
		hs := encrypted
		hs.ProtectedSettingsBase64 = "!"
		err := unmarshalProtectedSettings("config", hs, new(interface{}))
		requireSettingsCode(t, err, errorutil.CustomerInput_invalidProtectedSettingsBase64)
		var cause base64.CorruptInputError
		require.ErrorAs(t, err, &cause)
	})
	t.Run("cms 2417 and smime 2418 preserve both causes", func(t *testing.T) {
		mockSettingsMachine(t)
		cmsCause, smimeCause := errors.New("cms unavailable"), errors.New("smime unavailable")
		methods := []string{}
		decryptSettings = func(method, crt, prv string, input []byte) ([]byte, string, error) {
			methods = append(methods, method)
			require.Equal(t, filepath.Join("config", "..", "..", "thumb.crt"), crt)
			require.Equal(t, filepath.Join("config", "..", "..", "thumb.prv"), prv)
			require.Equal(t, []byte("encrypted"), input)
			if method == "cms" {
				return nil, "cms stderr", cmsCause
			}
			return nil, "smime stderr", smimeCause
		}
		err := unmarshalProtectedSettings("config", encrypted, new(interface{}))
		requireSettingsCode(t, err, errorutil.Os_FailedToDecryptSettingsWithSmime)
		require.ErrorIs(t, err, cmsCause)
		require.ErrorIs(t, err, smimeCause)
		joined, ok := err.(interface{ Unwrap() []error })
		require.True(t, ok)
		requireSettingsCode(t, joined.Unwrap()[1], errorutil.Os_FailedToDecryptSettingsWithCms)
		require.Equal(t, []string{"cms", "smime"}, methods)
		require.Contains(t, err.Error(), "cms stderr")
		require.Contains(t, err.Error(), "smime stderr")
	})
	t.Run("decrypted json 2419", func(t *testing.T) {
		mockSettingsMachine(t)
		decryptSettings = func(string, string, string, []byte) ([]byte, string, error) {
			return []byte("{"), "", nil
		}
		err := unmarshalProtectedSettings("config", encrypted, new(interface{}))
		requireSettingsCode(t, err, errorutil.CustomerInput_invalidDecryptedSettingsJson)
		var syntax *json.SyntaxError
		require.ErrorAs(t, err, &syntax)
	})
	t.Run("fallback succeeds", func(t *testing.T) {
		mockSettingsMachine(t)
		decryptSettings = func(method, _, _ string, _ []byte) ([]byte, string, error) {
			if method == "cms" {
				return nil, "", errors.New("unsupported")
			}
			return []byte(`{"runAsPassword":"secret"}`), "", nil
		}
		var output ProtectedSettings
		require.NoError(t, unmarshalProtectedSettings("config", encrypted, &output))
		require.Equal(t, "secret", output.RunAsPassword)
	})
	t.Run("empty protected settings", func(t *testing.T) {
		mockSettingsMachine(t)
		require.NoError(t, unmarshalProtectedSettings("", settings.SettingsCommon{}, new(interface{})))
	})
}

func TestArtifactClarifications(t *testing.T) {
	for _, tc := range []struct {
		name      string
		protected []ProtectedArtifactSource
		code      int
	}{
		{"count -2421", nil, errorutil.Internal_invalidArtifactSpecification},
		{"id -2422", []ProtectedArtifactSource{{ArtifactId: 2}}, errorutil.Internal_inconsistentArtifactSpecification},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs := HandlerSettings{
				PublicSettings:    PublicSettings{Artifacts: []PublicArtifactSource{{ArtifactId: 1}}},
				ProtectedSettings: ProtectedSettings{Artifacts: tc.protected},
			}
			_, err := hs.ReadArtifacts()
			requireSettingsCode(t, err, tc.code)
		})
	}
}

func TestSettingsPreserveLowerClarification(t *testing.T) {
	for _, lower := range []error{
		vmextension.NewErrorWithClarificationPtr(errorutil.CustomerInput_invalidSettingsJson, errors.New("lower pointer")),
		vmextension.NewErrorWithClarification(errorutil.CustomerInput_invalidSettingsJson, errors.New("lower value")),
	} {
		t.Run(fmt.Sprint(lower), func(t *testing.T) {
			mockSettingsMachine(t)
			readSettingsFile = func(string) ([]byte, error) { return nil, fmt.Errorf("file boundary: %w", lower) }
			_, _, err := readSettings("config.settings")
			requireSettingsCode(t, err, errorutil.CustomerInput_invalidSettingsJson)
			require.ErrorIs(t, err, lower)
		})
	}
}
