package exec

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

var (
	testHandlerSettings handlersettings.HandlerSettings = handlersettings.HandlerSettings{PublicSettings: handlersettings.PublicSettings{}, ProtectedSettings: handlersettings.ProtectedSettings{}}
	testContext                                         = log.NewContext(log.NewNopLogger())
)

func TestExec_SuccessExitCodeOkay(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	out := newCloseRecorder()
	errw := newCloseRecorder()

	exitCode, err := Exec(newCtx(), "echo hi", t.TempDir(), out, errw, cfg)
	require.Nil(t, err)
	require.Equal(t, constants.ExitCode_Okay, exitCode)
	require.Contains(t, out.String(), "hi")
}

func TestExec_AlwaysClosesStreams(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	out := newCloseRecorder()
	errw := newCloseRecorder()

	// Force runCommand to return error to ensure closures happen on error path.
	FnRunCommand = func(_ *exec.Cmd) error {
		return errors.New("the chipmunks have revolted")
	}

	_, _ = Exec(newCtx(), "/bin/true", t.TempDir(), out, errw, cfg)
	require.True(t, out.closed, "stdout should be closed")
	require.True(t, errw.closed, "stderr should be closed")
}

func TestExec_RunAsUser_InvalidScriptPathPrefix(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.RunAsUser = "someuser"

	out := newCloseRecorder()
	errw := newCloseRecorder()

	// Script path does NOT start with constants.DataDir => should fail early
	exitCode, err := Exec(newCtx(), "/tmp/not-under-datadir/script.sh", t.TempDir(), out, errw, cfg)
	require.Error(t, err)
	require.Equal(t, constants.Internal_IncorrectRunAsScriptPath, exitCode)
	VerifyErrorClarification(t, constants.Internal_IncorrectRunAsScriptPath, err)
}

func TestExec_RunAsUser_OpenSourceScriptFails_ReturnsOpenSourceFailed(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.RunAsUser = "someuser"

	// Fail opening the source script
	fnOsOpenFile = func(_ string, _ int, _ os.FileMode) (*os.File, error) {
		return nil, errors.New("open failed")
	}
	// Ensure we don't accidentally reach execution
	FnRunCommand = func(_ *exec.Cmd) error {
		t.Fatalf("fnRunCommand should not be called when RunAs setup fails")
		return nil
	}

	out := newCloseRecorder()
	errw := newCloseRecorder()

	code, err := Exec(newCtx(), constants.DataDir, t.TempDir(), out, errw, cfg)
	require.Error(t, err)
	require.Equal(t, constants.Internal_RunAsOpenSourceScriptFileFailed, code)
	VerifyErrorClarification(t, constants.Internal_RunAsOpenSourceScriptFileFailed, err)
}

func TestExec_RunAsUser_CreateDestScriptFails_ReturnsOpenSourceFailed(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.RunAsUser = "someuser"

	// Open source succeeds
	fnOsOpenFile = func(_ string, _ int, _ os.FileMode) (*os.File, error) {
		return nil, nil // File won't be used before the method fails
	}
	// Dest create fails
	fnOsCreate = func(_ string) (*os.File, error) {
		return nil, errors.New("create failed")
	}
	FnRunCommand = func(_ *exec.Cmd) error {
		t.Fatalf("fnRunCommand should not be called when RunAs setup fails")
		return nil
	}

	out := newCloseRecorder()
	errw := newCloseRecorder()

	code, err := Exec(newCtx(), constants.DataDir, t.TempDir(), out, errw, cfg)
	require.Error(t, err)
	require.Equal(t, constants.Internal_RunAsOpenSourceScriptFileFailed, code)
	VerifyErrorClarification(t, constants.Internal_RunAsOpenSourceScriptFileFailed, err)
}

