package handlersettings

import (
	"strings"

	"github.com/pkg/errors"
)

// handlerSettings holds the configuration of the extension handler.
type HandlerSettings struct {
	PublicSettings
	ProtectedSettings
}

// Gets the InstallAsService field from the RunCommand's properties
func (s HandlerSettings) InstallAsService() bool {
	return s.PublicSettings.Source.InstallAsService || strings.Contains(s.Script(), "installAsService=true")
}

func (s HandlerSettings) Script() string {
	return s.PublicSettings.Source.Script
}

func (s HandlerSettings) ScriptURI() string {
	return s.PublicSettings.Source.ScriptURI
}

func (s HandlerSettings) ScriptSAS() string {
	return s.ProtectedSettings.SourceSASToken
}

func (s HandlerSettings) ReadArtifacts() ([]UnifiedArtifact, error) {
	if s.ProtectedSettings.Artifacts == nil && s.PublicSettings.Artifacts == nil {
		return nil, nil
	}

	if len(s.ProtectedSettings.Artifacts) != len(s.PublicSettings.Artifacts) {
		return nil, errors.New(("RunCommand artifact download failed. Reason: Invalid artifact specification. This is a product bug."))
	}

	artifacts := make([]UnifiedArtifact, len(s.PublicSettings.Artifacts))

	for i := 0; i < len(s.PublicSettings.Artifacts); i++ {
		publicArtifact := s.PublicSettings.Artifacts[i]
		found := false

		for k := 0; k < len(s.ProtectedSettings.Artifacts); k++ {
			protectedArtifact := s.ProtectedSettings.Artifacts[k]
			if publicArtifact.ArtifactId == protectedArtifact.ArtifactId {
				found = true
				artifacts[i] = UnifiedArtifact{
					ArtifactId:              publicArtifact.ArtifactId,
					ArtifactUri:             publicArtifact.ArtifactUri,
					ArtifactSasToken:        protectedArtifact.ArtifactSasToken,
					FileName:                publicArtifact.FileName,
					ArtifactManagedIdentity: protectedArtifact.ArtifactManagedIdentity,
				}
			}
		}

		if !found {
			return nil, errors.New(("RunCommand artifact download failed. Reason: Invalid artifact specification. This is a product bug."))
		}
	}

	return artifacts, nil
}

// validate makes logical validation on the handlerSettings which already passed
// the schema validation.
func (s HandlerSettings) validate() error {

	if s.PublicSettings.Source == nil || (s.PublicSettings.Source.Script == "") == (s.PublicSettings.Source.ScriptURI == "") {
		return errSourceNotSpecified
	}
	return nil
}

// PublicSettings is the type deserialized from public configuration section of
// the extension handler. This should be in sync with publicSettingsSchema.
type PublicSettings struct {
	Source                          *ScriptSource         `json:"source"`
	Parameters                      []ParameterDefinition `json:"parameters"`
	RunAsUser                       string                `json:"runAsUser"`
	OutputBlobURI                   string                `json:"outputBlobUri"`
	ErrorBlobURI                    string                `json:"errorBlobUri"`
	TimeoutInSeconds                int                   `json:"timeoutInSeconds,int"`
	AsyncExecution                  bool                  `json:"asyncExecution,bool"`
	TreatFailureAsDeploymentFailure bool                  `json:"treatFailureAsDeploymentFailure,bool"`

	// List of artifacts to download before running the script
	Artifacts []PublicArtifactSource `json:"artifacts"`
}

// ProtectedSettings is the type decoded and deserialized from protected
// configuration section. This should be in sync with protectedSettingsSchema.
type ProtectedSettings struct {
	RunAsPassword       string                `json:"runAsPassword"`
	SourceSASToken      string                `json:"sourceSASToken"`
	OutputBlobSASToken  string                `json:"outputBlobSASToken"`
	ErrorBlobSASToken   string                `json:"errorBlobSASToken"`
	ProtectedParameters []ParameterDefinition `json:"protectedParameters"`

	// List of artifacts to download before running the script
	Artifacts []ProtectedArtifactSource `json:"artifacts"`

	// Managed identity to use for reading the script if its not a SAS and if the VM doesn't have a system managed identity
	SourceManagedIdentity *RunCommandManagedIdentity `json:"sourceManagedIdentity"`

	// Managed identity to use for writing the output blob if the VM doesn't have a system managed identity
	OutputBlobManagedIdentity *RunCommandManagedIdentity `json:"outputBlobManagedIdentity"`

	// Managed identity to use for writing the error blob if the VM doesn't have a system managed identity
	ErrorBlobManagedIdentity *RunCommandManagedIdentity `json:"errorBlobManagedIdentity"`
}

// Contains the public and protected information for the artifact to download
// This structure is only kept in memory. It is neither read nor persisted
type UnifiedArtifact struct {
	ArtifactId              int
	ArtifactUri             string
	FileName                string
	ArtifactSasToken        string
	ArtifactManagedIdentity *RunCommandManagedIdentity
}

// Contains all public information for the artifact. Any sas token will be removed from the uri and added to the ArtifactSource
// in the protected settings. The public and protected artifact settings are keyed by the artifactId.
type PublicArtifactSource struct {
	ArtifactId  int    `json:"id"`
	ArtifactUri string `json:"uri"`
	FileName    string `json:"fileName"`
}

// Contains secret information about an artifact to download to the VM. This includes the sas token for the uri (located in public settings)
// and the managed identity. The public and protected artifact sources are keyed by the artifactId.
type ProtectedArtifactSource struct {
	ArtifactId              int                        `json:"id"`
	ArtifactSasToken        string                     `json:"sasToken"`
	ArtifactManagedIdentity *RunCommandManagedIdentity `json:"artifactManagedIdentity"`
}

type RunCommandManagedIdentity struct {
	ObjectId string `json:"objectId"`
	ClientId string `json:"clientId"`
}

type ScriptSource struct {
	Script    string `json:"script"`
	ScriptURI string `json:"scriptUri"`
	// When the RunCommand extension sees the installAsService == true, it will apply the operations on the service as well.
	// This service will continuously poll HGAP for any new goal state.
	InstallAsService bool `json:"installAsService,bool"`
}

type ParameterDefinition struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
