package main

import (
	"encoding/json"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

var (
	errSourceNotSpecified = errors.New("Either 'source.script' or 'source.scriptUri' has to be specified")
)

// handlerSettings holds the configuration of the extension handler.
type handlerSettings struct {
	publicSettings
	protectedSettings
}

func (s handlerSettings) script() string {
	return s.publicSettings.Source.Script
}

func (s handlerSettings) scriptURI() string {
	return s.publicSettings.Source.ScriptURI
}

func (s handlerSettings) scriptSAS() string {
	return s.protectedSettings.SourceSASToken
}

func (s handlerSettings) readArtifacts() ([]unifiedArtifact, error) {
	if s.protectedSettings.Artifacts == nil && s.publicSettings.Artifacts == nil {
		return nil, nil
	}

	if len(s.protectedSettings.Artifacts) != len(s.publicSettings.Artifacts) {
		return nil, errors.New(("RunCommand artifact download failed. Reason: Invalid artifact specification. This is a product bug."))
	}

	artifacts := make([]unifiedArtifact, len(s.publicSettings.Artifacts))

	for i := 0; i < len(s.publicSettings.Artifacts); i++ {
		publicArtifact := s.publicSettings.Artifacts[i]
		found := false

		for k := 0; k < len(s.protectedSettings.Artifacts); k++ {
			protectedArtifact := s.protectedSettings.Artifacts[k]
			if publicArtifact.ArtifactId == protectedArtifact.ArtifactId {
				found = true
				artifacts[i] = unifiedArtifact{
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
func (s handlerSettings) validate() error {

	if s.publicSettings.Source == nil || (s.publicSettings.Source.Script == "") == (s.publicSettings.Source.ScriptURI == "") {
		return errSourceNotSpecified
	}
	return nil
}

// publicSettings is the type deserialized from public configuration section of
// the extension handler. This should be in sync with publicSettingsSchema.
type publicSettings struct {
	Source                          *scriptSource         `json:"source"`
	Parameters                      []parameterDefinition `json:"parameters"`
	RunAsUser                       string                `json:"runAsUser"`
	OutputBlobURI                   string                `json:"outputBlobUri"`
	ErrorBlobURI                    string                `json:"errorBlobUri"`
	TimeoutInSeconds                int                   `json:"timeoutInSeconds,int"`
	AsyncExecution                  bool                  `json:"asyncExecution,bool"`
	TreatFailureAsDeploymentFailure bool                  `json:"treatFailureAsDeploymentFailure,bool"`

	// List of artifacts to download before running the script
	Artifacts []publicArtifactSource `json:"artifacts"`
}

// protectedSettings is the type decoded and deserialized from protected
// configuration section. This should be in sync with protectedSettingsSchema.
type protectedSettings struct {
	RunAsPassword       string                `json:"runAsPassword"`
	SourceSASToken      string                `json:"sourceSASToken"`
	OutputBlobSASToken  string                `json:"outputBlobSASToken"`
	ErrorBlobSASToken   string                `json:"errorBlobSASToken"`
	ProtectedParameters []parameterDefinition `json:"protectedParameters"`

	// List of artifacts to download before running the script
	Artifacts []protectedArtifactSource `json:"artifacts"`

	// Managed identity to use for reading the script if its not a SAS and if the VM doesn't have a system managed identity
	SourceManagedIdentity *RunCommandManagedIdentity `json:"sourceManagedIdentity"`

	// Managed identity to use for writing the output blob if the VM doesn't have a system managed identity
	OutputBlobManagedIdentity *RunCommandManagedIdentity `json:"outputBlobManagedIdentity"`

	// Managed identity to use for writing the error blob if the VM doesn't have a system managed identity
	ErrorBlobManagedIdentity *RunCommandManagedIdentity `json:"errorBlobManagedIdentity"`
}

// Contains the public and protected information for the artifact to download
// This structure is only kept in memory. It is neither read nor persisted
type unifiedArtifact struct {
	ArtifactId              int
	ArtifactUri             string
	FileName                string
	ArtifactSasToken        string
	ArtifactManagedIdentity *RunCommandManagedIdentity
}

// Contains all public information for the artifact. Any sas token will be removed from the uri and added to the ArtifactSource
// in the protected settings. The public and protected artifact settings are keyed by the artifactId.
type publicArtifactSource struct {
	ArtifactId  int    `json:"id"`
	ArtifactUri string `json:"uri"`
	FileName    string `json:"fileName"`
}

// Contains secret information about an artifact to download to the VM. This includes the sas token for the uri (located in public settings)
// and the managed identity. The public and protected artifact sources are keyed by the artifactId.
type protectedArtifactSource struct {
	ArtifactId              int                        `json:"id"`
	ArtifactSasToken        string                     `json:"sasToken"`
	ArtifactManagedIdentity *RunCommandManagedIdentity `json:"artifactManagedIdentity"`
}

type RunCommandManagedIdentity struct {
	ObjectId string `json:"objectId"`
	ClientId string `json:"clientId"`
}

type scriptSource struct {
	Script    string `json:"script"`
	ScriptURI string `json:"scriptUri"`
}

type parameterDefinition struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// parseAndValidateSettings reads configuration from configFolder, decrypts it,
// runs JSON-schema and logical validation on it and returns it back.
func parseAndValidateSettings(ctx *log.Context, configFilePath string) (h handlerSettings, _ error) {
	ctx.Log("event", "reading configuration from "+configFilePath)
	pubJSON, protJSON, err := readSettings(configFilePath)
	if err != nil {
		return h, err
	}
	ctx.Log("event", "read configuration")

	ctx.Log("event", "validating json schema")
	if err := validateSettingsSchema(pubJSON, protJSON); err != nil {
		return h, errors.Wrap(err, "json validation error")
	}
	ctx.Log("event", "json schema valid")

	ctx.Log("event", "parsing configuration json")
	if err := UnmarshalHandlerSettings(pubJSON, protJSON, &h.publicSettings, &h.protectedSettings); err != nil {
		return h, errors.Wrap(err, "json parsing error")
	}
	ctx.Log("event", "parsed configuration json")

	ctx.Log("event", "validating configuration logically")
	if err := h.validate(); err != nil {
		return h, errors.Wrap(err, "invalid configuration")
	}
	ctx.Log("event", "validated configuration")
	return h, nil
}

// readSettings uses specified configFilePath (comes from HandlerEnvironment) to
// decrypt and parse the public/protected settings of the extension handler into
// JSON objects.
func readSettings(configFilePath string) (pubSettingsJSON, protSettingsJSON map[string]interface{}, err error) {
	pubSettingsJSON, protSettingsJSON, err = ReadSettings(configFilePath)
	err = errors.Wrapf(err, "error reading extension configuration")
	return
}

// validateSettings takes publicSettings and protectedSettings as JSON objects
// and runs JSON schema validation on them.
func validateSettingsSchema(pubSettingsJSON, protSettingsJSON map[string]interface{}) error {
	pubJSON, err := toJSON(pubSettingsJSON)
	if err != nil {
		return errors.Wrap(err, "failed to unmarshal public settings into json")
	}
	protJSON, err := toJSON(protSettingsJSON)
	if err != nil {
		return errors.Wrap(err, "failed to unmarshal protected settings into json")
	}

	if err := validatePublicSettings(pubJSON); err != nil {
		return err
	}
	if err := validateProtectedSettings(protJSON); err != nil {
		return err
	}
	return nil
}

// toJSON converts given in-memory JSON object representation into a JSON object string.
func toJSON(o map[string]interface{}) (string, error) {
	if o == nil { // instead of JSON 'null' assume empty object '{}'
		return "{}", nil
	}
	b, err := json.Marshal(o)
	return string(b), errors.Wrap(err, "failed to marshal into json")
}
