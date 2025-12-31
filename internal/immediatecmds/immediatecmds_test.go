package immediatecmds

import (
	"errors"
	"os"
	"testing"

	"github.com/Azure/azure-extension-platform/pkg/extensionevents"
	"github.com/Azure/azure-extension-platform/pkg/handlerenv"
	"github.com/Azure/azure-extension-platform/pkg/logging"
	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

// We keep originals so we can restore after each test.
var (
	origIsInstalled = fnServiceIsInstalled
	origIsEnabled   = fnServiceIsEnabled
	origRegister    = fnServiceRegister
	origDisable     = fnServiceDisable
	origEnable      = fnServiceEnable
	origStart       = fnServiceStart
	origDeRegister  = fnServiceDeRegister
)

func restoreServiceFns() {
	fnServiceIsInstalled = origIsInstalled
	fnServiceIsEnabled = origIsEnabled
	fnServiceRegister = origRegister
	fnServiceDisable = origDisable
	fnServiceEnable = origEnable
	fnServiceStart = origStart
	fnServiceDeRegister = origDeRegister
}

func TestUpdate_IsInstalledError(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) {
		return false, errors.New("boom")
	}

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "IsInstalledError")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Update(ctx, types.HandlerEnvironment{}, "ext", 5, events)

	require.Error(t, err)
	require.Equal(t, constants.FileSystem_CreateDataDirectoryFailed, code)
}

func TestUpdate_Installed_RegisterFails(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceRegister = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error {
		return errors.New("register failure")
	}

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "RegisterFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Update(ctx, types.HandlerEnvironment{}, "ext", 5, events)
	require.Error(t, err)
	require.Equal(t, constants.ExitCode_UpgradeInstalledServiceFailed, code)
}

func TestUpdate_Success_NoUpgradeNeeded(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return false, nil }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "NoUpgradeNeeded")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Update(ctx, types.HandlerEnvironment{}, "ext", 5, events)
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestUpdate_Success_UpgradeService(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceRegister = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error { return nil }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "UpgradeService")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Update(ctx, types.HandlerEnvironment{}, "ext", 5, events)
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestDisable_ErrorCheckingInstalled(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return false, errors.New("fail") }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "ErrorCheckingInstalled")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Disable(ctx, types.HandlerEnvironment{}, "ext", 1, events)
	require.Error(t, err)
	require.Equal(t, constants.ExitCode_DisableInstalledServiceFailed, code)
}

func TestDisable_Installed_ErrorCheckingEnabled(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceIsEnabled = func(ctx *log.Context) (bool, error) { return false, errors.New("fail") }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "ErrorCheckingEnabled")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Disable(ctx, types.HandlerEnvironment{}, "ext", 1, events)
	require.Error(t, err)
	require.Equal(t, constants.ExitCode_InstallServiceFailed, code)
}

func TestDisable_Installed_Enabled_DisableFails(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceIsEnabled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceDisable = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error {
		return errors.New("disable err")
	}

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "DisableFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Disable(ctx, types.HandlerEnvironment{}, "ext", 1, events)
	require.Error(t, err)
	require.Equal(t, constants.ExitCode_DisableInstalledServiceFailed, code)
}

func TestDisable_Installed_AlreadyDisabled(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceIsEnabled = func(ctx *log.Context) (bool, error) { return false, nil }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "AlreadyDisabled")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Disable(ctx, types.HandlerEnvironment{}, "ext", 1, events)
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestDisable_Installed_DisableSuccess(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceIsEnabled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceDisable = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error { return nil }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "DisableSuccess")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Disable(ctx, types.HandlerEnvironment{}, "ext", 1, events)
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestInstall_AlwaysOkay(t *testing.T) {
	code, err := Install()
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestUninstall_CheckInstalledError(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return false, errors.New("fail") }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "CheckInstalledError")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Uninstall(ctx, types.HandlerEnvironment{}, "x", 2, events)
	require.Error(t, err)
	require.Equal(t, constants.FileSystem_RemoveDataDirectoryFailed, code)
}

func TestUninstall_Installed_DeRegisterFails(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceDeRegister = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error {
		return errors.New("the chipmunks do not register")
	}

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "DeRegisterFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Uninstall(ctx, types.HandlerEnvironment{}, "x", 2, events)
	require.Error(t, err)
	require.Equal(t, constants.ExitCode_UninstallInstalledServiceFailed, code)
}

