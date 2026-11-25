package types

import (
	"errors"

	"github.com/go-kit/kit/log"
)

var ErrAlreadyProcessed = errors.New("the script configuration has already been processed, will not run again")

type cmdFunc func(ctx *log.Context, hEnv HandlerEnvironment, report *RunCommandInstanceView, metadata RCMetadata, c Cmd) (stdout string, stderr string, err error, exitCode int, cleanup *CleanupRequest)
type reportStatusFunc func(ctx *log.Context, hEnv HandlerEnvironment, metadata RCMetadata, statusType StatusType, c Cmd, msg string) error
type preFunc func(ctx *log.Context, hEnv HandlerEnvironment, metadata RCMetadata, c Cmd) error
type cleanupFunc func(ctx *log.Context, metadata RCMetadata, h HandlerEnvironment, runAsUser string)

// CleanupRequest is returned only when command execution has reached its cleanup stage.
type CleanupRequest struct {
	RunAsUser string
}

type Cmd struct {
	Name               string       // human readable string
	ShouldReportStatus bool         // determines if running this should report the status of the run command
	FailExitCode       int          // exitCode to use when commands fail
	Functions          CmdFunctions // functions used by the command
}

type CmdFunctions struct {
	Invoke       cmdFunc          // associated function
	Pre          preFunc          // executed before any status is reported
	ReportStatus reportStatusFunc // function to report status. Useful to write in .status file for RC and report to HGAP for Immediate Run Command.
	Cleanup      cleanupFunc      // processor runs requested cleanup after reporting for immediate commands; prechecks may call it directly
}

func (command Cmd) InitializeFunctions(input CmdFunctions) Cmd {
	command.Functions = input
	return command
}

var (
	CmdInstallTemplate    = Cmd{Name: "Install", ShouldReportStatus: false, FailExitCode: 52}
	CmdEnableTemplate     = Cmd{Name: "Enable", ShouldReportStatus: true, FailExitCode: 3}
	CmdDisableTemplate    = Cmd{Name: "Disable", ShouldReportStatus: false, FailExitCode: 3}
	CmdUpdateTemplate     = Cmd{Name: "Update", ShouldReportStatus: false, FailExitCode: 3}
	CmdUninstallTemplate  = Cmd{Name: "Uninstall", ShouldReportStatus: false, FailExitCode: 3}
	CmdRunServiceTemplate = Cmd{Name: "RunService", ShouldReportStatus: true, FailExitCode: 3}

	CmdTemplates = map[string]Cmd{
		"install":    CmdInstallTemplate,
		"enable":     CmdEnableTemplate,
		"disable":    CmdDisableTemplate,
		"update":     CmdUpdateTemplate,
		"uninstall":  CmdUninstallTemplate,
		"runService": CmdRunServiceTemplate,
	}
)
