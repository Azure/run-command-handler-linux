package handlersettings

import (
	"errors"
	"testing"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/stretchr/testify/require"
)

func TestReadArtifacts_BothNil_ReturnsNilNil(t *testing.T) {
	var s HandlerSettings
	s.PublicSettings.Artifacts = nil
	s.ProtectedSettings.Artifacts = nil

	got, err := s.ReadArtifacts()
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestReadArtifacts_CountMismatch_ReturnsArtifactCountMismatch(t *testing.T) {
	s := HandlerSettings{
		PublicSettings: PublicSettings{
			Artifacts: []PublicArtifactSource{
				{ArtifactId: 1, ArtifactUri: "https://example/1", FileName: "a.bin"},
			},
		},
		ProtectedSettings: ProtectedSettings{
			Artifacts: []ProtectedArtifactSource{
				{ArtifactId: 1, ArtifactSasToken: "?sig=1"},
				{ArtifactId: 2, ArtifactSasToken: "?sig=2"},
			},
		},
	}

	_, err := s.ReadArtifacts()
	VerifyErrorClarification(t, constants.Internal_ArtifactCountMismatch, err)
}

func TestReadArtifacts_HappyPath_MatchesById_AndPreservesPublicOrder(t *testing.T) {
	mi1 := &RunCommandManagedIdentity{ClientId: "client-1"}
	mi2 := &RunCommandManagedIdentity{ObjectId: "obj-2"}

	s := HandlerSettings{
		PublicSettings: PublicSettings{
			Artifacts: []PublicArtifactSource{
				{ArtifactId: 10, ArtifactUri: "https://storage/foo", FileName: "foo.txt"},
				{ArtifactId: 20, ArtifactUri: "https://storage/bar", FileName: "bar.txt"},
			},
		},
		ProtectedSettings: ProtectedSettings{
			// Protected list intentionally out of order to ensure matching is by ID, not index.
			Artifacts: []ProtectedArtifactSource{
				{ArtifactId: 20, ArtifactSasToken: "?sig=bar", ArtifactManagedIdentity: mi2},
				{ArtifactId: 10, ArtifactSasToken: "?sig=foo", ArtifactManagedIdentity: mi1},
			},
		},
	}

	got, err := s.ReadArtifacts()
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Must be in the same order as PublicSettings.Artifacts.
	require.Equal(t, 10, got[0].ArtifactId)
	require.Equal(t, "https://storage/foo", got[0].ArtifactUri)
	require.Equal(t, "foo.txt", got[0].FileName)
	require.Equal(t, "?sig=foo", got[0].ArtifactSasToken)
	require.Same(t, mi1, got[0].ArtifactManagedIdentity)

	require.Equal(t, 20, got[1].ArtifactId)
	require.Equal(t, "https://storage/bar", got[1].ArtifactUri)
	require.Equal(t, "bar.txt", got[1].FileName)
	require.Equal(t, "?sig=bar", got[1].ArtifactSasToken)
	require.Same(t, mi2, got[1].ArtifactManagedIdentity)
}

func TestReadArtifacts_MissingProtectedMatch_ReturnsInvalidArtifactSpecification(t *testing.T) {
	s := HandlerSettings{
		PublicSettings: PublicSettings{
			Artifacts: []PublicArtifactSource{
				{ArtifactId: 1, ArtifactUri: "https://storage/a", FileName: "a"},
				{ArtifactId: 2, ArtifactUri: "https://storage/b", FileName: "b"},
			},
		},
		ProtectedSettings: ProtectedSettings{
			Artifacts: []ProtectedArtifactSource{
				{ArtifactId: 1, ArtifactSasToken: "?sig=a"},
				{ArtifactId: 45, ArtifactSasToken: "?sig=b"},
			},
		},
	}

	_, err := s.ReadArtifacts()
	VerifyErrorClarification(t, constants.Internal_InvalidArtifactSpecification, err)
}

func VerifyErrorClarification(t *testing.T, expectedCode int, err error) {
	require.NotNil(t, err, "No error returned when one was expected")
	var ewc vmextension.ErrorWithClarification
	require.True(t, errors.As(err, &ewc), "Error is not of type ErrorWithClarification")
	require.Equal(t, expectedCode, ewc.ErrorCode, "Expected error %d but received %d", expectedCode, ewc.ErrorCode)
}
