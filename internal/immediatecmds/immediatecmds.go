package immediatecmds

import (
	"os"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/service"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

func Update(ctx *log.Context, h types.HandlerEnvironment, extName string, seqNum int) (int, error) {
	updatingFromVersion := os.Getenv("AZURE_GUEST_AGENT_UPDATING_FROM_VERSION")
	ctx.Log("message", "updating immediate run command from version "+updatingFromVersion)
	// parse the extension handler settings
	cfg, err := handlersettings.GetHandlerSettings(h.HandlerEnvironment.ConfigFolder, extName, seqNum, ctx)
	if err != nil {
		return constants.ExitCode_GetHandlerSettingsFailed, errors.Wrap(err, "failed to get configuration")
	}

	// If installAsService == true, then upgrade the service if already been installed before
	if cfg.InstallAsService() {
		isInstalled, err := service.IsInstalled(ctx)
		if err != nil {
			return constants.ExitCode_CreateDataDirectoryFailed, errors.Wrap(err, "failed to check if runcommand service is installed")
		}

		if isInstalled {
			err = service.Register(ctx)
			if err != nil {
				return constants.ExitCode_UpgradeInstalledServiceFailed, errors.Wrap(err, "failed to upgrade run command service")
			}
		}
	}

	return constants.ExitCode_Okay, nil
}

func Disable(ctx *log.Context, h types.HandlerEnvironment, extName string, seqNum int) (int, error) {
	// parse the extension handler settings
	cfg, err := handlersettings.GetHandlerSettings(h.HandlerEnvironment.ConfigFolder, extName, seqNum, ctx)
	if err != nil {
		return constants.ExitCode_GetHandlerSettingsFailed, errors.Wrap(err, "failed to get configuration")
	}

	// If installAsService == true, then disable the service (if any)
	if cfg.InstallAsService() {
		isInstalled, err := service.IsInstalled(ctx)
		if err != nil {
			return constants.ExitCode_DisableInstalledServiceFailed, errors.Wrap(err, "failed to check if runcommand service is installed")
		}

		if isInstalled {
			isEnabled, err := service.IsEnabled(ctx)
			if err != nil {
				return constants.ExitCode_InstallServiceFailed, errors.Wrap(err, "failed to check if service is enabled")
			}

			if isEnabled {
				err := service.Disable(ctx)
				if err != nil {
					return constants.ExitCode_DisableInstalledServiceFailed, errors.Wrap(err, "failed to disable run command service")
				}
			} else {
				ctx.Log("message", "Service installed but already got disabled. Skipping request to disable")
			}
		}
	}

	return constants.ExitCode_Okay, nil
}

func Install() (int, error) {
	// Not required to perform any action at this moment.
	return constants.ExitCode_Okay, nil
}

func Uninstall(ctx *log.Context, h types.HandlerEnvironment, extName string, seqNum int) (int, error) {
	ctx.Log("message", "proceeding to uninstall immediate run command")
	// parse the extension handler settings
	cfg, err := handlersettings.GetHandlerSettings(h.HandlerEnvironment.ConfigFolder, extName, seqNum, ctx)
	if err != nil {
		return constants.ExitCode_GetHandlerSettingsFailed, errors.Wrap(err, "failed to get configuration")
	}

	// If installAsService == true, then uninstall the service
	if cfg.InstallAsService() {
		isInstalled, err := service.IsInstalled(ctx)
		if err != nil {
			return constants.ExitCode_RemoveDataDirectoryFailed, errors.Wrap(err, "failed to check if runcommand service is installed")
		}

		if isInstalled {
			error := service.DeRegister(ctx)
			if error != nil {
				return constants.ExitCode_UninstallInstalledServiceFailed, errors.Wrap(err, "failed to uninstall run command service")
			}
		}
	}

	return constants.ExitCode_Okay, nil
}

func Enable(ctx *log.Context, h types.HandlerEnvironment, extName string, seqNum int, cfg handlersettings.HandlerSettings) (int, error) {
	// If installService == true, then install RunCommand as a service
	if cfg.InstallAsService() {
		isInstalled, err2 := service.IsInstalled(ctx)
		if err2 != nil {
			ctx.Log("message", "could not check if service is already installed. Proceeding to overwrite configuration file to make sure it gets installed.")
		}

		if !isInstalled {
			err3 := service.Register(ctx)
			if err3 != nil {
				return constants.ExitCode_InstallServiceFailed, errors.Wrap(err3, "failed to install RunCommand as a service")
			}
		} else {
			isEnabled, err3 := service.IsEnabled(ctx)
			if err3 != nil {
				return constants.ExitCode_InstallServiceFailed, errors.Wrap(err3, "failed to check if service is already enabled")
			}

			if !isEnabled {
				err4 := service.Enable(ctx)

				if err4 != nil {
					return constants.ExitCode_InstallServiceFailed, errors.Wrap(err4, "failed to enable service")
				}

				err5 := service.Start(ctx)

				if err5 != nil {
					return constants.ExitCode_InstallServiceFailed, errors.Wrap(err5, "failed to start service")
				}
			}
		}
	}

	return constants.ExitCode_Okay, nil
}
