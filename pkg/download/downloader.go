package download

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Azure/run-command-handler-linux/pkg/urlutil"
	"github.com/pkg/errors"
)

// Downloader describes a method to download files.
type Downloader interface {
	// GetRequest returns a new GET request for the resource.
	GetRequest() (*http.Request, error)
}

const (
	// MsiDownload404ErrorString describes Msi specific error
	MsiDownload404ErrorString = "please ensure that the blob location in the fileUri setting exists, and the specified Managed Identity has read permissions to the storage blob"

	// MsiDownload403ErrorString describes Msi permission specific error
	MsiDownload403ErrorString = "please ensure that the specified Managed Identity has read permissions to the storage blob"
)

var (
	// httpClient is the default client to be used in downloading files from
	// Internet. http.Get() uses a client without timeouts (http.DefaultClient)
	// so it is dangerous to use it for downloading files from the Internet.
	httpClient = &http.Client{
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}}
)

// Download retrieves a response body and checks the response status code to see
// if it is 200 OK and then returns the response body. It issues a new request
// every time called. It is caller's responsibility to close the response body.
func Download(downloader Downloader) (int, io.ReadCloser, error) {
	request, err := downloader.GetRequest()
	if err != nil {
		return -1, nil, errors.Wrapf(err, "failed to create http request")
	}

	response, err := httpClient.Do(request)
	if err != nil {
		err = urlutil.RemoveUrlFromErr(err)
		return -1, nil, errors.Wrapf(err, "http request failed")
	}

	if response.StatusCode == http.StatusOK {
		return response.StatusCode, response.Body, nil
	}

	err = fmt.Errorf("unexpected status code: actual=%d expected=%d", response.StatusCode, http.StatusOK)
	switch downloader.(type) {
	case *blobWithMsiToken:
		switch response.StatusCode {
		case http.StatusNotFound:
			return response.StatusCode, nil, errors.Wrapf(err, MsiDownload404ErrorString)
		case http.StatusForbidden:
			return response.StatusCode, nil, errors.Wrapf(err, MsiDownload403ErrorString)
		}
	}
	return response.StatusCode, nil, err
}
