package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PageAgentDef describes one page-scoped system agent.
type PageAgentDef struct {
	Scope        string
	Name         string
	Description  string
	Instructions string
}

// PageAgentDefs is the canonical list of page system agents auto-created per workspace.
var PageAgentDefs = []PageAgentDef{
	{
		Scope:        "account",
		Name:         "Account Agent",
		Description:  "Manages identity cards, generates agent descriptions, handles profile updates",
		Instructions: "You are the Account page system agent. You manage identity cards and agent profiles. Generate and update identity descriptions based on agent history, skills, and workspace context.",
	},
	{
		Scope:        "session",
		Name:         "Session Agent",
		Description:  "Manages auto-reply routing, ensures every message gets a reply, handles @mentions",
		Instructions: "You are the Session page system agent. You ensure every message in conversations gets an appropriate reply. When agents are @mentioned, enforce timely responses. Route messages to the most relevant agent.",
	},
	{
		Scope:        "project",
		Name:         "Project Agent",
		Description:  "Generates project plans, manages workflow execution, handles failure escalation",
		Instructions: "You are the Project page system agent. You generate project plans from conversation context, orchestrate multi-agent workflows, handle execution failures, and report results back to project channels.",
	},
	{
		Scope:        "file",
		Name:         "File Agent",
		Description:  "Manages file indexing, tracks file versions, links files to projects",
		Instructions: "You are the File page system agent. You manage the file index, track file versions from agent outputs and messages, and maintain file-to-project links.",
	},
}

// EnsurePageAgents idempotently creates all page system agents for a workspace.
// Safe to call repeatedly — existing agents are left untouched.
// The ownerID parameter is retained for call-site compatibility but no longer
// written to the row — page system agents have owner_id IS NULL after Account
// Phase 2 (agent_type_owner_match constraint).
func EnsurePageAgents(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, ownerID pgtype.UUID) {
	_ = ownerID
	// Ensure cloud runtime to use as FK for page system agents.
	cloudRuntime, rterr := q.EnsureCloudRuntime(ctx, workspaceID)
	if rterr != nil {
		slog.Warn("page agents: ensure cloud runtime failed", "error", rterr)
		return
	}

	for _, def := range PageAgentDefs {
		_, err := q.GetPageSystemAgent(ctx, db.GetPageSystemAgentParams{
			WorkspaceID: workspaceID,
			Scope:       pgtype.Text{String: def.Scope, Valid: true},
		})
		if err == nil {
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("page agent lookup failed", "scope", def.Scope, "error", err)
			continue
		}

		_, err = q.CreatePageSystemAgent(ctx, db.CreatePageSystemAgentParams{
			WorkspaceID:  workspaceID,
			Name:         def.Name,
			Description:  def.Description,
			Instructions: def.Instructions,
			Scope:        pgtype.Text{String: def.Scope, Valid: true},
			RuntimeID:    cloudRuntime.ID,
		})
		if err != nil {
			slog.Warn("failed to create page agent", "scope", def.Scope, "error", err)
			continue
		}
		slog.Info("created page system agent", "scope", def.Scope, "workspace_id", workspaceID)
	}
}
