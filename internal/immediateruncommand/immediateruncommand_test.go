package immediateruncommand

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/observer"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

// ---- helpers ----

func ptrString(s string) *string { return &s }
func ptrInt(i int) *int          { return &i }
func ptrInt32(i int32) *int32    { return &i }

func mkSetting(ext string, seq int, state string) settings.SettingsCommon {
	extCopy := ext
	seqCopy := seq
	stateCopy := state
	return settings.SettingsCommon{
		ExtensionName:  &extCopy,
		SeqNo:          &seqCopy,
		ExtensionState: &stateCopy,
	}
}

// ---- tests for getGoalStatesToProcess ----

func TestGetGoalStatesToProcess_ValidateSignatureError(t *testing.T) {
	orig := validateSignatureFn
	defer func() { validateSignatureFn = orig }()
	validateSignatureFn = func(_ hostgacommunicator.ImmediateExtensionGoalState) (bool, error) {
		return false, errors.New("boom")
	}

	gs := []hostgacommunicator.ImmediateExtensionGoalState{
		{Settings: []settings.SettingsCommon{mkSetting("RunCommand", 1, "state")}},
	}

	_, _, err := getGoalStatesToProcess(gs, 10)
	require.NotNil(t, err, "expected error, got nil")
}

func TestGetGoalStatesToProcess_InvalidSignatureSkipsAll(t *testing.T) {
	orig := validateSignatureFn
	defer func() { validateSignatureFn = orig }()
	validateSignatureFn = func(_ hostgacommunicator.ImmediateExtensionGoalState) (bool, error) {
		return false, nil
	}

	gs := []hostgacommunicator.ImmediateExtensionGoalState{
		{Settings: []settings.SettingsCommon{
			mkSetting("RunCommand", 1, "A"),
			mkSetting("RunCommand", 2, "B"),
		}},
	}

	newOnes, skipped, err := getGoalStatesToProcess(gs, 10)
	require.Nil(t, err, "unexpected err: %v", err)
	require.True(t, len(newOnes) == 0, "expected none, got new=%d", len(newOnes))
	require.True(t, len(skipped) == 0, "expected none, got skipped=%d", len(skipped))
}

func TestGetGoalStatesToProcess_RespectsMaxTasksToFetch(t *testing.T) {
	orig := validateSignatureFn
	defer func() { validateSignatureFn = orig }()
	validateSignatureFn = func(_ hostgacommunicator.ImmediateExtensionGoalState) (bool, error) {
		return true, nil
	}

	gs := []hostgacommunicator.ImmediateExtensionGoalState{
		{Settings: []settings.SettingsCommon{
			mkSetting("RunCommand", 1, "A"),
			mkSetting("RunCommand", 2, "B"),
			mkSetting("RunCommand", 3, "C"),
		}},
	}

	newOnes, skipped, err := getGoalStatesToProcess(gs, 2)
	require.Nil(t, err, "unexpected err: %v", err)
	require.Equal(t, 2, len(newOnes), "expected 2 new, got %d", len(newOnes))
	require.Equal(t, 1, len(skipped), "expected 1 skipped, got %d", len(skipped))
}

// ---- tests for processImmediateRunCommandGoalStates ----

func TestProcessImmediateRunCommandGoalStates_WhenAtCapacity_DoesNotFetch(t *testing.T) {
	// Arrange
	executingTasks = 0
	for i := int32(0); i < maxConcurrentTasks; i++ {
		executingTasks.Increment()
	}
	defer func() {
		// reset counter
		for executingTasks.Get() > 0 {
			executingTasks.Decrement()
		}
	}()

	origGet := getImmediateGoalStatesFn
	defer func() { getImmediateGoalStatesFn = origGet }()
	getImmediateGoalStatesFn = func(_ *log.Context, _ hostgacommunicator.IHostGACommunicator, _ string) ([]hostgacommunicator.ImmediateExtensionGoalState, string, error) {
		t.Fatalf("should not be called when at capacity")
		return nil, "", nil
	}

	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	var comm hostgacommunicator.HostGACommunicator // zero value ok for this test

	etag, err := processImmediateRunCommandGoalStates(ctx, comm, "etag-old")
	require.Nil(t, err, "unexpected err: %v", err)
	require.Equal(t, "etag-old", etag, "expected etag unchanged, got %q", etag)
}

