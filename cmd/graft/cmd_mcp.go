package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/odvcencio/graft/pkg/userconfig"
	"github.com/spf13/cobra"
)

const (
	mcpServerName      = "graft"
	mcpServerVersion   = "0.2.0"
	mcpProtocolVersion = "2024-11-05"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server for AI agent integration",
	}

	cmd.AddCommand(newMCPServeCmd())
	return cmd
}

func newMCPServeCmd() *cobra.Command {
	var withCodeintel bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start MCP JSON-RPC server over stdio",
		Long: `Start a JSON-RPC 2.0 MCP server over stdio with Content-Length framing.
Exposes graft coordination tools for AI agent integration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			server := newMCPServer(os.Stdin, os.Stdout, os.Stderr)
			server.withCodeintel = withCodeintel
			return server.run()
		},
	}

	cmd.Flags().BoolVar(&withCodeintel, "with-codeintel", false, "enable tree-sitter code intelligence tools (entities, symbols, references, exports, callers)")
	return cmd
}

// --- JSON-RPC types ---

type mcpRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpToolsListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpToolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type mcpToolCallResult struct {
	Content []mcpToolContent `json:"content,omitempty"`
	IsError bool             `json:"isError,omitempty"`
	Meta    map[string]any   `json:"_meta,omitempty"`
}

type mcpToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// --- Schema helpers ---

type mcpSchema struct {
	Properties map[string]mcpProperty
	Required   []string
}

type mcpProperty struct {
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

func (s mcpSchema) toMap() map[string]any {
	result := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for name, prop := range s.Properties {
			m := map[string]any{}
			if prop.Type != "" {
				m["type"] = prop.Type
			}
			if prop.Description != "" {
				m["description"] = prop.Description
			}
			props[name] = m
		}
		result["properties"] = props
	}
	if len(s.Required) > 0 {
		sorted := make([]string, len(s.Required))
		copy(sorted, s.Required)
		sort.Strings(sorted)
		result["required"] = sorted
	}
	return result
}

// --- MCP Server ---

type mcpServer struct {
	reader        *bufio.Reader
	writer        io.Writer
	log           *log.Logger
	outMu         sync.Mutex
	withCodeintel bool
}

func newMCPServer(in io.Reader, out io.Writer, logOut io.Writer) *mcpServer {
	return &mcpServer{
		reader: bufio.NewReader(in),
		writer: out,
		log:    log.New(logOut, "graft-mcp: ", log.LstdFlags),
	}
}

func (s *mcpServer) run() error {
	for {
		payload, err := mcpReadFramedMessage(s.reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var request mcpRPCRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			_ = s.sendError(json.RawMessage("null"), -32700, "parse error")
			continue
		}
		if strings.TrimSpace(request.Method) == "" {
			_ = s.sendError(request.ID, -32600, "invalid request: method is required")
			continue
		}

		// Notification path (no ID) -- except exit which stops server.
		if len(bytes.TrimSpace(request.ID)) == 0 || string(bytes.TrimSpace(request.ID)) == "null" {
			if request.Method == "exit" {
				return nil
			}
			continue
		}

		if request.Method == "exit" {
			_ = s.sendResult(request.ID, map[string]any{})
			return nil
		}

		result, rpcErr := s.handleRequest(request)
		if rpcErr != nil {
			if err := s.sendError(request.ID, rpcErr.Code, rpcErr.Message); err != nil {
				return err
			}
			continue
		}
		if err := s.sendResult(request.ID, result); err != nil {
			return err
		}
	}
}

func (s *mcpServer) handleRequest(request mcpRPCRequest) (any, *mcpRPCError) {
	switch request.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    mcpServerName,
				"version": mcpServerVersion,
			},
		}, nil
	case "initialized":
		return map[string]any{}, nil
	case "shutdown":
		return map[string]any{}, nil
	case "tools/list":
		tools := mcpToolDefs()
		tools = append(tools, mcpPlanToolDefs()...)
		if s.withCodeintel {
			tools = append(tools, mcpCodeintelToolDefs()...)
			tools = append(tools, mcpGrepToolDefs()...)
		}
		sort.Slice(tools, func(i, j int) bool {
			return tools[i].Name < tools[j].Name
		})
		return mcpToolsListResult{Tools: tools}, nil
	case "tools/call":
		var params mcpToolsCallParams
		if err := mcpDecodeParams(request.Params, &params); err != nil {
			return nil, &mcpRPCError{Code: -32602, Message: err.Error()}
		}
		if strings.TrimSpace(params.Name) == "" {
			return nil, &mcpRPCError{Code: -32602, Message: "missing tool name"}
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}

		started := time.Now()
		result, err := mcpDispatchAll(s.withCodeintel, params.Name, params.Arguments)
		durationMs := time.Since(started).Milliseconds()

		meta := map[string]any{
			"tool":        params.Name,
			"duration_ms": durationMs,
		}

		// Build coord summary for _meta.
		coordSummary := mcpBuildCoordSummary()
		if coordSummary != nil {
			meta["coord"] = coordSummary
		}

		if err != nil {
			meta["ok"] = false
			return mcpToolCallResult{
				IsError: true,
				Content: []mcpToolContent{
					{Type: "text", Text: err.Error()},
				},
				Meta: meta,
			}, nil
		}

		meta["ok"] = true
		encoded, encodeErr := json.MarshalIndent(result, "", "  ")
		if encodeErr != nil {
			encoded = []byte(`{"error":"failed to encode result"}`)
		}
		return mcpToolCallResult{
			Content: []mcpToolContent{
				{Type: "text", Text: string(encoded)},
			},
			Meta: meta,
		}, nil

	default:
		return nil, &mcpRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", request.Method)}
	}
}

func mcpDecodeParams(raw json.RawMessage, out any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (s *mcpServer) sendResult(id json.RawMessage, result any) error {
	return s.sendResponse(mcpRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *mcpServer) sendError(id json.RawMessage, code int, message string) error {
	return s.sendResponse(mcpRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &mcpRPCError{Code: code, Message: message},
	})
}

func (s *mcpServer) sendResponse(response mcpRPCResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return s.writeFramed(payload)
}

func (s *mcpServer) writeFramed(payload []byte) error {
	s.outMu.Lock()
	defer s.outMu.Unlock()

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(s.writer, header); err != nil {
		return err
	}
	_, err := s.writer.Write(payload)
	return err
}

func mcpReadFramedMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" {
				return nil, io.EOF
			}
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		if key != "content-length" {
			continue
		}
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid Content-Length %q", value)
		}
		contentLength = parsed
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// --- Tool definitions ---

func mcpToolDefs() []mcpTool {
	tools := []mcpTool{
		{
			Name:        "graft_workon",
			Description: "Join a coordination session as a named agent. Registers the agent and acquires entity claims as files are edited.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"name":          {Type: "string", Description: "agent name (required)"},
					"auto_discover": {Type: "boolean", Description: "discover workspaces from go.mod"},
					"notify":        {Type: "string", Description: "notification filter: all or breaking (default: all)"},
					"conflict_mode": {Type: "string", Description: "conflict mode: advisory, soft_block, hard_block"},
					"watch_only":    {Type: "boolean", Description: "observe only, don't claim entities"},
					"scope":         {Type: "string", Description: "limit coordination to package pattern"},
				},
				Required: []string{"name"},
			}.toMap(),
		},
		{
			Name:        "graft_workon_done",
			Description: "Leave the current coordination session. Deregisters the agent and releases all claims.",
			InputSchema: mcpSchema{}.toMap(),
		},
		{
			Name:        "graft_coord_status",
			Description: "Show coordination dashboard: agent count, claims, conflicts, and feed summary.",
			InputSchema: mcpSchema{}.toMap(),
		},
		{
			Name:        "graft_coord_agents",
			Description: "List all registered coordination agents with their workspace, host, and heartbeat info.",
			InputSchema: mcpSchema{}.toMap(),
		},
		{
			Name:        "graft_coord_claims",
			Description: "List all active entity claims with agent, mode, and file info.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"workspace": {Type: "string", Description: "filter claims by workspace name"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_coord_feed",
			Description: "Read coordination feed events (claim changes, commits, impact notifications).",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"since": {Type: "string", Description: "show events after this feed hash"},
					"mine":  {Type: "boolean", Description: "show only events from the active agent"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_coord_impact",
			Description: "Run cross-workspace impact analysis for entity changes. Shows which downstream workspaces and agents are affected.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"entities": {Type: "string", Description: "comma-separated entity keys to analyze (optional; uses recent feed if omitted)"},
				},
			}.toMap(),
		},
		{
			Name:        "graft_coord_check",
			Description: "Quick conflict check optimized for hook integration. Returns whether any other agents hold editing claims that conflict with the active agent.",
			InputSchema: mcpSchema{}.toMap(),
		},
		{
			Name:        "graft_coord_diff",
			Description: "Show another agent's claimed entities and info.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"agent_id": {Type: "string", Description: "target agent ID (required)"},
				},
				Required: []string{"agent_id"},
			}.toMap(),
		},
		{
			Name:        "graft_coord_xrefs",
			Description: "Reverse call lookup for a qualified symbol name. Shows all call sites that reference the symbol.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"name": {Type: "string", Description: "qualified symbol name (required)"},
				},
				Required: []string{"name"},
			}.toMap(),
		},
		{
			Name:        "graft_coord_graph",
			Description: "Show workspace dependency graph built from go.mod dependencies.",
			InputSchema: mcpSchema{}.toMap(),
		},
		{
			Name:        "graft_coord_watch",
			Description: "Add a watch claim on an entity. Watches receive notifications when the entity is modified by other agents.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"entity_key": {Type: "string", Description: "entity key to watch (required)"},
				},
				Required: []string{"entity_key"},
			}.toMap(),
		},
		{
			Name:        "graft_coord_unwatch",
			Description: "Remove a watch claim from an entity.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"entity_key": {Type: "string", Description: "entity key to unwatch (required)"},
				},
				Required: []string{"entity_key"},
			}.toMap(),
		},
		{
			Name:        "graft_coord_resolve",
			Description: "Release or transfer a claim. Use to resolve conflicts or hand off entities to another agent.",
			InputSchema: mcpSchema{
				Properties: map[string]mcpProperty{
					"key_hash": {Type: "string", Description: "entity key hash (required)"},
					"transfer": {Type: "string", Description: "agent ID to transfer the claim to (optional; releases if omitted)"},
				},
				Required: []string{"key_hash"},
			}.toMap(),
		},
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return tools
}

// --- Tool dispatch ---

func mcpDispatchTool(name string, args map[string]any) (any, error) {
	switch name {
	case "graft_workon":
		return mcpToolWorkon(args)
	case "graft_workon_done":
		return mcpToolWorkonDone(args)
	case "graft_coord_status":
		return mcpToolCoordStatus(args)
	case "graft_coord_agents":
		return mcpToolCoordAgents(args)
	case "graft_coord_claims":
		return mcpToolCoordClaims(args)
	case "graft_coord_feed":
		return mcpToolCoordFeed(args)
	case "graft_coord_impact":
		return mcpToolCoordImpact(args)
	case "graft_coord_check":
		return mcpToolCoordCheck(args)
	case "graft_coord_diff":
		return mcpToolCoordDiff(args)
	case "graft_coord_xrefs":
		return mcpToolCoordXrefs(args)
	case "graft_coord_graph":
		return mcpToolCoordGraph(args)
	case "graft_coord_watch":
		return mcpToolCoordWatch(args)
	case "graft_coord_unwatch":
		return mcpToolCoordUnwatch(args)
	case "graft_coord_resolve":
		return mcpToolCoordResolve(args)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

// mcpDispatchAll routes a tool call to the correct dispatcher.
func mcpDispatchAll(withCodeintel bool, name string, args map[string]any) (any, error) {
	// Route by prefix to avoid fragile error-string matching.
	switch {
	case strings.HasPrefix(name, "graft_plan_"):
		return mcpDispatchPlanTool(name, args)
	case strings.HasPrefix(name, "graft_ci_"):
		if !withCodeintel {
			return nil, fmt.Errorf("unknown tool %q (code intelligence tools require --with-codeintel)", name)
		}
		return mcpDispatchCodeintelTool(name, args)
	case strings.HasPrefix(name, "graft_grep"), name == "graft_entity_edit":
		if !withCodeintel {
			return nil, fmt.Errorf("unknown tool %q (structural grep tools require --with-codeintel)", name)
		}
		return mcpDispatchGrepTool(name, args)
	default:
		return mcpDispatchTool(name, args)
	}
}

// --- Arg helpers ---

func mcpArgString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func mcpArgBool(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if ok {
		return b
	}
	// Handle string "true"/"false" from some clients.
	s, ok := v.(string)
	if ok {
		return strings.EqualFold(s, "true")
	}
	return false
}

// --- Coord summary for _meta ---

func mcpBuildCoordSummary() map[string]any {
	r, err := repo.Open(".")
	if err != nil {
		return nil
	}
	c := coord.New(r, coord.DefaultConfig)

	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()
	feedEvents, _ := c.WalkFeed("", 100)

	activeID := readActiveAgentID(r)

	// Count my claims.
	myClaims := 0
	for _, cl := range claims {
		if cl.Agent == activeID {
			myClaims++
		}
	}

	// Count conflicts.
	conflictCount := 0
	claimsByEntity := make(map[string][]string)
	for _, cl := range claims {
		claimsByEntity[cl.EntityKeyHash] = append(claimsByEntity[cl.EntityKeyHash], cl.AgentName)
	}
	for _, holders := range claimsByEntity {
		if len(holders) > 1 {
			conflictCount++
		}
	}

	// Count unread feed (events not from us).
	unread := 0
	for _, ev := range feedEvents {
		if ev.AgentID != activeID {
			unread++
		}
	}

	return map[string]any{
		"active_agents": len(agents),
		"your_claims":   myClaims,
		"conflicts":     conflictCount,
		"unread_feed":   unread,
	}
}

// --- Tool implementations ---

func mcpToolWorkon(args map[string]any) (any, error) {
	name := mcpArgString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	r, err := repo.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	cfg := coord.DefaultConfig
	conflictMode := mcpArgString(args, "conflict_mode")
	if conflictMode != "" {
		cfg.ConflictMode = conflictMode
	}
	c := coord.New(r, cfg)

	hostname, _ := os.Hostname()
	info := coord.AgentInfo{
		Name:      name,
		Workspace: filepath.Base(r.RootDir),
		Host:      hostname,
	}

	id, err := c.RegisterAgent(info)
	if err != nil {
		return nil, fmt.Errorf("register agent: %w", err)
	}

	// Save agent ID.
	agentIDDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(agentIDDir, 0o755); err != nil {
		return nil, fmt.Errorf("create coord dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(agentIDDir, "agent-id"), []byte(id), 0o644); err != nil {
		return nil, fmt.Errorf("save agent ID: %w", err)
	}

	// Auto-discover workspaces if requested.
	var discovered map[string]string
	if mcpArgBool(args, "auto_discover") {
		discovered, _ = coord.AutoDiscoverWorkspaces(r.RootDir)
		if len(discovered) > 0 {
			ucfg, err := userconfig.Load()
			if err == nil {
				if ucfg.Workspaces == nil {
					ucfg.Workspaces = make(map[string]string)
				}
				for wsName, wsPath := range discovered {
					if _, exists := ucfg.Workspaces[wsName]; !exists {
						ucfg.Workspaces[wsName] = wsPath
					}
				}
				_ = userconfig.Save(ucfg)
			}
		}
	}

	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()

	result := map[string]any{
		"status":     "joined",
		"agent_id":   id,
		"agent_name": name,
		"workspace":  info.Workspace,
		"agents":     len(agents),
		"claims":     len(claims),
	}
	if len(discovered) > 0 {
		result["discovered"] = discovered
	}
	return result, nil
}

func mcpToolWorkonDone(_ map[string]any) (any, error) {
	r, err := repo.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}

	c := coord.New(r, coord.DefaultConfig)

	agentIDPath := filepath.Join(r.GraftDir, "coord", "agent-id")
	data, err := os.ReadFile(agentIDPath)
	if err != nil {
		return map[string]any{"status": "already_done"}, nil
	}
	agentID := strings.TrimSpace(string(data))

	agent, err := c.GetAgent(agentID)
	if err != nil {
		_ = os.Remove(agentIDPath)
		return map[string]any{"status": "already_done"}, nil
	}

	agentName := agent.Name
	if err := c.DeregisterAgent(agentID); err != nil {
		return nil, fmt.Errorf("deregister agent: %w", err)
	}

	_ = os.Remove(agentIDPath)

	return map[string]any{
		"status":     "left",
		"agent_id":   agentID,
		"agent_name": agentName,
	}, nil
}

func mcpToolCoordStatus(_ map[string]any) (any, error) {
	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	agents, _ := c.ListAgents()
	claims, _ := c.ListClaims()
	feedEvents, _ := c.WalkFeed("", 100)

	conflictCount := 0
	claimsByEntity := make(map[string][]string)
	for _, cl := range claims {
		claimsByEntity[cl.EntityKeyHash] = append(claimsByEntity[cl.EntityKeyHash], cl.AgentName)
	}
	for _, holders := range claimsByEntity {
		if len(holders) > 1 {
			conflictCount++
		}
	}

	activeID := readActiveAgentID(r)

	return map[string]any{
		"agents":    len(agents),
		"claims":    len(claims),
		"conflicts": conflictCount,
		"feed":      len(feedEvents),
		"active_id": activeID,
	}, nil
}

func mcpToolCoordAgents(_ map[string]any) (any, error) {
	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	agents, err := c.ListAgents()
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	if agents == nil {
		agents = []coord.AgentInfo{}
	}
	return agents, nil
}

func mcpToolCoordClaims(args map[string]any) (any, error) {
	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	claims, err := c.ListClaims()
	if err != nil {
		return nil, fmt.Errorf("list claims: %w", err)
	}

	workspace := mcpArgString(args, "workspace")
	if workspace != "" {
		var filtered []coord.ClaimInfo
		for _, cl := range claims {
			if strings.Contains(cl.AgentName, workspace) || strings.Contains(cl.File, workspace) {
				filtered = append(filtered, cl)
			}
		}
		claims = filtered
	}

	if claims == nil {
		claims = []coord.ClaimInfo{}
	}
	return claims, nil
}

func mcpToolCoordFeed(args map[string]any) (any, error) {
	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	since := mcpArgString(args, "since")
	events, err := c.WalkFeed(since, 50)
	if err != nil {
		return nil, fmt.Errorf("walk feed: %w", err)
	}

	if mcpArgBool(args, "mine") {
		activeID := readActiveAgentID(r)
		if activeID != "" {
			var filtered []coord.FeedEvent
			for _, ev := range events {
				if ev.AgentID == activeID {
					filtered = append(filtered, ev)
				}
			}
			events = filtered
		}
	}

	if events == nil {
		events = []coord.FeedEvent{}
	}
	return events, nil
}

func mcpToolCoordImpact(args map[string]any) (any, error) {
	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	cfg, _ := userconfig.Load()
	workspaces := make(map[string]string)
	if cfg != nil && cfg.Workspaces != nil {
		workspaces = cfg.Workspaces
	}

	var changes []coord.EntityChange

	entitiesStr := mcpArgString(args, "entities")
	if entitiesStr != "" {
		for _, key := range strings.Split(entitiesStr, ",") {
			key = strings.TrimSpace(key)
			if key != "" {
				changes = append(changes, coord.EntityChange{
					Key:    key,
					Change: "unknown",
				})
			}
		}
	} else {
		events, _ := c.WalkFeed("", 10)
		for _, ev := range events {
			changes = append(changes, ev.Entities...)
		}
	}

	if len(changes) == 0 {
		return &coord.ImpactReport{}, nil
	}

	report, err := c.AnalyzeImpact(changes, workspaces)
	if err != nil {
		return nil, fmt.Errorf("analyze impact: %w", err)
	}
	return report, nil
}

func mcpToolCoordCheck(_ map[string]any) (any, error) {
	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	activeID := readActiveAgentID(r)
	claims, _ := c.ListClaims()

	type conflict struct {
		EntityKey string `json:"entity_key"`
		File      string `json:"file"`
		HeldBy    string `json:"held_by"`
		Mode      string `json:"mode"`
	}

	var conflicts []conflict
	if activeID != "" {
		for _, cl := range claims {
			if cl.Agent != activeID && cl.Mode == coord.ClaimEditing {
				conflicts = append(conflicts, conflict{
					EntityKey: cl.EntityKey,
					File:      cl.File,
					HeldBy:    cl.AgentName,
					Mode:      cl.Mode,
				})
			}
		}
	}

	return map[string]any{
		"ok":        len(conflicts) == 0,
		"conflicts": conflicts,
	}, nil
}

func mcpToolCoordDiff(args map[string]any) (any, error) {
	agentID := mcpArgString(args, "agent_id")
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	agent, err := c.GetAgent(agentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}

	claims, _ := c.ListClaims()
	var agentClaims []coord.ClaimInfo
	for _, cl := range claims {
		if cl.Agent == agentID {
			agentClaims = append(agentClaims, cl)
		}
	}
	if agentClaims == nil {
		agentClaims = []coord.ClaimInfo{}
	}

	return map[string]any{
		"agent":  agent,
		"claims": agentClaims,
	}, nil
}

func mcpToolCoordXrefs(args map[string]any) (any, error) {
	name := mcpArgString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	idx, err := c.LoadXrefIndex()
	if err != nil {
		// Try building a fresh one.
		modulePath := ""
		gomodPath := filepath.Join(c.Repo.RootDir, "go.mod")
		if deps, parseErr := coord.ParseGoModDeps(gomodPath); parseErr == nil {
			modulePath = deps.Module
		}
		idx, err = coord.BuildXrefIndex(c.Repo.RootDir, modulePath)
		if err != nil {
			return nil, fmt.Errorf("build xref index: %w", err)
		}
	}

	sites, ok := idx.Refs[name]
	if !ok {
		return []coord.XrefCallSite{}, nil
	}
	return sites, nil
}

func mcpToolCoordGraph(_ map[string]any) (any, error) {
	cfg, err := userconfig.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if cfg.Workspaces == nil || len(cfg.Workspaces) == 0 {
		return map[string]any{
			"workspaces": map[string]string{},
			"edges":      []any{},
		}, nil
	}

	graph, err := coord.BuildWorkspaceGraph(cfg.Workspaces)
	if err != nil {
		return nil, fmt.Errorf("build workspace graph: %w", err)
	}

	type graphEdge struct {
		From string `json:"from"`
		To   string `json:"to"`
	}

	var edges []graphEdge
	for wsName := range cfg.Workspaces {
		deps := graph.DependentsOf(wsName)
		for _, dep := range deps {
			edges = append(edges, graphEdge{From: dep, To: wsName})
		}
	}
	if edges == nil {
		edges = []graphEdge{}
	}

	return map[string]any{
		"workspaces": cfg.Workspaces,
		"edges":      edges,
	}, nil
}

func mcpToolCoordWatch(args map[string]any) (any, error) {
	entityKey := mcpArgString(args, "entity_key")
	if entityKey == "" {
		return nil, fmt.Errorf("entity_key is required")
	}

	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	activeID := readActiveAgentID(r)
	if activeID == "" {
		return nil, fmt.Errorf("no active coordination session; use graft_workon first")
	}

	err = c.AcquireClaim(activeID, coord.ClaimRequest{
		EntityKey: entityKey,
		File:      "",
		Mode:      coord.ClaimWatching,
	})
	if err != nil {
		return nil, fmt.Errorf("watch: %w", err)
	}

	return map[string]any{
		"status":     "watching",
		"entity_key": entityKey,
	}, nil
}

func mcpToolCoordUnwatch(args map[string]any) (any, error) {
	entityKey := mcpArgString(args, "entity_key")
	if entityKey == "" {
		return nil, fmt.Errorf("entity_key is required")
	}

	c, _, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	keyHash := coord.EntityKeyHash(entityKey)
	if err := c.ReleaseClaim(keyHash); err != nil {
		return nil, fmt.Errorf("unwatch: %w", err)
	}

	return map[string]any{
		"status":     "unwatched",
		"entity_key": entityKey,
	}, nil
}

func mcpToolCoordResolve(args map[string]any) (any, error) {
	keyHash := mcpArgString(args, "key_hash")
	if keyHash == "" {
		return nil, fmt.Errorf("key_hash is required")
	}

	transferTo := mcpArgString(args, "transfer")

	c, r, err := openCoordinator()
	if err != nil {
		return nil, err
	}

	if transferTo != "" {
		activeID := readActiveAgentID(r)
		if activeID == "" {
			return nil, fmt.Errorf("no active session for transfer source")
		}
		if err := c.TransferClaim(keyHash, activeID, transferTo); err != nil {
			return nil, fmt.Errorf("transfer: %w", err)
		}
		return map[string]any{
			"status":   "transferred",
			"key_hash": keyHash,
			"to_agent": transferTo,
		}, nil
	}

	if err := c.ReleaseClaim(keyHash); err != nil {
		return nil, fmt.Errorf("release: %w", err)
	}

	return map[string]any{
		"status":   "released",
		"key_hash": keyHash,
	}, nil
}
