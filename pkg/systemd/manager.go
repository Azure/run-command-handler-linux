package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/Azure/run-command-handler-linux/pkg/versionutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	systemctl              = "systemctl"
	systemctl_daemonreload = "daemon-reload"
	systemctl_disable      = "disable"
	systemctl_enable       = "enable"
	systemctl_isactive     = "is-active"
	systemctl_isenabled    = "is-enabled"
	systemctl_start        = "start"
	systemctl_status       = "status"
	systemctl_stop         = "stop"

	unitConfigurationBasePath_preferred   = "/etc/systemd/system"           // system units created by the administrator path
	unitConfigurationBasePath_alternative = "/usr/local/lib/systemd/system" // system units installed by the administrator path
	unitConfigurationFilePermission       = 0644                            // rw-r--r--
)

type Manager struct {
}

var runSystemctl = func(args ...string) ([]byte, error) {
	return exec.Command(systemctl, args...).Output()
}

var (
	statUnitFile   = os.Stat
	writeUnitFile  = os.WriteFile
	removeUnitFile = os.Remove
	readUnitFile   = os.ReadFile
)

func NewUnitManager() *Manager {
	return &Manager{}
}

func (mgr *Manager) StartUnit(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to start unit")
	_, err := runSystemctl(systemctl_start, unitName)
	return errorutil.WithCode(errorutil.Systemd_failedToStartUnit, errors.Wrap(err, "failed to start systemd unit"))
}

func (mgr *Manager) StopUnit(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to stop unit")
	_, err := runSystemctl(systemctl_stop, unitName)
	return errorutil.WithCode(errorutil.Systemd_failedToStopUnit, errors.Wrap(err, "failed to stop systemd unit"))
}

func (mgr *Manager) EnableUnit(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to enable unit")
	_, err := runSystemctl(systemctl_enable, unitName)
	return errorutil.WithCode(errorutil.Systemd_failedToEnableUnit, errors.Wrap(err, "failed to enable systemd unit"))
}

func (mgr *Manager) DisableUnit(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to disable unit")
	_, err := runSystemctl(systemctl_disable, unitName)
	return errorutil.WithCode(errorutil.Systemd_failedToDisableUnit, errors.Wrap(err, "failed to disable systemd unit"))
}

func (mgr *Manager) DaemonReload(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to reload daemon")
	_, err := runSystemctl(systemctl_daemonreload)
	return errorutil.WithCode(errorutil.Systemd_failedToReloadDaemon, errors.Wrap(err, "failed to reload systemd daemon"))
}

func (mgr *Manager) IsUnitActive(unitName string, ctx *log.Context) error {
	ctx.Log("message", "running command to check if unit is active")
	_, err := runSystemctl(systemctl_isactive, unitName)
	return errorutil.WithCode(errorutil.Systemd_failedToCheckUnitActivity, errors.Wrap(err, "failed to check systemd unit activity"))
}

func (mgr *Manager) IsUnitEnabled(unitName string, ctx *log.Context) (bool, error) {
	ctx.Log("message", "running command to check if unit is already enabled")
	output, err := runSystemctl(systemctl_isenabled, unitName)
	sanitizedOutput := strings.Replace(string(output), "\n", "", -1)
	ctx.Log("message", fmt.Sprintf("%v %v output: %v", systemctl, systemctl_isenabled, sanitizedOutput))
	if sanitizedOutput == "enabled" {
		return true, nil
	} else if sanitizedOutput == "disabled" {
		return false, nil
	}

	return false, errorutil.WithCode(errorutil.Systemd_failedToCheckUnitEnabled, errors.Wrap(err, "failed to check whether systemd unit is enabled"))
}

func (mgr *Manager) IsUnitInstalled(unitName string, ctx *log.Context) (bool, error) {
	filePath, err := GetUnitConfigurationFilePath(unitName, ctx)
	if err != nil {
		return false, err
	}

	ctx.Log("message", fmt.Sprintf("Checking if unit file %v exists to verify presence of installed service", filePath))
	_, err = statUnitFile(filePath)

	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, errorutil.WithCode(errorutil.Systemd_failedToCheckUnitFile, errors.Wrap(err, "Error occurred while checking file existence"))
	}

	return true, nil
}

func (*Manager) CreateUnitConfigurationFile(unitName string, content []byte, ctx *log.Context) error {
	unitConfigPath, err := GetUnitConfigurationFilePath(unitName, ctx)
	if err != nil {
		return err
	}

	ctx.Log("message", "creating unit configuration file in "+unitConfigPath)
	return errorutil.WithCode(errorutil.Systemd_failedToWriteUnitConfiguration, errors.Wrap(writeUnitFile(unitConfigPath, content, unitConfigurationFilePermission), "failed to write systemd unit configuration"))
}

func (*Manager) RemoveUnitConfigurationFile(unitName string, ctx *log.Context) error {
	unitConfigPath, err := GetUnitConfigurationFilePath(unitName, ctx)
	if err != nil {
		return err
	}

	ctx.Log("message", "removing unit configuration file from "+unitConfigPath)
	return errorutil.WithCode(errorutil.Systemd_failedToRemoveUnitConfiguration, errors.Wrap(removeUnitFile(unitConfigPath), "failed to remove systemd unit configuration"))
}

// Gets the installed version of the extension. The version will look at the service definition and extract it from the
// ExecStart field as there isn't a way to include a version parameter in the definition. The field looks as follows:
// ExecStart=/var/lib/waagent/Microsoft.CPlat.Core.RunCOmmandHandlerLinux-1.0.0/bin/immediate-run-command-handler
func (*Manager) GetInstalledVersion(unitName string, ctx *log.Context) (string, error) {
	unitConfigPath, err := GetUnitConfigurationFilePath(unitName, ctx)
	if err != nil {
		return "", err
	}

	ctx.Log("message", "getting content from "+unitConfigPath)
	content, err := readUnitFile(unitConfigPath)
	if err != nil {
		return "", errorutil.WithCode(errorutil.Systemd_failedToReadUnitConfiguration, errors.Wrap(err, "failed to read systemd unit configuration"))
	}
	return versionutil.ExtractFromServiceDefinition(string(content), ctx)
}

var GetUnitConfigurationFilePath = func(unitName string, ctx *log.Context) (string, error) {
	base_path, err := GetSystemDConfigurationBasePath(ctx)
	if err != nil {
		return "", err
	}
	return path.Join(base_path, unitName), nil
}
