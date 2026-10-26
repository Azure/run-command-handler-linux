package files

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/pkg/download"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type fileErrorLogger struct{ err error }

func (l *fileErrorLogger) Log(values ...interface{}) error {
	for _, value := range values {
		if err, ok := value.(error); ok {
			l.err = err
		}
	}
	return nil
}

type fakeScriptWriter struct{ writeErr, closeErr error }

func (w fakeScriptWriter) WriteString(s string) (int, error) { return len(s), w.writeErr }
func (w fakeScriptWriter) Close() error                      { return w.closeErr }

func assertFileCode(t *testing.T, err error, want int) {
	t.Helper()
	code, ok := errorutil.Code(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, want, code)
}

func TestScriptClarifications(t *testing.T) {
	oldDownload, oldSave := downloadSASBlob, saveDownload
	oldText, oldRead, oldWrite, oldOpen := isScriptText, readScript, writeScript, openScriptFile
	oldMode := setScriptMode
	t.Cleanup(func() {
		downloadSASBlob, saveDownload = oldDownload, oldSave
		isScriptText, readScript, writeScript, openScriptFile = oldText, oldRead, oldWrite, oldOpen
		setScriptMode = oldMode
	})
	cause := errors.New("file failure")
	setScriptMode = func(string, os.FileMode) error { return nil }
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	_, err := downloadAndProcessURL(ctx, "://", ".", "script", "", nil)
	assertFileCode(t, err, errorutil.CustomerInput_invalidScriptUrl)
	downloadSASBlob = func(string, string, string) (string, error) { return "", cause }
	saveDownload = func(*log.Context, []download.Downloader, string, os.FileMode) (int64, error) { return 0, nil }
	isScriptText = func(string) (bool, error) { return false, nil }
	logger := &fileErrorLogger{}
	_, err = downloadAndProcessURL(log.NewContext(logger), "https://example.invalid/script", ".", "script", "sas", nil)
	require.NoError(t, err, "SAS failure still falls back to public download")
	assertFileCode(t, logger.err, errorutil.FileDownload_scriptSasDownloadFailed)
	require.ErrorIs(t, logger.err, cause)
	saveDownload = func(*log.Context, []download.Downloader, string, os.FileMode) (int64, error) { return 0, cause }
	_, err = downloadAndProcessURL(ctx, "https://example.invalid/script", ".", "script", "", nil)
	assertFileCode(t, err, errorutil.Os_FailedToMakeDownloadedFileExecutable)
	require.ErrorIs(t, err, cause)
	_, err = getDownloaders("", nil, download.ProdMsiDownloader{})
	assertFileCode(t, err, errorutil.CustomerInput_emptyDownloadUrl)
	_, err = getDownloaders("https://account.blob.core.windows.net/container/script", &handlersettings.RunCommandManagedIdentity{ClientId: "client", ObjectId: "object"}, download.ProdMsiDownloader{})
	assertFileCode(t, err, errorutil.CustomerInput_clientIdObjectIdBothSpecified)
	_, err = UrlToFileName("%")
	assertFileCode(t, err, errorutil.CustomerInput_invalidDownloadFileUrl)
	_, err = UrlToFileName("https://example.invalid/")
	assertFileCode(t, err, errorutil.CustomerInput_missingDownloadFileName)
	isScriptText = func(string) (bool, error) { return false, cause }
	assertFileCode(t, PostProcessFile("script"), errorutil.Os_FailedToDetermineScriptFileType)
	isScriptText = func(string) (bool, error) { return true, nil }
	readScript = func(string) ([]byte, error) { return nil, cause }
	assertFileCode(t, PostProcessFile("script"), errorutil.Os_FailedToReadScriptForPostprocessing)
	readScript = func(string) ([]byte, error) { return []byte("script\r\n"), nil }
	writeScript = func(string, []byte, os.FileMode) error { return cause }
	err = PostProcessFile("script")
	assertFileCode(t, err, errorutil.Os_FailedToWritePostprocessedScript)
	require.ErrorIs(t, err, cause, "write errors must not be overwritten by rename")
	for _, want := range []int{errorutil.Os_FailedToOpenEmbeddedScript, errorutil.Os_FailedToWriteEmbeddedScript, errorutil.Os_FailedToCloseEmbeddedScript} {
		openScriptFile = func(string, int, os.FileMode) (scriptWriter, error) {
			if want == errorutil.Os_FailedToOpenEmbeddedScript {
				return nil, cause
			}
			if want == errorutil.Os_FailedToWriteEmbeddedScript {
				return fakeScriptWriter{writeErr: cause}, nil
			}
			return fakeScriptWriter{closeErr: cause}, nil
		}
		err := SaveScriptFile("script", "content")
		assertFileCode(t, err, want)
		require.ErrorIs(t, err, cause)
	}
}

