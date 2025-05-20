package types

// StatusEventArgs is the event argument for the status change event for immediate run command.
type StatusEventArgs struct {
	TopLevelStatus StatusItem
	StatusKey      GoalStateKey
}

// GoalStateKey is a unique identifier for a goal state item.
// It is used to store the goal state item in the event map.
type GoalStateKey struct {
	ExtensionName        string
	SeqNumber            int
	RuntimeSettingsState string
}
