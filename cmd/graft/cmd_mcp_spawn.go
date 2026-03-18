package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/coordd"
)

func mcpSpawnToolDefs() []mcpTool {
	return []mcpTool{
		{
			Name:        "graft_spawn",
			Description: "Spawn a governed detached child workstream through coordd. The child inherits coordination lineage and runs under coordd profile/backend policy.",
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
				},
				"required": []string{"name", "command"},
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

	result, spawnErr := coordd.SpawnDetached(r, readActiveAgentID(r), coordd.SpawnRequest{
		Name:             name,
		Command:          append([]string{command}, mcpArgStringSlice(args, "args")...),
		RequestedProfile: strings.TrimSpace(mcpArgString(args, "profile")),
	})
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
		"status": "started",
		"result": result,
	}, nil
}

func mcpToolSpawns(_ map[string]any) (any, error) {
	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}
	return coordd.ListSpawnRecords(r.GraftDir)
}