func TestExec_RunAsUser_CopyFails_ReturnsCopyFailed(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.RunAsUser = "someuser"

	fnOsOpenFile = func(_ string, _ int, _ os.FileMode) (*os.File, error) {
		return nil, nil // File won't be used before we fail
	}

	fnOsCreate = func(_ string) (*os.File, error) {
		return nil, nil
	}

	// Copy fails
	fnIoCopy = func(_ io.Writer, _ io.Reader) (int64, error) {
		return 0, errors.New("the chipmunks do not copy")
	}
	FnRunCommand = func(_ *exec.Cmd) error {
		t.Fatalf("fnRunCommand should not be called when RunAs setup fails")
		return nil
	}

	out := newCloseRecorder()
	errw := newCloseRecorder()

	code, err := Exec(newCtx(), constants.DataDir, t.TempDir(), out, errw, cfg)
	require.Error(t, err)
	require.Equal(t, constants.Internal_RunAsCopySourceScriptToRunAsScriptFileFailed, code)
	VerifyErrorClarification(t, constants.Internal_RunAsCopySourceScriptToRunAsScriptFileFailed, err)
}

func TestExec_RunAsUser_LookupUserFails_ReturnsRunAsUserLogonFailed(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.RunAsUser = "someuser"

	// Open + create + copy succeed
	tmpSrc := filepath.Join(t.TempDir(), "src.sh")
	require.NoError(t, os.WriteFile(tmpSrc, []byte("echo hi\n"), 0600))
	fnOsOpenFile = func(_ string, _ int, _ os.FileMode) (*os.File, error) { return os.Open(tmpSrc) }
	fnOsCreate = func(_ string) (*os.File, error) { return os.CreateTemp(t.TempDir(), "dest-*") }
	fnIoCopy = func(_ io.Writer, _ io.Reader) (int64, error) { return 1, nil }

	// User lookup fails
	fnUserLookup = func(_ string) (*user.User, error) {
		return nil, errors.New("no such chipmunk")
	}
	FnRunCommand = func(_ *exec.Cmd) error {
		t.Fatalf("fnRunCommand should not be called when RunAs setup fails")
		return nil
	}

	out := newCloseRecorder()
	errw := newCloseRecorder()

	code, err := Exec(newCtx(), constants.DataDir, t.TempDir(), out, errw, cfg)
	require.Error(t, err)
	require.Equal(t, constants.CommandExecution_RunAsUserLogonFailed, code)
	VerifyErrorClarification(t, constants.CommandExecution_RunAsUserLogonFailed, err)
}

func TestExec_RunAsUser_UidParseFails_ReturnsLookupUserUidFailed(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.RunAsUser = "someuser"

	tmpSrc := filepath.Join(t.TempDir(), "src.sh")
	require.NoError(t, os.WriteFile(tmpSrc, []byte("echo hi\n"), 0600))
	fnOsOpenFile = func(_ string, _ int, _ os.FileMode) (*os.File, error) { return os.Open(tmpSrc) }
	fnOsCreate = func(_ string) (*os.File, error) { return os.CreateTemp(t.TempDir(), "dest-*") }
	fnIoCopy = func(_ io.Writer, _ io.Reader) (int64, error) { return 1, nil }

	// Lookup returns non-int Uid
	fnUserLookup = func(_ string) (*user.User, error) {
		return &user.User{Uid: "not-an-int"}, nil
	}
	FnRunCommand = func(_ *exec.Cmd) error {
		t.Fatalf("fnRunCommand should not be called when RunAs setup fails")
		return nil
	}

	out := newCloseRecorder()
	errw := newCloseRecorder()

	code, err := Exec(newCtx(), constants.DataDir, t.TempDir(), out, errw, cfg)
	require.Error(t, err)
	require.Equal(t, constants.Internal_RunAsLookupUserUidFailed, code)
	VerifyErrorClarification(t, constants.Internal_RunAsLookupUserUidFailed, err)
}

