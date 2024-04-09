package goalstate

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/cleanup"
	commands "github.com/Azure/run-command-handler-linux/internal/cmds"
	"github.com/Azure/run-command-handler-linux/internal/commandProcessor"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	enableCommand             string = "enable"
	maxExecutionTimeInMinutes int32  = 90
)

func HandleImmediateGoalState(ctx *log.Context, setting settings.SettingsCommon) error {
	done := make(chan bool)
	err := make(chan error)
	go startAsync(ctx, setting, done, err)
	select {
	case <-err:
		return errors.Wrapf(<-err, "error when trying to execute goal state")
	case <-done:
		ctx.Log("message", "goal state successfully finished")
		return nil
	case <-time.After(time.Minute * time.Duration(maxExecutionTimeInMinutes)):
		return errors.New("timeout when trying to execute goal state")
	}
}

func startAsync(ctx *log.Context, setting settings.SettingsCommon, done chan bool, err chan error) {
	cmd, ok := commands.Cmds[enableCommand]
	if !ok {
		err <- errors.New("missing enable command")
		return
	}

	// Overwrite function to report status to blob instead of a local file and the cleanup phase to delete everything after reaching a goal state
	cmd.Functions.ReportStatus = status.ReportStatusToBlob
	cmd.Functions.Cleanup = cleanup.ImmediateRunCommandCleanup

	var hs handlersettings.HandlerSettingsFile
	var runtimeSettings []handlersettings.RunTimeSettingsFile
	hs.RuntimeSettings = append(runtimeSettings, handlersettings.RunTimeSettingsFile{HandlerSettings: setting})
	ctx.Log("message", "executing immediate goal state")
	commandProcessor.ProcessImmediateHandlerCommand(cmd, hs, *setting.ExtensionName, *setting.SeqNo)

	// TODO: Remove (only for simulating long duration processes)
	rand.Seed(time.Now().UnixNano())
	randomInt := rand.Intn(5) + 2
	ctx.Log("report", fmt.Sprintf("sleeping for %v minutes", randomInt))
	time.Sleep(time.Minute * time.Duration(randomInt))
	ctx.Log("message", "done sleeping")
	// TODO: Remove

	done <- true
}
