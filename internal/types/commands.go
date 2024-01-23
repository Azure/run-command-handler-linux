package types

import (
	"github.com/go-kit/kit/log"
)

type cmdFunc func(ctx *log.Context, hEnv HandlerEnvironment, report *RunCommandInstanceView, extName string, seqNum int, metadata RCMetadata) (stdout string, stderr string, err error, exitCode int)
type preFunc func(ctx *log.Context, hEnv HandlerEnvironment, extName string, seqNum int, metadata RCMetadata) error

type Cmd struct {
	Invoke             cmdFunc // associated function
	Name               string  // human readable string
	ShouldReportStatus bool    // determines if running this should log to a .status file
	Pre                preFunc // executed before any status is reported
	FailExitCode       int     // exitCode to use when commands fail
}

type CmdFunctions struct {
	Invoke cmdFunc // associated function
	Pre    preFunc // executed before any status is reported
}

func CreateCommandWithProvidedFunctions(command Cmd, input CmdFunctions) Cmd {
	command.Invoke = input.Invoke
	command.Pre = input.Pre
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