func TestProcessImmediateRunCommandGoalStates_WhenEtagUnchanged_NoWork(t *testing.T) {
	// Arrange
	for executingTasks.Get() > 0 {
		executingTasks.Decrement()
	}

	origGet := getImmediateGoalStatesFn
	defer func() { getImmediateGoalStatesFn = origGet }()
	getImmediateGoalStatesFn = func(_ *log.Context, _ hostgacommunicator.IHostGACommunicator, last string) ([]hostgacommunicator.ImmediateExtensionGoalState, string, error) {
		return nil, last, nil // unchanged
	}

	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	var comm hostgacommunicator.HostGACommunicator

	etag, err := processImmediateRunCommandGoalStates(ctx, comm, "same")
	require.Nil(t, err, "unexpected err: %v", err)
	require.Equal(t, "same", etag, "expected same etag, got %q", etag)
}

func TestProcessImmediateRunCommandGoalStates_GoalStateFailed(t *testing.T) {
	// Make deterministic: run "goroutines" inline.
	origSpawn := spawnFn
	defer func() { spawnFn = origSpawn }()
	spawnFn = func(f func()) { f() }

	// Make deterministic time.
	origNow := nowFn
	defer func() { nowFn = origNow }()
	fixed := time.Date(2025, 12, 23, 10, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return fixed }

	// Signature validation: true
	origValidate := validateSignatureFn
	defer func() { validateSignatureFn = origValidate }()
	validateSignatureFn = func(_ hostgacommunicator.ImmediateExtensionGoalState) (bool, error) { return true, nil }

	gs := []hostgacommunicator.ImmediateExtensionGoalState{
		{Settings: []settings.SettingsCommon{
			mkSetting("RunCommand", 1, "A"),
		}},
	}

	origGet := getImmediateGoalStatesFn
	defer func() { getImmediateGoalStatesFn = origGet }()
	getImmediateGoalStatesFn = func(_ *log.Context, _ hostgacommunicator.IHostGACommunicator, _ string) ([]hostgacommunicator.ImmediateExtensionGoalState, string, error) {
		return gs, "etag-new", nil
	}

	// HandleImmediateGoalState called exactly once (maxTasksToFetch=1),
	// and we’ll return success (no final report for success).
	handleCalls := 0
	origHandle := handleImmediateGoalStateFn
	defer func() { handleImmediateGoalStateFn = origHandle }()
	handleImmediateGoalStateFn = func(_ *log.Context, _ settings.SettingsCommon, _ *observer.Notifier) (int, error) {
		handleCalls++
		return 0, vmextension.NewErrorWithClarification(constants.Hgap_InternalArgumentError, errors.New("the chipmunks do not see your argument"))
	}

	// ReportFinalStatus called for the failed item
	reportCalls := 0
	origReport := reportFinalStatusFn
	defer func() { reportFinalStatusFn = origReport }()
	reportFinalStatusFn = func(_ *log.Context, _ *observer.Notifier, _ types.GoalStateKey, statusType types.StatusType, instView *types.RunCommandInstanceView) error {
		require.Equal(t, types.StatusError, statusType, "expected StatusError report, got %v", statusType)
		require.Equal(t, constants.Hgap_InternalArgumentError, instView.ErrorClarificationValue, "expected %d error code, got %d", constants.Hgap_InternalArgumentError, instView.ExitCode)
		reportCalls++
		return nil
	}

	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	goalStateEventObserver.Initialize(ctx)
	goalStateEventObserver.ReportImmediateStatusFn = func(s status.ImmediateTopLevelStatus) error {
		return nil
	}

	tmpDir := t.TempDir()
	t.Setenv(constants.ExtensionPathEnvName, tmpDir)

	var comm hostgacommunicator.HostGACommunicator

	_, err := processImmediateRunCommandGoalStates(ctx, comm, "etag-old")
	require.Nil(t, err, "unexpected err: %v", err)
	require.Equal(t, 1, handleCalls, "expected 1 handled call, got %d", handleCalls)
	require.Equal(t, 1, reportCalls, "expected 1 reports, got %d", reportCalls)
}

