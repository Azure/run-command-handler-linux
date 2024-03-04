package servicehandler

import (
	"fmt"
	"os"
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/systemd"
	"github.com/go-kit/kit/log"
)

const (
	testUnitName                         = "testunit.service"
	systemdUnitDirectorypath             = "/etc/systemd/system"           // system units created by the administrator path
	systemdUnitDirectorypath_alternative = "/usr/local/lib/systemd/system" // system units installed by the administrator path
)

var systemdUnitPath = fmt.Sprintf("%v/%v", systemdUnitDirectorypath, testUnitName)
var systemdUnitPath_alternative = fmt.Sprintf("%v/%v", systemdUnitDirectorypath_alternative, testUnitName)

// Struct that represents the function invoked during the tests
type functionCalled struct {
	startunit_f            bool
	stopunit_f             bool
	enableunit_f           bool
	disableunit_f          bool
	daemondreload_f        bool
	isunitactive_f         bool
	isunitenabled_f        bool
	isunitinstalled_f      bool
	removeunitconfigfile_f bool
	createunitconfigfile_f bool
}

// Struct with all the methods needed to mock the UnitManager interface.
type ManagerMock struct {
	start_f                   func(unitName string, ctx *log.Context) error
	stop_f                    func(unitName string, ctx *log.Context) error
	enable_f                  func(unitName string, ctx *log.Context) error
	disable_f                 func(unitName string, ctx *log.Context) error
	daemondreload_f           func(unitName string, ctx *log.Context) error
	isactive_f                func(unitName string, ctx *log.Context) error
	isenabled_f               func(unitName string, ctx *log.Context) (bool, error)
	isinstalled_f             func(unitName string, ctx *log.Context) (bool, error)
	removeUnitConfiguration_f func(unitName string, ctx *log.Context) error
	createUnitConfiguration_f func(unitName string, content []byte, ctx *log.Context) error
	functionCalled            functionCalled
}

func (s *ManagerMock) StartUnit(unitName string, ctx *log.Context) error {
	s.functionCalled.startunit_f = true
	return s.start_f(unitName, ctx)
}

func (s *ManagerMock) StopUnit(unitName string, ctx *log.Context) error {
	s.functionCalled.stopunit_f = true
	return s.stop_f(unitName, ctx)
}

func (s *ManagerMock) EnableUnit(unitName string, ctx *log.Context) error {
	s.functionCalled.enableunit_f = true
	return s.enable_f(unitName, ctx)
}

func (s *ManagerMock) DisableUnit(unitName string, ctx *log.Context) error {
	s.functionCalled.disableunit_f = true
	return s.disable_f(unitName, ctx)
}

func (s *ManagerMock) DaemonReload(unitName string, ctx *log.Context) error {
	s.functionCalled.daemondreload_f = true
	return s.daemondreload_f(unitName, ctx)
}

func (s *ManagerMock) IsUnitActive(unitName string, ctx *log.Context) error {
	s.functionCalled.isunitactive_f = true
	return s.isactive_f(unitName, ctx)
}

func (s *ManagerMock) IsUnitEnabled(unitName string, ctx *log.Context) (bool, error) {
	s.functionCalled.isunitenabled_f = true
	return s.isenabled_f(unitName, ctx)
}

func (s *ManagerMock) IsUnitInstalled(unitName string, ctx *log.Context) (bool, error) {
	s.functionCalled.isunitinstalled_f = true
	return s.isinstalled_f(unitName, ctx)
}

func (s *ManagerMock) RemoveUnitConfigurationFile(unitName string, ctx *log.Context) error {
	s.functionCalled.removeunitconfigfile_f = true
	return s.removeUnitConfiguration_f(unitName, ctx)
}

func (s *ManagerMock) CreateUnitConfigurationFile(unitName string, content []byte, ctx *log.Context) error {
	s.functionCalled.createunitconfigfile_f = true
	return s.createUnitConfiguration_f(unitName, content, ctx)
}

func getManagerMock() *ManagerMock {
	return &ManagerMock{
		start_f: func(unitName string, ctx *log.Context) error {
			return nil
		},
		stop_f: func(unitName string, ctx *log.Context) error {
			return nil
		},
		enable_f: func(unitName string, ctx *log.Context) error {
			return nil
		},
		disable_f: func(unitName string, ctx *log.Context) error {
			return nil
		},
		daemondreload_f: func(unitName string, ctx *log.Context) error {
			return nil
		},
		isactive_f: func(unitName string, ctx *log.Context) error {
			return nil
		},
		isenabled_f: func(unitName string, ctx *log.Context) (bool, error) {
			return true, nil
		},
		isinstalled_f: func(unitName string, ctx *log.Context) (bool, error) {
			return true, nil
		},
		removeUnitConfiguration_f: func(unitName string, ctx *log.Context) error {
			return nil
		},
		createUnitConfiguration_f: func(unitName string, content []byte, ctx *log.Context) error {
			return nil
		},
		functionCalled: functionCalled{
			daemondreload_f: false,
			disableunit_f:   false,
			enableunit_f:    false,
			startunit_f:     false,
			stopunit_f:      false,
			isunitactive_f:  false,
			isunitenabled_f: false},
	}
}

