package download

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Azure/run-command-handler-linux/pkg/urlutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

// Downloader describes a method to download files.
type Downloader interface {
	// GetRequest returns a new GET request for the resource.
	GetRequest() (*http.Request, error)
}

const (
	// MsiDownload404ErrorString describes Msi specific error
	MsiDownload404ErrorString = "please ensure that the blob exists, and the specified Managed Identity has read permissions to the storage blob"

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
func Download(ctx *log.Context, downloader Downloader) (int, io.ReadCloser, error) {
	request, err := downloader.GetRequest()
	if err != nil {
		return -1, nil, errors.Wrapf(err, "failed to create http request")
	}

	requestID := request.Header.Get(xMsClientRequestIdHeaderName)
	if len(requestID) > 0 {
		ctx.Log("info", fmt.Sprintf("starting download with client request ID %s", requestID))
	}

	response, err := httpClient.Do(request)
	if err != nil {
		err = urlutil.RemoveUrlFromErr(err)
		return -1, nil, errors.Wrapf(err, "http request failed")
	}

	if response.StatusCode == http.StatusOK {
		return response.StatusCode, response.Body, nil
	}

	errString := ""
	requestId := response.Header.Get(xMsServiceRequestIdHeaderName)
	switch downloader.(type) {
	case *blobWithMsiToken:
		switch response.StatusCode {
		case http.StatusNotFound:
			notFoundError := fmt.Sprintf("RunCommand failed to download blob '%s' and recieved response code '%s'. Make sure Azure blob and managed identity exist, and identity has access to storage blob's container with 'Storage Blob Data Reader' role assignment. For user assigned identity, add it under VM's identity. For more info, see https://aka.ms/RunCommandManagedLinux", request.URL.Opaque, response.Status)
			errString = fmt.Sprintf("%s: %s", MsiDownload404ErrorString, notFoundError)
		case http.StatusForbidden,
			http.StatusUnauthorized,
			http.StatusBadRequest,
			http.StatusConflict:
			forbiddenError := fmt.Sprintf("RunCommand failed to download blob '%s' and recieved response code '%s'. Make sure managed identity has access to storage blob's container with 'Storage Blob Data Reader' role assignment. For user assigned identity, add it under VM's identity. For more info, see https://aka.ms/RunCommandManagedLinux", request.URL.Opaque, response.Status)
			errString = fmt.Sprintf("%s: %s", MsiDownload403ErrorString, forbiddenError)
		}
		break
	default:
		hostname := request.URL.Host
		switch response.StatusCode {
		case http.StatusUnauthorized:
			errString = fmt.Sprintf("RunCommand failed to download the file from %s because access was denied. Please fix the blob permissions and try again, the response code and message returned were: %q",
				hostname,
				response.Status)
			break
		case http.StatusNotFound:
			errString = fmt.Sprintf("RunCommand failed to download the file from %s because it does not exist. Please create the blob and try again, the response code and message returned were: %q",
				hostname,
				response.Status)

		case http.StatusBadRequest:
			errString = fmt.Sprintf("RunCommand failed to download the file from %s because parts of the request were incorrectly formatted, missing, and/or invalid. The response code and message returned were: %q",
				hostname,
				response.Status)

		case http.StatusInternalServerError:
			errString = fmt.Sprintf("RunCommand failed to download the file from %s due to an issue with storage. The response code and message returned were: %q",
				hostname,
				response.Status)
		default:
			errString = fmt.Sprintf("RunCommand failed to download the file from %s because the server returned a response code and message of %q Please verify the machine has network connectivity.",
				hostname,
				response.Status)
		}
	}

	if len(requestId) > 0 {
		errString += fmt.Sprintf(" (Service request ID: %s)", requestId)
	}
	return response.StatusCode, nil, fmt.Errorf(errString)
}
