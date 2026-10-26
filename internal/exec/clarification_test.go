package exec

import (
	"errors"
	"io"
	"os"
	osexec "os/exec"
	"os/user"
	"strconv"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

func fmtCode(code int) string { return strconv.Itoa(code) }

type clarificationFile struct {
	readErr, writeErr, chownErr, chmodErr, closeErr error
	closed                                          bool
}

func (f *clarificationFile) Read([]byte) (int, error)    { return 0, f.readErr }
func (f *clarificationFile) Write(p []byte) (int, error) { return len(p), f.writeErr }
func (f *clarificationFile) Close() error                { f.closed = true; return f.closeErr }
func (f *clarificationFile) Chown(int, int) error        { return f.chownErr }
func (f *clarificationFile) Chmod(os.FileMode) error     { return f.chmodErr }

func mockExecution(t *testing.T) {
	t.Helper()
	open, mkdir, remove, lookup := openScriptFile, makeScriptDirectory, removeScriptFile, lookupRunAsUser
	setenv, run, copy, status := setEnvironment, runCommand, copyScript, commandExitStatus
	t.Cleanup(func() {
		openScriptFile, makeScriptDirectory, removeScriptFile, lookupRunAsUser = open, mkdir, remove, lookup
		setEnvironment, runCommand, copyScript, commandExitStatus = setenv, run, copy, status
	})
	openScriptFile = func(string, int, os.FileMode) (scriptFile, error) { return &clarificationFile{}, nil }
	makeScriptDirectory = func(string, os.FileMode) error { return nil }
	removeScriptFile = func(string) error { return nil }
	lookupRunAsUser = func(string) (*user.User, error) { return &user.User{Uid: "1000"}, nil }
	setEnvironment = func(string, string) error { return nil }
	runCommand = func(*osexec.Cmd) error { return nil }
	copyScript = func(io.Writer, io.Reader) (int64, error) { return 0, nil }
	commandExitStatus = func(error) (int, bool, bool) { return 0, false, false }
}

func TestExecutionClarificationErrors(t *testing.T) {
	failure := errors.New("injected machine failure")
	tests := []struct {
		code  int
		setup func(*handlersettings.HandlerSettings, *clarificationFile, *clarificationFile)
	}{
		{errorutil.Internal_incorrectRunAsScriptPath, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
		}},
		{errorutil.Os_FailedToCreateRunAsDirectory, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			makeScriptDirectory = func(string, os.FileMode) error { return failure }
		}},
		{errorutil.Os_FailedToOpenRunAsSourceScript, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			openScriptFile = func(string, int, os.FileMode) (scriptFile, error) { return nil, failure }
		}},
		{errorutil.Os_FailedToCopyRunAsScript, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			copyScript = func(io.Writer, io.Reader) (int64, error) { return 0, failure }
		}},
		{errorutil.CommandExecution_runAsUserNotFound, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			lookupRunAsUser = func(string) (*user.User, error) { return nil, failure }
		}},
		{errorutil.Os_InvalidRunAsUserId, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			lookupRunAsUser = func(string) (*user.User, error) { return &user.User{Uid: "invalid"}, nil }
		}},
		{errorutil.Os_FailedToChangeRunAsScriptOwner, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			openScriptFile = func(string, int, os.FileMode) (scriptFile, error) { return &clarificationFile{chownErr: failure}, nil }
		}},
		{errorutil.Os_FailedToChangeRunAsScriptPermissions, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			openScriptFile = func(string, int, os.FileMode) (scriptFile, error) { return &clarificationFile{chmodErr: failure}, nil }
		}},
		{errorutil.Os_FailedToRemoveRunAsScript, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			removeScriptFile = func(string) error { return failure }
		}},
		{errorutil.Os_FailedToCreateRunAsScript, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			openScriptFile = func(_ string, flag int, _ os.FileMode) (scriptFile, error) {
				if flag&os.O_CREATE != 0 {
					return nil, failure
				}
				return &clarificationFile{}, nil
			}
		}},
		{errorutil.CustomerInput_invalidParameterEnvironmentVariable, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.Parameters = []handlersettings.ParameterDefinition{{Name: "parameter", Value: "value"}, {Name: "next", Value: "value"}}
			calls := 0
			setEnvironment = func(string, string) error { calls++; require.Equal(t, 1, calls); return failure }
		}},
		{errorutil.CommandExecution_failedUnknownError, func(_ *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			runCommand = func(*osexec.Cmd) error { return failure }
		}},
		{errorutil.CommandExecution_failureExitCode, func(_ *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			runCommand = func(*osexec.Cmd) error { return failure }
			commandExitStatus = func(error) (int, bool, bool) { return 12, false, true }
		}},
		{errorutil.CommandExecution_interruptedByVmShutdown, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.TimeoutInSeconds = 10
			runCommand = func(*osexec.Cmd) error { return failure }
			commandExitStatus = func(error) (int, bool, bool) { return -1, true, true }
		}},
		{errorutil.Os_FailedToCloseStdOut, func(_ *handlersettings.HandlerSettings, out, _ *clarificationFile) { out.closeErr = failure }},
		{errorutil.Os_FailedToCloseStdErr, func(_ *handlersettings.HandlerSettings, _, stderr *clarificationFile) { stderr.closeErr = failure }},
		{errorutil.Os_FailedToCloseRunAsSourceScript, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			openScriptFile = func(_ string, flag int, _ os.FileMode) (scriptFile, error) {
				file := &clarificationFile{}
				if flag&os.O_CREATE == 0 {
					file.closeErr = failure
				}
				return file, nil
			}
		}},
		{errorutil.Os_FailedToCloseRunAsScript, func(cfg *handlersettings.HandlerSettings, _, _ *clarificationFile) {
			cfg.PublicSettings.RunAsUser = "user"
			openScriptFile = func(_ string, flag int, _ os.FileMode) (scriptFile, error) {
				file := &clarificationFile{}
				if flag&os.O_CREATE != 0 {
					file.closeErr = failure
				}
				return file, nil
			}
		}},
	}
	for _, test := range tests {
		t.Run(fmtCode(test.code), func(t *testing.T) {
			mockExecution(t)
			cfg := handlersettings.HandlerSettings{}
			out, stderr := &clarificationFile{}, &clarificationFile{}
			test.setup(&cfg, out, stderr)
			path := constants.DataDir + "/download/script.sh"
			if test.code == errorutil.Internal_incorrectRunAsScriptPath {
				path = "/unexpected/script.sh"
			}
			_, err := Exec(testContext, path, "/mock/workdir", out, stderr, &cfg)
			code, ok := errorutil.Code(err)
			require.True(t, ok, "error was not classified: %v", err)
			require.Equal(t, test.code, code)
			require.True(t, out.closed)
			require.True(t, stderr.closed)
		})
	}
}