// Test that the Register handler from servicehandler.go perform the required actions.
func TestHandlerSuccessfulRegister(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)
	handler.Register(ctx, "")

	if !m.functionCalled.removeunitconfigfile_f {
		t.Errorf("missing call to method to remove unit configuration file")
	}

	if !m.functionCalled.createunitconfigfile_f {
		t.Errorf("missing call to create updated unit configuration file")
	}

	if !m.functionCalled.daemondreload_f {
		t.Errorf("missing systemctl daemon reload command")
	}

	if !m.functionCalled.enableunit_f {
		t.Errorf("missing systemctl command to enable unit")
	}

	if m.functionCalled.startunit_f {
		t.Errorf("unexpected systemctl command")
	}
}

func TestHandlerSuccessfulRegisterOnRemoveUnitConfigurationFileNotExistsError(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	m.removeUnitConfiguration_f = func(unitName string, ctx *log.Context) error {
		_, err := os.Stat("nonexistent_file.txt")
		if !os.IsNotExist(err) {
			t.Errorf("expected not exist error, got %v", err)
		}
		return err
	}

	// assert that the register call returns an error
	err := handler.Register(ctx, "")

	if err != nil {
		t.Errorf("unexpected failure registration call")
	}
}

func TestHandlerRegisterFailsOnUnitConfigurationFileDeletion(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	m.removeUnitConfiguration_f = func(unitName string, ctx *log.Context) error {
		return fmt.Errorf("Failed to remove unit configuration")
	}

	// assert that the register call returns an error
	err := handler.Register(ctx, "")

	if err == nil {
		t.Errorf("unexpected successful registration call")
	}
}

func TestHandlerRegisterFailsOnUnitConfigurationFileCreation(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	m.createUnitConfiguration_f = func(unitName string, content []byte, ctx *log.Context) error {
		return fmt.Errorf("Failed to create unit configuration")
	}

	// assert that the register call returns an error
	err := handler.Register(ctx, "")

	if err == nil {
		t.Errorf("unexpected successful registration call")
	}
}

func TestHandlerRegisterFailsOnDaemonReload(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// mock failing daemon call
	m.daemondreload_f = func(unitName string, ctx *log.Context) error {
		return fmt.Errorf("Failed to reload daemon")
	}

	// assert that the register call returns an error
	err := handler.Register(ctx, "")
	if err == nil {
		t.Errorf("unexpected successful registration call")
	}
}

func TestHandlerRegisterFailsOnEnable(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// mock failing enable call
	m.enable_f = func(unitName string, ctx *log.Context) error {
		return fmt.Errorf("Failed to enable unit")
	}

	// assert that the register call returns an error
	err := handler.Register(ctx, "")
	if err == nil {
		t.Errorf("unexpected successful registration call")
	}
}

func TestHandlerSuccessfulDeRegister(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)
	handler.DeRegister(ctx)

	if !m.functionCalled.stopunit_f {
		t.Errorf("missing call to stop unit")
	}

	if !m.functionCalled.disableunit_f {
		t.Errorf("missing call to disable unit")
	}

	if !m.functionCalled.removeunitconfigfile_f {
		t.Errorf("missing call to remove unit configuration file from system")
	}

	if m.functionCalled.enableunit_f || m.functionCalled.startunit_f || m.functionCalled.createunitconfigfile_f {
		t.Errorf("unexpected systemctl command")
	}
}

func TestHandlerSuccessfulStart(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// assert that the start call does not return an error
	err := handler.Start()
	if err != nil {
		t.Errorf("unexpected failure when trying to start the unit")
	}

	if !m.functionCalled.startunit_f {
		t.Errorf("missing systemctl start unit command")
	}
}

func TestHandlerStartFailsOnStartUnit(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// mock failing start unit call
	m.start_f = func(unitName string, ctx *log.Context) error {
		return fmt.Errorf("Failed to start unit")
	}

	// assert that the start call returns an error
	err := handler.Start()
	if err == nil {
		t.Errorf("unexpected successful start unit command")
	}
}

func TestHandlerSuccessfulStop(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// assert that the stop call does not return an error
	err := handler.Stop()
	if err != nil {
		t.Errorf("unexpected failure when trying to stop the unit")
	}

	if !m.functionCalled.stopunit_f {
		t.Errorf("missing systemctl start unit command")
	}
}

func TestHandlerStopFailsOnStopUnit(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// mock failing stop unit call
	m.stop_f = func(unitName string, ctx *log.Context) error {
		return fmt.Errorf("Failed to stop unit")
	}

	// assert that the stop call returns an error
	err := handler.Stop()
	if err == nil {
		t.Errorf("unexpected successful stop unit command")
	}
}

func TestHandlerSuccessfulEnable(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// assert that the enable call does not return an error
	err := handler.Enable()
	if err != nil {
		t.Errorf("unexpected failure when trying to enable the unit")
	}

	if !m.functionCalled.enableunit_f {
		t.Errorf("missing systemctl enable unit command")
	}
}

