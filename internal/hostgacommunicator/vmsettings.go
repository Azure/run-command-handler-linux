package hostgacommunicator

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	requesthelper "github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	vmSettingsOperation = "immediateGoalState"
)

const (
	vmSettingsRequestTimeout = 30 * time.Second
)

type VMImmediateExtensionsGoalState struct {
	ImmediateExtensionGoalStates []ImmediateExtensionGoalState `json:"immediateExtensionsGoalStates"`
}

type ImmediateExtensionGoalState struct {
	Name     string                    `json:"name"`
	Settings []settings.SettingsCommon `json:"settings"`
}

// Struct used to wrap the url to use when making requests
type requestFactory struct {
	url string
}

// Returns a new RequestManager object useful to make GET Requests
func GetVMSettingsRequestManager(ctx *log.Context) (*requesthelper.RequestManager, error) {
	factory, err := newVMSettingsRequestFactory(ctx)
	if err != nil {
		return nil, vmextension.NewErrorWithClarification(constants.Hgap_FailedToCreateRequestFactory, errors.Wrapf(err, "failed to create request factory"))
	}

	return requesthelper.GetRequestManager(factory, vmSettingsRequestTimeout), nil
}

// Returns a new requestFactory object with the VMSettings API Uri set
func newVMSettingsRequestFactory(ctx *log.Context) (*requestFactory, error) {
	url, err := getOperationUri(ctx, vmSettingsOperation)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to obtain VMSettingsURI")
	}

	return &requestFactory{url}, nil
}

// GetRequest returns a new request with the provided url
func (u requestFactory) GetRequest(ctx *log.Context, eTag string) (*http.Request, error) {
	request, err := http.NewRequest("GET", u.url, nil)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create request to %v", u.url)
		return nil, vmextension.NewErrorWithClarification(constants.Hgap_FailedCreateRequest, errors.Wrap(err, errMsg))
	}

	if eTag != "" {
		request.Header.Set(constants.IfNoneMatchHeaderName, eTag)
	}
	return request, nil
}

func (goalState *ImmediateExtensionGoalState) ValidateSignature() (bool, error) {
	he, err := handlersettings.GetHandlerEnv()
	if err != nil {
		return false, err
	}

	configFolder := he.HandlerEnvironment.ConfigFolder
	// TODO: Check that certificate exists or download it if is missing
	// Do we need to re-download or can we assume the cert is already there?
	for _, s := range goalState.Settings {
		if s.ProtectedSettingsBase64 == "" {
			continue
		}

		if s.SettingsCertThumbprint == "" {
			return false, vmextension.NewErrorWithClarification(constants.Hgap_NoCertThumbprint, errors.New("HandlerSettings has protected settings but no cert thumbprint"))
		}

		// go two levels up where certs are placed (/var/lib/waagent)
		crt := filepath.Join(configFolder, "..", "..", fmt.Sprintf("%s.crt", s.SettingsCertThumbprint))
		prv := filepath.Join(configFolder, "..", "..", fmt.Sprintf("%s.prv", s.SettingsCertThumbprint))

		if !fileExists(crt) || !fileExists(prv) {
			message := fmt.Sprintf("Certificate %v needed by %v is missing from the goal state", s.SettingsCertThumbprint, s.ExtensionName)
			return false, vmextension.NewErrorWithClarification(constants.Hgap_CertificateMissingFromGoalState, errors.New(message))
		}
	}

	return true, nil
}

// Checks if the given file exists
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
