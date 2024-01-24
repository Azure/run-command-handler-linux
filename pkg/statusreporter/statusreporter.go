package statusreporter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"
)

type PutStatusRequest struct {
	Content string
}

func ReportStatus(putStatusEndpoint string, statusToUpload string) (*http.Response, error) {
	// If the statusToUpload is empty, the above call to create the blob will clear out the
	// contents of the blob and set it to empty. We can return now.
	if statusToUpload == "" {
		return nil, nil
	}

	requestContent := PutStatusRequest{Content: base64.StdEncoding.EncodeToString([]byte(statusToUpload))}
	serializedRequestContent, err := json.Marshal(requestContent)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal PutStatusRequest")
	}

	return uploadData(putStatusEndpoint, serializedRequestContent)
}

func uploadData(putStatusEndpoint string, serializedRequestContent []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPut, putStatusEndpoint, bytes.NewBuffer(serializedRequestContent))
	if err != nil {
		return nil, errors.Wrap(err, "could not create new http request to send provided content")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to send http request")
	}

	return resp, nil
}
