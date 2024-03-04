// Code from here is a potential candidate to add to https://github.com/Azure/azure-extension-platform/blob/main/pkg/status/status.go
// and have others extensions benefit from it as windows extensions can already delegate code
// from https://msazure.visualstudio.com/One/_git/Compute-ART-GuestAgentHostPlugin.
package statusreporter

import (
	"fmt"
	"net/http"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/pkg/errors"
)

type IGuestInformationServiceClient interface {
	ReportStatus(statusToUpload string) (*http.Response, error)
	GetEndpoint() string
}

type GuestInformationServiceClient struct {
	Endpoint string
}

func NewGuestInformationServiceClient(e string) GuestInformationServiceClient {
	return GuestInformationServiceClient{Endpoint: e}
}

func (c GuestInformationServiceClient) GetEndpoint() string {
	return c.Endpoint
}

func (c GuestInformationServiceClient) ReportStatus(statusToUpload string) (*http.Response, error) {
	if statusToUpload == "" {
		return nil, errors.New("provided status to upload is empty")
	}

	putStatusUri := fmt.Sprintf(constants.PutStatusFormatString, c.GetEndpoint())
	return ReportStatus(putStatusUri, statusToUpload)
}
