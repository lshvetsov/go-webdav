package caldav

// ConflictDecision defines the decision for conflict resolution
type ConflictDecision int

const (
	// UseLocal use local version of the event
	UseLocal ConflictDecision = iota

	// UseRemote use remote version of the event
	UseRemote

	// Merge manual merge of changes is required
	Merge

	// Skip the change
	Skip
)

// String returns string representation of conflict decision
func (cd ConflictDecision) String() string {
	switch cd {
	case UseLocal:
		return "use_local"
	case UseRemote:
		return "use_remote"
	case Merge:
		return "merge"
	case Skip:
		return "skip"
	default:
		return "unknown"
	}
}

// ConflictResolver defines interface for conflict resolution policies
type ConflictResolver interface {
	// Resolve takes local and remote versions of an event
	// and returns decision on which version to use
	Resolve(local, remote *CalendarObject) ConflictDecision
}

// LastModifiedWinsResolver - default conflict resolution policy
// Selects version with more recent modification time
type LastModifiedWinsResolver struct{}

// Resolve implements ConflictResolver for LastModifiedWinsResolver
func (r *LastModifiedWinsResolver) Resolve(local, remote *CalendarObject) ConflictDecision {
	if local == nil && remote == nil {
		return Skip
	}

	if local == nil {
		return UseRemote
	}

	if remote == nil {
		return UseLocal
	}

	// Compare modification times
	if local.ModTime.After(remote.ModTime) {
		return UseLocal
	} else if remote.ModTime.After(local.ModTime) {
		return UseRemote
	}

	// If times are equal, use local version
	return UseLocal
}

// AlwaysUseLocalResolver - policy that always chooses local version
type AlwaysUseLocalResolver struct{}

// Resolve implements ConflictResolver for AlwaysUseLocalResolver
func (r *AlwaysUseLocalResolver) Resolve(local, _ *CalendarObject) ConflictDecision {
	if local == nil {
		return UseRemote
	}
	return UseLocal
}

// AlwaysUseRemoteResolver - policy that always chooses remote version
type AlwaysUseRemoteResolver struct{}

// Resolve implements ConflictResolver for AlwaysUseRemoteResolver
func (r *AlwaysUseRemoteResolver) Resolve(_ *CalendarObject, remote *CalendarObject) ConflictDecision {
	if remote == nil {
		return UseLocal
	}
	return UseRemote
}
