package status

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

// ReportStatus saves operation status to the status file for the extension
// handler with the optional given message, if the given cmd requires reporting
// status.
//
// If an error occurs reporting the status, it will be logged and returned.
func ReportStatus(ctx *log.Context, hEnv types.HandlerEnvironment, extName string, seqNum int, t types.StatusType, c types.Cmd, msg string) error {
	if !c.ShouldReportStatus {
		ctx.Log("status", "not reported for operation (by design)")
		return nil
	}

	s := New(t, c.Name, msg)
	if err := Save(hEnv.HandlerEnvironment.StatusFolder, extName, seqNum, s); err != nil {
		ctx.Log("event", "failed to save handler status", "error", err)
		return errors.Wrap(err, "failed to save handler status")
	}
	return nil
}

// Save persists the status message to the specified status folder using the
// sequence number. The operation consists of writing to a temporary file in the
// same folder and moving it to the final destination for atomicity.
func Save(statusFolder string, extName string, seqNo int, r types.StatusReport) error {
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

	b, err := json.MarshalIndent(r, "", "\t")
	if err != nil {
		return fmt.Errorf("status: failed to marshal into json: %v", err)
	}

	if err := os.WriteFile(tmpFile.Name(), b, 0644); err != nil {
		return fmt.Errorf("status: failed to path=%s error=%v", tmpFile.Name(), err)
	}

	if err := os.Rename(tmpFile.Name(), path); err != nil {
		return fmt.Errorf("status: failed to move to path=%s error=%v", path, err)
	}

	return nil
}

// New creates a new Status instance
func New(t types.StatusType, operation string, message string) types.StatusReport {
	return []types.StatusItem{
		{
			Version:      1, // this is the protocol version do not change unless you are sure
			TimestampUTC: time.Now().UTC().Format(time.RFC3339),
			Status: types.Status{
				Operation: operation,
				Status:    t,
				FormattedMessage: types.FormattedMessage{
					Lang:    "en",
					Message: message},
			},
		},
	}
}
