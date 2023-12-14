package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	// Contains the details of the service to be installed.
	// Please use this constant as follows as it depends on the RunCommand's version for some fields:
	// 	s := fmt.Sprintf(serviceDetails, runCommandVersion)
	serviceDetails = `
	[Unit]
	Description=Managed RunCommand Service

	[Service] 
	User=root
	Restart=always
	RestartSec=5
	WorkingDirectory=/var/lib/waagent/Microsoft.CPlat.Core.RunCommandHandlerLinux-%[1]s
	ExecStart=/var/lib/waagent/Microsoft.CPlat.Core.RunCommandHandlerLinux-%[1]s/bin/run-command-handler runService

	[Install]
	WantedBy=multi-user.target`

	// The name of the systemd configuration file
	systemdServiceName = "managed-run-command.service"

	// The full path of the systemd configuration for the RunCommand service
	systemServiceFilePath = "/lib/systemd/system/" + systemdServiceName
)

// Installs RunCommand as a service on the client
func InstallRunCommandService(ctx *log.Context) (bool, error) {
	ctx.Log("event", "Trying to install run command as a service")
	_, err := createOrUpdateRunCommandService(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to install service")
	}

	// Important to enable the service to start automatically at system boot as current
	// configuration does not allow that action
	_, err = enableService(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to enable service after install")
	}

	return startService(ctx)
}

// Upgrades the RunCommand service with the latest available one (if any service exists)
func UpgradeRunCommandService(ctx *log.Context) (bool, error) {
	ctx.Log("event", "Trying to upgrade run command service")
	_, err := createOrUpdateRunCommandService(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to upgrade service")
	}

	ctx.Log("event", "Trying to reload service configuration after upgrade")
	_, err = exec.Command("systemctl", "start daemon-reload").Output()
	if err != nil {
		return false, errors.Wrap(err, "failed to reload service configuration")
	}

	ctx.Log("event", "Upgrade succeeded")
	return true, nil
}

// Stops and removes the installed service from the VM.
func UninstallRunCommandService(ctx *log.Context) (bool, error) {
	ctx.Log("event", "Trying to uninstall run command service")
	_, err := stopService(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed to remove service")
	}

	ctx.Log("event", "Deleting systemd configuration file")
	err = os.Remove(systemServiceFilePath)
	if err != nil {
		return false, errors.Wrap(err, "failed to delete systemd configuration")
	}

	ctx.Log("event", "Run command service has been uninstalled")
	return true, nil
}

// Checks if the service is installed by checking for the presence of the systemd configuration file
func RunCommandServiceIsInstalled(ctx *log.Context) (bool, error) {
	ctx.Log("event", "Checking if runcommand has previously been installed")
	_, err := os.Stat(systemServiceFilePath)

	if errors.Is(err, os.ErrNotExist) {
		ctx.Log("event", "Service does not exists")
		return false, nil
	}

	if err != nil {
		return false, errors.Wrap(err, "failed to check if systemd configuration file exists")
	}

	ctx.Log("event", "Systemd file exists. Service has been installed before")
	return true, nil
}

// Updates the version of RunCommand to execute.
// It will update the 'WorkingDirectory' and 'ExecStart' paths of the systemd configuration.
// If this is the first time the method is getting invoked, then it will create the config file with the required details.
// Subsequent calls will update the version of RunCommand to use.
func createOrUpdateRunCommandService(ctx *log.Context) (bool, error) {
	// TODO: Get the actual runCommand version
	runCommandVersion := "1.3.5"
	systemdConfig := fmt.Sprintf(serviceDetails, runCommandVersion)

	ctx.Log("event", "Using run command version: "+runCommandVersion)
	err := os.WriteFile(systemServiceFilePath, []byte(systemdConfig), 0666)
	ctx.Log("error", err)
	if err != nil {
		return false, errors.Wrap(err, "failed to write systemd configuration for runcommand version: "+runCommandVersion)
	}

	ctx.Log("event", fmt.Sprintf("File %v to start run command as a service was successfully created", systemServiceFilePath))
	return true, nil
}

// Starts the RunCommand service by invoking 'systemctl start'
func startService(ctx *log.Context) (bool, error) {
	ctx.Log("event", "Trying to start run command service")
	output, err := exec.Command("systemctl", "start", systemdServiceName).Output()
	ctx.Log("event", output)
	if err != nil {
		return false, errors.Wrap(err, "failed to start service")
	}

	ctx.Log("event", "Run command service has started")
	return true, nil
}

// Stops the RunCommand service by invoking 'systemctl stop'
func stopService(ctx *log.Context) (bool, error) {
	ctx.Log("event", "Trying to stop run command service")
	_, err := exec.Command("systemctl", "stop", systemdServiceName).Output()
	if err != nil {
		return false, errors.Wrap(err, "failed to stop service")
	}

	ctx.Log("event", "Run command service has been stopped")
	return true, nil
}

// Enables the RunCommand service by invoking 'systemctl enable'
func enableService(ctx *log.Context) (bool, error) {
	ctx.Log("event", "Trying to enable run command service to start at system boot")
	_, err := exec.Command("systemctl", "enable", systemdServiceName).Output()
	if err != nil {
		return false, errors.Wrap(err, "failed to enable service")
	}

	ctx.Log("event", "Run command service has been enable to start at system boot")
	return true, nil
}
