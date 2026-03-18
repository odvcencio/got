package coordd

type RuntimeProfile struct {
	Name            string `json:"name"`
	FilesystemScope string `json:"filesystem_scope,omitempty"`
	Network         string `json:"network,omitempty"`
	DeleteScope     string `json:"delete_scope,omitempty"`
	RequireSnapshot bool   `json:"require_snapshot,omitempty"`
}

const (
	FilesystemScopeRepoRO   = "repo_ro"
	FilesystemScopeRepoRW   = "repo_rw"
	FilesystemScopeHostProc = "host_process"

	NetworkDeny    = "deny"
	NetworkAllow   = "allow"
	NetworkAmbient = "ambient"

	DeleteScopeNone    = "none"
	DeleteScopeRepo    = "repo_only"
	DeleteScopeAmbient = "ambient"
)

func ResolveRuntimeProfile(name string, action ActionPolicyAction) RuntimeProfile {
	switch name {
	case "blocked":
		return RuntimeProfile{
			Name:        "blocked",
			DeleteScope: DeleteScopeNone,
			Network:     NetworkDeny,
		}
	case "read_only":
		return RuntimeProfile{
			Name:            "read_only",
			FilesystemScope: FilesystemScopeRepoRO,
			Network:         NetworkDeny,
			DeleteScope:     DeleteScopeNone,
		}
	case "repo_write":
		return RuntimeProfile{
			Name:            "repo_write",
			FilesystemScope: FilesystemScopeRepoRW,
			Network:         NetworkDeny,
			DeleteScope:     DeleteScopeRepo,
			RequireSnapshot: true,
		}
	case "network_read":
		return RuntimeProfile{
			Name:            "network_read",
			FilesystemScope: FilesystemScopeRepoRO,
			Network:         NetworkAllow,
			DeleteScope:     DeleteScopeNone,
		}
	case "repo_write_network":
		return RuntimeProfile{
			Name:            "repo_write_network",
			FilesystemScope: FilesystemScopeRepoRW,
			Network:         NetworkAllow,
			DeleteScope:     DeleteScopeRepo,
			RequireSnapshot: true,
		}
	}

	if action.Network {
		if action.WritesRepo || action.WritesFilesystem || action.WritesCoord {
			return ResolveRuntimeProfile("repo_write_network", action)
		}
		return ResolveRuntimeProfile("network_read", action)
	}
	if action.WritesRepo || action.WritesFilesystem || action.WritesCoord {
		return ResolveRuntimeProfile("repo_write", action)
	}
	return ResolveRuntimeProfile("read_only", action)
}
