package types

import (
	"github.com/go-kit/kit/log"
)

type cmdFunc func(ctx *log.Context, hEnv HandlerEnvironment, report *RunCommandInstanceView, metadata RCMetadata, c Cmd) (stdout string, stderr string, err error, exitCode int)
type reportStatusFunc func(ctx *log.Context, hEnv HandlerEnvironment, metadata RCMetadata, statusType StatusType, c Cmd, msg string) error
type preFunc func(ctx *log.Context, hEnv HandlerEnvironment, metadata RCMetadata, c Cmd) error
type cleanupFunc func(ctx *log.Context, metadata RCMetadata, h HandlerEnvironment, runAsUser string)

type Cmd struct {
	Name               string       // human readable string
	ShouldReportStatus bool         // determines if running this should report the status of the run command
	FailExitCode       int          // exitCode to use when commands fail
	Functions          CmdFunctions // functions used by the command
}

type CmdFunctions struct {
	Invoke       cmdFunc          // associated function
	Pre          preFunc          // executed before any status is reported
	ReportStatus reportStatusFunc // function to report status. Useful to write in .status file for RC and upload to blob for ImmediateRC
	Cleanup      cleanupFunc      // function called after the extension has reached a terminal state to perform cleanup steps
}

func (command Cmd) InitializeFunctions(input CmdFunctions) Cmd {
	command.Functions = input
	return command
}

var (
	CmdInstallTemplate    = Cmd{Name: "Install", ShouldReportStatus: false, FailExitCode: 52}
	CmdEnableTemplate     = Cmd{Name: "Enable", ShouldReportStatus: true, FailExitCode: 3}
	CmdDisableTemplate    = Cmd{Name: "Disable", ShouldReportStatus: true, FailExitCode: 3}
	CmdUpdateTemplate     = Cmd{Name: "Update", ShouldReportStatus: true, FailExitCode: 3}
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
