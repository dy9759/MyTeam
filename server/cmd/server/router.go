package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/service/asr"
	"github.com/multica-ai/multica/server/internal/service/embed"
	"github.com/multica-ai/multica/server/internal/service/memory"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/pkg/agent_runner"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func allowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	}
	if raw == "" {
		return []string{"http://localhost:3000"}
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	if len(origins) == 0 {
		return []string{"http://localhost:3000"}
	}
	return origins
}

// NewRouter creates the fully-configured Chi router with all middleware and routes.
func NewRouter(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus) chi.Router {
	queries := db.New(pool)
	emailSvc := service.NewEmailService()
	s3 := storage.NewS3StorageFromEnv()
	cfSigner := auth.NewCloudFrontSignerFromEnv()
	memorySvc := memory.NewService(queries).WithBus(bus)
	// Phase N: wire embedder + vector store when DASHSCOPE_API_KEY is set.
	// Without it, Search returns ErrIndexingNotWired (handler maps to 503),
	// CRUD/List still work — graceful degradation per env config.
	if apiKey := os.Getenv("DASHSCOPE_API_KEY"); apiKey != "" {
		model := os.Getenv("DASHSCOPE_EMBED_MODEL")
		if model == "" {
			model = "text-embedding-v4"
		}
		embedder := embed.NewDashScopeClient(apiKey, model)
		store := memory.NewPgvectorStore(pool, embedder.Dim())
		memorySvc = memorySvc.WithIndexing(memory.NewMarkdownChunker(), embedder, store)
	}
	h := handler.New(queries, pool, hub, bus, emailSvc, s3, cfSigner, memorySvc)
	// Meeting deps — Secrets+Storage always available; ASR client wired
	// when env config present (NewMiaojiClient has no required env, so
	// always wire). Endpoints return 503 only if Secrets/ASR are nil.
	secrets := service.NewSecretService(queries)
	h.Secrets = secrets
	h.ASR = asr.NewMiaojiClient()
	h.StorageFactory = &storage.Factory{
		Secrets:  secrets,
		Fallback: storage.NewS3Adapter(s3),
	}
	// Single Claude Agent SDK runner shared across every cloud-mode invocation
	// path so a system / personal / project agent's cloud_llm_config controls
	// the same SDK installation regardless of who triggered it.
	cloudRunner := agent_runner.NewRunner()
	h.AutoReplyService = service.NewAutoReplyService(queries, hub, cloudRunner)
	h.PlanGenerator = service.NewPlanGeneratorService(queries, cloudRunner)
	h.IdentityGenerator = service.NewIdentityGeneratorService(queries, cloudRunner)
	// Scheduler / Slots / Artifacts / Reviews / Quota are constructed inside
	// handler.New so route handlers and lifecycle services share the same
	// instances. See internal/handler/handler.go.

	// Identity generator + scheduler. Reuse the handler-bound instance so the
	// scheduler writes through the same Runner + Queries as ad-hoc invocations.
	identitySched := service.NewIdentitySchedulerService(queries, h.IdentityGenerator)
	identitySched.Start()

	// Start auto-reply poll daemon
	go h.AutoReplyService.StartPollDaemon(context.Background())

	// Start cloud executor service. Scheduler is wired in so that completing
	// or failing executions on the new execution table cascades into the
	// Plan/Run state machine (per PRD §10.3). The legacy agent_task_queue
	// path inside CloudExecutorService is unaffected.
	cloudExecutor := service.NewCloudExecutorService(queries, hub, bus, h.TaskService, h.Scheduler, cloudRunner)
	cloudExecutor.Start(context.Background())

	// Audit + notification services
	auditSvc := service.NewAuditService(queries)
	auditSvc.SubscribeToEvents(bus)

	notifSvc := service.NewNotificationService(queries, hub)
	notifSvc.SubscribeToEvents(bus)

	// Execution engine services (Phase 3)
	executionNotifier := service.NewExecutionNotifierService(queries, hub, bus)
	executionNotifier.Start()

	projectLifecycle := service.NewProjectLifecycleService(queries, hub, bus, h.Scheduler)
	projectLifecycle.Start()

	// File indexer service (Phase 4) - indexes files from messages and workflow outputs
	fileIndexer := service.NewFileIndexerService(queries, bus)
	fileIndexer.Start()

	// Results reporter service (Phase 4) - reports run completions to channels and inboxes
	resultsReporter := service.NewResultsReporterService(queries, hub, bus)
	resultsReporter.Start()

	// Mediation service — drives the Session page system agent.
	mediationSvc := service.NewMediationService(queries, hub, bus, h.AutoReplyService, pool)
	mediationSvc.Start()

	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(middleware.RequestLogger)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins(),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Workspace-ID", "X-Request-ID", "X-Agent-ID", "X-Task-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Prometheus metrics (public; scrapers don't carry JWT).
	r.Handle("/metrics", promhttp.Handler())

	// WebSocket
	mc := &membershipChecker{queries: queries}
	pr := &patResolver{queries: queries}
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		realtime.HandleWebSocket(hub, mc, pr, w, r)
	})

	// Auth (public)
	r.Post("/auth/send-code", h.SendCode)
	r.Post("/auth/verify-code", h.VerifyCode)

	// Daemon API routes (all require a valid token)
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(middleware.Auth(queries))

		r.Post("/register", h.DaemonRegister)
		r.Post("/deregister", h.DaemonDeregister)
		r.Post("/heartbeat", h.DaemonHeartbeat)

		r.Post("/runtimes/{runtimeId}/tasks/claim", h.ClaimTaskByRuntime)
		r.Get("/runtimes/{runtimeId}/tasks/pending", h.ListPendingTasksByRuntime)
		r.Post("/runtimes/{runtimeId}/usage", h.ReportRuntimeUsage)
		r.Post("/runtimes/{runtimeId}/ping/{pingId}/result", h.ReportPingResult)
		r.Post("/runtimes/{runtimeId}/update/{updateId}/result", h.ReportUpdateResult)

		r.Get("/tasks/{taskId}/status", h.GetTaskStatus)
		r.Post("/tasks/{taskId}/start", h.StartTask)
		r.Post("/tasks/{taskId}/progress", h.ReportTaskProgress)
		r.Post("/tasks/{taskId}/complete", h.CompleteTask)
		r.Post("/tasks/{taskId}/fail", h.FailTask)
		r.Post("/tasks/{taskId}/messages", h.ReportTaskMessages)
		r.Get("/tasks/{taskId}/messages", h.ListTaskMessages)

		// Plan 5 §10.2: Project Execution endpoints. The daemon polls
		// these alongside the Issue task endpoints above; when both
		// queues have work at the same priority, Project Execution wins.
		r.Get("/runtimes/{runtimeId}/executions/pending", h.ListPendingExecutions)
		r.Post("/runtimes/{runtimeId}/executions/claim", h.ClaimExecution)
		r.Post("/executions/{id}/start", h.StartExecution)
		r.Post("/executions/{id}/progress", h.ProgressExecution)
		r.Post("/executions/{id}/complete", h.CompleteExecution)
		r.Post("/executions/{id}/fail", h.FailExecution)
		r.Post("/executions/{id}/messages", h.StreamExecutionMessage)
	})

	// Protected API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))

		// --- User-scoped routes (no workspace context required) ---
		r.Get("/api/me", h.GetMe)
		r.Patch("/api/me", h.UpdateMe)
		r.Post("/api/upload-file", h.UploadFile)

		r.Route("/api/workspaces", func(r chi.Router) {
			r.Get("/", h.ListWorkspaces)
			r.Post("/", h.CreateWorkspace)
			r.Route("/{id}", func(r chi.Router) {
				// Member-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/", h.GetWorkspace)
					r.Get("/members", h.ListMembersWithUser)
					r.Post("/leave", h.LeaveWorkspace)
				})
				// Admin-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Put("/", h.UpdateWorkspace)
					r.Patch("/", h.UpdateWorkspace)
					r.Post("/members", h.CreateMember)
					r.Route("/members/{memberId}", func(r chi.Router) {
						r.Patch("/", h.UpdateMember)
						r.Delete("/", h.DeleteMember)
					})
					// Workspace secrets (admin/owner only — values authorize
					// external integrations and are never exposed to members).
					r.Route("/secrets", func(r chi.Router) {
						r.Get("/", h.ListWorkspaceSecrets)
						// Convenience PUT for the 5-key TOS storage block.
						// Routed before {key} so chi matches the literal path first.
						r.Put("/storage", h.SetStorageSecrets)
						r.Get("/{key}", h.GetWorkspaceSecret)
						r.Put("/{key}", h.SetWorkspaceSecret)
						r.Delete("/{key}", h.DeleteWorkspaceSecret)
					})
				})
				// Owner-only access
				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).Delete("/", h.DeleteWorkspace)
			})
		})

		r.Route("/api/tokens", func(r chi.Router) {
			r.Get("/", h.ListPersonalAccessTokens)
			r.Post("/", h.CreatePersonalAccessToken)
			r.Delete("/{id}", h.RevokePersonalAccessToken)
		})

		// Provider registry (static catalog of execution providers)
		providerHandler := handler.NewProviderHandler()
		r.Get("/api/providers", providerHandler.List)

		// --- Workspace-scoped routes (all require workspace membership) ---
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			// Typing
			r.Post("/api/typing", h.SendTypingIndicator)

			// Remote sessions
			r.Route("/api/remote-sessions", func(r chi.Router) {
				r.Post("/", h.CreateRemoteSession)
				r.Get("/", h.ListRemoteSessions)
				r.Route("/{remoteSessionID}", func(r chi.Router) {
					r.Get("/", h.GetRemoteSession)
					r.Patch("/status", h.UpdateRemoteSessionStatus)
					r.Post("/events", h.AddRemoteSessionEvent)
				})
			})

			// Search
			r.Get("/api/search", h.Search)

			// Memories
			r.Route("/api/memories", func(r chi.Router) {
				r.Post("/", h.CreateMemory)
				r.Get("/", h.ListMemories)
				r.Post("/search", h.SearchMemories)
				r.Post("/{id}/promote", h.PromoteMemory)
			})

			// Issues
			r.Route("/api/issues", func(r chi.Router) {
				r.Get("/", h.ListIssues)
				r.Post("/", h.CreateIssue)
				r.Post("/batch-update", h.BatchUpdateIssues)
				r.Post("/batch-delete", h.BatchDeleteIssues)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetIssue)
					r.Put("/", h.UpdateIssue)
					r.Delete("/", h.DeleteIssue)
					r.Post("/comments", h.CreateComment)
					r.Get("/comments", h.ListComments)
					r.Get("/timeline", h.ListTimeline)
					r.Get("/subscribers", h.ListIssueSubscribers)
					r.Post("/subscribe", h.SubscribeToIssue)
					r.Post("/unsubscribe", h.UnsubscribeFromIssue)
					r.Get("/active-task", h.GetActiveTaskForIssue)
					r.Post("/tasks/{taskId}/cancel", h.CancelTask)
					r.Get("/task-runs", h.ListTasksByIssue)
					r.Post("/reactions", h.AddIssueReaction)
					r.Delete("/reactions", h.RemoveIssueReaction)
					r.Get("/attachments", h.ListAttachments)
				})
			})

			// System Agent
			r.Get("/api/system-agent", h.GetOrCreateSystemAgent)
			r.Post("/api/system-agents", h.CreateSystemAgent)
			r.Patch("/api/system-agents/{id}", h.UpdateSystemAgent)
			r.Delete("/api/system-agents/{id}", h.ArchiveSystemAgent)

			// Page system agents (account / session / project / file).
			r.Get("/api/page-agents", h.ListPageAgents)
			r.Get("/api/page-agents/{scope}", h.GetPageAgent)

			// Personal Agent
			r.Get("/api/personal-agent", h.GetPersonalAgent)
			r.Patch("/api/personal-agent/config", h.UpdatePersonalAgentConfig)

			// Attachments
			r.Get("/api/attachments/{id}", h.GetAttachmentByID)
			r.Delete("/api/attachments/{id}", h.DeleteAttachment)
			r.Get("/api/files/{id}/versions", h.ListFileVersions)

			// Comments
			r.Route("/api/comments/{commentId}", func(r chi.Router) {
				r.Put("/", h.UpdateComment)
				r.Delete("/", h.DeleteComment)
				r.Post("/reactions", h.AddReaction)
				r.Delete("/reactions", h.RemoveReaction)
			})

			// Agents
			r.Route("/api/agents", func(r chi.Router) {
				r.Get("/", h.ListAgents)
				r.With(middleware.RequireWorkspaceRole(queries, "owner", "admin")).Post("/", h.CreateAgent)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetAgent)
					r.Put("/", h.UpdateAgent)
					r.Post("/archive", h.ArchiveAgent)
					r.Post("/restore", h.RestoreAgent)
					r.Get("/tasks", h.ListAgentTasks)
					r.Get("/skills", h.ListAgentSkills)
					r.Put("/skills", h.SetAgentSkills)

					// Agent profile & auto-reply (AgentMesh integration)
					r.Get("/profile", h.GetAgentProfile)
					r.Patch("/profile", h.UpdateAgentProfile)
					r.Get("/auto-reply", h.GetAgentAutoReply)
					r.Patch("/auto-reply", h.UpdateAgentAutoReply)

					// Impersonation (Owner 附身)
					r.Post("/impersonate", h.StartImpersonation)
					r.Post("/release", h.EndImpersonation)
					r.Get("/impersonation", h.GetImpersonation)

					// Agent inbox — pull fallback for the WS push from
					// realtime.Hub.SendToAgent. REST is the ground truth
					// so an agent that missed a push still recovers on poll.
					r.Get("/inbox", h.GetAgentInbox)
				})
			})

			// Agent ↔ agent interaction protocol (migration 075).
			// Unified send endpoint covers DM / channel / capability
			// broadcast / session-scoped message; schema field gives the
			// receiver a routing hint for typed handlers.
			r.Route("/api/interactions", func(r chi.Router) {
				r.Post("/", h.SendInteraction)
				r.Post("/{id}/ack", h.AckInteraction)
			})

			// Skills
			r.Route("/api/skills", func(r chi.Router) {
				r.Get("/", h.ListSkills)
				r.With(middleware.RequireWorkspaceRole(queries, "owner", "admin")).Post("/", h.CreateSkill)
				r.With(middleware.RequireWorkspaceRole(queries, "owner", "admin")).Post("/import", h.ImportSkill)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetSkill)
					r.Put("/", h.UpdateSkill)
					r.Delete("/", h.DeleteSkill)
					r.Get("/files", h.ListSkillFiles)
					r.Put("/files", h.UpsertSkillFile)
					r.Delete("/files/{fileId}", h.DeleteSkillFile)
					r.Post("/broadcast", h.SkillBroadcast)
				})
			})

			// Subagents — templates that wrap skills. Only subagents can
			// bridge an agent to a skill; agents cannot link skills
			// directly anymore (enforced by PlanGenerator + the fact
			// that subagent_skill is the only live write-path).
			r.Route("/api/subagents", func(r chi.Router) {
				r.Get("/", h.ListSubagents)
				r.Post("/", h.CreateSubagent)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetSubagent)
					r.Patch("/", h.UpdateSubagent)
					r.Delete("/", h.DeleteSubagent)
					r.Post("/skills", h.LinkSubagentSkill)
					r.Delete("/skills/{skillID}", h.UnlinkSubagentSkill)
				})
			})

			// Runtimes
			r.Route("/api/runtimes", func(r chi.Router) {
				r.Get("/", h.ListAgentRuntimes)
				r.Get("/{runtimeId}/usage", h.GetRuntimeUsage)
				r.Get("/{runtimeId}/activity", h.GetRuntimeTaskActivity)
				r.Post("/{runtimeId}/ping", h.InitiatePing)
				r.Get("/{runtimeId}/ping/{pingId}", h.GetPing)
				r.Post("/{runtimeId}/update", h.InitiateUpdate)
				r.Get("/{runtimeId}/update/{updateId}", h.GetUpdate)
			})

			// DM conversation per-user state (archive).
			r.Route("/api/conversations", func(r chi.Router) {
				r.Post("/archive", h.ArchiveDMConversation)
				r.Get("/archived", h.ListArchivedDMPeers)
			})

			// Messaging (AgentMesh integration)
			r.Route("/api/messages", func(r chi.Router) {
				r.Post("/", h.CreateMessage)
				r.Get("/", h.ListMessages)
				r.Post("/read", h.MarkMessagesRead)
				r.Get("/conversations", h.ListConversations)
				r.Route("/{messageID}", func(r chi.Router) {
					r.Get("/thread", h.ListThread)
				})
			})

			r.Post("/api/threads/{threadID}/promote", h.PromoteThread)

			r.Route("/api/channels", func(r chi.Router) {
				r.Post("/", h.CreateChannel)
				r.Get("/", h.ListChannels)
				r.Post("/from-dm", h.CreateChannelFromDM)
				r.Route("/{channelID}", func(r chi.Router) {
					r.Get("/", h.GetChannel)
					r.Post("/join", h.JoinChannel)
					r.Post("/leave", h.LeaveChannel)
					r.Get("/members", h.ListChannelMembers)
					r.Post("/members", h.AddChannelMemberByID)
					r.Delete("/members/{memberID}", h.RemoveChannelMemberByID)
					r.Get("/messages", h.ListChannelMessages)
					r.Patch("/visibility", h.UpdateChannelVisibility)
					r.Patch("/category", h.UpdateChannelCategory)
					r.Post("/transfer-founder", h.TransferFounder)
					r.Post("/split", h.SplitChannel)
					r.Post("/merge-request", h.CreateMergeRequest)
					r.Post("/archive", h.ArchiveChannel)
					r.Post("/unarchive", h.UnarchiveChannel)
					// Thread API (Plan 3)
					r.Get("/threads", h.ListThreads)
					r.Post("/threads", h.CreateThread)
				})
			})

			// Thread API (Plan 3 / Phase 2)
			r.Route("/api/threads/{threadID}", func(r chi.Router) {
				r.Get("/", h.GetThread)
				r.Get("/messages", h.ListThreadMessages)
				r.Post("/messages", h.PostThreadMessage)
				r.Get("/context-items", h.ListThreadContextItems)
				r.Post("/context-items", h.CreateThreadContextItem)
				r.Delete("/context-items/{itemID}", h.DeleteThreadContextItem)

				// Meeting lifecycle. handler/meeting.go enforces
				// thread.workspace_id == request workspace + member check.
				r.Route("/meeting", func(r chi.Router) {
					r.Post("/start", h.StartMeeting)
					r.Post("/audio", h.UploadMeetingAudio)
					r.Post("/summarize", h.SummarizeMeeting)
					r.Get("/action-items", h.ListMeetingActionItems)
				})
			})

			// Action items live outside the thread route because the
			// approve/reject ops only need the item id; handler resolves
			// the parent thread + workspace from the row itself.
			r.Route("/api/action-items/{itemID}", func(r chi.Router) {
				r.Post("/approve", h.ApproveActionItem)
				r.Post("/reject", h.RejectActionItem)
			})

			// Merge requests
			r.Post("/api/merge-requests/{mergeID}/approve", h.ApproveMergeRequest)

			// Listen (long-poll)
			r.Get("/api/listen", h.Listen)

			r.Route("/api/sessions", func(r chi.Router) {
				r.Post("/", h.CreateSession)
				r.Get("/", h.ListSessions)
				r.Route("/{sessionID}", func(r chi.Router) {
					r.Get("/", h.GetSession)
					r.Patch("/", h.UpdateSession)
					r.Post("/join", h.JoinSession)
					r.Get("/messages", h.ListSessionMessages)
					r.Get("/summary", h.SessionSummary)
					r.Post("/auto-start", h.StartAutoDiscussion)
					r.Post("/auto-stop", h.StopAutoDiscussion)
					r.Put("/context", h.ShareSessionContext)
				})
			})

			// Plans
			r.Route("/api/plans", func(r chi.Router) {
				r.Post("/", h.CreatePlan)
				r.Get("/", h.ListPlans)
				r.Post("/generate", h.GeneratePlan)
				r.Route("/{planID}", func(r chi.Router) {
					r.Get("/", h.GetPlan)
					r.Delete("/", h.DeletePlan)
					r.Post("/approve", h.ApprovePlan)
					// Plan 5 §10: Task surface scoped to a plan.
					r.Get("/tasks", h.ListTasksByPlan)
					// Phase 3: partial edits to the plan's input_files /
					// user_inputs context blobs from the 计划 tab.
					r.Patch("/context", h.UpdatePlanContext)
				})
			})

			// Plan 5 §10 — Project five-layer API: tasks, slots,
			// executions, artifacts, reviews. State transitions flow
			// through SchedulerService; PATCH on tasks accepts only
			// status=cancelled (see handler/task.go).
			r.Route("/api/tasks", func(r chi.Router) {
				r.Post("/", h.CreateTaskHandler)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetTaskHandler)
					r.Patch("/", h.UpdateTaskHandler)
					r.Get("/slots", h.ListTaskSlots)
					r.Post("/slots", h.CreateTaskSlot)
					r.Get("/executions", h.ListTaskExecutions)
					r.Get("/artifacts", h.ListTaskArtifacts)
				})
			})
			r.Get("/api/slots/{id}/submissions", h.ListSlotSubmissions)
			r.Post("/api/slots/{id}/submit", h.SubmitSlotInput)
			r.Route("/api/artifacts/{id}", func(r chi.Router) {
				r.Get("/", h.GetArtifactHandler)
				r.Get("/reviews", h.ListArtifactReviews)
				r.Post("/reviews", h.CreateReviewHandler)
			})
			// /api/runs/{runID}/start — kick off a ProjectRun via
			// SchedulerService.ScheduleRun. Reads of runs themselves
			// continue to live under /api/projects/{id}/runs.
			r.Post("/api/runs/{runID}/start", h.StartRunHandler)

			// Triggers (AgentMesh integration)
			r.Post("/api/triggers/check-mentions", h.CheckMentions)

			// Projects (Phase 2)
			r.Route("/api/projects", func(r chi.Router) {
				r.Post("/", h.CreateProject)
				r.Get("/", h.ListProjects)
				r.Post("/from-chat", h.CreateProjectFromChat)
				r.Route("/{projectID}", func(r chi.Router) {
					r.Get("/", h.GetProject)
					r.Patch("/", h.UpdateProject)
					r.Delete("/", h.DeleteProject)
					r.Post("/fork", h.ForkProject)
					r.Get("/versions", h.ListProjectVersions)
					r.Get("/runs", h.GetProjectRuns)
					r.Post("/start", h.StartProjectExecution)
					r.Post("/reject", h.RejectPlan)
					r.Get("/files", h.GetFilesByProject)
				})
			})

			// File index (Phase 4)
			r.Get("/api/files", h.ListFiles)
			r.Get("/api/files/mine", h.ListOwnerAndAgentFiles)

			// Metrics (Phase 5)
			r.Get("/api/metrics", h.GetWorkspaceMetrics)

			// Activity log (Plan 4 — PRD §3)
			r.Get("/api/activity-log", h.ListActivityLog)

			// Inbox
			r.Route("/api/inbox", func(r chi.Router) {
				r.Get("/", h.ListInbox)
				r.Get("/unread-count", h.CountUnreadInbox)
				r.Post("/mark-all-read", h.MarkAllInboxRead)
				r.Post("/archive-all", h.ArchiveAllInbox)
				r.Post("/archive-all-read", h.ArchiveAllReadInbox)
				r.Post("/archive-completed", h.ArchiveCompletedInbox)
				r.Post("/{id}/read", h.MarkInboxRead)
				r.Post("/{id}/archive", h.ArchiveInboxItem)
				// Plan 4 §8: actionable inbox items must be resolved
				// with an explicit user-facing resolution.
				r.Post("/{id}/resolve", h.ResolveInboxItem)
			})
		})
	})

	return r
}

// membershipChecker implements realtime.MembershipChecker using database queries.
type membershipChecker struct {
	queries *db.Queries
}

func (mc *membershipChecker) IsMember(ctx context.Context, userID, workspaceID string) bool {
	_, err := mc.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	return err == nil
}

// patResolver implements realtime.PATResolver using database queries.
type patResolver struct {
	queries *db.Queries
}

func (pr *patResolver) ResolveUserIDFromPATHash(ctx context.Context, hash string) (string, error) {
	pat, err := pr.queries.GetPersonalAccessTokenByHash(ctx, hash)
	if err != nil {
		return "", err
	}
	return uuidToString(pat.UserID), nil
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func parseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}
	}
	return u
}
