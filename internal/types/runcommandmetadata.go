package types

import (
	"path/filepath"

	"github.com/Azure/run-command-handler-linux/internal/constants"
)

type RCMetadata struct {
	// Most recent sequence, which was previously traced by seqNumFile. This was
	// incorrect. The correct way is mrseq.  This file is auto-preserved by the agent.
	MostRecentSequence string

	// Filename where active process keeps track of process id and process start time
	PidFilePath string

	// DownloadDir is where we store the downloaded files in the "{downloadDir}/{seqnum}/file"
	// format and the logs as "{downloadDir}/{seqnum}/std(out|err)". Stored under dataDir
	// multiconfig support - when extName is set we use {downloadDir}/{extName}/...
	DownloadDir string

	// Download path is the full path where the files are stored.
	// E.g., /var/lib/waagent/run-command-handler/{downloadDir}/{seqnum}/file
	DownloadPath string

	// The name of the current extension. E.g., RC0001
	ExtName string

	// The sequence number. E.g., 1
	SeqNum int
}

func NewRCMetadata(extensionName string, seqNum int, downloadFolder string, dataDir string) RCMetadata {
	result := RCMetadata{}
	result.ExtName = extensionName
	result.SeqNum = seqNum
	result.DownloadDir = filepath.Join(downloadFolder, extensionName)
	result.DownloadPath = filepath.Join(dataDir, result.DownloadDir)
	result.MostRecentSequence = extensionName + constants.MrSeqFileExtension
	result.PidFilePath = extensionName + ".pidstart"
	return result
}
