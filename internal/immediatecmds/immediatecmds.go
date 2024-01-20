package immediatecmds

import (
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/immediateruncommand"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
)

var (
	CmdRunService = types.CreateCommandWithProvidedFunctions(types.CmdRunServiceTemplate, types.CmdFunctions{Invoke: runService, Pre: nil})

	Cmds = map[string]types.Cmd{
		"runService": CmdRunService,
	}
)

// Runs the extension as a service. This is the default behavior for when the program is initiated as a service by systemd.
func runService(ctx *log.Context, h types.HandlerEnvironment, report *types.RunCommandInstanceView, extName string, seqNum int) (string, string, error, int) {
	immediateruncommand.StartImmediateRunCommand(ctx)
	return "", "", nil, constants.ExitCode_Okay
}
