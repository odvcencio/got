package repo

// StagedFile describes a file in the staging area.
type StagedFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// EntityChange describes a single entity-level change.
type EntityChange struct {
	Path             string `json:"path"`
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	DeclKind         string `json:"decl_kind"`
	SignatureChanged bool   `json:"signature_changed"`
}

// EntityDiff groups added, modified, and removed entity changes.
type EntityDiff struct {
	Added    []EntityChange `json:"added,omitempty"`
	Modified []EntityChange `json:"modified,omitempty"`
	Removed  []EntityChange `json:"removed,omitempty"`
}

// HookRefUpdate describes a reference update in hook payloads.
type HookRefUpdate struct {
	Name       string `json:"name"`
	Old        string `json:"old"`
	New        string `json:"new"`
	LocalRef   string `json:"local_ref"`
	RemoteRef  string `json:"remote_ref"`
	LocalHash  string `json:"local_hash"`
	RemoteHash string `json:"remote_hash"`
}

// PreCommitPayload is the JSON payload sent to pre-commit hooks.
type PreCommitPayload struct {
	Hook        string       `json:"hook"`
	Repo        string       `json:"repo"`
	Branch      string       `json:"branch"`
	Author      string       `json:"author"`
	StagedFiles []StagedFile `json:"staged_files,omitempty"`
	EntityDiff  *EntityDiff  `json:"entity_diff,omitempty"`
}

// PostCommitPayload is the JSON payload sent to post-commit hooks.
type PostCommitPayload struct {
	Hook    string `json:"hook"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Commit  string `json:"commit"`
	Message string `json:"message"`
	Author  string `json:"author"`
}

// PrePushPayload is the JSON payload sent to pre-push hooks.
type PrePushPayload struct {
	Hook       string      `json:"hook"`
	Repo       string      `json:"repo"`
	Remote     string      `json:"remote"`
	RemoteURL  string      `json:"remote_url"`
	Refs       []HookRefUpdate `json:"refs,omitempty"`
	Commits    []string        `json:"commits,omitempty"`
	EntityDiff *EntityDiff `json:"entity_diff,omitempty"`
}

// PostPushPayload is the JSON payload sent to post-push hooks.
type PostPushPayload struct {
	Hook          string      `json:"hook"`
	Remote        string      `json:"remote"`
	RemoteURL     string      `json:"remote_url"`
	Refs          []HookRefUpdate `json:"refs,omitempty"`
	ObjectsPushed int             `json:"objects_pushed"`
}
