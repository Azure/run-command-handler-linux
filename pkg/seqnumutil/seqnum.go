package seqnumutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

const (
	// chmod is used to set the mode bits for new seqnum files.
	chmod = os.FileMode(0600)
)

// FindSequenceNumberFromConfig finds the file with the highest sequence number for an extension under configFolder
// named like <RunCommandName>.0.settings, <RunCommandName>.1.settings so on.
func FindSequenceNumberFromConfig(path, fileExtension string, extensionName string) (int, error) {
	g, err := filepath.Glob(filepath.Join(path, fmt.Sprintf("%s.*%s", extensionName, fileExtension)))
	if err != nil {
		return 0, err
	}
	seqs := make([]int, len(g))
	for _, v := range g {
		f := filepath.Base(v)
		fileNameWithoutExtension := strings.TrimSuffix(f, filepath.Ext(f))
		dotAndSequenceNumberString := filepath.Ext(fileNameWithoutExtension) // returns something like ".<sequenceNumber>"
		sequenceNumberString := dotAndSequenceNumberString[1:]               // Remove '.' in the front
		i, err := strconv.Atoi(sequenceNumberString)
		if err != nil {
			continue // continue to the next filename if Atoi fails
		}
		seqs = append(seqs, i)
	}
	if len(seqs) == 0 {
		return 0, fmt.Errorf("can't find out seqnum from %s, not enough files", path)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(seqs)))
	return seqs[0], nil
}

// SaveSeqNum replaces the stored sequence number in file, or creates a new file at
// path if it does not exist.
func SaveSeqNum(path string, num int) error {
	b := []byte(fmt.Sprintf("%v", num))
	return errors.Wrap(os.WriteFile(path, b, chmod), "seqnum: failed to write")
}

// IsSmallerThan returns true if the sequence number stored at path is smaller
// than the provided num. If no number is stored, returns true and no
// error.
func IsSmallerThan(path string, num int) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, errors.Wrap(err, "seqnum: failed to read")
	}
	seqNum := strings.TrimSuffix(string(b), "\n")
	stored, err := strconv.Atoi(seqNum)
	return stored < num, errors.Wrapf(err, "seqnum: cannot parse number %q", b)
}
