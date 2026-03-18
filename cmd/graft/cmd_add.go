package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var quiet bool
	var skipEntities bool
	var forceEntities bool
	var forceCoord bool
	var stdin bool
	var stdin0 bool

	cmd := &cobra.Command{
		Use:   "add <files...>",
		Short: "Stage files for the next commit",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !stdin && !stdin0 {
				return fmt.Errorf("requires at least 1 arg(s), only received 0")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if stdin || stdin0 {
				scanner := bufio.NewScanner(os.Stdin)
				if stdin0 {
					scanner.Split(splitNull)
				}
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if line != "" {
						args = append(args, line)
					}
				}
				if err := scanner.Err(); err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			if err := configureCoordAddHook(r, cmd.ErrOrStderr(), forceCoord); err != nil {
				return err
			}

			opts := repo.AddOptions{
				SkipEntities:  skipEntities,
				ForceEntities: forceEntities,
			}

			if quiet {
				return r.AddWithOptions(args, nil, opts)
			}

			out := cmd.ErrOrStderr()
			progressLineActive := false
			progress := func(event repo.AddProgress) {
				switch event.Phase {
				case repo.AddProgressPhaseScanStart:
					fmt.Fprintln(out, "Scanning files...")
				case repo.AddProgressPhaseScanComplete:
					fmt.Fprintf(out, "Found %d file(s) to stage\n", event.Total)
				case repo.AddProgressPhaseStageFile:
					if shouldRenderAddProgress(event.Current, event.Total) {
						fmt.Fprintf(out, "\rStaging files... %d/%d", event.Current, event.Total)
						progressLineActive = true
					}
				case repo.AddProgressPhaseEntityStart:
					if progressLineActive {
						fmt.Fprintln(out)
						progressLineActive = false
					}
					fmt.Fprintln(out, "Extracting entities...")
				case repo.AddProgressPhaseEntityFile:
					if shouldRenderAddProgress(event.Current, event.Total) {
						fmt.Fprintf(out, "\rExtracting entities... %d/%d", event.Current, event.Total)
						progressLineActive = true
					}
				case repo.AddProgressPhaseEntityComplete:
					if progressLineActive {
						fmt.Fprintln(out)
						progressLineActive = false
					}
				case repo.AddProgressPhaseWriteIndex:
					if progressLineActive {
						fmt.Fprintln(out)
						progressLineActive = false
					}
					fmt.Fprintf(out, "Updated staging index (%d file(s))\n", event.Total)
				}
			}

			if err := r.AddWithOptions(args, progress, opts); err != nil {
				if progressLineActive {
					fmt.Fprintln(out)
				}
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress add progress output")
	cmd.Flags().BoolVar(&skipEntities, "skip-entities", false, "skip entity extraction (faster, lower memory)")
	cmd.Flags().BoolVar(&forceEntities, "force-entities", false, "force entity extraction for data formats above size threshold")
	cmd.Flags().BoolVar(&forceCoord, "force", false, "override coordination soft blocks during staging")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read file paths from stdin (one per line)")
	cmd.Flags().BoolVar(&stdin0, "stdin0", false, "read file paths from stdin, null-separated (for git ls-files -z)")
	return cmd
}

type coordAddHookError struct {
	message string
	block   bool
}

func (e *coordAddHookError) Error() string {
	return e.message
}

func (e *coordAddHookError) BlocksAdd() bool {
	return e.block
}

func configureCoordAddHook(r *repo.Repo, out io.Writer, forceCoord bool) error {
	activeID := readActiveAgentID(r)
	if activeID == "" {
		return nil
	}

	c := coord.New(r, coord.DefaultConfig)
	if _, err := c.GetAgent(activeID); err != nil {
		return nil
	}

	session, err := activeCoordSession(r.GraftDir, activeID)
	if err != nil {
		return fmt.Errorf("load coordination session: %w", err)
	}
	if session != nil && session.Mode == "watching" {
		return nil
	}

	scope := ""
	if session != nil {
		scope = session.Scope
	}

	r.AddHook = func(path string, entityKeys []string) error {
		if !pathWithinCoordScope(path, scope) {
			return nil
		}
		for _, entityKey := range entityKeys {
			if err := handleCoordAddClaim(c, out, activeID, coord.ClaimRequest{
				EntityKey: entityKey,
				File:      path,
				Mode:      coord.ClaimEditing,
			}, forceCoord); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

func handleCoordAddClaim(c *coord.Coordinator, out io.Writer, activeID string, req coord.ClaimRequest, forceCoord bool) error {
	for attempt := 0; attempt < 3; attempt++ {
		ctx, err := c.InspectClaimDecision(activeID, req)
		if err != nil {
			return &coordAddHookError{
				message: fmt.Sprintf("coord: inspect %s: %v", req.EntityKey, err),
				block:   true,
			}
		}
		if ctx == nil || ctx.Decision == nil {
			return &coordAddHookError{
				message: fmt.Sprintf("coord: missing decision for %s in %s", req.EntityKey, req.File),
				block:   true,
			}
		}

		message := coordDecisionMessage(req, ctx)
		switch ctx.Decision.Action {
		case "Allow":
			err := c.AcquireClaim(activeID, req)
			if err == nil {
				recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
					Status:        "claim_acquired",
					Message:       "claim acquired",
					ClaimAcquired: true,
				})
				return nil
			}
			var conflict *coord.ClaimConflictError
			switch {
			case errors.Is(err, coord.ErrEntityProtected):
				recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
					Status:  "hard_blocked",
					Message: "entity is protected",
					Error:   err.Error(),
				})
				return &coordAddHookError{
					message: fmt.Sprintf("coord: blocked %s in %s: entity is protected", req.EntityKey, req.File),
					block:   true,
				}
			case errors.As(err, &conflict):
				continue
			default:
				recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
					Status:  "error",
					Message: "claim acquisition failed",
					Error:   err.Error(),
				})
				return &coordAddHookError{
					message: fmt.Sprintf("coord: acquire %s: %v", req.EntityKey, err),
					block:   true,
				}
			}
		case "Advisory":
			fmt.Fprintln(out, message)
			recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
				Status:  "advisory_reported",
				Message: message,
			})
			return nil
		case "ReclaimSuggested":
			fmt.Fprintln(out, message)
			keyHash := coord.EntityKeyHash(req.EntityKey)
			if ctx.Existing != nil && ctx.Existing.EntityKeyHash != "" {
				keyHash = ctx.Existing.EntityKeyHash
			}
			if err := c.ReleaseClaim(keyHash); err != nil {
				recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
					Status:        "error",
					Message:       "failed to release stale claim",
					ClaimReleased: false,
					Error:         err.Error(),
				})
				return &coordAddHookError{
					message: fmt.Sprintf("coord: reclaim %s: %v", req.EntityKey, err),
					block:   true,
				}
			}
			if err := c.AcquireClaim(activeID, req); err != nil {
				var conflict *coord.ClaimConflictError
				if errors.As(err, &conflict) {
					continue
				}
				recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
					Status:        "error",
					Message:       "failed to acquire reclaimed claim",
					ClaimReleased: true,
					Error:         err.Error(),
				})
				return &coordAddHookError{
					message: fmt.Sprintf("coord: reclaim %s: %v", req.EntityKey, err),
					block:   true,
				}
			}
			fmt.Fprintf(out, "coord: reclaimed stale claim on %s\n", req.EntityKey)
			recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
				Status:        "stale_reclaimed",
				Message:       "stale claim reclaimed and acquired",
				ClaimReleased: true,
				ClaimAcquired: true,
			})
			return nil
		case "SoftBlock":
			if forceCoord {
				expectedOwnerID := ctx.Input.ExistingClaim.HeldByID
				forceResult, err := c.ForceAcquireClaim(activeID, req, expectedOwnerID)
				if err == nil {
					outcomeStatus := "force_acquired"
					outcomeMessage := message + "; claim acquired due to --force"
					outcome := coord.DecisionOutcome{
						Status:        outcomeStatus,
						Message:       outcomeMessage,
						ForceApplied:  true,
						ClaimAcquired: true,
					}
					if forceResult != nil && forceResult.Transferred {
						outcomeStatus = "force_transferred"
						holder := forceResult.PreviousAgentName
						if holder == "" {
							holder = forceResult.PreviousAgentID
						}
						outcomeMessage = message + "; claim transferred due to --force"
						if holder != "" {
							outcomeMessage += " from " + holder
						}
						outcome.Status = outcomeStatus
						outcome.Message = outcomeMessage
						outcome.ClaimTransferred = true
						outcome.TransferredFrom = holder
						outcome.TransferredFromID = forceResult.PreviousAgentID
					}
					fmt.Fprintln(out, outcomeMessage)
					recordCoordDecision(c, out, "graft add", activeID, req, ctx, outcome)
					return nil
				}

				var conflict *coord.ClaimConflictError
				switch {
				case errors.Is(err, coord.ErrEntityProtected):
					recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
						Status:       "hard_blocked",
						Message:      "entity is protected",
						ForceApplied: true,
						Error:        err.Error(),
					})
					return &coordAddHookError{
						message: fmt.Sprintf("coord: blocked %s in %s: entity is protected", req.EntityKey, req.File),
						block:   true,
					}
				case errors.As(err, &conflict), errors.Is(err, repo.ErrRefCASMismatch):
					continue
				default:
					recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
						Status:       "error",
						Message:      "force acquire failed",
						ForceApplied: true,
						Error:        err.Error(),
					})
					return &coordAddHookError{
						message: fmt.Sprintf("coord: force %s: %v", req.EntityKey, err),
						block:   true,
					}
				}
			}
			recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
				Status:  "soft_blocked",
				Message: message,
			})
			return &coordAddHookError{
				message: message + "; rerun with --force to stage anyway",
				block:   true,
			}
		default:
			recordCoordDecision(c, out, "graft add", activeID, req, ctx, coord.DecisionOutcome{
				Status:  "hard_blocked",
				Message: message,
			})
			return &coordAddHookError{
				message: message,
				block:   true,
			}
		}
	}

	return &coordAddHookError{
		message: fmt.Sprintf("coord: unable to settle claim for %s in %s after concurrent updates", req.EntityKey, req.File),
		block:   true,
	}
}

