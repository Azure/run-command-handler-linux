package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

var immediateGSInTerminalStatusLock = sync.Mutex{}

type temporaryStatusFile interface {
	Name() string
	Close() error
}

var (
	statusCreateTemp = func(dir, pattern string) (temporaryStatusFile, error) { return os.CreateTemp(dir, pattern) }
	statusWriteFile  = os.WriteFile
	statusReadFile   = os.ReadFile
	statusRename     = os.Rename
	statusStat       = os.Stat
	statusRemove     = os.Remove
)

// ReportStatusToLocalFile saves operation status to the status file for the extension
// handler with the optional given message, if the given cmd requires reporting
// status.
//
// If an error occurs reporting the status, it will be logged and returned.
//
// This function is used by default for reporting status to the local file system unless a different method is specified.
func ReportStatusToLocalFile(ctx *log.Context, hEnv types.HandlerEnvironment, metadata types.RCMetadata, statusType types.StatusType, c types.Cmd, msg string) error {
	if !c.ShouldReportStatus {
		ctx.Log("status", "not reported for operation (by design)")
		return nil
	}

	rootStatusJson, err := getRootStatusJson(ctx, statusType, c, msg, true, metadata.ExtName, metadata.ClarificationError)
	if err != nil {
		return errors.Wrap(err, "failed to get json for status report")
	}

	ctx.Log("message", "reporting status by writing status file locally")
	err = SaveStatusReport(hEnv.HandlerEnvironment.StatusFolder, metadata.ExtName, metadata.SeqNum, rootStatusJson)
	if err != nil {
		ctx.Log("event", "failed to save handler status", "error", err)
		return errors.Wrap(err, "failed to save handler status")
	}

	ctx.Log("message", "Run Command status was written to file successfully.")
	return nil
}

// SaveStatusReport persists the status message to the specified status folder using the
// sequence number. The operation consists of writing to a temporary file in the
// same folder and moving it to the final destination for atomicity.
func SaveStatusReport(statusFolder string, extName string, seqNo int, rootStatusJson []byte) error {
	fn := fmt.Sprintf("%d.status", seqNo)
	// Support multiconfig extensions where status file name should be: extName.seqNo.status
	if extName != "" {
		fn = extName + "." + fn
	}

	path := filepath.Join(statusFolder, fn)
	tmpFile, err := statusCreateTemp(statusFolder, fn)
	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToCreateTemporaryStatusFile, fmt.Errorf("status: failed to create temporary file: %w", err))
	}
	defer statusRemove(tmpFile.Name())
	if err := tmpFile.Close(); err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToCloseTemporaryStatusFile, fmt.Errorf("status: failed to close temporary file: %w", err))
	}

	if err := statusWriteFile(tmpFile.Name(), rootStatusJson, 0644); err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToWriteTemporaryStatusFile, fmt.Errorf("status: failed to path=%s error=%w", tmpFile.Name(), err))
	}

	if err := statusRename(tmpFile.Name(), path); err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToPublishStatusFile, fmt.Errorf("status: failed to move to path=%s error=%w", path, err))
	}

	return nil
}

func SaveGoalStatesInTerminalStatus(ctx *log.Context, newStatusInTerminalState []ImmediateStatus) error {
	immediateGSInTerminalStatusLock.Lock()
	defer immediateGSInTerminalStatusLock.Unlock()

	newExtensionDirectory := os.Getenv(constants.ExtensionPathEnvName)
	immediateStatusFolder := filepath.Join(newExtensionDirectory, constants.ImmediateStatusFileDirectory)

	ctx.Log("message", "saving goal states in terminal state to file")
	statusFile := filepath.Join(immediateStatusFolder, constants.ImmediateGoalStatesInTerminalStatusFileName)
	tempStatusFile := statusFile + ".tmp"

	ctx.Log("message", "marshalling the content to json")
	rootStatusJson, err := json.MarshalIndent(newStatusInTerminalState, "", "\t")
	if err != nil {
		return errorutil.WithCode(errorutil.Internal_failedToMarshalTerminalGoalStates, fmt.Errorf("status: failed to marshal status report into json: %w", err))
	}

	ctx.Log("message", "writing the content to the temporary status file")
	err = statusWriteFile(tempStatusFile, rootStatusJson, 0644)

	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToWriteTerminalGoalStates, fmt.Errorf("status: failed to write status file: %w", err))
	}

	ctx.Log("message", "Renaming the temporary status file to the final status file")
	err = statusRename(tempStatusFile, statusFile)

	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToPublishTerminalGoalStates, fmt.Errorf("status: failed to move status file: %w", err))
	}

	return nil
}

