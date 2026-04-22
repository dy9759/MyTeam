// Package skillsbundle seeds the shipped skills + subagents bundle into
// the database at server startup. Files live under data/ and are
// embedded into the binary so the deployment artifact is self-contained
// (no runtime filesystem dependency on a neighbouring bundle dir).
//
// On every startup the loader does a three-pass sync:
//
//  1. Walk data/<source>/skills/<slug>/SKILL.md and upsert a
//     source='bundle' row keyed by the relative path (source_ref).
//  2. Walk data/<source>/agents/<slug>.md and upsert a subagent row
//     (agent.kind = 'subagent', source = 'bundle').
//  3. Delete bundle rows whose source_ref is no longer present in the
//     tree — cascades through subagent_skill so unlinked skills drop
//     automatically.
//
// Categories are inferred from the YAML frontmatter if present, else
// from the <source> directory name (e.g. "addyosmani"). Keep categories
// in the frontmatter when you want a skill to cross the provider
// boundary (e.g. put both sources' debugging skills in "debugging").
package skillsbundle

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"gopkg.in/yaml.v3"

	db "github.com/MyAIOSHub/MyTeam/server/pkg/db/generated"
)

// Data holds the compiled-in bundle tree. The `all:` prefix includes
// dotfiles (LICENSE, etc); we filter on extension when walking.
//
//go:embed all:data
var Data embed.FS

const (
	rootDir = "data"
	// marker filenames — skills always live in <slug>/SKILL.md, agents
	// always live in a flat .md next to agents/.
	skillMarker = "SKILL.md"
)

// Frontmatter is the subset of YAML we care about at ingest time.
// Unknown fields are ignored so the upstream format can evolve without
// breaking the loader.
type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Category    string `yaml:"category"`
	Model       string `yaml:"model"`
}

// Loader is an idempotent bundle sync. Construct with the sqlc Queries
// instance and call Run once during server boot.
type Loader struct {
	Queries *db.Queries
}

// Run walks the embedded tree and reconciles the bundle rows. Returns
// an error for any I/O / DB failure; individual parse errors are
// logged and skipped so a single malformed file can't brick startup.
func (l *Loader) Run(ctx context.Context) error {
	if l.Queries == nil {
		return errors.New("skillsbundle: Queries is nil")
	}

	skillRefs, err := l.syncSkills(ctx)
	if err != nil {
		return fmt.Errorf("syncSkills: %w", err)
	}

	subRefs, err := l.syncSubagents(ctx)
	if err != nil {
		return fmt.Errorf("syncSubagents: %w", err)
	}

	if err := l.Queries.DeleteBundleSkillsNotInRefs(ctx, skillRefs); err != nil {
		return fmt.Errorf("prune skills: %w", err)
	}
	if err := l.Queries.DeleteBundleSubagentsNotInRefs(ctx, subRefs); err != nil {
		return fmt.Errorf("prune subagents: %w", err)
	}

	// Wire subagent_skill links so bundle subagents actually hold a
	// skill roster. Without this pass, every skill is orphaned and
	// the "agents reach skills only via subagents" rule can't be
	// enforced end-to-end. The linking is heuristic by design —
	// upstream bundles don't declare skill/subagent relationships
	// explicitly, so we infer via skill-name occurrences in the
	// subagent's description + instructions text, scoped to the
	// same upstream source.
	linkCount, err := l.syncSubagentSkillLinks(ctx)
	if err != nil {
		return fmt.Errorf("link subagent skills: %w", err)
	}

	// Re-run the role-agent seed (same logic as migration 074) so any
	// bundle subagents that just landed on disk get a runnable
	// workspace agent counterpart. The query's NOT EXISTS guard makes
	// this idempotent — workspaces that already have a role agent for
	// a given subagent name are skipped.
	if err := l.Queries.SeedRoleAgentsFromBundleSubagents(ctx); err != nil {
		return fmt.Errorf("seed role agents: %w", err)
	}

	slog.Info("skills bundle synced",
		"skills", len(skillRefs),
		"subagents", len(subRefs),
		"skill_links", linkCount,
	)
	return nil
}