func TestHandlerEnableFailsOnEnableUnit(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// mock failing enable unit call
	m.enable_f = func(unitName string, ctx *log.Context) error {
		return fmt.Errorf("Failed to enable unit")
	}

	// assert that the enable call returns an error
	err := handler.Enable()
	if err == nil {
		t.Errorf("unexpected successful enable unit command")
	}
}

func TestHandlerSuccessfulDisable(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// assert that the disable call does not return an error
	err := handler.Disable()
	if err != nil {
		t.Errorf("unexpected failure when trying to disable the unit")
	}

	if !m.functionCalled.disableunit_f {
		t.Errorf("missing systemctl disable unit command")
	}
}

func TestHandlerDisableFailsOnDisableUnit(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// mock failing disable unit call
	m.disable_f = func(unitName string, ctx *log.Context) error {
		return fmt.Errorf("Failed to disable unit")
	}

	// assert that the disable call returns an error
	err := handler.Disable()
	if err == nil {
		t.Errorf("unexpected successful disable unit command")
	}
}

func TestHandlerSuccessfulDaemonReload(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// assert that the daemon reload call does not return an error
	err := handler.DaemonReload()
	if err != nil {
		t.Errorf("unexpected failure when trying to reload the daemon")
	}

	if !m.functionCalled.daemondreload_f {
		t.Errorf("missing systemctl daemon reload command")
	}
}

func TestHandlerDaemonReloadFailsOnDaemonReload(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)

	// mock failing daemon reload call
	m.daemondreload_f = func(unitName string, ctx *log.Context) error {
		return fmt.Errorf("Failed to reload daemon")
	}

	// assert that the daemon reload call returns an error
	err := handler.DaemonReload()
	if err == nil {
		t.Errorf("unexpected successful daemon reload command")
	}
}

func TestHandlerIsActiveTrue(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)
	m.isactive_f = func(unitName string, ctx *log.Context) error {
		return nil
	}

	isActive, err := handler.IsActive()

	if !m.functionCalled.isunitactive_f {
		t.Errorf("missing IsUnitActive method call")
	}

	if err != nil {
		t.Errorf("unexpected error from IsUnitActive call")
	}

	if isActive == false {
		t.Errorf("unexpected isactive false")
	}
}

func TestHandlerIsActiveFalse(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)
	m.isactive_f = func(unitName string, ctx *log.Context) error {
		return fmt.Errorf("Failed to check if unit is active")
	}

	isActive, _ := handler.IsActive()
	if !m.functionCalled.isunitactive_f {
		t.Errorf("missing IsUnitActive method call")
	}

	if isActive == true {
		t.Errorf("unexpected isactive true")
	}
}

func TestHandlerIsEnabledTrue(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)
	isEnabled, err := handler.IsEnabled()

	if !m.functionCalled.isunitenabled_f {
		t.Errorf("missing IsUnitEnabled method call")
	}

	if err != nil {
		t.Errorf("unexpected error from IsUnitEnabled call")
	}

	if isEnabled == false {
		t.Errorf("unexpected isEnabled false")
	}
}

func TestHandlerIsEnabledFalse(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)
	m.isenabled_f = func(unitName string, ctx *log.Context) (bool, error) {
		return false, nil
	}

	isEnabled, err := handler.IsEnabled()
	if !m.functionCalled.isunitenabled_f {
		t.Errorf("missing IsUnitEnabled method call")
	}

	if err != nil {
		t.Errorf("unexpected error from IsUnitEnabled call")
	}

	if isEnabled == true {
		t.Errorf("unexpected isEnabled true")
	}
}

func TestHandlerIsInstalledTrue(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)
	isInstalled, err := handler.IsInstalled()

	if !m.functionCalled.isunitinstalled_f {
		t.Errorf("missing IsUnitInstalled method call")
	}

	if err != nil {
		t.Errorf("unexpected error from IsUnitInstalled call")
	}

	if isInstalled == false {
		t.Errorf("unexpected isInstalled false")
	}
}

func TestHandlerIsInstalledFalse(t *testing.T) {
	config := NewConfiguration(testUnitName)

	m := getManagerMock()
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)

	handler := NewHandler(m, config, ctx)
	m.isinstalled_f = func(unitName string, ctx *log.Context) (bool, error) {
		return false, nil
	}

	isInstalled, err := handler.IsInstalled()
	if !m.functionCalled.isunitinstalled_f {
		t.Errorf("missing IsUnitInstalled method call")
	}

	if err != nil {
		t.Errorf("unexpected error from IsUnitInstalled call")
	}

	if isInstalled == true {
		t.Errorf("unexpected isInstalled true")
	}
}

func TestGetUnitConfigurationPathSystemD(t *testing.T) {
	if !systemd.IsSystemDPresent() {
		t.Skip("test only valid for systemD")
	}

	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp)
	path, err := systemd.GetUnitConfigurationFilePath(testUnitName, ctx)

	if err != nil {
		t.Error(err)
	}

	if path != systemdUnitPath && path != systemdUnitPath_alternative {
		t.Errorf("unexpected unit configuration path\nreturned path was %s", path)
	}
}
