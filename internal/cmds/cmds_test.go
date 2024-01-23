package commands

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/files"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/ahmetalpbalkan/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_commandsExist(t *testing.T) {
	// we expect these subcommands to be handled
	expect := []string{"install", "enable", "disable", "uninstall", "update"}
	for _, c := range expect {
		_, ok := Cmds[c]
		if !ok {
			t.Fatalf("cmd '%s' is not handled", c)
		}
	}
}

func Test_commands_shouldReportStatus(t *testing.T) {
	// - certain extension invocations are supposed to write 'N.status' files and some do not.

	// these subcommands should NOT report status
	require.False(t, Cmds["install"].ShouldReportStatus, "install should not report status")
	require.False(t, Cmds["uninstall"].ShouldReportStatus, "uninstall should not report status")

	// these subcommands SHOULD report status
	require.True(t, Cmds["enable"].ShouldReportStatus, "enable should report status")
	require.True(t, Cmds["disable"].ShouldReportStatus, "disable should report status")
	require.True(t, Cmds["update"].ShouldReportStatus, "update should report status")
}

func Test_checkAndSaveSeqNum_fails(t *testing.T) {
	// pass in invalid seqnum format
	_, err := checkAndSaveSeqNum(log.NewNopLogger(), 0, "/non/existing/dir")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), `failed to save sequence number`)
}

func Test_checkAndSaveSeqNum(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	fp := filepath.Join(dir, "seqnum")
	defer os.RemoveAll(dir)

	nop := log.NewNopLogger()

	// no sequence number, 0 comes in.
	shouldExit, err := checkAndSaveSeqNum(nop, 0, fp)
	require.Nil(t, err)
	require.False(t, shouldExit)

	// file=0, seq=0 comes in. (should exit)
	shouldExit, err = checkAndSaveSeqNum(nop, 0, fp)
	require.Nil(t, err)
	require.True(t, shouldExit)

	// file=0, seq=1 comes in.
	shouldExit, err = checkAndSaveSeqNum(nop, 1, fp)
	require.Nil(t, err)
	require.False(t, shouldExit)

	// file=1, seq=1 comes in. (should exit)
	shouldExit, err = checkAndSaveSeqNum(nop, 1, fp)
	require.Nil(t, err)
	require.True(t, shouldExit)

	// file=1, seq=0 comes in. (should exit)
	shouldExit, err = checkAndSaveSeqNum(nop, 1, fp)
	require.Nil(t, err)
	require.True(t, shouldExit)
}

func Test_runCmd_success(t *testing.T) {
	var script = "date"
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	metadata := types.NewRCMetadata("extName", 0)
	err, exitCode := runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{Source: &handlersettings.ScriptSource{Script: script}},
	}, metadata)
	require.Nil(t, err, "command should run successfully")
	require.Equal(t, constants.ExitCode_Okay, exitCode)

	// check stdout stderr files
	_, err = os.Stat(filepath.Join(dir, "stdout"))
	require.Nil(t, err, "stdout should exist")
	_, err = os.Stat(filepath.Join(dir, "stderr"))
	require.Nil(t, err, "stderr should exist")

	// Check embedded script if saved to file
	_, err = os.Stat(filepath.Join(dir, "script.sh"))
	require.Nil(t, err, "script.sh should exist")
	content, err := ioutil.ReadFile(filepath.Join(dir, "script.sh"))
	require.Nil(t, err, "script.sh read failure")
	require.Equal(t, script, string(content))
}

func Test_runCmd_fail(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	metadata := types.NewRCMetadata("extName", 0)
	err, exitCode := runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{Source: &handlersettings.ScriptSource{Script: "non-existing-cmd"}},
	}, metadata)
	require.NotNil(t, err, "command terminated with exit status")
	require.Contains(t, err.Error(), "failed to execute command")
	require.NotEqual(t, constants.ExitCode_Okay, exitCode)
}

func Test_downloadScriptUri(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	downloadedFilePath, err := downloadScript(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
			},
		})
	require.Nil(t, err)

	// check the downloaded file
	fp := filepath.Join(dir, "10")
	require.Equal(t, fp, downloadedFilePath)
	_, err = os.Stat(fp)
	require.Nil(t, err, "%s is missing from download dir", fp)
}

func Test_downloadArtifacts_Invalid(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	// The count of public vs protected settings differs
	err = downloadArtifacts(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
				Artifacts: []handlersettings.PublicArtifactSource{
					{
						ArtifactId:  1,
						ArtifactUri: srv.URL + "/status/404",
						FileName:    "flipper",
					},
				},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				Artifacts: []handlersettings.ProtectedArtifactSource{},
			},
		})

	require.NotNil(t, err)
	require.Contains(t, err.Error(), "RunCommand artifact download failed. Reason: Invalid artifact specification. This is a product bug.")

	// ArtifactIds don't match
	err = downloadArtifacts(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
				Artifacts: []handlersettings.PublicArtifactSource{
					{
						ArtifactId:  1,
						ArtifactUri: srv.URL + "/status/404",
						FileName:    "flipper",
					},
				},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				Artifacts: []handlersettings.ProtectedArtifactSource{
					{
						ArtifactId: 2,
					},
				},
			},
		})

	require.NotNil(t, err)
	require.Contains(t, err.Error(), "RunCommand artifact download failed. Reason: Invalid artifact specification. This is a product bug.")
}

