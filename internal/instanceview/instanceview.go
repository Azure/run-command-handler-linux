package instanceview

import (
	"fmt"

	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
)

// ReportInstanceView saves operation status to the status file for the extension
// handler with the optional given message, if the given cmd requires reporting
// status.
//
// If an error occurs reporting the status, it will be logged and returned.
func ReportInstanceView(ctx *log.Context, hEnv types.HandlerEnvironment, extName string, seqNum int, t types.StatusType, c types.Cmd, instanceview *types.RunCommandInstanceView) error {
	if !c.ShouldReportStatus {
		ctx.Log("status", "not reported for operation (by design)")
		return nil
	}

	msg, err := serializeInstanceView(instanceview)
	if err != nil {
		return err
	}
	return status.ReportStatus(ctx, hEnv, extName, seqNum, t, c, msg)
}

func serializeInstanceView(instanceview *types.RunCommandInstanceView) (string, error) {
	bytes, err := instanceview.Marshal()
	if err != nil {
		return "", fmt.Errorf("status: failed to marshal into json: %v", err)
	}
	return string(bytes), err
}
