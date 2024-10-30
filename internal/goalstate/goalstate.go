package goalstate

import (
	"time"

	"github.com/Azure/run-command-handler-linux/internal/cleanup"
	commands "github.com/Azure/run-command-handler-linux/internal/cmds"
	"github.com/Azure/run-command-handler-linux/internal/commandProcessor"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/observer"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	enableCommand             string = "enable"
	maxExecutionTimeInMinutes int32  = 90
)

func HandleImmediateGoalState(ctx *log.Context, setting settings.SettingsCommon, notifier *observer.Notifier) error {
	done := make(chan bool)
	err := make(chan error)
	go startAsync(ctx, setting, notifier, done, err)
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

func HandleSkippedImmediateGoalState(ctx *log.Context, notifier *observer.Notifier, goalStateKey types.GoalStateKey, msg string) error {
	cmd, ok := commands.Cmds[enableCommand]
	if !ok {
		return errors.New("missing enable command")
	}

	if !cmd.ShouldReportStatus {
		ctx.Log("status", "not reported for operation (by design)")
		return nil
	}

	statusItem, err := status.GetSingleStatusItem(ctx, types.StatusSkipped, cmd, msg)
	if err != nil {
		return errors.Wrap(err, "failed to get status item")
	}

	ctx.Log("message", "reporting status of skipped goal state by notifying the observer to then send to HGAP")
	return notifier.Notify(types.StatusEventArgs{
		StatusKey:      goalStateKey,
		TopLevelStatus: statusItem,
	})
}

func startAsync(ctx *log.Context, setting settings.SettingsCommon, notifier *observer.Notifier, done chan bool, err chan error) {
	cmd, ok := commands.Cmds[enableCommand]
	if !ok {
		err <- errors.New("missing enable command")
		return
	}

	// Overwrite function to report status to HGAP. This function prepares the status to be sent to the HGAP and then calls the notifier to send it.
	cmd.Functions.ReportStatus = func(ctx *log.Context, hEnv types.HandlerEnvironment, metadata types.RCMetadata, statusType types.StatusType, c types.Cmd, msg string) error {
		if !c.ShouldReportStatus {
			ctx.Log("status", "not reported for operation (by design)")
			return nil
		}

		statusItem, err := status.GetSingleStatusItem(ctx, statusType, c, msg)
		if err != nil {
			return errors.Wrap(err, "failed to get status item")
		}

		ctx.Log("message", "reporting status by notifying the observer to then send to HGAP")
		return notifier.Notify(types.StatusEventArgs{
			StatusKey: types.GoalStateKey{
				ExtensionName: metadata.ExtName,
				SeqNumber:     metadata.SeqNum,
			},
			TopLevelStatus: statusItem,
		})
	}

	// Overwrite function to cleanup the command. This function is called after the command has been executed.
	cmd.Functions.Cleanup = cleanup.ImmediateRunCommandCleanup

	var hs handlersettings.HandlerSettingsFile
	var runtimeSettings []handlersettings.RunTimeSettingsFile
	hs.RuntimeSettings = append(runtimeSettings, handlersettings.RunTimeSettingsFile{HandlerSettings: setting})
	ctx.Log("message", "executing immediate goal state")
	commandProcessor.ProcessImmediateHandlerCommand(cmd, hs, *setting.ExtensionName, *setting.SeqNo)
	done <- true
}