// syncSubagentSkillLinks walks the bundle subagents and, for each
// one, links every same-source skill whose name appears as a token
// in the subagent's description/instructions. This is intentionally
// coarse — it'll over-link a chatty subagent that name-drops every
// skill in its system prompt and under-link a terse one. The product
// gesture is "subagents come pre-wired with the skills they reference"
// so users see a meaningful roster out of the box; manual editing via
// /api/subagents/:id/skills remains the authoritative path for
// precise curation.
func (l *Loader) syncSubagentSkillLinks(ctx context.Context) (int, error) {
	skills, err := l.Queries.ListBundleSkillsForLinking(ctx)
	if err != nil {
		return 0, fmt.Errorf("list skills: %w", err)
	}
	type skillHit struct {
		id        pgtype.UUID
		nameLower string
	}
	// Group skills by upstream source so the loader never crosses the
	// addyosmani/superpowers/everything-claude-code boundary.
	bySource := make(map[string][]skillHit, 4)
	for _, sk := range skills {
		if !sk.SourceRef.Valid {
			continue
		}
		src := firstPathSegment(sk.SourceRef.String)
		bySource[src] = append(bySource[src], skillHit{
			id:        sk.ID,
			nameLower: strings.ToLower(strings.TrimSpace(sk.Name)),
		})
	}

	subs, err := l.Queries.ListBundleSubagentsForLinking(ctx)
	if err != nil {
		return 0, fmt.Errorf("list subagents: %w", err)
	}

	// Track skill coverage and pick a per-source catchall subagent so
	// we can backfill every orphan skill at the end. Catchall is the
	// first-seen subagent per source; stable because ListBundle…ForLinking
	// returns rows in deterministic order.
	linkedSkills := make(map[string]struct{}, len(skills))
	catchall := make(map[string]pgtype.UUID, len(bySource))
	total := 0
	for _, sa := range subs {
		if !sa.SourceRef.Valid {
			continue
		}
		src := firstPathSegment(sa.SourceRef.String)
		if _, ok := catchall[src]; !ok {
			catchall[src] = sa.ID
		}
		candidates := bySource[src]
		if len(candidates) == 0 {
			continue
		}
		haystack := strings.ToLower(sa.Name + " " + sa.Description + " " + sa.Instructions)
		pos := int32(0)
		for _, c := range candidates {
			if c.nameLower == "" {
				continue
			}
			if !strings.Contains(haystack, c.nameLower) {
				continue
			}
			if err := l.Queries.LinkSubagentSkill(ctx, db.LinkSubagentSkillParams{
				SubagentID: sa.ID,
				SkillID:    c.id,
				Position:   pos,
			}); err != nil {
				return total, fmt.Errorf("link %s -> %s: %w", sa.Name, c.nameLower, err)
			}
			linkedSkills[uuidKey(c.id)] = struct{}{}
			pos++
			total++
		}
	}

	// Catchall pass — every orphan skill gets linked to the first
	// subagent of its source so the product invariant "every skill
	// is reachable through at least one subagent" holds post-sync.
	// Users can prune or reassign via the /subagents UI.
	for _, sk := range skills {
		if !sk.SourceRef.Valid {
			continue
		}
		if _, hit := linkedSkills[uuidKey(sk.ID)]; hit {
			continue
		}
		src := firstPathSegment(sk.SourceRef.String)
		owner, ok := catchall[src]
		if !ok {
			continue
		}
		if err := l.Queries.LinkSubagentSkill(ctx, db.LinkSubagentSkillParams{
			SubagentID: owner,
			SkillID:    sk.ID,
			Position:   999, // tail of the roster so explicit matches sort first
		}); err != nil {
			return total, fmt.Errorf("catchall link %s: %w", sk.Name, err)
		}
		total++
	}
	return total, nil
}

func uuidKey(u pgtype.UUID) string {
	return fmt.Sprintf("%x", u.Bytes)
}

