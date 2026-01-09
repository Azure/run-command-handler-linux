package hostgacommunicator

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	hostGaPluginPort = "32526"
)

var (
	WireServerFallbackAddress = "http://168.63.129.16:32526"
)

// test seam
var withRetriesFn = requesthelper.WithRetries

type ResponseData struct {
	VMSettings *VMImmediateExtensionsGoalState
	ETag       string
	Modified   bool
}

// Interface for operations available when communicating with HostGAPlugin
type IHostGACommunicator interface {
	GetImmediateVMSettings(ctx *log.Context, eTag string) (*ResponseData, *vmextension.ErrorWithClarification)
}

// HostGaCommunicator provides methods for retrieving VMSettings from the HostGAPlugin
type HostGACommunicator struct {
	vmRequestManager IVMSettingsRequestManager
}

func NewHostGACommunicator(requestManager IVMSettingsRequestManager) HostGACommunicator {
	return HostGACommunicator{vmRequestManager: requestManager}
}

type IVMSettingsRequestManager interface {
	GetVMSettingsRequestManager(ctx *log.Context) (*requesthelper.RequestManager, *vmextension.ErrorWithClarification)
}

// GetVMSettings returns the VMSettings for the current machine
func (c *HostGACommunicator) GetImmediateVMSettings(ctx *log.Context, eTag string) (*ResponseData, *vmextension.ErrorWithClarification) {
	requestManager, ewc := c.vmRequestManager.GetVMSettingsRequestManager(ctx)
	if ewc != nil {
		return nil, vmextension.CreateWrappedErrorWithClarification(ewc, "could not create the request manager to get immediate VMsettings")
	}

	resp, err := withRetriesFn(ctx, requestManager, requesthelper.ActualSleep, eTag)
	if err != nil {
		return nil, vmextension.CreateWrappedErrorWithClarification(err, "request to retrieve VMSettings failed with retries.")
	}

	// If the response is 304 Not Modified or 404 Not Found, return nil VMSettings as there are not new goal states to process
	if resp.StatusCode == http.StatusNotModified || resp.StatusCode == http.StatusNotFound {
		return &ResponseData{VMSettings: nil, ETag: eTag, Modified: false}, nil
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	var vmSettings VMImmediateExtensionsGoalState
	if err := json.Unmarshal(body, &vmSettings); err != nil {
		return nil, vmextension.NewErrorWithClarificationPtr(constants.Hgap_FailedToParseImmediateSettings, errors.Wrapf(err, "failed to parse immediate VMSettings json"))
	}

	newETag := resp.Header.Get(constants.ETagHeaderName)
	if newETag == "" {
		return nil, vmextension.NewErrorWithClarificationPtr(constants.Hgap_EtagNotFound, errors.New("ETag not found in response header when retrieving immediate VMSettings"))
	}

	return &ResponseData{VMSettings: &vmSettings, ETag: newETag, Modified: eTag != newETag}, nil
}

// Gets the URI to use to call the given operation name
func getOperationUri(ctx *log.Context, operationName string) (string, *vmextension.ErrorWithClarification) {
	// TODO: investigate why other extensions use the env var AZURE_GUEST_AGENT_WIRE_PROTOCOL_ADDRESS
	// and decide if we want to add that wire protocol address as a potential endpoint to use when provided
	uri, err := url.Parse(WireServerFallbackAddress)
	if err != nil {
		return "", vmextension.NewErrorWithClarificationPtr(constants.Hgap_FailedToParseAddress, errors.Wrap(err, "could not parse address "+WireServerFallbackAddress))
	}
	uri.Path = operationName
	return uri.String(), nil
}
