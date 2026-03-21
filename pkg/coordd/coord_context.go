package coordd

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

// ActionPolicyCoord holds coordination context injected into every
// action-policy evaluation so that arbiter rules can reason about
// multi-agent state (claims, presence, feed).
type ActionPolicyCoord struct {
	Active            bool                 `json:"active"`
	AgentID           string               `json:"agent_id"`
	AgentName         string               `json:"agent_name"`
	FilesTouched      []string             `json:"files_touched"`
	ConflictingClaims []CoordClaimConflict `json:"conflicting_claims"`
	UnreadConflicts   int                  `json:"unread_conflicts"`
	PresenceOverlap   []CoordPresenceEntry `json:"presence_overlap"`
	WatchingClaims    int                  `json:"watching_claims"`
	LastHeartbeatAge  int                  `json:"last_heartbeat_age_s"`
}

// CoordClaimConflict describes an editing claim held by another agent
// that overlaps with the files being touched by the current action.
type CoordClaimConflict struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	EntityKey string `json:"entity_key"`
	File      string `json:"file"`
	Mode      string `json:"mode"`
}

// CoordPresenceEntry describes another agent currently reading a file
// that the current action touches.
type CoordPresenceEntry struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	File      string `json:"file"`
}

// LoadCoordContext builds coordination context from live state.
// On error, returns Active=true with zeroed counts (preserves coordination
// intent while degrading gracefully).
func LoadCoordContext(r *repo.Repo, agentID string, argv []string) ActionPolicyCoord {
	if r == nil || strings.TrimSpace(agentID) == "" {
		return ActionPolicyCoord{Active: false}
	}

	ctx := ActionPolicyCoord{
		Active:  true,
		AgentID: agentID,
	}

	ctx.FilesTouched = extractFilesFromArgv(argv)

	c := coord.New(r, coord.DefaultConfig)
	c.AgentID = agentID

	// Agent name and heartbeat age
	if agent, err := c.GetAgent(agentID); err == nil {
		ctx.AgentName = agent.Name
		ctx.LastHeartbeatAge = int(time.Since(agent.HeartbeatAt).Seconds())
	}

	// Build file set for conflict checking
	fileSet := make(map[string]bool)
	for _, f := range ctx.FilesTouched {
		fileSet[f] = true
	}

	// Load claims and check for conflicts
	if claims, err := c.ListClaims(); err == nil {
		for _, cl := range claims {
			if cl.Agent == agentID {
				continue
			}
			if !fileSet[cl.File] {
				continue
			}
			if cl.Mode == coord.ClaimWatching {
				ctx.WatchingClaims++
				continue
			}
			ctx.ConflictingClaims = append(ctx.ConflictingClaims, CoordClaimConflict{
				AgentID:   cl.Agent,
				AgentName: cl.AgentName,
				EntityKey: cl.EntityKey,
				File:      cl.File,
				Mode:      cl.Mode,
			})
		}
	}

	// Load presence overlap
	if entries, err := c.ListPresence(); err == nil {
		for _, p := range entries {
			if p.AgentID == agentID {
				continue
			}
			if fileSet[p.File] {
				ctx.PresenceOverlap = append(ctx.PresenceOverlap, CoordPresenceEntry{
					AgentID:   p.AgentID,
					AgentName: p.AgentName,
					File:      p.File,
				})
			}
		}
	}

	// Unread feed events from other agents
	if cursor, err := c.LoadCursor(agentID); err == nil {
		if events, err := c.WalkFeed(cursor, 100); err == nil {
			for _, ev := range events {
				if ev.AgentID != agentID {
					ctx.UnreadConflicts++
				}
			}
		}
	}

	return ctx
}

// extractFilesFromArgv conservatively extracts file paths from command argv.
// Handles: git add/checkout/restore, touch, cp, mv, rm, graft add.
// Returns nil for unrecognized programs.
func extractFilesFromArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	program := filepath.Base(argv[0])
	args := argv[1:]

	switch program {
	case "git":
		return extractGitFiles(args)
	case "graft":
		return extractGraftFiles(args)
	case "touch", "rm":
		return filterPaths(args)
	case "cp", "mv":
		return filterPaths(args)
	default:
		return nil
	}
}

func extractGitFiles(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "add", "checkout", "restore":
		return filterPaths(args[1:])
	default:
		return nil
	}
}

func extractGraftFiles(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	if args[0] == "add" {
		return filterPaths(args[1:])
	}
	return nil
}

func filterPaths(args []string) []string {
	var paths []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		paths = append(paths, a)
	}
	return paths
}
