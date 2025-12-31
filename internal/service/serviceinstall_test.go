package service

import (
	"fmt"
	"os"
	"testing"

	"github.com/Azure/azure-extension-platform/pkg/extensionevents"
	"github.com/Azure/azure-extension-platform/pkg/handlerenv"
	"github.com/Azure/azure-extension-platform/pkg/logging"
	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/pkg/servicehandler"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

type mockHandler struct {
	IsInstalledRet bool
	IsInstalledErr error

	GetInstalledVersionRet string
	GetInstalledVersionErr error

	StartErr                       error
	StopErr                        error
	EnableErr                      error
	DisableErr                     error
	RemoveUnitConfigurationFileErr error
	CreateUnitConfigurationFileErr error
	DaemonReloadErr                error

	IsActiveRet  bool
	IsActiveErr  error
	IsEnabledRet bool
	IsEnabledErr error
}

func (m *mockHandler) StartUnit(unitName string, ctx *log.Context) error    { return m.StartErr }
func (m *mockHandler) StopUnit(unitName string, ctx *log.Context) error     { return m.StopErr }
func (m *mockHandler) EnableUnit(unitName string, ctx *log.Context) error   { return m.EnableErr }
func (m *mockHandler) DisableUnit(unitName string, ctx *log.Context) error  { return m.DisableErr }
func (m *mockHandler) DaemonReload(unitName string, ctx *log.Context) error { return m.DaemonReloadErr }
func (m *mockHandler) IsUnitActive(unitName string, ctx *log.Context) error { return m.IsActiveErr }
func (m *mockHandler) IsUnitEnabled(unitName string, ctx *log.Context) (bool, error) {
	return m.IsEnabledRet, m.IsEnabledErr
}
func (m *mockHandler) IsUnitInstalled(unitName string, ctx *log.Context) (bool, error) {
	return m.IsInstalledRet, m.IsInstalledErr
}
func (m *mockHandler) RemoveUnitConfigurationFile(unitName string, ctx *log.Context) error {
	return m.RemoveUnitConfigurationFileErr
}
func (m *mockHandler) CreateUnitConfigurationFile(unitName string, content []byte, ctx *log.Context) error {
	return m.CreateUnitConfigurationFileErr
}
func (m *mockHandler) GetInstalledVersion(unitName string, ctx *log.Context) (string, error) {
	return m.GetInstalledVersionRet, m.GetInstalledVersionErr
}

func injectMocks(t *testing.T, sysd bool, handler servicehandler.UnitManager, chmodOverride func(name string, mode os.FileMode) error) func() {
	setChmodOverride := chmodOverride
	if setChmodOverride == nil {
		setChmodOverride = func(name string, mode os.FileMode) error {
			return nil
		}
	}
	origIsSystemdPresent := fnIsSystemDPresent
	origNewUnitManager := fnGetUnitManager
	origChmod := fnChmod

	fnChmod = setChmodOverride
	fnIsSystemDPresent = func() bool { return sysd }
	fnGetUnitManager = func() servicehandler.UnitManager {
		return handler
	}

	return func() {
		fnIsSystemDPresent = origIsSystemdPresent
		fnGetUnitManager = origNewUnitManager
		fnChmod = origChmod
	}
}

func TestRegister_SystemdUnsupported(t *testing.T) {
	restore := injectMocks(t, false, &mockHandler{}, nil)
	defer restore()

	tempDir, _ := os.MkdirTemp("", "SystemdUnsupported")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	VerifyErrorClarification(t, constants.Immediate_Systemd_NotSupported, err)
}

func TestRegister_IsInstalledError(t *testing.T) {
	handler := &mockHandler{IsInstalledErr: errors.New("fail")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	os.Setenv(constants.ExtensionVersionEnvName, "1.0.0")

	tempDir, _ := os.MkdirTemp("", "IsInstalledError")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	VerifyErrorClarification(t, constants.Immediate_CouldNotDetermineServiceInstalled, err)
}

func TestRegister_InstalledVersionCheckError(t *testing.T) {
	handler := &mockHandler{
		IsInstalledRet:         true,
		GetInstalledVersionErr: errors.New("boom"),
	}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	os.Setenv(constants.ExtensionVersionEnvName, "2.0")
	tempDir, _ := os.MkdirTemp("", "InstalledVersionCheckError")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	VerifyErrorClarification(t, constants.Immediate_CouldNotDetermineInstalledVersion, err)
}

func TestRegister_SameVersion_NoOp(t *testing.T) {
	handler := &mockHandler{
		IsInstalledRet:         true,
		GetInstalledVersionRet: "3.1",
	}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	os.Setenv(constants.ExtensionVersionEnvName, "3.1")
	tempDir, _ := os.MkdirTemp("", "SameVersion")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	require.NoError(t, err)
}

func TestRegister_ChmodFailure(t *testing.T) {
	fnChmod = func(name string, mode os.FileMode) error {
		return errors.New("the chipmunks are using this file")
	}
	handler := &mockHandler{}
	restore := injectMocks(t, true, handler, fnChmod)
	defer restore()

	tempDir, _ := os.MkdirTemp("", "ChModFailure")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	VerifyErrorClarification(t, constants.Immediate_CouldNotMarkBinaryAsExecutable, err)
}

func TestRegister_HandlerDaemonReloadFails(t *testing.T) {
	handler := &mockHandler{DaemonReloadErr: errors.New("fail reg")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	tmpDir := t.TempDir()
	os.Setenv(constants.ExtensionPathEnvName, tmpDir)
	os.WriteFile(tmpDir+"/bin/immediate-run-command-handler", []byte("x"), 0755)
	os.Setenv(constants.ExtensionVersionEnvName, "6.0")

	tempDir, _ := os.MkdirTemp("", "HandlerRegisterFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	VerifyErrorClarification(t, constants.Immediate_ErrorReloadingDaemonWorker, err)
}

func TestRegister_HandlerRemoveUnitConfigurationFails(t *testing.T) {
	handler := &mockHandler{RemoveUnitConfigurationFileErr: errors.New("fail unit config")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	tmpDir := t.TempDir()
	os.Setenv(constants.ExtensionPathEnvName, tmpDir)
	os.WriteFile(tmpDir+"/bin/immediate-run-command-handler", []byte("x"), 0755)
	os.Setenv(constants.ExtensionVersionEnvName, "6.0")

	tempDir, _ := os.MkdirTemp("", "HandlerRegisterFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	VerifyErrorClarification(t, constants.Immediate_CouldNotRemoveOldUnitConfigFile, err)
}

func TestRegister_HandlerCreateUnitConfigurationFails(t *testing.T) {
	handler := &mockHandler{CreateUnitConfigurationFileErr: errors.New("fail unit config")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	tmpDir := t.TempDir()
	os.Setenv(constants.ExtensionPathEnvName, tmpDir)
	os.WriteFile(tmpDir+"/bin/immediate-run-command-handler", []byte("x"), 0755)
	os.Setenv(constants.ExtensionVersionEnvName, "6.0")

	tempDir, _ := os.MkdirTemp("", "HandlerRegisterFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	VerifyErrorClarification(t, constants.Immediate_ErrorCreatingUnitConfig, err)
}

func TestRegister_HandlerEnableUnitFails(t *testing.T) {
	handler := &mockHandler{EnableErr: errors.New("fail enable")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	tmpDir := t.TempDir()
	os.Setenv(constants.ExtensionPathEnvName, tmpDir)
	os.WriteFile(tmpDir+"/bin/immediate-run-command-handler", []byte("x"), 0755)
	os.Setenv(constants.ExtensionVersionEnvName, "6.0")

	tempDir, _ := os.MkdirTemp("", "HandlerRegisterFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	VerifyErrorClarification(t, constants.Immediate_ErrorEnablingUnit, err)
}

func TestRegister_StartFails(t *testing.T) {
	handler := &mockHandler{StartErr: errors.New("cannot start")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	tmp := t.TempDir()
	os.MkdirAll(tmp+"/bin", 0755)
	os.WriteFile(tmp+"/bin/immediate-run-command-handler", []byte("x"), 0755)

	os.Setenv(constants.ExtensionPathEnvName, tmp)
	os.Setenv(constants.ExtensionVersionEnvName, "7.0")

	tempDir, _ := os.MkdirTemp("", "StartFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	VerifyErrorClarification(t, constants.Immediate_CouldNotStartService, err)
}

func TestRegister_Success(t *testing.T) {
	handler := &mockHandler{}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	tmp := t.TempDir()
	os.MkdirAll(tmp+"/bin", 0755)
	os.WriteFile(tmp+"/bin/immediate-run-command-handler", []byte("run"), 0755)

	os.Setenv(constants.ExtensionPathEnvName, tmp)
	os.Setenv(constants.ExtensionVersionEnvName, "1.9")

	tempDir, _ := os.MkdirTemp("", "TestRegister")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)
	ctx := log.NewContext(log.NewNopLogger())

	err := Register(ctx, evt)
	require.NoError(t, err)
}

func TestDeRegister_Success(t *testing.T) {
	handler := &mockHandler{}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "TestDeRegister")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)

	require.NoError(t, DeRegister(ctx, evt))
}

func TestDeRegister_SystemdUnsupported(t *testing.T) {
	restore := injectMocks(t, false, &mockHandler{}, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "SystemdUnsupported")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)

	require.NoError(t, DeRegister(ctx, evt)) // No-op
}

func TestEnable_Error(t *testing.T) {
	handler := &mockHandler{EnableErr: errors.New("fail")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "EnableError")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)

	require.Error(t, Enable(ctx, evt))
}

func TestEnable_Success(t *testing.T) {
	handler := &mockHandler{}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "EnableSuccess")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)

	require.NoError(t, Enable(ctx, evt))
}

func TestDisable_Success(t *testing.T) {
	handler := &mockHandler{}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "DisableSuccess")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)

	require.NoError(t, Disable(ctx, evt))
}

func TestDisable_StopFails(t *testing.T) {
	handler := &mockHandler{StopErr: errors.New("stop err")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "StopFails")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)

	require.Error(t, Disable(ctx, evt))
}

func TestStop_Success(t *testing.T) {
	handler := &mockHandler{}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "StopSuccess")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)

	require.NoError(t, Stop(ctx, evt))
}

