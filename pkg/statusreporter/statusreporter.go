package statusreporter

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

var statusHTTPClient = &http.Client{}

type PutStatusRequest struct {
	Content string
}

func ReportStatus(ctx *log.Context, putStatusEndpoint string, statusToUpload string) (*http.Response, error) {
	// If the statusToUpload is empty, the above call to create the blob will clear out the
	// contents of the blob and set it to empty. We can return now.
	if statusToUpload == "" {
		return nil, nil
	}

	return uploadData(putStatusEndpoint, []byte(statusToUpload))
}

func uploadData(putStatusEndpoint string, byteContent []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPut, putStatusEndpoint, bytes.NewBuffer(byteContent))
	if err != nil {
		return nil, errorutil.WithCode(errorutil.HostGA_failedToCreateStatusRequest, errors.Wrap(err, "could not create new http request to send provided content"))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := statusHTTPClient.Do(req)
	if err != nil {
		return nil, errorutil.WithCode(errorutil.HostGA_failedToSendStatusRequest, errors.Wrap(err, "failed to send http request"))
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		status := resp.Status
		if status == "" {
			status = fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		return resp, errorutil.WithCode(errorutil.HostGA_statusUploadReturnedErrorStatus, fmt.Errorf("status upload returned HTTP %s", status))
	}
	return resp, nil
}
