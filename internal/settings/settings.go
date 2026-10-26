package settings

import (
	"encoding/json"
	"fmt"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/pkg/errors"
)

type SettingsCommon struct {
	PublicSettingsRaw       interface{}            `json:"publicSettings"`
	PublicSettings          map[string]interface{} `json:"-"`
	ProtectedSettingsBase64 string                 `json:"protectedSettings"`
	SettingsCertThumbprint  string                 `json:"protectedSettingsCertThumbprint"`
	SeqNo                   *int                   `json:"seqNo"`
	ExtensionName           *string                `json:"extensionName"`
	ExtensionState          *string                `json:"extensionState"`
}

func (li *SettingsCommon) UnmarshalJSON(data []byte) error {
	type localItem SettingsCommon
	var loc localItem
	if err := json.Unmarshal(data, &loc); err != nil {
		return errorutil.WithCode(errorutil.CustomerInput_invalidSettingsJson, err)
	}
	*li = SettingsCommon(loc)

	switch li.PublicSettingsRaw.(type) {
	case string:
		// When a string type is found, we need to attempt an extra parsing step.
		// This is needed to handle the response from the VMSettings API for immediate run command
		publicSettingsRawString := li.PublicSettingsRaw.(string)
		var publicSettings map[string]interface{}
		if publicSettingsRawString != "" {
			if err := json.Unmarshal([]byte(publicSettingsRawString), &publicSettings); err != nil {
				return errorutil.WithCode(errorutil.CustomerInput_invalidPublicSettingsJson, errors.Wrapf(err, "failed to parse public settings from json"))
			}
		}

		li.PublicSettings = publicSettings
	case map[string]interface{}:
		// This covers the scenario to parse the settings for a normal run command request
		li.PublicSettings = li.PublicSettingsRaw.(map[string]interface{})
	case nil:
	default:
		return errorutil.WithCode(errorutil.CustomerInput_publicSettingsNotObject, fmt.Errorf("public settings must be an object or JSON object string"))
	}

	return nil
}

func (li SettingsCommon) MarshalJSON() ([]byte, error) {
	type SettingsCommonHelper SettingsCommon
	s := SettingsCommonHelper(li)
	publicSettingsRaw, err := json.Marshal(s.PublicSettings)
	if err != nil {
		return []byte{}, errorutil.WithCode(errorutil.Internal_failedToMarshalPublicSettings, errors.Wrap(err, "could not marshal public settings"))
	}
	s.PublicSettingsRaw = string(publicSettingsRaw)
	b, err := json.Marshal(s)
	return b, errorutil.WithCode(errorutil.Internal_failedToMarshalSettings, errors.Wrap(err, "could not marshal settings"))
}