func (l *Loader) syncSkills(ctx context.Context) ([]string, error) {
	var refs []string
	err := fs.WalkDir(Data, rootDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || path.Base(p) != skillMarker {
			return nil
		}
		// ref = relative path without the rootDir prefix so migrations
		// that rename the root don't invalidate every source_ref.
		ref := strings.TrimPrefix(p, rootDir+"/")
		source := firstPathSegment(ref)

		body, err := fs.ReadFile(Data, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		fm, content, err := splitFrontmatter(body)
		if err != nil {
			slog.Warn("skillsbundle: skip malformed skill", "path", p, "error", err)
			return nil
		}
		name := firstNonEmpty(fm.Name, defaultNameFromPath(ref))
		category := firstNonEmpty(fm.Category, source)

		if _, err := l.Queries.UpsertBundleSkill(ctx, db.UpsertBundleSkillParams{
			Name:        name,
			Description: fm.Description,
			Content:     string(content),
			Category:    category,
			SourceRef:   textPtr(ref),
		}); err != nil {
			return fmt.Errorf("upsert skill %s: %w", ref, err)
		}
		refs = append(refs, ref)
		return nil
	})
	return refs, err
}

func (l *Loader) syncSubagents(ctx context.Context) ([]string, error) {
	var refs []string
	err := fs.WalkDir(Data, rootDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		// Agents live at data/<source>/agents/<slug>.md — no SKILL.md
		// marker, so we key on the parent-dir name.
		parent := path.Base(path.Dir(p))
		if parent != "agents" {
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		ref := strings.TrimPrefix(p, rootDir+"/")
		source := firstPathSegment(ref)

		body, err := fs.ReadFile(Data, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		fm, content, err := splitFrontmatter(body)
		if err != nil {
			slog.Warn("skillsbundle: skip malformed subagent", "path", p, "error", err)
			return nil
		}
		name := firstNonEmpty(fm.Name, defaultNameFromPath(ref))
		category := firstNonEmpty(fm.Category, source)

		if _, err := l.Queries.UpsertBundleSubagent(ctx, db.UpsertBundleSubagentParams{
			Name:         name,
			Description:  fm.Description,
			Instructions: string(content),
			Category:     category,
			SourceRef:    textPtr(ref),
		}); err != nil {
			return fmt.Errorf("upsert subagent %s: %w", ref, err)
		}
		refs = append(refs, ref)
		return nil
	})
	return refs, err
}

// splitFrontmatter accepts the standard `---\n...yaml...\n---\nbody` form
// and returns both halves. Files without frontmatter are treated as
// pure content with empty metadata, but the caller can still derive a
// name from the path.
func splitFrontmatter(raw []byte) (Frontmatter, []byte, error) {
	var fm Frontmatter
	// Normalise line endings so CRLF-checked-in files still parse.
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(raw, []byte("---\n")) {
		return fm, raw, nil
	}
	rest := raw[len("---\n"):]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return fm, raw, errors.New("frontmatter: missing closing ---")
	}
	yamlPart := rest[:end]
	body := rest[end+len("\n---\n"):]
	if err := yaml.Unmarshal(yamlPart, &fm); err != nil {
		return fm, body, fmt.Errorf("yaml: %w", err)
	}
	return fm, body, nil
}

// firstPathSegment returns "addyosmani" for "addyosmani/skills/foo/SKILL.md".
func firstPathSegment(rel string) string {
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return rel
}

// defaultNameFromPath picks a human-readable fallback when frontmatter
// is missing a `name:` field — prefer the slug dir for skills, filename
// (sans ext) for agents.
func defaultNameFromPath(rel string) string {
	base := path.Base(rel)
	if base == skillMarker {
		return path.Base(path.Dir(rel))
	}
	return strings.TrimSuffix(base, path.Ext(base))
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// textPtr wraps a string into a non-null pgtype.Text. Empty strings map
// to a zero-length valid Text (not NULL) so writes stay consistent with
// the upsert targets' NOT NULL columns.
func textPtr(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}
