package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/spf13/cobra"
)

func newCoordNoteCmd(jsonFlag *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Manage shared coordination notes",
		Long:  `Notes live in refs/coord/notes and act as shared scratch, handoff, status, and decision space outside tracked source history.`,
	}

	cmd.AddCommand(newCoordNoteListCmd(jsonFlag))
	cmd.AddCommand(newCoordNoteGetCmd(jsonFlag))
	cmd.AddCommand(newCoordNoteCreateCmd(jsonFlag))
	cmd.AddCommand(newCoordNoteUpdateCmd(jsonFlag))
	cmd.AddCommand(newCoordNoteDeleteCmd(jsonFlag))

	return cmd
}

func newCoordNoteListCmd(jsonFlag *bool) *cobra.Command {
	var (
		kindFilter   string
		statusFilter string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List shared coordination notes",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			notes, err := c.ListNotes()
			if err != nil {
				return fmt.Errorf("list notes: %w", err)
			}

			filtered := make([]*coord.Note, 0, len(notes))
			for _, note := range notes {
				if kindFilter != "" && note.Kind != kindFilter {
					continue
				}
				if statusFilter != "" && note.Status != statusFilter {
					continue
				}
				filtered = append(filtered, note)
			}

			if *jsonFlag {
				return outputJSON(filtered)
			}

			if len(filtered) == 0 {
				fmt.Println("No notes.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tKIND\tSTATUS\tTITLE\tUPDATED")
			for _, note := range filtered {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					note.ID,
					note.Kind,
					note.Status,
					truncate(note.Title, 48),
					note.UpdatedAt.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&kindFilter, "kind", "", "filter by note kind")
	cmd.Flags().StringVar(&statusFilter, "status", "", "filter by note status")
	return cmd
}

func newCoordNoteGetCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "get <note-id>",
		Short: "Show a shared coordination note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			note, err := c.GetNote(args[0])
			if err != nil {
				return fmt.Errorf("get note: %w", err)
			}

			if *jsonFlag {
				return outputJSON(note)
			}

			fmt.Printf("Note: %s\n", note.Title)
			fmt.Printf("  ID:        %s\n", note.ID)
			fmt.Printf("  Kind:      %s\n", note.Kind)
			fmt.Printf("  Status:    %s\n", note.Status)
			if note.Author != "" {
				fmt.Printf("  Author:    %s\n", note.Author)
			}
			if note.Workspace != "" {
				fmt.Printf("  Workspace: %s\n", note.Workspace)
			}
			if note.PlanID != "" {
				fmt.Printf("  Plan:      %s\n", note.PlanID)
			}
			if note.TaskID != "" {
				fmt.Printf("  Task:      %s\n", note.TaskID)
			}
			fmt.Printf("  Created:   %s\n", note.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("  Updated:   %s\n", note.UpdatedAt.Format("2006-01-02 15:04:05"))
			if len(note.Tags) > 0 {
				fmt.Printf("  Tags:      %s\n", strings.Join(note.Tags, ", "))
			}
			if note.Body != "" {
				fmt.Println()
				fmt.Println(note.Body)
			}
			return nil
		},
	}
}

func newCoordNoteCreateCmd(jsonFlag *bool) *cobra.Command {
	var (
		body      string
		bodyFile  string
		kind      string
		status    string
		workspace string
		planID    string
		taskID    string
		tags      []string
	)

	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a shared coordination note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, r, err := openCoordinator()
			if err != nil {
				return err
			}

			bodyText, err := readCoordNoteBody(body, bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}

			note := &coord.Note{
				Title:     args[0],
				Body:      bodyText,
				Kind:      kind,
				Status:    status,
				Author:    readActiveAgentID(r),
				Workspace: workspace,
				PlanID:    planID,
				TaskID:    taskID,
				Tags:      tags,
			}
			if err := c.CreateNote(note); err != nil {
				return fmt.Errorf("create note: %w", err)
			}

			if *jsonFlag {
				return outputJSON(note)
			}
			fmt.Printf("Created note %s: %s\n", note.ID, note.Title)
			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "note body text")
	cmd.Flags().StringVar(&bodyFile, "file", "", "read note body from file path or '-' for stdin")
	cmd.Flags().StringVar(&kind, "kind", "scratch", "note kind (scratch, handoff, status, decision)")
	cmd.Flags().StringVar(&status, "status", "active", "note status (active, paused, resolved, archived)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace/repo name")
	cmd.Flags().StringVar(&planID, "plan", "", "linked plan ID")
	cmd.Flags().StringVar(&taskID, "task", "", "linked task ID")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "tag to attach to the note (repeatable)")
	return cmd
}