func TestExec_RunAsUser_ChownFails_ReturnsChangeOwnerFailed(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.RunAsUser = "someuser"

	tmpSrc := filepath.Join(t.TempDir(), "src.sh")
	require.NoError(t, os.WriteFile(tmpSrc, []byte("echo hi\n"), 0600))
	fnOsOpenFile = func(_ string, _ int, _ os.FileMode) (*os.File, error) { return os.Open(tmpSrc) }
	fnOsCreate = func(_ string) (*os.File, error) { return os.CreateTemp(t.TempDir(), "dest-*") }
	fnIoCopy = func(_ io.Writer, _ io.Reader) (int64, error) { return 1, nil }

	fnUserLookup = func(_ string) (*user.User, error) {
		return &user.User{Uid: "1234"}, nil
	}
	fnOsChown = func(_ string, _ int, _ int) error {
		return errors.New("chown failed")
	}
	FnRunCommand = func(_ *exec.Cmd) error {
		t.Fatalf("fnRunCommand should not be called when RunAs setup fails")
		return nil
	}

	out := newCloseRecorder()
	errw := newCloseRecorder()

	code, err := Exec(newCtx(), constants.DataDir, t.TempDir(), out, errw, cfg)
	require.Error(t, err)
	require.Equal(t, constants.Internal_RunAsScriptFileChangeOwnerFailed, code)
	VerifyErrorClarification(t, constants.Internal_RunAsScriptFileChangeOwnerFailed, err)
}

func TestExec_RunAsUser_ChmodFails_ReturnsChangePermissionsFailed(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.RunAsUser = "someuser"

	tmpSrc := filepath.Join(t.TempDir(), "src.sh")
	require.NoError(t, os.WriteFile(tmpSrc, []byte("echo hi\n"), 0600))
	fnOsOpenFile = func(_ string, _ int, _ os.FileMode) (*os.File, error) { return os.Open(tmpSrc) }
	fnOsCreate = func(_ string) (*os.File, error) { return os.CreateTemp(t.TempDir(), "dest-*") }
	fnIoCopy = func(_ io.Writer, _ io.Reader) (int64, error) { return 1, nil }

	fnUserLookup = func(_ string) (*user.User, error) {
		return &user.User{Uid: "1234"}, nil
	}
	fnOsChown = func(_ string, _ int, _ int) error { return nil }
	fnOsChMod = func(_ string, _ os.FileMode) error {
		return errors.New("chmod failed")
	}

	FnRunCommand = func(_ *exec.Cmd) error {
		t.Fatalf("fnRunCommand should not be called when RunAs setup fails")
		return nil
	}

	out := newCloseRecorder()
	errw := newCloseRecorder()

	code, err := Exec(newCtx(), constants.DataDir, t.TempDir(), out, errw, cfg)
	require.Error(t, err)
	require.Equal(t, constants.Internal_RunAsScriptFileChangePermissionsFailed, code)
	VerifyErrorClarification(t, constants.Internal_RunAsScriptFileChangePermissionsFailed, err)
}

func TestExecCmdInDir_OpenStdoutFails(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()

	fnOsOpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		if strings.HasSuffix(name, "stdout") {
			return nil, errors.New("open stdout failed")
		}
		return os.CreateTemp(t.TempDir(), "stderr-*")
	}

	err, code := ExecCmdInDir(newCtx(), "echo hi", t.TempDir(), cfg)
	require.Error(t, err)
	require.Equal(t, constants.FileSystem_OpenStandardOutFailed, code)
	VerifyErrorClarification(t, constants.FileSystem_OpenStandardOutFailed, err)
}

func TestExecCmdInDir_OpenStderrFails(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()

	fnOsOpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		if strings.HasSuffix(name, "stdout") {
			return nil, nil // The directory won't be used before stderr fails
		}
		return nil, errors.New("open stderr failed")
	}

	err, code := ExecCmdInDir(newCtx(), "echo hi", t.TempDir(), cfg)
	require.Error(t, err)
	require.Equal(t, constants.FileSystem_OpenStandardErrorFailed, code)
	VerifyErrorClarification(t, constants.FileSystem_OpenStandardErrorFailed, err)
}

func TestExec_failure_exitError(t *testing.T) {
	ec, err := Exec(testContext, "exit 12", "/", new(mockFile), new(mockFile), &testHandlerSettings)
	require.NotNil(t, err)
	require.EqualError(t, err, "command terminated with exit status=12") // error is customized
	require.EqualValues(t, 12, ec)
}

func TestExec_failure_timeout(t *testing.T) {
	testHandlerSettings.PublicSettings.TimeoutInSeconds = 1
	ec, err := Exec(testContext, "sleep 20", "/", new(mockFile), new(mockFile), &testHandlerSettings)
	testHandlerSettings.PublicSettings.TimeoutInSeconds = 0
	require.NotNil(t, err)
	require.EqualError(t, err, "command terminated with exit status=-1") // error is customized
	require.EqualValues(t, -1, ec)
}

