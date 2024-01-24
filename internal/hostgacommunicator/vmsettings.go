package hostgacommunicator

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	requesthelper "github.com/Azure/run-command-handler-linux/internal/request"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	// TODO: Change the relative path to use the upcoming immediateVMSettings and immediateExtensionStatus APIs
	vmSettingsOperation = "vmSettings"
)

var (
	vmSettingsRequestTimeout = 30 * time.Second
)

type ExtensionGoalStates struct {
	Name                string                    `json:"name"`
	Version             string                    `json:"version"`
	Location            string                    `json:"location"`
	Failoverlocation    string                    `json:"failoverlocation"`
	AdditionalLocations []string                  `json:"additionalLocations"`
	State               string                    `json:"state"`
	AutoUpgrade         bool                      `json:"autoUpgrade"`
	RunAsStartupTask    bool                      `json:"runAsStartupTask"`
	IsJson              bool                      `json:"isJson"`
	UpgradeGuid         string                    `json:"upgradeGuid"`
	UseExactVersion     bool                      `json:"useExactVersion"`
	SettingsSeqNo       int                       `json:"settingsSeqNo"`
	ExtensionName       string                    `json:"extensionName"`
	IsMultiConfig       bool                      `json:"isMultiConfig"`
	Settings            []settings.SettingsCommon `json:"settings"`
}

type VMSettings struct {
	HostGAPluginVersion       string                `json:"hostGAPluginVersion"`
	VmSettingsSchemaVersion   string                `json:"vmSettingsSchemaVersion"`
	ActivityId                string                `json:"activityId"`
	CorrelationId             string                `json:"correlationId"`
	ExtensionGoalStatesSource string                `json:"extensionGoalStatesSource"`
	ExtensionGoalStates       []ExtensionGoalStates `json:"extensionGoalStates"`
}

// Struct used to wrap the url to use when making requests
type requestFactory struct {
	url string
}

// Returns a new RequestManager object useful to make GET Requests
func getVMSettingsRequestManager(ctx *log.Context) (*requesthelper.RequestManager, error) {
	factory, err := newVMSettingsRequestFactory(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create request factory")
	}

	return requesthelper.GetRequestManager(factory, vmSettingsRequestTimeout), nil
}

// Returns a new requestFactory object with the VMSettings API Uri set
func newVMSettingsRequestFactory(ctx *log.Context) (*requestFactory, error) {
	ctx.Log("message", "trying to create new request factory")
	url, err := getOperationUri(ctx, vmSettingsOperation)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to obtain VMSettingsURI")
	}

	return &requestFactory{url}, nil
}

// GetRequest returns a new request with the provided url
func (u requestFactory) GetRequest(ctx *log.Context) (*http.Request, error) {
	ctx.Log("message", fmt.Sprintf("performing make request to %v", u.url))
	return http.NewRequest("GET", u.url, nil)
}

func (goalState *ExtensionGoalStates) ValidateSignature() (bool, error) {
	he, err := handlersettings.GetHandlerEnv()
	if err != nil {
		return false, errors.Wrap(err, "failed to parse handlerenv")
	}

	configFolder := he.HandlerEnvironment.ConfigFolder
	// TODO: Check that certificate exists or download it if is missing
	// Do we need to re-download or can we assume the cert is already there?
	for _, s := range goalState.Settings {
		if s.ProtectedSettingsBase64 == "" {
			continue
		}

		if s.SettingsCertThumbprint == "" {
			return false, errors.New("HandlerSettings has protected settings but no cert thumbprint")
		}

		// go two levels up where certs are placed (/var/lib/waagent)
		crt := filepath.Join(configFolder, "..", "..", fmt.Sprintf("%s.crt", s.SettingsCertThumbprint))
		prv := filepath.Join(configFolder, "..", "..", fmt.Sprintf("%s.prv", s.SettingsCertThumbprint))

		if !fileExists(crt) || !fileExists(prv) {
			message := fmt.Sprintf("Certificate %v needed by %v is missing from the goal state", s.SettingsCertThumbprint, s.ExtensionName)
			return false, errors.New(message)
		}
	}

	return true, nil
}

// Checks if the given file exists
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
