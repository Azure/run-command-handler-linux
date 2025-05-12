package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
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

	rootStatusJson, err := getRootStatusJson(ctx, statusType, c, msg, true, metadata.ExtName)
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
	tmpFile, err := os.CreateTemp(statusFolder, fn)
	if err != nil {
		return fmt.Errorf("status: failed to create temporary file: %v", err)
	}
	tmpFile.Close()

	if err := os.WriteFile(tmpFile.Name(), rootStatusJson, 0644); err != nil {
		return fmt.Errorf("status: failed to path=%s error=%v", tmpFile.Name(), err)
	}

	if err := os.Rename(tmpFile.Name(), path); err != nil {
		return fmt.Errorf("status: failed to move to path=%s error=%v", path, err)
	}

	return nil
}

func SaveGoalStatesInTerminalStatus(ctx *log.Context, newStatusInTerminalState []ImmediateStatus) error {
	newExtensionDirectory := os.Getenv(constants.ExtensionPathEnvName)
	immediateStatusFolder := filepath.Join(newExtensionDirectory, constants.ImmediateStatusFileDirectory)

	ctx.Log("message", "saving goal states in terminal state to file")
	statusFile := filepath.Join(immediateStatusFolder, constants.ImmediateGoalStatesInTerminalStatusFileName)
	tempStatusFile := statusFile + ".tmp"
	content := newStatusInTerminalState

	if _, err := os.Stat(statusFile); err == nil {
		ctx.Log("message", "status file already exists. Reading the content")
		fileContent, err := os.ReadFile(statusFile)
		if err != nil {
			return fmt.Errorf("status: failed to read status file: %v", err)
		}

		ctx.Log("message", "unmarshalling the content of the status file")
		if err := json.Unmarshal(fileContent, &content); err != nil {
			return fmt.Errorf("status: failed to unmarshal status file: %v", err)
		}

		ctx.Log("message", "appending new status to the content")
		content = append(content, newStatusInTerminalState...)
	}

	ctx.Log("message", "marshalling the content to json")
	rootStatusJson, err := json.MarshalIndent(content, "", "\t")
	if err != nil {
		return fmt.Errorf("status: failed to marshal status report into json: %v", err)
	}

	ctx.Log("message", "writing the content to the temporary status file")
	if err := os.WriteFile(tempStatusFile, rootStatusJson, 0644); err != nil {
		return fmt.Errorf("status: failed to write status file: %v", err)
	}

	ctx.Log("message", "moving the temporary status file to the final status file")
	if err := os.Rename(tempStatusFile, statusFile); err != nil {
		return fmt.Errorf("status: failed to move status file: %v", err)
	}

	ctx.Log("message", "removing the temporary status file")
	if err := os.Remove(tempStatusFile); err != nil {
		return fmt.Errorf("status: failed to remove temporary status file: %v", err)
	}

	return nil
}

func getRootStatusJson(ctx *log.Context, statusType types.StatusType, c types.Cmd, msg string, indent bool, extName string) ([]byte, error) {
	ctx.Log("message", "creating json to report status")
	statusReport := types.NewStatusReport(statusType, c.Name, msg, extName)

	b, err := MarshalStatusReportIntoJson(statusReport, indent)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal status report into json")
	}

	return b, nil
}

// getSingleStatusItem returns a single status item for the given status type, command, and message.
// This is useful when only a single status item is needed for an immediate status report.
func GetSingleStatusItem(ctx *log.Context, statusType types.StatusType, c types.Cmd, msg string, extName string) (types.StatusItem, error) {
	ctx.Log("message", "creating json to report status")
	statusReport := types.NewStatusReport(statusType, c.Name, msg, extName)
	if len(statusReport) != 1 {
		return types.StatusItem{}, errors.New("expected a single status item")
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

	return b, err
}