func TestUninstall_Installed_DeRegisterSuccess(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceDeRegister = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error { return nil }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "deregistersuccess")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Uninstall(ctx, types.HandlerEnvironment{}, "x", 2, events)
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestUninstall_NotInstalled(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return false, nil }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "notinstalled")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Uninstall(ctx, types.HandlerEnvironment{}, "x", 2, events)
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestEnable_NoServiceInstallRequested(t *testing.T) {
	cfg := handlersettings.HandlerSettings{}
	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "noserviceinstallrequested")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Enable(ctx, types.HandlerEnvironment{}, "ext", 1, cfg, events)
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestEnable_Install_CheckInstalledFails(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	cfg := getInstallAsServiceCfg()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return false, errors.New("check failed") }
	fnServiceRegister = func(ctx *log.Context, extensionEvents *extensionevents.ExtensionEventManager) error { return nil }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "checkinstallfails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Enable(ctx, types.HandlerEnvironment{}, "ext", 1, cfg, events)
	require.NoError(t, err) // Notice: Enable ignores this error; only logs
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestEnable_Install_NotInstalled_RegisterFails(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	cfg := getInstallAsServiceCfg()
	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return false, nil }
	fnServiceRegister = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error {
		return errors.New("reg fail")
	}

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "registerfails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Enable(ctx, types.HandlerEnvironment{}, "ext", 1, cfg, events)
	require.Error(t, err)
	require.Equal(t, constants.Immediate_CouldNotStartService, code)
}

func TestEnable_Install_NotInstalled_RegisterSuccess(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	cfg := getInstallAsServiceCfg()
	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return false, nil }
	fnServiceRegister = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error { return nil }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "registersuccess")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Enable(ctx, types.HandlerEnvironment{}, "ext", 1, cfg, events)
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func TestEnable_Install_Installed_CheckEnabledFails(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	cfg := getInstallAsServiceCfg()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceIsEnabled = func(ctx *log.Context) (bool, error) { return false, errors.New("oops") }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "checkenabledfails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Enable(ctx, types.HandlerEnvironment{}, "ext", 1, cfg, events)
	require.Error(t, err)
	require.Equal(t, constants.Immediate_CouldNotCheckServiceAlreadyEnabled, code)
	VerifyErrorClarification(t, constants.Immediate_CouldNotCheckServiceAlreadyEnabled, err)
}

func TestEnable_Install_Installed_NotEnabled_EnableFails(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	cfg := getInstallAsServiceCfg()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceIsEnabled = func(ctx *log.Context) (bool, error) { return false, nil }
	fnServiceEnable = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error {
		return errors.New("enablefail")
	}

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "enablefails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Enable(ctx, types.HandlerEnvironment{}, "ext", 1, cfg, events)
	require.Error(t, err)
	require.Equal(t, constants.Immediate_EnableServiceFailed, code)
}

func TestEnable_Install_Installed_NotEnabled_StartFails(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	cfg := getInstallAsServiceCfg()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceIsEnabled = func(ctx *log.Context) (bool, error) { return false, nil }
	fnServiceEnable = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error { return nil }
	fnServiceStart = func(ctx *log.Context, ev *extensionevents.ExtensionEventManager) error {
		return errors.New("startfail")
	}

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "startfails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Enable(ctx, types.HandlerEnvironment{}, "ext", 1, cfg, events)
	require.Error(t, err)
	require.Equal(t, constants.Immediate_CouldNotStartService, code)
}

func TestEnable_Install_Installed_Enabled_NoOp(t *testing.T) {
	restoreServiceFns()
	defer restoreServiceFns()

	cfg := getInstallAsServiceCfg()

	fnServiceIsInstalled = func(ctx *log.Context) (bool, error) { return true, nil }
	fnServiceIsEnabled = func(ctx *log.Context) (bool, error) { return true, nil }

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "noop")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}
	extensionLogger := logging.New(nil)
	events := extensionevents.New(extensionLogger, &handlerEnvironment)

	code, err := Enable(ctx, types.HandlerEnvironment{}, "ext", 1, cfg, events)
	require.NoError(t, err)
	require.Equal(t, constants.ExitCode_Okay, code)
}

func getInstallAsServiceCfg() handlersettings.HandlerSettings {
	cfg := handlersettings.HandlerSettings{}
	cfg.PublicSettings.InstallAsService = true
	return cfg
}

func VerifyErrorClarification(t *testing.T, expectedCode int, err error) {
	require.NotNil(t, err, "No error returned when one was expected")
	var ewc vmextension.ErrorWithClarification
	require.True(t, errors.As(err, &ewc), "Error is not of type ErrorWithClarification")
	require.Equal(t, expectedCode, ewc.ErrorCode, "Expected error %d but received %d", expectedCode, ewc.ErrorCode)
}
