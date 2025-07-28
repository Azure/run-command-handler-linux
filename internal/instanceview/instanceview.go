package instanceview

import (
	"fmt"

	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
)

// ReportInstanceView saves operation status to the status file for the extension
// handler with the optional given message, if the given cmd requires reporting
// status.
//
// If an error occurs reporting the status, it will be logged and returned.
func ReportInstanceView(ctx *log.Context, hEnv types.HandlerEnvironment, metadata types.RCMetadata, t types.StatusType, c types.Cmd, instanceview *types.RunCommandInstanceView) error {
	if !c.ShouldReportStatus {
		ctx.Log("status", "not reported for operation (by design)")
		return nil
	}

	msg, err := SerializeInstanceView(instanceview)
	if err != nil {
		return err
	}

	if c.Functions.ErrorReport == nil {
		return c.Functions.ReportStatus(ctx, hEnv, metadata, t, c, msg)
	}
	slice := []int{instanceview.ExitCode}
	return c.Functions.ReportStatus(ctx, hEnv, metadata, t, c, msg, slice...)
	// return c.Functions.ErrorReport(ctx, hEnv, metadata, t, c, msg, instanceview.ExitCode)
}

func SerializeInstanceView(instanceview *types.RunCommandInstanceView) (string, error) {
	bytes, err := instanceview.Marshal()
	if err != nil {
		return "", fmt.Errorf("status: failed to marshal into json: %v", err)
	}
	return string(bytes), err
}
