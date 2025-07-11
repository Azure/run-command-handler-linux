package statusreporter

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/go-kit/kit/log"
)

type TestGuestInformationClient struct {
	Endpoint string
}

func (c TestGuestInformationClient) GetEndpoint() string {
	return c.Endpoint
}

func (c TestGuestInformationClient) GetPutStatusUri() string {
	return fmt.Sprintf(constants.PutImmediateStatusFormatString, c.GetEndpoint())
}

func (c TestGuestInformationClient) ReportStatus(ctx *log.Context, statusToUpload string) (*http.Response, error) {
	w := httptest.NewRecorder()
	resp := w.Result()
	resp.Request = httptest.NewRequest(http.MethodPut, c.Endpoint, nil)
	return resp, nil
}