func Test_downloadArtifactsFail(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	err = downloadArtifacts(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
				Artifacts: []handlersettings.PublicArtifactSource{
					{
						ArtifactId:  1,
						ArtifactUri: srv.URL + "/status/404",
						FileName:    "flipper",
					},
				},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				Artifacts: []handlersettings.ProtectedArtifactSource{
					{
						ArtifactId: 1,
					},
				},
			},
		})

	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to download artifact")
}

func Test_downloadArtifacts(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	err = downloadArtifacts(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/bytes/10"},
				Artifacts: []handlersettings.PublicArtifactSource{
					{
						ArtifactId:  1,
						ArtifactUri: srv.URL + "/bytes/255",
						FileName:    "flipper",
					},
					{
						ArtifactId:  2,
						ArtifactUri: srv.URL + "/bytes/256",
					},
				},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				Artifacts: []handlersettings.ProtectedArtifactSource{
					{
						ArtifactId: 1,
					},
					{
						ArtifactId: 2,
					},
				},
			},
		})
	require.Nil(t, err)

	// check the downloaded files
	fp := filepath.Join(dir, "flipper")
	_, err = os.Stat(fp)
	require.Nil(t, err, "%s is missing from download dir", fp)

	fp = filepath.Join(dir, "Artifact2")
	_, err = os.Stat(fp)
	require.Nil(t, err, "%s is missing from download dir", fp)
}

func Test_decodeScript(t *testing.T) {
	testSubject := "bHMK"
	s, info, err := decodeScript(testSubject)

	require.NoError(t, err)
	require.Equal(t, info, "4;3;gzip=0")
	require.Equal(t, s, "ls\n")
}

func Test_decodeScriptGzip(t *testing.T) {
	testSubject := "H4sIACD731kAA8sp5gIAfShLWgMAAAA="
	s, info, err := decodeScript(testSubject)

	require.NoError(t, err)
	require.Equal(t, info, "32;3;gzip=1")
	require.Equal(t, s, "ls\n")
}

func Test_downloadScriptUri_BySASFailsSucceedsByManagedIdentity(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	files.UseMockSASDownloadFailure = true
	handler := func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.RequestURI, "/samplecontainer/sample.sh?SASToken") {
			writer.WriteHeader(http.StatusOK) // Download successful using managed identity
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	_, err = downloadScript(log.NewContext(log.NewNopLogger()),
		dir,
		&handlersettings.HandlerSettings{
			PublicSettings: handlersettings.PublicSettings{
				Source: &handlersettings.ScriptSource{ScriptURI: srv.URL + "/samplecontainer/sample.sh?SASToken"},
			},
			ProtectedSettings: handlersettings.ProtectedSettings{
				SourceSASToken: "SASToken",
				SourceManagedIdentity: &handlersettings.RunCommandManagedIdentity{
					ClientId: "00b64c6a-6dbf-41e0-8707-74132d5cf53f",
				},
			},
		})
	require.Nil(t, err)
	files.UseMockSASDownloadFailure = false
}

// This test just makes sure using TreatFailureAsDeploymentFailure flag, script is executed as expected.
// The interpretation of the result (Succeeded or Failed, when TreatFailureAsDeploymentFailure is true)
//
//	is done in main.go
func Test_TreatFailureAsDeploymentFailureIsTrue_Fails(t *testing.T) {
	var script = "ech HelloWorld" // ech is an unknown command. Sh returns error and 127 status code
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	metadata := types.NewRCMetadata("extName", 0)
	err, exitCode := runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{Source: &handlersettings.ScriptSource{Script: script}, TreatFailureAsDeploymentFailure: true},
	}, metadata)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to execute command: command terminated with exit status=127")
	require.NotEqual(t, constants.ExitCode_Okay, exitCode)
}

// This test just makes sure using TreatFailureAsDeploymentFailure flag, script is executed as expected.
// The interpretation of the result (Succeeded or Failed, when TreatFailureAsDeploymentFailure is true)
//
//	is done in main.go
func Test_TreatFailureAsDeploymentFailureIsTrue_SimpleScriptSucceeds(t *testing.T) {
	var script = "echo HelloWorld" // ech is an unknown command. Sh returns error and 127 status code
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	metadata := types.NewRCMetadata("extName", 0)
	err, exitCode := runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlersettings.HandlerSettings{
		PublicSettings: handlersettings.PublicSettings{Source: &handlersettings.ScriptSource{Script: script}, TreatFailureAsDeploymentFailure: false},
	}, metadata)
	require.Nil(t, err)
	require.Equal(t, constants.ExitCode_Okay, exitCode)
}
