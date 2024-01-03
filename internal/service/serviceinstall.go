package service

import (
	"fmt"
	"strings"

	"github.com/Azure/run-command-handler-linux/pkg/servicehandler"
	"github.com/Azure/run-command-handler-linux/pkg/systemd"
	"github.com/go-kit/kit/log"
)

const (
	systemdUnitName                          = "managedruncommand.service"
	systemdUnitConfigurationPath             = "misc/managedruncommand.service"
	runcommand_working_directory_placeholder = "%run_command_working_directory%"
	systemdUnitConfigurationTemplate         = `
[Unit]
Description=Managed RunCommand Service

[Service] 
User=root
Restart=always
RestartSec=5
WorkingDirectory=%run_command_working_directory%
ExecStart=%run_command_working_directory%/bin/run-command-handler runService

[Install]
WantedBy=multi-user.target`
)

func Register(ctx *log.Context, workingDirectory string) error {
	if isSystemdSupported(ctx) {
		ctx.Log("message", "Generating service configuration files")
		systemdUnitContent := generateServiceConfigurationContent(ctx, workingDirectory)

		ctx.Log("message", "Registering service")
		serviceHandler := getSystemdHandler(ctx)
		err := serviceHandler.Register(ctx, systemdUnitContent)
		if err != nil {
			return err
		}

		err = Start(ctx)
		if err != nil {
			return err
		}

		ctx.Log("message", "Service registration complete")
	}

	return nil
}

func DeRegister(ctx *log.Context) error {
	if isSystemdSupported(ctx) {
		ctx.Log("message", "Deregistering service")
		serviceHandler := getSystemdHandler(ctx)
		err := serviceHandler.DeRegister(ctx)
		if err != nil {
			return err
		}

		ctx.Log("message", "Service deregistration complete")
	}

	return nil
}

func Start(ctx *log.Context) error {
	if isSystemdSupported(ctx) {
		ctx.Log("message", "Starting service")
		serviceHandler := getSystemdHandler(ctx)
		err := serviceHandler.Start()
		if err != nil {
			return err
		}

		ctx.Log("message", "Service started")
	}

	return nil
}

func Stop(ctx *log.Context) error {
	if isSystemdSupported(ctx) {
		ctx.Log("message", "Stopping service")
		serviceHandler := getSystemdHandler(ctx)
		err := serviceHandler.Stop()
		if err != nil {
			return err
		}
		ctx.Log("message", "Service stopped")
	}

	return nil
}

func IsActive(ctx *log.Context) (bool, error) {
	if isSystemdSupported(ctx) {
		serviceHandler := getSystemdHandler(ctx)
		isActive, err := serviceHandler.IsActive()
		if err != nil {
			return false, err
		}

		ctx.Log("message", fmt.Sprintf("Service is active : %v", isActive))
		return isActive, nil
	}

	return false, nil
}

func IsInstalled(ctx *log.Context) (bool, error) {
	if isSystemdSupported(ctx) {
		ctx.Log("message", "Checking if service is installed")
		serviceHandler := getSystemdHandler(ctx)
		isInstalled, err := serviceHandler.IsInstalled()

		ctx.Log("message", fmt.Sprintf("Service is installed: %v", isInstalled))
		return isInstalled, err
	}

	return false, nil
}

func getSystemdHandler(ctx *log.Context) *servicehandler.Handler {
	ctx.Log("message", "Getting service handler for "+systemdUnitName)
	config := servicehandler.NewConfiguration(systemdUnitName)
	handler := servicehandler.NewHandler(systemd.NewUnitManager(), config, ctx)
	return &handler
}

func generateServiceConfigurationContent(ctx *log.Context, workingDirectory string) string {
	systemdConfigContent := strings.Replace(systemdUnitConfigurationTemplate, runcommand_working_directory_placeholder, workingDirectory, -1)
	ctx.Log("message", "Using working directory: "+workingDirectory)
	return systemdConfigContent
}

func isSystemdSupported(ctx *log.Context) bool {
	ctx.Log("message", "Check if systemd is present on the system before applying next operation")
	result := systemd.IsSystemDPresent()

	if result {
		ctx.Log("message", "systemd was found on the system")
	} else {
		ctx.Log("message", "systemd was not found on the system")
	}

	return result
}
