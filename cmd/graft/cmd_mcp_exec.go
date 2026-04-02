package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/coordd"
)

func mcpExecToolDefs() []mcpTool {
	return []mcpTool{
		{
			Name:        "graft_exec",
			Description: "Execute a command through coordd governance. This is the MCP command-execution path and always applies coordd preflight, runtime profile selection, and backend enforcement before running the child process.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "program to execute (required)",
					},
					"args": map[string]any{
						"type":        "array",
						"description": "argv tail as an array of strings",
						"items": map[string]any{
							"type": "string",
						},
					},
					"stdin": map[string]any{
						"type":        "string",
						"description": "stdin payload to pass to the child process",
					},
					"check_only": map[string]any{
						"type":        "boolean",
						"description": "only run preflight and return the policy decision without executing",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

func mcpDispatchExecTool(name string, args map[string]any) (any, error) {
	switch name {
	case "graft_exec":
		return mcpToolExec(args)
	default:
		return nil, fmt.Errorf("unknown exec tool %q", name)
	}
}

func mcpToolExec(args map[string]any) (any, error) {
	command := strings.TrimSpace(mcpArgString(args, "command"))
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}

	argv := []string{command}
	argv = append(argv, mcpArgStringSlice(args, "args")...)

	r, _, err := openCoorddRuntime()
	if err != nil {
		return nil, err
	}
	activeID := readActiveAgentID(r)

	input, err := coordd.BuildShellActionInput(r, activeID, argv)
	if err != nil {
		return nil, err
	}
	decision, err := coordd.EvaluateActionPolicyWithRepo(r, input)
	if err != nil {
		return nil, err
	}
	_ = coordd.RecordPreflightDecision(r.GraftDir, input, decision)

	result := map[string]any{
		"input":    input,
		"decision": decision,
		"allowed":  decision.Action != "HardBlock",
	}
	if mcpArgBool(args, "check_only") {
		result["status"] = "preflight"
		return result, nil
	}

	stdinPayload := mcpArgString(args, "stdin")
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	execResult, execErr := coordd.ExecuteGuardedWithIO(r, input, decision, coordd.ExecIO{
		Stdin:  strings.NewReader(stdinPayload),
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
	})
	if execResult == nil {
		execResult = &coordd.ExecResult{Decision: decision}
	}

	result["exec"] = execResult
	result["stdout"] = stdoutBuf.String()
	result["stderr"] = stderrBuf.String()

	if execErr != nil {
		var exitCoder interface{ ExitCode() int }
		if errors.As(execErr, &exitCoder) {
			result["exit_code"] = exitCoder.ExitCode()
			result["error"] = execErr.Error()
			if decision.Action == "HardBlock" {
				result["status"] = "blocked"
			} else {
				result["status"] = "failed"
			}
			return result, nil
		}
		return nil, execErr
	}

	result["exit_code"] = execResult.ExitCode
	result["status"] = "completed"
	return result, nil
}

func mcpArgStringSlice(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch typed := v.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			switch value := item.(type) {
			case string:
				out = append(out, value)
			default:
				out = append(out, fmt.Sprintf("%v", value))
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return strings.Fields(typed)
	default:
		return []string{fmt.Sprintf("%v", typed)}
	}
}
