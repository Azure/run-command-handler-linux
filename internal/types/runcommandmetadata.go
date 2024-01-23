package types

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
}

func NewRCMetadata(extensionName string) RCMetadata {
	result := RCMetadata{}
	result.DownloadDir = "download/" + extensionName
	result.MostRecentSequence = extensionName + ".mrseq"
	result.PidFilePath = extensionName + ".pidstart"
	return result
}