func TestDownloadedScriptPermissionClarifications(t *testing.T) {
	oldDownload, oldText, oldMode := downloadSASBlob, isScriptText, setScriptMode
	t.Cleanup(func() { downloadSASBlob, isScriptText, setScriptMode = oldDownload, oldText, oldMode })
	cause := errors.New("permission failure")
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	downloadSASBlob = func(string, string, string) (string, error) { return "script", nil }
	isScriptText = func(string) (bool, error) { return false, nil }
	for _, want := range []int{errorutil.Os_FailedToMakeDownloadedFileWritable, errorutil.Os_FailedToRestoreDownloadedFilePermissions} {
		var modes []os.FileMode
		setScriptMode = func(_ string, mode os.FileMode) error {
			modes = append(modes, mode)
			if want == errorutil.Os_FailedToMakeDownloadedFileWritable || len(modes) == 2 {
				return cause
			}
			return nil
		}
		_, err := downloadAndProcessURL(ctx, "https://example.invalid/script", ".", "script", "sas", nil)
		assertFileCode(t, err, want)
		require.ErrorIs(t, err, cause)
		if want == errorutil.Os_FailedToRestoreDownloadedFilePermissions {
			require.Equal(t, []os.FileMode{0700, 0500}, modes)
		}
	}
	var modes []os.FileMode
	setScriptMode = func(_ string, mode os.FileMode) error {
		modes = append(modes, mode)
		return nil
	}
	isScriptText = func(string) (bool, error) { return false, cause }
	_, err := downloadAndProcessURL(ctx, "https://example.invalid/script", ".", "script", "sas", nil)
	assertFileCode(t, err, errorutil.Os_FailedToDetermineScriptFileType)
	require.ErrorIs(t, err, cause)
	require.Equal(t, []os.FileMode{0700, 0500}, modes, "permissions are restored after post-processing errors")
}

type tailFileInfo struct{}

func (tailFileInfo) Name() string       { return "file" }
func (tailFileInfo) Size() int64        { return 10 }
func (tailFileInfo) Mode() os.FileMode  { return 0600 }
func (tailFileInfo) ModTime() time.Time { return time.Time{} }
func (tailFileInfo) IsDir() bool        { return false }
func (tailFileInfo) Sys() interface{}   { return nil }

type failingReadableFile struct {
	stage string
	err   error
}

func (f failingReadableFile) Stat() (os.FileInfo, error) {
	if f.stage == "stat" {
		return nil, f.err
	}
	return tailFileInfo{}, nil
}
func (f failingReadableFile) Seek(int64, int) (int64, error) {
	if f.stage == "seek" {
		return 0, f.err
	}
	return 0, nil
}
func (f failingReadableFile) Read([]byte) (int, error) { return 0, f.err }
func (f failingReadableFile) Close() error             { return nil }

func TestTailClarifications(t *testing.T) {
	oldOpen := openReadableFile
	t.Cleanup(func() { openReadableFile = oldOpen })
	cause := errors.New("read failure")
	for _, tc := range []struct {
		stage        string
		tailCode     int
		positionCode int
	}{
		{"open", errorutil.Os_FailedToOpenTailFile, errorutil.Os_FailedToOpenOutputFile},
		{"stat", errorutil.Os_FailedToStatTailFile, errorutil.Os_FailedToStatOutputFile},
		{"seek", errorutil.Os_FailedToSeekTailFile, errorutil.Os_FailedToSeekOutputFile},
		{"read", errorutil.Os_FailedToReadTailFile, errorutil.Os_FailedToReadOutputFile},
	} {
		openReadableFile = func(string) (readableFile, error) {
			if tc.stage == "open" {
				return nil, cause
			}
			return failingReadableFile{stage: tc.stage, err: cause}, nil
		}
		_, err := TailFile("file", 1)
		assertFileCode(t, err, tc.tailCode)
		require.ErrorIs(t, err, cause)
		_, err = GetFileFromPosition("file", 0)
		assertFileCode(t, err, tc.positionCode)
		require.ErrorIs(t, err, cause)
	}
	openReadableFile = func(string) (readableFile, error) { return nil, os.ErrNotExist }
	_, err := TailFile("missing", 1)
	require.NoError(t, err)
	_, err = GetFileFromPosition("missing", 0)
	require.NoError(t, err)
}
