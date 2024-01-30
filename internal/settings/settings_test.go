package settings

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

var jsonVMSettingsAPI = `{
    "protectedSettingsCertThumbprint": "B73871D921632FC885F6685A8A5F943856518397",
    "protectedSettings": "SomeProtectedSettingsInBase64",
    "publicSettings": "{\"fileUris\":[\"https://sappe.infra.windows365.microsoft.com/sh01s-prov/prov/0.1.6-EF7EF39EEBEFBA101E5CB6544EA76546D7419D4AA7EB4FC535F931BECAD7E49F/IntuneEnroll.ps1?skoid=63b965a4-8844-4ebe-ba15-cf3af20c4819&sktid=cdc5aeea-15c5-4db6-b079-fcadd2505dc2&skt=2023-06-26T05%3A09%3A12Z&ske=2023-07-02T05%3A09%3A12Z&sks=b&skv=2021-08-06&sv=2021-08-06&se=2023-06-29T21%3A04%3A20Z&sr=b&sp=r&sig=AyMW7n2BI%2BhFcyBIraWeWUYu4mnIu%2B0CWotL6oZzJ3g%3D\"]}"
}`

var jsonBadFormatVMSettingsAPI = `{
    \"protectedSettingsCertThumbprint\": \"B73871D921632FC885F6685A8A5F943856518397\",
    "protectedSettings": "SomeProtectedSettingsInBase64",
    "publicSettings": "{\"fileUris\":[\"https://sappe.infra.windows365.microsoft.com/sh01s-prov/prov/0.1.6-EF7EF39EEBEFBA101E5CB6544EA76546D7419D4AA7EB4FC535F931BECAD7E49F/IntuneEnroll.ps1?skoid=63b965a4-8844-4ebe-ba15-cf3af20c4819&sktid=cdc5aeea-15c5-4db6-b079-fcadd2505dc2&skt=2023-06-26T05%3A09%3A12Z&ske=2023-07-02T05%3A09%3A12Z&sks=b&skv=2021-08-06&sv=2021-08-06&se=2023-06-29T21%3A04%3A20Z&sr=b&sp=r&sig=AyMW7n2BI%2BhFcyBIraWeWUYu4mnIu%2B0CWotL6oZzJ3g%3D\"]}"
}`

var jsonFromLocalVMForStandardRC = `{
	"extensionName": "testExtension",
    "publicSettings": {
        "source": {
            "script": "echo Hello World!"
        },
        "runAsUser": ""
    },
    "protectedSettings": "SomeProtectedSettingsInBase64",
    "protectedSettingsCertThumbprint": "SomeCertificateThumbprint"
}`

func Test_UnmarshalSettingsFromVMSettingsAPI(t *testing.T) {
	var settings SettingsCommon
	err := json.Unmarshal([]byte(jsonVMSettingsAPI), &settings)
	require.Nil(t, err)
	require.NotNil(t, settings.PublicSettings)
	require.NotNil(t, settings.ProtectedSettingsBase64)
	require.NotNil(t, settings.SettingsCertThumbprint)
}

func Test_UnmarshalSettingsFromStandardRunCommand(t *testing.T) {
	var settings SettingsCommon
	err := json.Unmarshal([]byte(jsonFromLocalVMForStandardRC), &settings)
	require.Nil(t, err)
	require.NotNil(t, settings.PublicSettings)
	require.NotNil(t, settings.ProtectedSettingsBase64)
	require.NotNil(t, settings.SettingsCertThumbprint)
}

func Test_FailedToUnmarshalSettingsFromVMSettingsAPI(t *testing.T) {
	var settings SettingsCommon
	err := json.Unmarshal([]byte(jsonBadFormatVMSettingsAPI), &settings)
	require.ErrorContains(t, err, "invalid character")
}

func Test_MarshalVMSettingAPI(t *testing.T) {
	extName := "testExtension"
	settings := SettingsCommon{
		ExtensionName:           &extName,
		ProtectedSettingsBase64: "SomeProtectedSettingsInBase64",
		SettingsCertThumbprint:  "SomeProtectedSettingsInBase64",
		PublicSettings: map[string]interface{}{
			"source": map[string]interface{}{
				"script": "echo Hello World!",
			},
			"runAsUser": "",
		},
	}

	r, err := json.Marshal(settings)
	json := string(r)
	require.Nil(t, err)
	require.Contains(t, json, *settings.ExtensionName)
	require.Contains(t, json, settings.ProtectedSettingsBase64)
	require.Contains(t, json, settings.SettingsCertThumbprint)
	require.Contains(t, json, "publicSettings", "missing public settings field. This gets exported via PublicSettingsRaw.")
	require.Contains(t, json, "echo Hello World!")
}
