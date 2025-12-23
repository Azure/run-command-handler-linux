package servicehandler

import (
	"fmt"
	"os"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

type UnitManager interface {
	StartUnit(unitName string, ctx *log.Context) error
	StopUnit(unitName string, ctx *log.Context) error
	EnableUnit(unitName string, ctx *log.Context) error
	DisableUnit(unitName string, ctx *log.Context) error
	DaemonReload(unitName string, ctx *log.Context) error
	IsUnitActive(unitName string, ctx *log.Context) error
	IsUnitEnabled(unitName string, ctx *log.Context) (bool, error)
	IsUnitInstalled(unitName string, ctx *log.Context) (bool, error)
	RemoveUnitConfigurationFile(unitName string, ctx *log.Context) error
	CreateUnitConfigurationFile(unitName string, content []byte, ctx *log.Context) error
	GetInstalledVersion(unitName string, ctx *log.Context) (string, error)
}

type Configuration struct {
	Name string
}

type Handler struct {
	config  Configuration
	manager UnitManager
	ctx     *log.Context
}

func NewHandler(manager UnitManager, configuration Configuration, ctx *log.Context) Handler {
	return Handler{config: configuration, manager: manager, ctx: ctx}
}

func NewConfiguration(unitName string) Configuration {
	return Configuration{Name: unitName}
}

func (handler *Handler) Start() error {
	err := handler.manager.StartUnit(handler.config.Name, handler.ctx)
	return err
}

func (handler *Handler) Stop() error {
	err := handler.manager.StopUnit(handler.config.Name, handler.ctx)
	return err
}

func (handler *Handler) Enable() error {
	err := handler.manager.EnableUnit(handler.config.Name, handler.ctx)
	return err
}

func (handler *Handler) Disable() error {
	err := handler.manager.DisableUnit(handler.config.Name, handler.ctx)
	return err
}

func (handler *Handler) DaemonReload() error {
	err := handler.manager.DaemonReload(handler.config.Name, handler.ctx)
	return err
}

func (handler *Handler) IsActive() (bool, error) {
	err := handler.manager.IsUnitActive(handler.config.Name, handler.ctx)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (handler *Handler) IsEnabled() (bool, error) {
	return handler.manager.IsUnitEnabled(handler.config.Name, handler.ctx)
}

func (handler *Handler) IsInstalled() (bool, error) {
	return handler.manager.IsUnitInstalled(handler.config.Name, handler.ctx)
}

func (handler *Handler) Register(ctx *log.Context, unitConfigContent string) error {
	err := handler.manager.RemoveUnitConfigurationFile(handler.config.Name, ctx)
	if err != nil && !os.IsNotExist(err) {
		return vmextension.NewErrorWithClarification(constants.Immediate_CouldNotRemoveOldUnitConfigFile, fmt.Errorf("error while removing old unit configuration file: %v", err))
	}

	err = handler.manager.CreateUnitConfigurationFile(handler.config.Name, []byte(unitConfigContent), ctx)
	if err != nil {
		return vmextension.NewErrorWithClarification(constants.Immediate_ErrorCreatingUnitConfig, fmt.Errorf("error while creating unit configuration file: %v", err))
	}

	err = handler.DaemonReload()
	if err != nil {
		return vmextension.NewErrorWithClarification(constants.Immediate_ErrorReloadingDaemonWorker, fmt.Errorf("error while reloading daemon worker: %v", err))
	}

	err = handler.Enable()
	if err != nil {
		return vmextension.NewErrorWithClarification(constants.Immediate_ErrorEnablingUnit, fmt.Errorf("error while enabling unit: %v", err))
	}

	return nil
}

func (handler *Handler) DeRegister(ctx *log.Context) error {
	// We need to make sure the version that the VM Agent is trying to uninstall is the correct one.
	// Failing to check this can cause to uninstall the service during the update workflow.
	targetVersion := os.Getenv(constants.ExtensionVersionEnvName)
	ctx.Log("message", "trying to uninstall extension with version: "+targetVersion)

	installedVersion, err := handler.GetInstalledVersion(ctx)
	if err != nil {
		return errors.Wrap(err, "error while checking the installed version of the service")
	}

	if targetVersion == installedVersion {
		err = handler.Stop()
		if err != nil {
			return fmt.Errorf("error while stopping unit: %v", err)
		}

		err = handler.Disable()
		if err != nil {
			return fmt.Errorf("error while disabling unit: %v", err)
		}

		err = handler.manager.RemoveUnitConfigurationFile(handler.config.Name, ctx)
		if err != nil {
			return fmt.Errorf("error while removing unit configuration: %v", err)
		}
	} else {
		ctx.Log("message", "Skipping action. Target version is not current installed")
	}

	return nil
}

func (handler *Handler) GetInstalledVersion(ctx *log.Context) (string, error) {
	return handler.manager.GetInstalledVersion(handler.config.Name, ctx)
}
