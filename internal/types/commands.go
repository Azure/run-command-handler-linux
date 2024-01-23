package types

import (
	"github.com/go-kit/kit/log"
)

type cmdFunc func(ctx *log.Context, hEnv HandlerEnvironment, report *RunCommandInstanceView, metadata RCMetadata, c Cmd) (stdout string, stderr string, err error, exitCode int)
type reportStatusFunc func(ctx *log.Context, hEnv HandlerEnvironment, metadata RCMetadata, t StatusType, c Cmd, msg string) error
type preFunc func(ctx *log.Context, hEnv HandlerEnvironment, metadata RCMetadata, c Cmd) error

type Cmd struct {
	Invoke             cmdFunc          // associated function
	Name               string           // human readable string
	ShouldReportStatus bool             // determines if running this should report the status of the run command
	Pre                preFunc          // executed before any status is reported
	FailExitCode       int              // exitCode to use when commands fail
	ReportStatus       reportStatusFunc // function to report status. Useful to write in .status file for RC and upload to blob for ImmediateRC
}

type CmdFunctions struct {
	Invoke       cmdFunc          // associated function
	Pre          preFunc          // executed before any status is reported
	ReportStatus reportStatusFunc // to report the status of the process
}

func CreateCommandWithProvidedFunctions(command Cmd, input CmdFunctions) Cmd {
	command.Invoke = input.Invoke
	command.Pre = input.Pre
	command.ReportStatus = input.ReportStatus
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