func coordDecisionMessage(req coord.ClaimRequest, ctx *coord.ClaimDecisionContext) string {
	if ctx == nil || ctx.Decision == nil {
		return fmt.Sprintf("coord: unresolved decision for %s in %s", req.EntityKey, req.File)
	}

	message := fmt.Sprintf("coord: %s for %s in %s", strings.ToLower(ctx.Decision.Action), req.EntityKey, req.File)
	holder := ctx.Input.ExistingClaim.HeldBy
	if holder == "" {
		holder = ctx.Input.ExistingClaim.HeldByID
	}
	if holder != "" {
		message += ": held by " + holder
	}
	if ctx.Decision.Reason != "" {
		message += " (" + ctx.Decision.Reason + ")"
	}
	return message
}

func recordCoordDecision(c *coord.Coordinator, out io.Writer, source, activeID string, req coord.ClaimRequest, ctx *coord.ClaimDecisionContext, outcome coord.DecisionOutcome) {
	outcome.Message = strings.TrimSpace(outcome.Message)
	if _, err := c.RecordClaimDecision(source, activeID, req, ctx, outcome); err != nil {
		fmt.Fprintf(out, "coord: warning: record decision for %s: %v\n", req.EntityKey, err)
	}
}

func activeCoordSession(graftDir, agentID string) (*coord.Session, error) {
	sessions, err := coord.ListSessions(graftDir)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].AgentID == agentID {
			return &sessions[i], nil
		}
	}
	return nil, nil
}

func pathWithinCoordScope(path, scope string) bool {
	if scope == "" {
		return true
	}
	if strings.HasSuffix(scope, "/...") {
		prefix := strings.TrimSuffix(scope, "/...")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	if matched, err := filepath.Match(scope, path); err == nil && matched {
		return true
	}
	return path == scope || strings.HasPrefix(path, scope+"/")
}

func splitNull(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func shouldRenderAddProgress(current, total int) bool {
	if total <= 0 {
		return false
	}
	if current <= 1 || current == total {
		return true
	}
	if total <= 100 {
		return true
	}
	step := total / 100 // cap updates to around 100 writes for huge adds
	if step < 10 {
		step = 10
	}
	return current%step == 0
}
