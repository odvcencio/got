package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/coordd"
)

func mcpSpawnToolDefs() []mcpTool {
	return []mcpTool{
		{
			Name:        "graft_spawn",
			Description: "Start or authorize a governed child workstream through coordd. Use launch=detached to launch now or launch=lease to authorize an in-process child.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "child agent/workstream name (required)",
					},
					"command": map[string]any{
						"type":        "string",
						"description": "program to execute for the child workstream (required)",
					},
					"args": map[string]any{
						"type":        "array",
						"description": "argv tail as an array of strings",
						"items": map[string]any{
							"type": "string",
						},
					},
					"profile": map[string]any{
						"type":        "string",
						"description": "optional requested child runtime profile",
					},
					"runtime": map[string]any{
						"type":        "string",
						"description": "spawn runtime: auto, detached, or container",
					},
					"launch": map[string]any{
						"type":        "string",
						"description": "spawn delivery: detached or lease",
					},
					"bootstrap_coord": map[string]any{
						"type":        "boolean",
						"description": "auto-register a coord agent/session for the child lease or launch",
					},
					"task_id": map[string]any{
						"type":        "string",
						"description": "optional coord task id to bind to the child workstream",
					},
				},
				"required": []string{"name", "command"},
			},
		},
		{
			Name:        "graft_spawn_consume",
			Description: "Mark a leased child workstream active and return its lease bootstrap package.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "spawn record id (required)",
					},
					"child_agent_id": map[string]any{
						"type":        "string",
						"description": "optional in-process child agent id",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "graft_spawn_trace",
			Description: "Fetch the unified trace bundle for a child workstream, including raw spawn decisions, lease metadata, persisted exec traces, and related coordd events.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "spawn record id (required)",
					},
					"execs": map[string]any{
						"type":        "integer",
						"description": "maximum exec traces to include",
					},
					"events": map[string]any{
						"type":        "integer",
						"description": "maximum events to include",
					},
					"view": map[string]any{
						"type":        "string",
						"description": "trace view: raw or summary",
					},
					"matched_only": map[string]any{
						"type":        "boolean",
						"description": "when using summary view, include only matched rules",
					},
					"collapse_heartbeats": map[string]any{
						"type":        "boolean",
						"description": "when using summary view, collapse consecutive heartbeat events",
					},
					"phases": map[string]any{
						"type":        "array",
						"description": "when using summary view, limit output to selected phases",
						"items": map[string]any{
							"type": "string",
						},
					},
					"no_default_fallbacks": map[string]any{
						"type":        "boolean",
						"description": "when using summary view, hide default and fallback rules",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "graft_spawn_get",
			Description: "Fetch a child workstream record plus lease bootstrap package.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "spawn record id (required)",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "graft_spawn_heartbeat",
			Description: "Record heartbeat for a leased child workstream.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "spawn record id (required)",
					},
					"child_agent_id": map[string]any{
						"type":        "string",
						"description": "optional in-process child agent id",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "graft_spawn_finish",
			Description: "Finish a child workstream record with a terminal status.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "spawn record id (required)",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "completed, failed, aborted, or blocked",
					},
					"child_agent_id": map[string]any{
						"type":        "string",
						"description": "optional in-process child agent id",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "graft_spawn_wait",
			Description: "Wait for a child workstream to reach a terminal status.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "spawn record id (required)",
					},
					"timeout_ms": map[string]any{
						"type":        "integer",
						"description": "timeout in milliseconds",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "graft_spawns",
			Description: "List coordd-managed child workstreams for the current repo.",
			InputSchema: mcpSchema{}.toMap(),
		},
	}
}

func mcpDispatchSpawnTool(name string, args map[string]any) (any, error) {
	switch name {
	case "graft_spawn":
		return mcpToolSpawn(args)
	case "graft_spawn_consume":
		return mcpToolSpawnConsume(args)
	case "graft_spawn_trace":
		return mcpToolSpawnTrace(args)
	case "graft_spawn_get":
		return mcpToolSpawnGet(args)
	case "graft_spawn_heartbeat":
		return mcpToolSpawnHeartbeat(args)
	case "graft_spawn_finish":
		return mcpToolSpawnFinish(args)
	case "graft_spawn_wait":
		return mcpToolSpawnWait(args)
	case "graft_spawns":
		return mcpToolSpawns(args)
	default:
		return nil, fmt.Errorf("unknown spawn tool %q", name)
	}
}