func TestStop_Error(t *testing.T) {
	handler := &mockHandler{StopErr: fmt.Errorf("fail stop")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())
	tempDir, _ := os.MkdirTemp("", "StopError")
	defer os.RemoveAll(tempDir)
	handlerEnvironment := handlerenv.HandlerEnvironment{
		EventsFolder: tempDir,
	}

	extensionLogger := logging.New(nil)
	evt := extensionevents.New(extensionLogger, &handlerEnvironment)

	require.Error(t, Stop(ctx, evt))
}

func TestIsActive_Success(t *testing.T) {
	handler := &mockHandler{IsActiveRet: true}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())

	ok, err := IsActive(ctx)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestIsActive_Error(t *testing.T) {
	handler := &mockHandler{IsActiveErr: errors.New("fail")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())

	_, err := IsActive(ctx)
	require.Error(t, err)
}

func TestIsInstalled_NoSystemd(t *testing.T) {
	restore := injectMocks(t, false, &mockHandler{}, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())

	ok, err := IsInstalled(ctx)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIsInstalled_Success(t *testing.T) {
	handler := &mockHandler{IsInstalledRet: true}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())

	ok, err := IsInstalled(ctx)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestIsInstalled_Error(t *testing.T) {
	handler := &mockHandler{IsInstalledErr: errors.New("fail install")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())

	_, err := IsInstalled(ctx)
	require.Error(t, err)
}

func TestIsEnabled_Success(t *testing.T) {
	handler := &mockHandler{IsEnabledRet: true}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())

	ok, err := IsEnabled(ctx)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestIsEnabled_Error(t *testing.T) {
	handler := &mockHandler{IsEnabledErr: errors.New("fail enabled")}
	restore := injectMocks(t, true, handler, nil)
	defer restore()

	ctx := log.NewContext(log.NewNopLogger())

	_, err := IsEnabled(ctx)
	require.Error(t, err)
}

func VerifyErrorClarification(t *testing.T, expectedCode int, err error) {
	require.NotNil(t, err, "No error returned when one was expected")
	var ewc vmextension.ErrorWithClarification
	require.True(t, errors.As(err, &ewc), "Error is not of type ErrorWithClarification")
	require.Equal(t, expectedCode, ewc.ErrorCode, "Expected error %d but received %d", expectedCode, ewc.ErrorCode)
}
