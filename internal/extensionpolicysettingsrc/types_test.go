package extensionpolicysettingsrc

import (
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
)

func TestTypeDefinitions_AreStable(t *testing.T) {
	t.Run("file type values are stable", func(t *testing.T) {
		if got := string(All); got != "all" {
			t.Fatalf("All = %q, want %q", got, "all")
		}
		if got := string(NoFiles); got != "none" {
			t.Fatalf("NoFiles = %q, want %q", got, "none")
		}
		if got := string(Scripts); got != "scripts" {
			t.Fatalf("Scripts = %q, want %q", got, "scripts")
		}
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
			if tt.got != tt.want {
				t.Fatalf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
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
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
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
			name:  "inline plus gallery",
			input: "inline,gallery",
			want:  Inline | Gallery,
		},
		{
			name:  "allowed command id plus gallery plus inline",
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
			name:  "whitespace and capitalization",
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
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
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
			name: "invalid limit scripts value",
			input: RCv2ExtensionPolicySettings{
				LimitScripts: "inline,notARealType",
			},
			wantErr: "at least one of the values in LimitScripts is not a valid script type: inline,notARealType",
		},
		{
			name: "downloaded allowlist present but downloaded blocked",
			input: RCv2ExtensionPolicySettings{
				LimitScripts:               "inline,gallery",
				DownloadedScriptsAllowlist: []string{"hash1"},
			},
			wantErr: "DownloadedScriptsAllowlist not empty, but LimitScripts does not allow 'downloaded' scripts",
		},
		{
			name: "command id allowlist present but command id blocked",
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
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
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
		// {
		// 	name:       "none allowed gallery denied",
		// 	scriptType: handlersettings.GalleryScript,
		// 	allowed:    AllowedScriptNone,
		// 	wantErr:    "gallery scripts are not allowed by policy",
		// },
		{
			name:       "allow all inline",
			scriptType: handlersettings.InlineScript,
			allowed:    AllowAll,
		},
		{
			name:       "allow all downloaded",
			scriptType: handlersettings.DownloadedScript,
			allowed:    AllowAll,
		},
		{
			name:       "allow all gallery",
			scriptType: handlersettings.GalleryScript,
			allowed:    AllowAll,
		},
		{
			name:       "allow all diagnostic",
			scriptType: handlersettings.DiagnosticScript,
			allowed:    AllowAll,
		},
		{
			name:       "allow all command id",
			scriptType: handlersettings.CommandIdScript,
			allowed:    AllowAll,
		},
		{
			name:       "diagnostic only inline denied",
			scriptType: handlersettings.InlineScript,
			allowed:    Diagnostic,
			wantErr:    "inline scripts are not allowed by policy",
		},
		{
			name:       "allowed downloaded permits downloaded",
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
			name:       "none script currently treated as unknown",
			scriptType: handlersettings.NoneScript,
			allowed:    AllowAll,
			wantErr:    "unknown script type: none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CompareScriptTypeToAllowedScriptType(tt.scriptType, tt.allowed)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
