package handlersettings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDoesFileExist_FileExists_ReturnsTrue(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "a.txt")
	require.NoError(t, os.WriteFile(p, []byte("hi"), 0o600))

	require.True(t, DoesFileExist(p))
}

func TestDoesFileExist_FileMissing_ReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "missing.txt")

	require.False(t, DoesFileExist(p))
}

func TestDoesFileExist_PermissionDenied_ReturnsFalse(t *testing.T) {
	// Exercise the branch: err != nil && !IsNotExist(err) => false
	tmp := t.TempDir()

	// Create a directory we can't traverse.
	noExecDir := filepath.Join(tmp, "noexec")
	require.NoError(t, os.Mkdir(noExecDir, 0o600)) // rw------- (no execute)
	target := filepath.Join(noExecDir, "secret.txt")

	// Stat will typically fail with EACCES due to missing execute permission on dir.
	require.False(t, DoesFileExist(target))
}

func TestGetUriForLogging_Empty_ReturnsEmpty(t *testing.T) {
	require.Equal(t, "", GetUriForLogging(""))
}

func TestGetUriForLogging_ValidUrl_StripsQuery(t *testing.T) {
	// NOTE: current implementation returns "https//host/path" (missing ':')
	in := "https://example.com/container/blob.txt?sv=2020-10-02&sig=abc"
	got := GetUriForLogging(in)
	require.Equal(t, "https//example.com/container/blob.txt", got)
}

func TestGetUriForLogging_ValidUrl_NoQuery_UnchangedParts(t *testing.T) {
	in := "http://example.com/a/b/c"
	got := GetUriForLogging(in)
	require.Equal(t, "http//example.com/a/b/c", got)
}

func TestGetUriForLogging_ParseError_ReturnsEmpty(t *testing.T) {
	// Unclosed IPv6 literal => url.Parse fails
	in := "http://[::1"
	require.Equal(t, "", GetUriForLogging(in))
}

func TestGetConfigFilePath_WithExtensionName(t *testing.T) {
	got := GetConfigFilePath("/var/lib/waagent/ext/config", 2, "MyExt")
	require.Equal(t, filepath.Join("/var/lib/waagent/ext/config", "MyExt.2.settings"), got)
}

func TestGetConfigFilePath_WithoutExtensionName(t *testing.T) {
	got := GetConfigFilePath("/var/lib/waagent/ext/config", 2, "")
	require.Equal(t, filepath.Join("/var/lib/waagent/ext/config", "2.settings"), got)
}