// getGoalStatesInTerminalStatus retrieves the goal states in terminal status from the file
// The file is located in the extension directory under the immediate status folder
func GetGoalStatesInTerminalStatus(ctx *log.Context) ([]ImmediateStatus, error) {
	immediateGSInTerminalStatusLock.Lock()
	defer immediateGSInTerminalStatusLock.Unlock()

	newExtensionDirectory := os.Getenv(constants.ExtensionPathEnvName)
	immediateStatusFolder := filepath.Join(newExtensionDirectory, constants.ImmediateStatusFileDirectory)

	ctx.Log("message", "getting goal states in terminal status from file")
	statusFile := filepath.Join(immediateStatusFolder, constants.ImmediateGoalStatesInTerminalStatusFileName)

	result := []ImmediateStatus{}

	if _, err := statusStat(statusFile); err == nil {
		ctx.Log("message", "status file already exists. Reading the content")
		fileContent, err := statusReadFile(statusFile)
		if err != nil {
			return result, errorutil.WithCode(errorutil.Os_FailedToReadTerminalGoalStates, fmt.Errorf("status: failed to read status file: %w", err))
		}

		ctx.Log("message", "unmarshalling the content of the status file")
		var existingStatus []ImmediateStatus
		if err := json.Unmarshal(fileContent, &existingStatus); err != nil {
			return result, errorutil.WithCode(errorutil.Os_InvalidTerminalGoalStatesJson, fmt.Errorf("status: failed to unmarshal status file: %w", err))
		}

		ctx.Log("message", "merging the new status with the existing content")
		result = existingStatus
		ctx.Log("message", fmt.Sprintf("Found %v goal states in terminal state", len(result)))
	} else if os.IsNotExist(err) {
		ctx.Log("message", "status file does not exist. No goal states in terminal status")
	} else {
		return result, errorutil.WithCode(errorutil.Os_FailedToStatTerminalGoalStates, fmt.Errorf("status: failed to stat status file: %w", err))
	}

	return result, nil
}

// GetGoalStatesInTerminalStatus retrieves the goal states in terminal status from the file
// Then it removes the disabled goal states and updated goal states from the list
// This avoid reporting disabled goal states to the HGAP and also avoid reporting blocking the communication channel when the goal state is updated
func RemoveDisabledAndUpdatedGoalStatesInLocalStatusFile(ctx *log.Context, goalStateKeysToRemove []types.GoalStateKey) error {
	if len(goalStateKeysToRemove) > 0 {
		statusInTerminalState, err := GetGoalStatesInTerminalStatus(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to get goal states in terminal status from file")
		}

		ctx.Log("message", fmt.Sprintf("Checking %v goal states to remove from the statusInTerminalState list", len(goalStateKeysToRemove)))
		for _, goalStateKey := range goalStateKeysToRemove {
			if goalStateKey.RuntimeSettingsState == "disabled" {
				for i, status := range statusInTerminalState {
					if status.SequenceNumber == goalStateKey.SeqNumber && status.Status.Name == goalStateKey.ExtensionName {
						ctx.Log("message", fmt.Sprintf("Goal state %v is disabled. Removing it from the statusInTerminalState list.", goalStateKey))
						statusInTerminalState = append(statusInTerminalState[:i], statusInTerminalState[i+1:]...)
						break
					}
				}
			} else if goalStateKey.RuntimeSettingsState == "enabled" {
				for i, status := range statusInTerminalState {
					// If the sequence number is less than the goal state key sequence number, it means that the goal state got updated
					if status.SequenceNumber < goalStateKey.SeqNumber && status.Status.Name == goalStateKey.ExtensionName {
						ctx.Log("message", fmt.Sprintf("Goal state %v is updated. Removing it from the statusInTerminalState list.", goalStateKey))
						statusInTerminalState = append(statusInTerminalState[:i], statusInTerminalState[i+1:]...)
						break
					}
				}
			} else {
				return errorutil.WithCode(errorutil.Internal_invalidGoalStateRemovalState, errors.New(fmt.Sprintf("goal state %v is not disabled. Cannot remove it from the statusInTerminalState list", goalStateKey)))
			}
		}

		err = SaveGoalStatesInTerminalStatus(ctx, statusInTerminalState)
		if err != nil {
			return errors.Wrap(err, "failed to save goal states in terminal status")
		}
	} else {
		ctx.Log("message", "No disabled goal states to remove from the statusInTerminalState list")
	}

	return nil
}

func getRootStatusJson(ctx *log.Context, statusType types.StatusType, c types.Cmd, msg string, indent bool, extName string, clarificationErrors ...error) ([]byte, error) {
	ctx.Log("message", "creating json to report status")
	statusReport := types.NewStatusReport(statusType, c.Name, msg, extName, clarificationErrors...)

	b, err := MarshalStatusReportIntoJson(statusReport, indent)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal status report into json")
	}

	return b, nil
}

// getSingleStatusItem returns a single status item for the given status type, command, and message.
// This is useful when only a single status item is needed for an immediate status report.
func GetSingleStatusItem(ctx *log.Context, statusType types.StatusType, c types.Cmd, msg string, extName string, clarificationErrors ...error) (types.StatusItem, error) {
	ctx.Log("message", "creating json to report status")
	statusReport := types.NewStatusReport(statusType, c.Name, msg, extName, clarificationErrors...)
	if len(statusReport) != 1 {
		return types.StatusItem{}, errorutil.WithCode(errorutil.Internal_unexpectedStatusItemCount, errors.New("expected a single status item"))
	}
	return statusReport[0], nil
}

func MarshalStatusReportIntoJson(statusReport types.StatusReport, indent bool) ([]byte, error) {
	var b []byte
	var err error
	if indent {
		b, err = json.MarshalIndent(statusReport, "", "\t")
	} else {
		b, err = json.Marshal(statusReport)
	}

	return b, errorutil.WithCode(errorutil.Internal_failedToMarshalStatusReport, err)
}