func TestSetEnvironmentVariables_NamedAndUnnamed(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.Parameters = []handlersettings.ParameterDefinition{
		{Name: "FOO", Value: "bar"}, // named -> env
		{Name: "", Value: "arg1"},   // unnamed -> arg
	}
	cfg.ProtectedSettings.ProtectedParameters = []handlersettings.ParameterDefinition{
		{Name: "BAZ", Value: "qux"},
		{Name: "", Value: "arg2"},
	}

	setCalls := map[string]string{}
	fnOsSetEnv = func(k, v string) error {
		setCalls[k] = v
		return nil
	}

	args, err := SetEnvironmentVariables(cfg)
	require.NoError(t, err)
	require.Contains(t, args, " arg1")
	require.Contains(t, args, " arg2")
	require.Equal(t, "bar", setCalls["FOO"])
	require.Equal(t, "qux", setCalls["BAZ"])
}

func TestSetEnvironmentVariables_ReturnsLastSetEnvError(t *testing.T) {
	restore := saveAndRestoreFns()
	defer restore()

	cfg := minimalCfg()
	cfg.PublicSettings.Parameters = []handlersettings.ParameterDefinition{
		{Name: "X", Value: "1"},
		{Name: "Y", Value: "2"},
	}

	var call int
	wantErr := errors.New("setenv failed")
	fnOsSetEnv = func(k, v string) error {
		call++
		if call == 2 {
			return wantErr
		}
		return nil
	}

	_, err := SetEnvironmentVariables(cfg)
	require.Error(t, err)
	require.Equal(t, wantErr, err)
}

func TestExec_failure_genericError(t *testing.T) {
	_, err := Exec(testContext, "date", "/non-existing-path", new(mockFile), new(mockFile), &testHandlerSettings)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to execute command:") // error is wrapped
}

func TestExec_failure_fdClosed(t *testing.T) {
	out := new(mockFile)
	require.Nil(t, out.Close())

	_, err := Exec(testContext, "date", "/", out, out, &testHandlerSettings)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "file closed") // error is wrapped
}

func TestExec_failure_redirectsStdStreams_closesFds(t *testing.T) {
	o, e := new(mockFile), new(mockFile)
	require.False(t, o.closed, "stdout open")
	require.False(t, e.closed, "stderr open")

	_, err := Exec(testContext, `/bin/echo 'I am stdout!'>&1; /bin/echo 'I am stderr!'>&2; exit 12`, "/", o, e, &testHandlerSettings)
	require.NotNil(t, err)
	require.Equal(t, "I am stdout!\n", string(o.b.Bytes()))
	require.Equal(t, "I am stderr!\n", string(e.b.Bytes()))
	require.True(t, o.closed, "stdout closed")
	require.True(t, e.closed, "stderr closed")
}

func TestExecCmdInDir(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	err, exitCode := ExecCmdInDir(testContext, "/bin/echo 'Hello world'", dir, &testHandlerSettings)
	require.Nil(t, err)
	require.True(t, fileExists(t, filepath.Join(dir, "stdout")), "stdout file should be created")
	require.True(t, fileExists(t, filepath.Join(dir, "stderr")), "stderr file should be created")
	require.Equal(t, constants.ExitCode_Okay, exitCode)

	b, err := ioutil.ReadFile(filepath.Join(dir, "stdout"))
	require.Nil(t, err)
	require.Equal(t, "Hello world\n", string(b))

	b, err = ioutil.ReadFile(filepath.Join(dir, "stderr"))
	require.Nil(t, err)
	require.EqualValues(t, 0, len(b), "stderr file must be empty")
}

func TestExecCmdInDir_cantOpenError(t *testing.T) {
	err, exitCode := ExecCmdInDir(testContext, "/bin/echo 'Hello world'", "/non-existing-dir", &testHandlerSettings)
	require.Contains(t, err.Error(), "failed to open stdout file")
	require.NotEqual(t, constants.ExitCode_Okay, exitCode)
}

