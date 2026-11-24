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
		{`{"seqNo":"invalid"}`, errorutil.InvalidSettingsJson},
		{`{"publicSettings":"{"}`, errorutil.InvalidPublicSettingsJson},
		{`{"publicSettings":true}`, errorutil.PublicSettingsNotObject},
		{`{"publicSettings":[]}`, errorutil.PublicSettingsNotObject},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var settings SettingsCommon
			err := json.Unmarshal([]byte(tc.input), &settings)
			code, ok := errorutil.FindClarification(err)
			require.True(t, ok)
			require.Equal(t, tc.code, code)
		})
	}
	_, err := (SettingsCommon{PublicSettings: map[string]interface{}{"bad": make(chan int)}}).MarshalJSON()
	code, ok := errorutil.FindClarification(err)
	require.True(t, ok)
	require.Equal(t, errorutil.FailedToMarshalPublicSettings, code)
	for _, input := range []string{`{}`, `{"publicSettings":null}`, `{"publicSettings":{}}`, `{"publicSettings":"{}"}`} {
		var settings SettingsCommon
		require.NoError(t, json.Unmarshal([]byte(input), &settings))
	}
	// errorutil.FailedToMarshalSettings cannot currently fail: the only interface field in the helper is
	// replaced with a string after the independently tested errorutil.FailedToMarshalPublicSettings marshal.
}