func TestProcessImmediateRunCommandGoalStates_LaunchesAndReportsSkipped(t *testing.T) {
	// Make deterministic: run "goroutines" inline.
	origSpawn := spawnFn
	defer func() { spawnFn = origSpawn }()
	spawnFn = func(f func()) { f() }

	// Make deterministic time.
	origNow := nowFn
	defer func() { nowFn = origNow }()
	fixed := time.Date(2025, 12, 23, 10, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return fixed }

	// Signature validation: true
	origValidate := validateSignatureFn
	defer func() { validateSignatureFn = origValidate }()
	validateSignatureFn = func(_ hostgacommunicator.ImmediateExtensionGoalState) (bool, error) { return true, nil }

	// Return 3 goal states; max tasks should be 5 in empty case,
	// but we’ll artificially fill executingTasks to force maxTasksToFetch=1.
	for executingTasks.Get() > 0 {
		executingTasks.Decrement()
	}
	// executing=4 => maxTasksToFetch=1
	for i := 0; i < 4; i++ {
		executingTasks.Increment()
	}
	defer func() {
		for executingTasks.Get() > 0 {
			executingTasks.Decrement()
		}
	}()

	gs := []hostgacommunicator.ImmediateExtensionGoalState{
		{Settings: []settings.SettingsCommon{
			mkSetting("RunCommand", 1, "A"),
			mkSetting("RunCommand", 2, "B"),
			mkSetting("RunCommand", 3, "C"),
		}},
	}

	origGet := getImmediateGoalStatesFn
	defer func() { getImmediateGoalStatesFn = origGet }()
	getImmediateGoalStatesFn = func(_ *log.Context, _ hostgacommunicator.IHostGACommunicator, _ string) ([]hostgacommunicator.ImmediateExtensionGoalState, string, error) {
		return gs, "etag-new", nil
	}

	// HandleImmediateGoalState called exactly once (maxTasksToFetch=1),
	// and we’ll return success (no final report for success).
	handleCalls := 0
	origHandle := handleImmediateGoalStateFn
	defer func() { handleImmediateGoalStateFn = origHandle }()
	handleImmediateGoalStateFn = func(_ *log.Context, _ settings.SettingsCommon, _ *observer.Notifier) (int, error) {
		handleCalls++
		return 0, nil
	}

	// ReportFinalStatus called for skipped items (2 of them).
	reportCalls := 0
	origReport := reportFinalStatusFn
	defer func() { reportFinalStatusFn = origReport }()
	reportFinalStatusFn = func(_ *log.Context, _ *observer.Notifier, _ types.GoalStateKey, statusType types.StatusType, instView *types.RunCommandInstanceView) error {
		require.Equal(t, types.StatusSkipped, statusType, "expected StatusSkipped report, got %v", statusType)
		require.Equal(t, constants.ImmediateRC_CommandSkipped, instView.ExitCode, "expected skipped exit code, got %d", instView.ExitCode)
		reportCalls++
		return nil
	}

	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	goalStateEventObserver.Initialize(ctx)
	goalStateEventObserver.ReportImmediateStatusFn = func(s status.ImmediateTopLevelStatus) error {
		return nil
	}

	tmpDir := t.TempDir()
	t.Setenv(constants.ExtensionPathEnvName, tmpDir)

	var comm hostgacommunicator.HostGACommunicator

	newEtag, err := processImmediateRunCommandGoalStates(ctx, comm, "etag-old")
	require.Nil(t, err, "unexpected err: %v", err)
	require.Equal(t, "etag-new", newEtag, "expected etag-new, got %q", newEtag)
	require.Equal(t, 1, handleCalls, "expected 1 handled call, got %d", handleCalls)
	require.Equal(t, 2, reportCalls, "expected 2 skipped reports, got %d", reportCalls)
}
