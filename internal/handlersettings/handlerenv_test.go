package handlersettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/stretchr/testify/require"
)

func writeHandlerEnvJSON(t *testing.T, path string, he types.HandlerEnvironment) {
	t.Helper()
	b, err := json.Marshal([]types.HandlerEnvironment{he})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o644))
}

func TestParseHandlerEnv_UnmarshalFailed(t *testing.T) {
	_, err := ParseHandlerEnv([]byte("{not-json"))
	VerifyErrorClarification(t, constants.HandlerEnv_UnmarshalFailed, err)
}

func TestParseHandlerEnv_InvalidConfigCount_Zero(t *testing.T) {
	b, err := json.Marshal([]types.HandlerEnvironment{})
	require.NoError(t, err)

	_, ewc := ParseHandlerEnv(b)
	VerifyErrorClarification(t, constants.HandlerEnv_InvalidConfigCount, ewc)
}

func TestParseHandlerEnv_InvalidConfigCount_Two(t *testing.T) {
	b, err := json.Marshal([]types.HandlerEnvironment{
		{Version: 1.0},
		{Version: 1.0},
	})
	require.NoError(t, err)

	_, ewc := ParseHandlerEnv(b)
	VerifyErrorClarification(t, constants.HandlerEnv_InvalidConfigCount, ewc)
}

func TestParseHandlerEnv_Success(t *testing.T) {
	want := types.HandlerEnvironment{
		Version: 1.0,
		HandlerEnvironment: types.HandlerEnvironmentDetails{
			LogFolder:           "/var/log/azure/ext/log",
			ConfigFolder:        "/var/lib/waagent/ext/config",
			StatusFolder:        "/var/lib/waagent/ext/status",
			HeartbeatFile:       "/var/lib/waagent/ext/heartbeat",
			DeploymentID:        "dep",
			RoleName:            "role",
			Instance:            "inst",
			HostResolverAddress: "168.63.129.16",
			EventsFolder:        "/var/log/azure/ext/events",
		},
	}

	b, err := json.Marshal([]types.HandlerEnvironment{want})
	require.NoError(t, err)

	got, ewc := ParseHandlerEnv(b)
	require.Nil(t, ewc)
	require.Equal(t, want, got)
}

func TestGetHandlerEnv_FindsHandlerEnvironmentNextToExecutable(t *testing.T) {
	tmp := t.TempDir()

	// Simulate "executable" location: tmp/ext/
	exeDir := filepath.Join(tmp, "ext")
	require.NoError(t, os.MkdirAll(exeDir, 0o755))

	exePath := filepath.Join(exeDir, "run-command-handler-linux")
	_ = os.WriteFile(exePath, []byte("dummy"), 0o755)

	// HandlerEnvironment.json placed next to executable
	want := types.HandlerEnvironment{Version: 1.0}
	writeHandlerEnvJSON(t, filepath.Join(exeDir, HandlerEnvFileName), want)

	origArgs0 := os.Args[0]
	t.Cleanup(func() { os.Args[0] = origArgs0 })
	os.Args[0] = exePath

	got, ewc := GetHandlerEnv()
	require.Nil(t, ewc)
	require.Equal(t, want, got)
}

func TestGetHandlerEnv_FindsHandlerEnvironmentOneLevelAboveExecutable(t *testing.T) {
	tmp := t.TempDir()

	// Simulate "bin" layout: tmp/ext/bin/<exe>, and HandlerEnvironment.json in tmp/ext/
	extDir := filepath.Join(tmp, "ext")
	binDir := filepath.Join(extDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	exePath := filepath.Join(binDir, "run-command-handler-linux")
	_ = os.WriteFile(exePath, []byte("dummy"), 0o755)

	want := types.HandlerEnvironment{Version: 1.0}
	writeHandlerEnvJSON(t, filepath.Join(extDir, HandlerEnvFileName), want)

	origArgs0 := os.Args[0]
	t.Cleanup(func() { os.Args[0] = origArgs0 })
	os.Args[0] = exePath

	got, ewc := GetHandlerEnv()
	require.Nil(t, ewc)
	require.Equal(t, want, got)
}

func TestGetHandlerEnv_NotFound(t *testing.T) {
	tmp := t.TempDir()

	exeDir := filepath.Join(tmp, "ext")
	require.NoError(t, os.MkdirAll(exeDir, 0o755))

	exePath := filepath.Join(exeDir, "run-command-handler-linux")
	_ = os.WriteFile(exePath, []byte("dummy"), 0o755)

	origArgs0 := os.Args[0]
	t.Cleanup(func() { os.Args[0] = origArgs0 })
	os.Args[0] = exePath

	_, err := GetHandlerEnv()
	VerifyErrorClarification(t, constants.HandlerEnv_NotFound, err)
}

func TestGetHandlerEnv_HandlingError_OnReadFailure(t *testing.T) {
	// On Windows, chmod perms tests are unreliable due to ACL behavior; skip.
	if runtime.GOOS == "windows" {
		t.Skip("permission-based read failure test is unreliable on Windows")
	}

	tmp := t.TempDir()

	exeDir := filepath.Join(tmp, "ext")
	require.NoError(t, os.MkdirAll(exeDir, 0o755))

	exePath := filepath.Join(exeDir, "run-command-handler-linux")
	_ = os.WriteFile(exePath, []byte("dummy"), 0o755)

	// Create a HandlerEnvironment.json but remove read permissions to trigger an error != IsNotExist
	envPath := filepath.Join(exeDir, HandlerEnvFileName)
	writeHandlerEnvJSON(t, envPath, types.HandlerEnvironment{Version: 1.0})
	require.NoError(t, os.Chmod(envPath, 0o000))

	origArgs0 := os.Args[0]
	t.Cleanup(func() {
		os.Args[0] = origArgs0
		_ = os.Chmod(envPath, 0o644) // restore for cleanup on some fs
	})
	os.Args[0] = exePath

	_, err := GetHandlerEnv()
	VerifyErrorClarification(t, constants.HandlerEnv_HandlingError, err)
}
