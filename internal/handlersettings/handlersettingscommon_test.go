package handlersettings

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, content, mode))
}

func makeSettingsFile(t *testing.T, path string, hs settings.SettingsCommon) {
	t.Helper()

	f := HandlerSettingsFile{
		RuntimeSettings: []RunTimeSettingsFile{
			{HandlerSettings: hs},
		},
	}
	b, err := json.Marshal(f)
	require.NoError(t, err)
	writeFile(t, path, b, 0o644)
}

// Creates a temp PATH entry containing a fake "openssl" script.
// behavior:
//   - if cmsShouldFail=true, cms exits nonzero and writes stderr
//   - if smimeShouldFail=true, smime exits nonzero and writes stderr
//   - success prints outputJSON to stdout
func installFakeOpenSSL(t *testing.T, cmsShouldFail, smimeShouldFail bool, outputJSON string) (restore func()) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("These tests rely on creating a fake openssl script in PATH; adapt for Windows if needed.")
	}

	tmp := t.TempDir()
	fake := filepath.Join(tmp, "openssl")

	script := "#!/bin/sh\n" +
		"cmd=\"$1\"\n" +
		"shift\n" +
		"if [ \"$cmd\" = \"cms\" ]; then\n" +
		"  if [ \"" + boolToStr(cmsShouldFail) + "\" = \"true\" ]; then\n" +
		"    echo \"cms failed\" 1>&2\n" +
		"    exit 1\n" +
		"  fi\n" +
		"  echo '" + escapeSingleQuotes(outputJSON) + "'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$cmd\" = \"smime\" ]; then\n" +
		"  if [ \"" + boolToStr(smimeShouldFail) + "\" = \"true\" ]; then\n" +
		"    echo \"smime failed\" 1>&2\n" +
		"    exit 1\n" +
		"  fi\n" +
		"  echo '" + escapeSingleQuotes(outputJSON) + "'\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo \"unexpected openssl subcommand: $cmd\" 1>&2\n" +
		"exit 2\n"

	writeFile(t, fake, []byte(script), 0o755)

	origPath := os.Getenv("PATH")
	require.NoError(t, os.Setenv("PATH", tmp+string(os.PathListSeparator)+origPath))

	return func() {
		_ = os.Setenv("PATH", origPath)
	}
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func escapeSingleQuotes(s string) string {
	// for shell: wrap in single quotes, escape internal single quotes
	// ' -> '"'"'
	out := ""
	for _, r := range s {
		if r == '\'' {
			out += "'\"'\"'"
		} else {
			out += string(r)
		}
	}
	return out
}

func makeConfigAndCerts(t *testing.T, thumb string) (configFolder string) {
	t.Helper()

	root := t.TempDir()
	// config folder is .../some/ext/config ; certs are two levels up from configFolder
	// so: configFolder = root/ext/config, certs live in root/{thumb}.crt and root/{thumb}.prv
	configFolder = filepath.Join(root, "ext", "config")
	require.NoError(t, os.MkdirAll(configFolder, 0o755))

	crt := filepath.Join(configFolder, "..", "..", thumb+".crt")
	prv := filepath.Join(configFolder, "..", "..", thumb+".prv")
	writeFile(t, crt, []byte("dummycrt"), 0o644)
	writeFile(t, prv, []byte("dummyprv"), 0o600)

	return configFolder
}

/* -------------------- parseHandlerSettingsFile -------------------- */

func TestParseHandlerSettingsFile_ReadError(t *testing.T) {
	_, err := parseHandlerSettingsFile(filepath.Join(t.TempDir(), "missing.settings"))
	VerifyErrorClarification(t, constants.Internal_CouldNotParseSettings, err)
}

func TestParseHandlerSettingsFile_EmptyFile_OK(t *testing.T) {
	p := filepath.Join(t.TempDir(), "0.settings")
	writeFile(t, p, []byte{}, 0o644)

	got, err := parseHandlerSettingsFile(p)
	require.Nil(t, err)
	// empty settings file -> zero-value SettingsCommon
	require.Equal(t, settings.SettingsCommon{}, got)
}

