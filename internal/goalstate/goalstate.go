package goalstate

import (
	"fmt"
	"time"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/cleanup"
	commands "github.com/Azure/run-command-handler-linux/internal/cmds"
	"github.com/Azure/run-command-handler-linux/internal/commandProcessor"
	"github.com/Azure/run-command-handler-linux/internal/constants"
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
	disableCommand            string = "disable"
	maxExecutionTimeInMinutes int32  = 90
)

var statusToCommandMap = map[string]string{
	"enabled":  enableCommand,
	"disabled": disableCommand,
}

// HandleImmediateGoalState handles the immediate goal state by executing the command and waiting for it to finish.
// ctx: The logger context.
// setting: The settings for the command.
// notifier: The notifier to send the status to the HGAP. This is a notifier that must have been initialized and a status observer must have been added to it.
// Returns the exit code and an error if there was an issue executing the goal state.
func HandleImmediateGoalState(ctx *log.Context, setting settings.SettingsCommon, notifier *observer.Notifier) (int, *vmextension.ErrorWithClarification) {
	done := make(chan bool)
	err := make(chan error)
	go startAsync(ctx, setting, notifier, done, err)
	select {
	case e := <-err:
		ctx.Log("error", fmt.Sprintf("error when trying to execute goal state: %v", e))
		return constants.ImmediateRC_UnknownFailure, vmextension.NewErrorWithClarificationPtr(constants.ImmediateRC_UnknownFailure, errors.New("error when trying to execute goal state"))
	case <-done:
		ctx.Log("message", "goal state successfully finished")
		return constants.ExitCode_Okay, nil
	case <-time.After(time.Minute * time.Duration(maxExecutionTimeInMinutes)):
		ctx.Log("message", "timeout when trying to execute goal state")
		return constants.ImmediateRC_TaskTimeout, vmextension.NewErrorWithClarificationPtr(constants.ImmediateRC_TaskTimeout, errors.New("timeout when trying to execute goal state"))
	}
}

// ReportFinalStatusForImmediateGoalState reports the final status of the immediate goal state to the HGAP.
// Reporting the status to HGAP is done by notifying the observer previously added to the notifier.
// This function is called when the goal state is skipped or when the goal state fails to execute.
// It is important to get the instance view from the goal state and report it to the HGAP so that the user can see the final status of the goal state.
// The instance view is normally added as a message to the status item.
func ReportFinalStatusForImmediateGoalState(ctx *log.Context, notifier *observer.Notifier, goalStateKey types.GoalStateKey, statusType types.StatusType, instanceview *types.RunCommandInstanceView) error {
	if notifier == nil {
		return errors.New("notifier is nil. Cannot report status to HGAP")
	}

	extensionState := goalStateKey.RuntimeSettingsState
	cmdToReport := statusToCommandMap[extensionState]
	cmd, ok := commands.Cmds[cmdToReport]
	if !ok {
		return errors.New(fmt.Sprintf("missing command %v", extensionState))
	}

	if !cmd.ShouldReportStatus {
		ctx.Log("status", "status not reported for operation (by design)")
		return nil
	}

	msg, err := instanceview.Marshal()
	if err != nil {
		return errors.Wrapf(err, "failed to marshal instance view")
	}

	statusItem, err := status.GetSingleStatusItem(ctx, statusType, cmd, string(msg), goalStateKey.ExtensionName)
	if err != nil {
		return errors.Wrap(err, "failed to get status item")
	}

	ctx.Log("message", "reporting status of skipped goal state by notifying the observer to then send to HGAP")
	return notifier.Notify(types.StatusEventArgs{
		StatusKey:      goalStateKey,
		TopLevelStatus: statusItem,
	})
}

// startAsync starts the command asynchronously. The command to execute is the enable command.
// The function to report status is overwritten to report the status to the HGAP. The notifier is used to send the status to the HGAP.
// The function to cleanup the command is overwritten to cleanup the command immediately after it has been executed.
func startAsync(ctx *log.Context, setting settings.SettingsCommon, notifier *observer.Notifier, done chan bool, err chan error) {
	if notifier == nil {
		err <- errors.New("notifier is nil. Cannot report status to HGAP")
		return
	}

	extensionState := *setting.ExtensionState
	ctx.Log("message", fmt.Sprintf("starting command for extension state %v", extensionState))

	cmdToReport := statusToCommandMap[extensionState]
	cmd, ok := commands.Cmds[cmdToReport]
	if !ok {
		err <- errors.New(fmt.Sprintf("missing command %v", extensionState))
		return
	}

	// Overwrite function to report status to HGAP. This function prepares the status to be sent to the HGAP and then calls the notifier to send it.
	cmd.Functions.ReportStatus = func(ctx *log.Context, _ types.HandlerEnvironment, metadata types.RCMetadata, statusType types.StatusType, c types.Cmd, msg string, exitcode ...int) error {
		if !c.ShouldReportStatus {
			ctx.Log("status", fmt.Sprintf("status not reported for operation %v (by design)", c.Name))
			return nil
		}

		statusItem, err := status.GetSingleStatusItem(ctx, statusType, c, msg, metadata.ExtName)
		if err != nil {
			return errors.Wrap(err, "failed to get status item")
		}

		ctx.Log("message", fmt.Sprintf("reporting status by notifying the observer to then send to HGAP for extension name %v and seq number %v", metadata.ExtName, metadata.SeqNum))
		return notifier.Notify(types.StatusEventArgs{
			StatusKey: types.GoalStateKey{
				ExtensionName:        metadata.ExtName,
				SeqNumber:            metadata.SeqNum,
				RuntimeSettingsState: extensionState,
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
