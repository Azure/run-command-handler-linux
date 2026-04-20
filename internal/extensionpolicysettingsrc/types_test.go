package extensionpolicysettingsrc

import (
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/stretchr/testify/require"
)

func TestTypeDefinitions_AreStable(t *testing.T) {
	t.Run("file type values are stable", func(t *testing.T) {
		require.Equal(t, "all", string(All))
		require.Equal(t, "none", string(NoFiles))
		require.Equal(t, "scripts", string(Scripts))
	})

	t.Run("allowed script flag values are stable", func(t *testing.T) {
		tests := []struct {
			name string
			got  AllowedScriptTypeFlag
			want AllowedScriptTypeFlag
		}{
			{name: "AllowedScriptNone", got: AllowedScriptNone, want: 0},
			{name: "AllowedCommandId", got: AllowedCommandId, want: 1},
			{name: "Gallery", got: Gallery, want: 2},
			{name: "Diagnostic", got: Diagnostic, want: 4},
			{name: "Inline", got: Inline, want: 8},
			{name: "AllowedDownloaded", got: AllowedDownloaded, want: 16},
			{name: "AllowAll", got: AllowAll, want: 31},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				require.Equal(t, tt.want, tt.got)
			})
		}
	})

	t.Run("script type values are stable", func(t *testing.T) {
		tests := []struct {
			name string
			got  handlersettings.ScriptType
			want handlersettings.ScriptType
		}{
			{name: "InlineScript", got: handlersettings.InlineScript, want: "inline"},
			{name: "DownloadedScript", got: handlersettings.DownloadedScript, want: "downloaded"},
			{name: "GalleryScript", got: handlersettings.GalleryScript, want: "gallery"},
			{name: "DiagnosticScript", got: handlersettings.DiagnosticScript, want: "diagnostic"},
			{name: "CommandIdScript", got: handlersettings.CommandIdScript, want: "commandId"},
			{name: "NoneScript", got: handlersettings.NoneScript, want: "none"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				require.Equal(t, tt.want, tt.got)
			})
		}
	})
}

func TestStringToAllowedScriptTypeFlag(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    AllowedScriptTypeFlag
		wantErr string
	}{
		{
			name:  "inline",
			input: "inline",
			want:  Inline,
		},
		{
			name:  "inline, gallery",
			input: "inline,gallery",
			want:  Inline | Gallery,
		},
		{
			name:  "allowed command ID, gallery, inline",
			input: "allowedcommandid,gallery,inline",
			want:  AllowedCommandId | Gallery | Inline,
		},
		{
			name:  "all explicit types",
			input: "alloweddownloaded,allowedcommandid,diagnostic,inline,gallery",
			want:  AllowedDownloaded | AllowedCommandId | Diagnostic | Inline | Gallery,
		},
		{
			name:  "allow all",
			input: "allowall",
			want:  AllowAll,
		},
		{
			name:  "whitespace and capitalization test",
			input: "  InLiNe , GALLERY , allowedCommandId  ",
			want:  Inline | Gallery | AllowedCommandId,
		},
		{
			name:    "unknown string",
			input:   "inline,banana",
			wantErr: "Unknown script type in policy: banana",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := StringToAllowedScriptTypeFlag(tt.input)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Equal(t, tt.wantErr, err.Error())
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidateFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   RCv2ExtensionPolicySettings
		wantErr string
	}{
		{
			name: "valid policy",
			input: RCv2ExtensionPolicySettings{
				LimitScripts:               "alloweddownloaded,allowedcommandid,diagnostic,inline,gallery",
				DownloadedScriptsAllowlist: []string{"hash1"},
				CommandIdAllowlist:         []string{"cmd1"},
				RunAsUser:                  "alice",
				DisableOutputBlobs:         true,
			},
		},
		{
			name: "invalid value for limit scripts",
			input: RCv2ExtensionPolicySettings{
				LimitScripts: "inline,notARealType",
			},
			wantErr: "at least one of the values in LimitScripts is not a valid script type: inline,notARealType",
		},
		{
			name: "downloaded allowlist present, but downloaded scripts are blocked",
			input: RCv2ExtensionPolicySettings{
				LimitScripts:               "inline,gallery",
				DownloadedScriptsAllowlist: []string{"hash1"},
			},
			wantErr: "DownloadedScriptsAllowlist not empty, but LimitScripts does not allow 'downloaded' scripts",
		},
		{
			name: "command ID allowlist present, but command IDs are blocked",
			input: RCv2ExtensionPolicySettings{
				LimitScripts:       "inline,gallery",
				CommandIdAllowlist: []string{"cmd1"},
			},
			wantErr: "CommandIdAllowlist not empty, but LimitScripts does not allow 'commandId' scripts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.ValidateFormat()

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Equal(t, tt.wantErr, err.Error())
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestCompareScriptTypeToAllowedScriptType(t *testing.T) {
	tests := []struct {
		name       string
		scriptType handlersettings.ScriptType
		allowed    AllowedScriptTypeFlag
		wantErr    string
	}{
		{
			name:       "none allowed, gallery denied",
			scriptType: handlersettings.GalleryScript,
			allowed:    AllowedScriptNone,
			wantErr:    "gallery scripts are not allowed by policy",
		},
		{
			name:       "allow all, allow inline",
			scriptType: handlersettings.InlineScript,
			allowed:    AllowAll,
		},
		{
			name:       "allow all, allow downloaded",
			scriptType: handlersettings.DownloadedScript,
			allowed:    AllowAll,
		},
		{
			name:       "allow all, allow gallery",
			scriptType: handlersettings.GalleryScript,
			allowed:    AllowAll,
		},
		{
			name:       "allow all, allow diagnostic",
			scriptType: handlersettings.DiagnosticScript,
			allowed:    AllowAll,
		},
		{
			name:       "allow all, allow command id",
			scriptType: handlersettings.CommandIdScript,
			allowed:    AllowAll,
		},
		{
			name:       "diagnostic only, inline denied",
			scriptType: handlersettings.InlineScript,
			allowed:    Diagnostic,
			wantErr:    "inline scripts are not allowed by policy",
		},
		{
			name:       "allowed downloaded, allow downloaded",
			scriptType: handlersettings.DownloadedScript,
			allowed:    AllowedDownloaded,
		},
		{
			name:       "unknown script type",
			scriptType: handlersettings.ScriptType("made-up"),
			allowed:    AllowAll,
			wantErr:    "unknown script type: made-up",
		},
		{
			name:       "'none' script currently treated as unknown",
			scriptType: handlersettings.NoneScript,
			allowed:    AllowAll,
			wantErr:    "unknown script type: none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CompareScriptTypeToAllowedScriptType(tt.scriptType, tt.allowed)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Equal(t, tt.wantErr, err.Error())
				return
			}

			require.NoError(t, err)
		})
	}
}