func TestParseHandlerSettingsFile_InvalidJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "0.settings")
	writeFile(t, p, []byte("{not-json"), 0o644)

	_, err := parseHandlerSettingsFile(p)
	VerifyErrorClarification(t, constants.Internal_InvalidHandlerSettingsJson, err)
}

func TestParseHandlerSettingsFile_WrongRuntimeSettingsCount(t *testing.T) {
	p := filepath.Join(t.TempDir(), "0.settings")

	f := HandlerSettingsFile{
		RuntimeSettings: []RunTimeSettingsFile{
			{}, {},
		},
	}
	b, err := json.Marshal(f)
	require.NoError(t, err)
	writeFile(t, p, b, 0o644)

	_, ewc := parseHandlerSettingsFile(p)
	VerifyErrorClarification(t, constants.Internal_InvalidHandlerSettingsCount, ewc)
}

func TestParseHandlerSettingsFile_Success(t *testing.T) {
	p := filepath.Join(t.TempDir(), "0.settings")

	hs := settings.SettingsCommon{
		PublicSettings: map[string]interface{}{"k": "v"},
	}
	makeSettingsFile(t, p, hs)

	got, err := parseHandlerSettingsFile(p)
	require.Nil(t, err)
	require.Equal(t, hs.PublicSettings, got.PublicSettings)
}

/* -------------------- ReadSettings -------------------- */

func TestReadSettings_NoProtected_ReturnsPublicAndNilProtected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "0.settings")
	hs := settings.SettingsCommon{
		PublicSettings:          map[string]interface{}{"hello": "world"},
		ProtectedSettingsBase64: "",
		SettingsCertThumbprint:  "",
	}
	makeSettingsFile(t, p, hs)

	pub, prot, err := ReadSettings(p)
	require.Nil(t, err)
	require.Equal(t, hs.PublicSettings, pub)
	require.Nil(t, prot) // nothing set
}

func TestReadSettings_PropagatesParseError(t *testing.T) {
	_, _, ewc := ReadSettings(filepath.Join(t.TempDir(), "missing.settings"))
	VerifyErrorClarification(t, constants.Internal_CouldNotParseSettings, ewc)
}

/* -------------------- unmarshalSettings + UnmarshalHandlerSettings -------------------- */

func TestUnmarshalSettings_MarshalError_ReturnsClarification(t *testing.T) {
	// json.Marshal fails on channel / func values
	in := map[string]interface{}{"bad": make(chan int)}
	var out map[string]interface{}
	err := unmarshalSettings(in, &out)
	VerifyErrorClarification(t, constants.Internal_UnmarshalSettingsFailed, err)
}

func TestUnmarshalHandlerSettings_PublicUnmarshalFails(t *testing.T) {
	public := map[string]interface{}{"bad": make(chan int)} // will fail marshal
	protected := map[string]interface{}{"ok": "x"}
	var pubV map[string]interface{}
	var protV map[string]interface{}

	err := UnmarshalHandlerSettings(public, protected, &pubV, &protV)
	VerifyErrorClarification(t, constants.Internal_UnmarshalPublicSettingsFailed, err)
}

func TestUnmarshalHandlerSettings_ProtectedUnmarshalFails(t *testing.T) {
	public := map[string]interface{}{"ok": "x"}
	protected := map[string]interface{}{"bad": make(chan int)} // will fail marshal
	var pubV map[string]interface{}
	var protV map[string]interface{}

	err := UnmarshalHandlerSettings(public, protected, &pubV, &protV)
	VerifyErrorClarification(t, constants.Internal_UnmarshalProtectedSettingsFailed, err)
}