func newCoordNoteUpdateCmd(jsonFlag *bool) *cobra.Command {
	var (
		title     string
		body      string
		bodyFile  string
		kind      string
		status    string
		workspace string
		planID    string
		taskID    string
		tags      []string
	)

	cmd := &cobra.Command{
		Use:   "update <note-id>",
		Short: "Update a shared coordination note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}

			note, err := c.GetNote(args[0])
			if err != nil {
				return fmt.Errorf("get note: %w", err)
			}

			if title != "" {
				note.Title = title
			}
			if cmd.Flags().Changed("body") || cmd.Flags().Changed("file") {
				bodyText, err := readCoordNoteBody(body, bodyFile, cmd.InOrStdin())
				if err != nil {
					return err
				}
				note.Body = bodyText
			}
			if kind != "" {
				note.Kind = kind
			}
			if status != "" {
				note.Status = status
			}
			if cmd.Flags().Changed("workspace") {
				note.Workspace = workspace
			}
			if cmd.Flags().Changed("plan") {
				note.PlanID = planID
			}
			if cmd.Flags().Changed("task") {
				note.TaskID = taskID
			}
			if cmd.Flags().Changed("tag") {
				note.Tags = tags
			}

			if err := c.UpdateNote(note); err != nil {
				return fmt.Errorf("update note: %w", err)
			}

			if *jsonFlag {
				return outputJSON(note)
			}
			fmt.Printf("Updated note %s\n", note.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&body, "body", "", "replace note body with this text")
	cmd.Flags().StringVar(&bodyFile, "file", "", "replace note body from file path or '-' for stdin")
	cmd.Flags().StringVar(&kind, "kind", "", "new note kind")
	cmd.Flags().StringVar(&status, "status", "", "new note status")
	cmd.Flags().StringVar(&workspace, "workspace", "", "set linked workspace/repo")
	cmd.Flags().StringVar(&planID, "plan", "", "set linked plan ID (empty string clears when passed)")
	cmd.Flags().StringVar(&taskID, "task", "", "set linked task ID (empty string clears when passed)")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "replace note tags (repeatable)")
	return cmd
}

func newCoordNoteDeleteCmd(jsonFlag *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <note-id>",
		Short: "Delete a shared coordination note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := openCoordinator()
			if err != nil {
				return err
			}
			if err := c.DeleteNote(args[0]); err != nil {
				return fmt.Errorf("delete note: %w", err)
			}
			if *jsonFlag {
				return outputJSON(map[string]string{
					"status": "deleted",
					"id":     args[0],
				})
			}
			fmt.Printf("Deleted note %s\n", args[0])
			return nil
		},
	}
}

func readCoordNoteBody(body, bodyFile string, in io.Reader) (string, error) {
	switch {
	case body != "" && bodyFile != "":
		return "", fmt.Errorf("use only one of --body or --file")
	case bodyFile == "":
		return body, nil
	case bodyFile == "-":
		data, err := io.ReadAll(in)
		if err != nil {
			return "", fmt.Errorf("read note body from stdin: %w", err)
		}
		return string(data), nil
	default:
		data, err := os.ReadFile(bodyFile)
		if err != nil {
			return "", fmt.Errorf("read note body file: %w", err)
		}
		return string(data), nil
	}
}
