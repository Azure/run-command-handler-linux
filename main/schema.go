package main

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/xeipuuv/gojsonschema"
)

// Refer to http://json-schema.org/ on how to use JSON Schemas.

const (
	publicSettingsSchema = `{
  "$schema": "http://json-schema.org/draft-04/schema#",
  "title": "Run Command - Public Settings",
  "type": "object",
  "properties": {
    "source": {
      "description": "Source of the script to be executed",
      "type": "object",
      "properties": {
        "script": {
          "description": "Script to be executed",
          "type": "string"
        },
        "scriptUri": {
          "description": "ScriptUri specify the script source download location",
          "type": "string",
          "format": "uri"
        },
        "installAsService": {
          "description": "Indicates the RunCommand extension to self install as a service on the VM",
          "type": "boolean"
        }
      }
    },
    "parameters": {
      "description": "List of parameters",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name": {
            "description": "Property name",
            "type": "string"
          },
          "value": {
            "description": "Property value",
            "type": "string"
          }
        }
      }
    },
    "runAsUser": {
      "description": "User name to run the script",
      "type": "string"
    },
    "outputBlobUri": {
      "description": "Output storage blob to write the script console output stream",
      "type": "string",
      "format": "uri"
    },
    "errorBlobUri": {
      "description": "Error storage blob to write the script error stream",
      "type": "string",
      "format": "uri"
    },
    "timeoutInSeconds": {
      "description": "Time limit to execute the script",
      "type": "integer"
    },
    "asyncExecution": {
      "description": "Async script execution",
      "type": "boolean"
    },
    "treatFailureAsDeploymentFailure": {
      "description": "If set to true, extension status will be set to error if the Run Command exits with a non-zero exit code.",
      "type": "boolean"
    },

    "timestamp": {
      "description": "An integer, intended to trigger re-execution of the script when changed",
      "type": "integer"
    }
  },
  "additionalProperties": false
}`

	protectedSettingsSchema = `{
  "$schema": "http://json-schema.org/draft-04/schema#",
  "title": "Run Command - Protected Settings",
  "type": "object",
  "properties": {
    "runAsPassword": {
      "description": "User password",
      "type": "string"
    },
    "sourceSASToken": {
      "description": "SAS token to access the scriptUri blob",
      "type": "string"
    },
    "outputBlobSASToken": {
      "description": "SAS token to access the outputBlobUri blob",
      "type": "string"
    },
    "errorBlobSASToken": {
      "description": "SAS token to access the errorBlobUri blob",
      "type": "string"
    },
    "sourceManagedIdentity": {
      "description": "Managed identity to use for reading the script pointed by scriptUri if sourceSASToken not provided(using system-assigned or user-assigned managed identity)",
      "type": "object"
    },
    "outputBlobManagedIdentity": {
      "description": "Managed identity to use for writing Run Command output to blob pointed by outputBlobUri if outputBlobSASToken not provided(using system-assigned or user-assigned managed identity)",
      "type": "object"
    },
    "errorBlobManagedIdentity": {
      "description": "Managed identity to use for writing Run Command error to blob pointed by errorBlobUri if errorBlobSASToken not provided(using system-assigned or user-assigned managed identity)",
      "type": "object"
    },
    "protectedParameters": {
      "description": "List of parameters",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name": {
            "description": "Property name",
            "type": "string"
          },
          "value": {
            "description": "Property value",
            "type": "string"
          }
        }
      }
    }
  },
  "additionalProperties": false
}`
)

// validateObjectJSON validates the specified json with schemaJSON.
// If json is empty string, it will be converted into an empty JSON object
// before being validated.
func validateObjectJSON(schema *gojsonschema.Schema, json string) error {
	if json == "" {
		json = "{}"
	}

	doc := gojsonschema.NewStringLoader(json)
	res, err := schema.Validate(doc)
	if err != nil {
		return err
	}
	if !res.Valid() {
		for _, err := range res.Errors() {
			// return with the first error
			return fmt.Errorf("%s", err)
		}
	}
	return nil
}

func validateSettingsObject(settingsType, schemaJSON, docJSON string) error {
	schema, err := gojsonschema.NewSchema(gojsonschema.NewStringLoader(schemaJSON))
	if err != nil {
		return errors.Wrapf(err, "failed to load %s settings schema", settingsType)
	}
	if err := validateObjectJSON(schema, docJSON); err != nil {
		return errors.Wrapf(err, "invalid %s settings JSON", settingsType)
	}
	return nil
}

func validatePublicSettings(json string) error {
	return validateSettingsObject("public", publicSettingsSchema, json)
}

func validateProtectedSettings(json string) error {
	return validateSettingsObject("protected", protectedSettingsSchema, json)
}