func TestOutputFileClarificationErrors(t *testing.T) {
	for _, code := range []int{errorutil.Os_FailedToOpenStdOut, errorutil.Os_FailedToOpenStdErr} {
		t.Run(fmtCode(code), func(t *testing.T) {
			mockExecution(t)
			out := &clarificationFile{}
			calls := 0
			openScriptFile = func(string, int, os.FileMode) (scriptFile, error) {
				calls++
				if code == errorutil.Os_FailedToOpenStdOut || calls == 2 {
					return nil, errors.New("open failed")
				}
				return out, nil
			}
			err, _ := ExecCmdInDir(testContext, "unused", "/mock", &handlersettings.HandlerSettings{})
			actual, ok := errorutil.Code(err)
			require.True(t, ok)
			require.Equal(t, code, actual)
			if code == errorutil.Os_FailedToOpenStdErr {
				require.True(t, out.closed)
			}
		})
	}
}

func TestExecutionPreservesLowerClarification(t *testing.T) {
	mockExecution(t)
	cause := errorutil.WithCode(errorutil.CustomerInput_invalidSettingsJson, errors.New("lower failure"))
	runCommand = func(*osexec.Cmd) error { return cause }
	_, err := Exec(testContext, "unused", "/mock", &clarificationFile{}, &clarificationFile{}, &handlersettings.HandlerSettings{})
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.CustomerInput_invalidSettingsJson, code)
	require.ErrorIs(t, err, cause)
}

func TestExecutionInjectedSuccess(t *testing.T) {
	for _, runAs := range []string{"", "customer"} {
		t.Run(runAs, func(t *testing.T) {
			mockExecution(t)
			cfg := handlersettings.HandlerSettings{}
			cfg.PublicSettings.RunAsUser = runAs
			cfg.PublicSettings.TimeoutInSeconds = 30
			cfg.PublicSettings.Parameters = []handlersettings.ParameterDefinition{{Name: "name", Value: "value"}, {Value: "argument"}}
			var environment map[string]string = make(map[string]string)
			setEnvironment = func(name, value string) error { environment[name] = value; return nil }
			runCommand = func(command *osexec.Cmd) error {
				require.Equal(t, "/mock/workdir", command.Dir)
				require.Contains(t, command.Args[2], " argument")
				if runAs != "" {
					require.Contains(t, command.Args[2], "sudo -n -u customer")
				}
				return nil
			}
			out, stderr := &clarificationFile{}, &clarificationFile{}
			code, err := Exec(testContext, constants.DataDir+"/download/script.sh", "/mock/workdir", out, stderr, &cfg)
			require.NoError(t, err)
			require.Zero(t, code)
			require.Equal(t, map[string]string{"name": "value"}, environment)
			require.True(t, out.closed)
			require.True(t, stderr.closed)
		})
	}
}
