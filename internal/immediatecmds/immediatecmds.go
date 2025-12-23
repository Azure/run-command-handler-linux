package immediatecmds

import (
	"fmt"

	"github.com/Azure/azure-extension-platform/pkg/extensionevents"
	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/service"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

// Updates the service definition if any immediate run command service exists.
// The action is skipped if the service has already been upgraded.
func Update(ctx *log.Context, h types.HandlerEnvironment, extName string, seqNum int, extensionEvents *extensionevents.ExtensionEventManager) (int, error) {
	ctx.Log("message", "updating immediate run command")
	isInstalled, err := service.IsInstalled(ctx)
	if err != nil {
		errMessage := fmt.Sprintf("Failed to check if any runcommand service is installed: %v", err)
		extensionEvents.LogErrorEvent("immediateupdate", errMessage)
		return constants.FileSystem_CreateDataDirectoryFailed, errors.Wrap(err, "failed to check if any runcommand service is installed")
	}

	if isInstalled {
		err = service.Register(ctx, extensionEvents)
		if err != nil {
			errMessage := fmt.Sprintf("Failed to upgrade run command service: %v", err)
			extensionEvents.LogErrorEvent("immediateupdate", errMessage)
			return constants.ExitCode_UpgradeInstalledServiceFailed, errors.Wrap(err, "failed to upgrade run command service")
		}
	}

	return constants.ExitCode_Okay, nil
}

func Disable(ctx *log.Context, h types.HandlerEnvironment, extName string, seqNum int, extensionEvents *extensionevents.ExtensionEventManager) (int, error) {
	isInstalled, err := service.IsInstalled(ctx)
	if err != nil {
		errMessage := fmt.Sprintf("Failed to check if runcommand service is installed: %v", err)
		extensionEvents.LogErrorEvent("immediatedisable", errMessage)
		return constants.ExitCode_DisableInstalledServiceFailed, errors.Wrap(err, "failed to check if runcommand service is installed")
	}

	if isInstalled {
		isEnabled, err := service.IsEnabled(ctx)
		if err != nil {
			errMessage := fmt.Sprintf("Failed to check if service is enabled: %v", err)
			extensionEvents.LogErrorEvent("immediatedisable", errMessage)
			return constants.ExitCode_InstallServiceFailed, errors.Wrap(err, "failed to check if service is enabled")
		}

		if isEnabled {
			err := service.Disable(ctx, extensionEvents)
			if err != nil {
				errMessage := fmt.Sprintf("Failed to disable run command service: %v", err)
				extensionEvents.LogErrorEvent("immediatedisable", errMessage)
				return constants.ExitCode_DisableInstalledServiceFailed, errors.Wrap(err, "failed to disable run command service")
			}
		} else {
			ctx.Log("message", "Service installed but already got disabled. Skipping request to disable")
			extensionEvents.LogInformationalEvent("immediatedisable", "Service installed but already got disabled. Skipping request to disable")
		}
	}

	return constants.ExitCode_Okay, nil
}

func Install() (int, error) {
	// Not required to perform any action at this moment.
	return constants.ExitCode_Okay, nil
}

func Uninstall(ctx *log.Context, h types.HandlerEnvironment, extName string, seqNum int, extensionEvents *extensionevents.ExtensionEventManager) (int, error) {
	ctx.Log("message", "proceeding to uninstall immediate run command")
	isInstalled, err := service.IsInstalled(ctx)
	if err != nil {
		errMessage := fmt.Sprintf("Failed to check if runcommand service is installed: %v", err)
		extensionEvents.LogErrorEvent("immediatedisable", errMessage)
		return constants.FileSystem_RemoveDataDirectoryFailed, errors.Wrap(err, "failed to check if runcommand service is installed")
	}

	if isInstalled {
		error := service.DeRegister(ctx, extensionEvents)
		if error != nil {
			errMessage := fmt.Sprintf("Failed to uninstall run command service: %v", error)
			extensionEvents.LogErrorEvent("immediatedisable", errMessage)
			return constants.ExitCode_UninstallInstalledServiceFailed, errors.Wrap(err, "failed to uninstall run command service")
		}
	}
	return constants.ExitCode_Okay, nil
}

func Enable(ctx *log.Context, h types.HandlerEnvironment, extName string, seqNum int, cfg handlersettings.HandlerSettings, extensionEvents *extensionevents.ExtensionEventManager) (int, error) {
	// If installService == true, then install RunCommand as a service
	if cfg.InstallAsService() {
		isInstalled, err2 := service.IsInstalled(ctx)
		if err2 != nil {
			ctx.Log("message", "could not check if service is already installed. Proceeding to overwrite configuration file to make sure it gets installed.")
			extensionEvents.LogErrorEvent("immediateenable", "could not check if service is already installed. Proceeding to overwrite configuration file to make sure it gets installed.")
		}

		if !isInstalled {
			err3 := service.Register(ctx, extensionEvents)
			if err3 != nil {
				errMessage := fmt.Sprintf("Failed to install RunCommand as a service: %v", err3)
				extensionEvents.LogErrorEvent("immediateenable", errMessage)
				return constants.Immediate_CouldNotStartService, err3
			}
		} else {
			isEnabled, err3 := service.IsEnabled(ctx)
			if err3 != nil {
				errMessage := fmt.Sprintf("Failed to check if service is already enabled: %v", err3)
				extensionEvents.LogErrorEvent("immediateenable", errMessage)
				return constants.Immediate_CouldNotCheckServiceAlreadyEnabled, vmextension.NewErrorWithClarification(constants.Immediate_CouldNotCheckServiceAlreadyEnabled, errors.Wrap(err3, errMessage))
			}

			if !isEnabled {
				err4 := service.Enable(ctx, extensionEvents)

				if err4 != nil {
					errMessage := fmt.Sprintf("Failed to enable service: %v", err4)
					extensionEvents.LogErrorEvent("immediateenable", errMessage)
					return constants.Immediate_EnableServiceFailed, vmextension.NewErrorWithClarification(constants.Immediate_EnableServiceFailed, errors.Wrap(err4, errMessage))
				}

				err5 := service.Start(ctx, extensionEvents)

				if err5 != nil {
					errMessage := fmt.Sprintf("Failed to start service: %v", err5)
					extensionEvents.LogErrorEvent("immediateenable", errMessage)
					return constants.Immediate_CouldNotStartService, vmextension.NewErrorWithClarification(constants.Immediate_CouldNotStartService, errors.Wrap(err5, errMessage))
				}
			}
		}
	}

	return constants.ExitCode_Okay, nil
}