func TestUnmarshalHandlerSettings_Success_PopulatesStructs(t *testing.T) {
	type Pub struct {
		A string `json:"a"`
	}
	type Prot struct {
		B int `json:"b"`
	}

	public := map[string]interface{}{"a": "hello"}
	protected := map[string]interface{}{"b": 42}
	var pub Pub
	var prot Prot

	err := UnmarshalHandlerSettings(public, protected, &pub, &prot)
	require.Nil(t, err)

	require.Equal(t, "hello", pub.A)
	require.Equal(t, 42, prot.B)
}

/* -------------------- unmarshalProtectedSettings -------------------- */

func TestUnmarshalProtectedSettings_NoProtectedSettings_ReturnsNil(t *testing.T) {
	cfg := makeConfigAndCerts(t, "thumb")
	hs := settings.SettingsCommon{
		ProtectedSettingsBase64: "",
		SettingsCertThumbprint:  "thumb",
	}
	var out map[string]interface{}
	err := unmarshalProtectedSettings(cfg, hs, &out)
	require.Nil(t, err)
}

func TestUnmarshalProtectedSettings_ProtectedButNoThumbprint(t *testing.T) {
	cfg := makeConfigAndCerts(t, "thumb")
	hs := settings.SettingsCommon{
		ProtectedSettingsBase64: base64.StdEncoding.EncodeToString([]byte("anything")),
		SettingsCertThumbprint:  "",
	}
	var out map[string]interface{}
	err := unmarshalProtectedSettings(cfg, hs, &out)
	VerifyErrorClarification(t, constants.Internal_NoHandlerSettingsThumbprint, err)
}

func TestUnmarshalProtectedSettings_Base64DecodeFails(t *testing.T) {
	cfg := makeConfigAndCerts(t, "thumb")
	hs := settings.SettingsCommon{
		ProtectedSettingsBase64: "!!!notbase64!!!",
		SettingsCertThumbprint:  "thumb",
	}
	var out map[string]interface{}
	err := unmarshalProtectedSettings(cfg, hs, &out)
	VerifyErrorClarification(t, constants.Internal_HandlerSettingsFailedToDecode, err)
}

func TestUnmarshalProtectedSettings_CmsFails_SmimeSucceeds(t *testing.T) {
	restore := installFakeOpenSSL(t, true, false, `{"p":"ok"}`)
	defer restore()

	cfg := makeConfigAndCerts(t, "thumb")
	hs := settings.SettingsCommon{
		ProtectedSettingsBase64: base64.StdEncoding.EncodeToString([]byte("ciphertext")),
		SettingsCertThumbprint:  "thumb",
	}

	var out map[string]interface{}
	err := unmarshalProtectedSettings(cfg, hs, &out)
	require.Nil(t, err)
	require.Equal(t, "ok", out["p"])
}

func TestUnmarshalProtectedSettings_CmsFails_SmimeFails_ReturnsDecryptError(t *testing.T) {
	restore := installFakeOpenSSL(t, true, true, `{"p":"ok"}`)
	defer restore()

	cfg := makeConfigAndCerts(t, "thumb")
	hs := settings.SettingsCommon{
		ProtectedSettingsBase64: base64.StdEncoding.EncodeToString([]byte("ciphertext")),
		SettingsCertThumbprint:  "thumb",
	}

	var out map[string]interface{}
	err := unmarshalProtectedSettings(cfg, hs, &out)
	VerifyErrorClarification(t, constants.Internal_DecryptingProtectedSettingsFailed, err)
}

func TestUnmarshalProtectedSettings_DecryptedNotJSON_ReturnsUnmarshalProtectedFailed(t *testing.T) {
	restore := installFakeOpenSSL(t, false, false, `not-json`)
	defer restore()

	cfg := makeConfigAndCerts(t, "thumb")
	hs := settings.SettingsCommon{
		ProtectedSettingsBase64: base64.StdEncoding.EncodeToString([]byte("ciphertext")),
		SettingsCertThumbprint:  "thumb",
	}

	var out map[string]interface{}
	err := unmarshalProtectedSettings(cfg, hs, &out)
	VerifyErrorClarification(t, constants.Internal_UnmarshalProtectedSettingsFailed, err)
}
