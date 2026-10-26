package settings

import (
	"encoding/json"
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

func TestSettingsClarifications(t *testing.T) {
	for _, tc := range []struct {
		input string
		code  int
	}{
		{`{"seqNo":"invalid"}`, errorutil.CustomerInput_invalidSettingsJson},
		{`{"publicSettings":"{"}`, errorutil.CustomerInput_invalidPublicSettingsJson},
		{`{"publicSettings":true}`, errorutil.CustomerInput_publicSettingsNotObject},
		{`{"publicSettings":[]}`, errorutil.CustomerInput_publicSettingsNotObject},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var settings SettingsCommon
			err := json.Unmarshal([]byte(tc.input), &settings)
			code, ok := errorutil.Code(err)
			require.True(t, ok)
			require.Equal(t, tc.code, code)
		})
	}
	_, err := (SettingsCommon{PublicSettings: map[string]interface{}{"bad": make(chan int)}}).MarshalJSON()
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.Internal_failedToMarshalPublicSettings, code)
	for _, input := range []string{`{}`, `{"publicSettings":null}`, `{"publicSettings":{}}`, `{"publicSettings":"{}"}`} {
		var settings SettingsCommon
		require.NoError(t, json.Unmarshal([]byte(input), &settings))
	}
	// errorutil.Internal_failedToMarshalSettings cannot currently fail: the only interface field in the helper is
	// replaced with a string after the independently tested errorutil.Internal_failedToMarshalPublicSettings marshal.
}
