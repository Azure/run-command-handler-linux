package statusreporter

import (
	"bytes"
	"net/http"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

type PutStatusRequest struct {
	Content string
}

func ReportStatus(ctx *log.Context, putStatusEndpoint string, statusToUpload string) (*http.Response, error) {
	// If the statusToUpload is empty, the above call to create the blob will clear out the
	// contents of the blob and set it to empty. We can return now.
	if statusToUpload == "" {
		return nil, nil
	}

	ctx.Log("message", "status to upload", "status", statusToUpload)
	return uploadData(putStatusEndpoint, []byte(statusToUpload))
}

func uploadData(putStatusEndpoint string, byteContent []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPut, putStatusEndpoint, bytes.NewBuffer(byteContent))
	if err != nil {
		return nil, errors.Wrap(err, "could not create new http request to send provided content")
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send http request")
	}

	return resp, nil
}
