package versionutil

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionString(t *testing.T) {
	defer resetStrings()

	Initialize("1.0.0", "03669cef", "DATE", "dirty")
	require.Equal(t, "v1.0.0/git@03669cef-dirty", VersionString())
}

func TestDetailedVersionString(t *testing.T) {
	defer resetStrings()

	goVersion := runtime.Version()
	Initialize("1.0.0", "03669cef", "DATE", "dirty")
	require.Equal(t, "v1.0.0 git:03669cef-dirty build:DATE "+goVersion, DetailedVersionString())
}

func resetStrings() { Version, GitCommit, BuildDate, GitState = "", "", "", "" }