func TestExecCmdInDir_truncates(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	err, exitCode := ExecCmdInDir(testContext, "/bin/echo '1:out'; /bin/echo '1:err'>&2", dir, &testHandlerSettings)
	require.Nil(t, err)
	require.Equal(t, constants.ExitCode_Okay, exitCode)

	err, exitCode = ExecCmdInDir(testContext, "/bin/echo '2:out'; /bin/echo '2:err'>&2", dir, &testHandlerSettings)
	require.Nil(t, err)
	require.Equal(t, constants.ExitCode_Okay, exitCode)

	b, err := ioutil.ReadFile(filepath.Join(dir, "stdout"))
	require.Nil(t, err)
	require.Equal(t, "2:out\n", string(b), "stdout did not truncate")

	b, err = ioutil.ReadFile(filepath.Join(dir, "stderr"))
	require.Nil(t, err)
	require.Equal(t, "2:err\n", string(b), "stderr did not truncate")
}

func Test_logPaths(t *testing.T) {
	stdout, stderr := LogPaths("/tmp")
	require.Equal(t, "/tmp/stdout", stdout)
	require.Equal(t, "/tmp/stderr", stderr)
}

// Test utilities

type closeRecorder struct {
	buf    *bytes.Buffer
	closed bool
}

func newCloseRecorder() *closeRecorder {
	return &closeRecorder{buf: &bytes.Buffer{}}
}

func (c *closeRecorder) Write(p []byte) (int, error) { return c.buf.Write(p) }
func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}
func (c *closeRecorder) String() string { return c.buf.String() }

func newCtx() *log.Context {
	return log.NewContext(log.NewNopLogger())
}

func minimalCfg() *handlersettings.HandlerSettings {
	return &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{
			RunAsUser:                       "",
			TimeoutInSeconds:                0,
			Parameters:                      nil,
			TreatFailureAsDeploymentFailure: false,
		},
		ProtectedSettings: handlersettings.ProtectedSettings{
			RunAsPassword:       "",
			ProtectedParameters: nil,
		},
	}
}

type savedFns struct {
	ioCopy     func(dst io.Writer, src io.Reader) (written int64, err error)
	chmod      func(string, os.FileMode) error
	chown      func(string, int, int) error
	create     func(string) (*os.File, error)
	mkdirAll   func(string, os.FileMode) error
	openFile   func(string, int, os.FileMode) (*os.File, error)
	setEnv     func(string, string) error
	runCommand func(*exec.Cmd) error
	userLookup func(string) (*user.User, error)
}

func saveAndRestoreFns() func() {
	s := savedFns{
		ioCopy:     fnIoCopy,
		chmod:      fnOsChMod,
		chown:      fnOsChown,
		create:     fnOsCreate,
		mkdirAll:   fnOsMkDirAll,
		openFile:   fnOsOpenFile,
		setEnv:     fnOsSetEnv,
		runCommand: FnRunCommand,
		userLookup: fnUserLookup,
	}
	return func() {
		fnIoCopy = s.ioCopy
		fnOsChMod = s.chmod
		fnOsChown = s.chown
		fnOsCreate = s.create
		fnOsMkDirAll = s.mkdirAll
		fnOsOpenFile = s.openFile
		fnOsSetEnv = s.setEnv
		FnRunCommand = s.runCommand
		fnUserLookup = s.userLookup
	}
}

type mockFile struct {
	b      bytes.Buffer
	closed bool
}

func (m *mockFile) Write(p []byte) (n int, err error) {
	if m.closed {
		return 0, errors.New("file closed")
	}
	return m.b.Write(p)
}

func (m *mockFile) Close() error {
	m.closed = true
	return nil
}

func fileExists(t *testing.T, path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("failed to check if %s exists: %v", path, err)
	return false
}

func VerifyErrorClarification(t *testing.T, expectedCode int, ewc *vmextension.ErrorWithClarification) {
	require.NotNil(t, ewc, "No error returned when one was expected")
	require.Equal(t, expectedCode, ewc.ErrorCode, "Expected error %d but received %d", expectedCode, ewc.ErrorCode)
}