func mcpToolSpawn(args map[string]any) (any, error) {
	name := strings.TrimSpace(mcpArgString(args, "name"))
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	command := strings.TrimSpace(mcpArgString(args, "command"))
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}

	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}

	launchMode := strings.TrimSpace(mcpArgString(args, "launch"))
	req := coordd.SpawnRequest{
		Name:             name,
		Command:          append([]string{command}, mcpArgStringSlice(args, "args")...),
		RequestedProfile: strings.TrimSpace(mcpArgString(args, "profile")),
		Runtime:          strings.TrimSpace(mcpArgString(args, "runtime")),
		Launch:           launchMode,
		BootstrapCoord:   mcpArgBool(args, "bootstrap_coord"),
		TaskID:           strings.TrimSpace(mcpArgString(args, "task_id")),
	}

	var (
		result   *coordd.SpawnResult
		spawnErr error
	)
	if launchMode == "lease" {
		result, spawnErr = coordd.AuthorizeSpawn(r, readActiveAgentID(r), req)
	} else {
		result, spawnErr = coordd.SpawnDetached(r, readActiveAgentID(r), req)
	}
	if spawnErr != nil {
		var exitCoder interface{ ExitCode() int }
		if errors.As(spawnErr, &exitCoder) {
			return map[string]any{
				"status":    "blocked",
				"result":    result,
				"exit_code": exitCoder.ExitCode(),
				"error":     spawnErr.Error(),
			}, nil
		}
		return nil, spawnErr
	}
	return map[string]any{
		"status": spawnStatusLabel(launchMode),
		"result": result,
	}, nil
}

func mcpToolSpawnConsume(args map[string]any) (any, error) {
	id := strings.TrimSpace(mcpArgString(args, "id"))
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}
	return coordd.ConsumeSpawn(r.GraftDir, id, strings.TrimSpace(mcpArgString(args, "child_agent_id")))
}

func mcpToolSpawnTrace(args map[string]any) (any, error) {
	id := strings.TrimSpace(mcpArgString(args, "id"))
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}
	trace, err := coordd.LoadSpawnTrace(r.GraftDir, id, mcpArgInt(args, "execs"), mcpArgInt(args, "events"))
	if err != nil {
		return nil, err
	}
	if trace == nil {
		return nil, fmt.Errorf("spawn %q not found", id)
	}
	if strings.EqualFold(strings.TrimSpace(mcpArgString(args, "view")), "raw") {
		return trace, nil
	}
	return coordd.BuildSpawnTraceView(trace, coordd.SpawnTraceViewOptions{
		MatchedOnly:        mcpArgBoolDefault(args, "matched_only", true),
		CollapseHeartbeats: mcpArgBoolDefault(args, "collapse_heartbeats", true),
		Phases:             mcpArgStringSlice(args, "phases"),
		NoDefaultFallbacks: mcpArgBool(args, "no_default_fallbacks"),
	}), nil
}

func mcpToolSpawnGet(args map[string]any) (any, error) {
	id := strings.TrimSpace(mcpArgString(args, "id"))
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}
	view, err := coordd.LoadSpawnView(r.GraftDir, id)
	if err != nil {
		return nil, err
	}
	if view == nil {
		return nil, fmt.Errorf("spawn %q not found", id)
	}
	return view, nil
}

func mcpToolSpawnHeartbeat(args map[string]any) (any, error) {
	id := strings.TrimSpace(mcpArgString(args, "id"))
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}
	return coordd.TouchSpawn(r.GraftDir, id, strings.TrimSpace(mcpArgString(args, "child_agent_id")))
}

func mcpToolSpawnFinish(args map[string]any) (any, error) {
	id := strings.TrimSpace(mcpArgString(args, "id"))
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}
	return coordd.FinishSpawn(r.GraftDir, id, strings.TrimSpace(mcpArgString(args, "status")), strings.TrimSpace(mcpArgString(args, "child_agent_id")))
}

func mcpToolSpawnWait(args map[string]any) (any, error) {
	id := strings.TrimSpace(mcpArgString(args, "id"))
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}
	timeout := 30 * time.Second
	if raw := strings.TrimSpace(mcpArgString(args, "timeout_ms")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			timeout = time.Duration(parsed) * time.Millisecond
		}
	}
	return coordd.WaitSpawn(r.GraftDir, id, timeout, 200*time.Millisecond)
}

func mcpToolSpawns(_ map[string]any) (any, error) {
	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}
	return coordd.ListSpawnRecords(r.GraftDir)
}

func spawnStatusLabel(launchMode string) string {
	if strings.TrimSpace(launchMode) == "lease" {
		return "authorized"
	}
	return "started"
}
