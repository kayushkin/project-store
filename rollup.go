package projectstore

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A rollup is worked out on request from what a project owns and from the
// cards kanban-store links to it. Nothing in it is stored here, so it is never
// stale and never disagrees with the stores it reads.

// ProjectCard is a card kanban-store links to the project.
type ProjectCard struct {
	CardID string `json:"card_id"`
	Title  string `json:"title"`
	// Status is noteboard's: open, done or archived.
	Status string `json:"status"`
	// WorkState is kanban-store's shared state for the card, from the column
	// it was last moved into on any board; "" when that column maps to none.
	WorkState string `json:"work_state"`
}

// RepoRollup is where one of the project's repos stands.
type RepoRollup struct {
	RepoStoreID int64  `json:"repo_store_id"`
	Name        string `json:"name"`
	// UnmergedBranches are local branches the default branch does not contain.
	UnmergedBranches []UnmergedBranch `json:"unmerged_branches"`
	// LatestDeploy is nil when the ledger has no deploy of the repo.
	LatestDeploy *Deploy `json:"latest_deploy"`
}

// UnmergedBranch is a branch whose work has not landed.
type UnmergedBranch struct {
	Name         string   `json:"name"`
	HeadSHA      string   `json:"head_sha"`
	CommittedAt  int64    `json:"committed_at"`
	WorktreePath string   `json:"worktree_path"`
	SessionIDs   []string `json:"session_ids"`
}

// Deploy is a row of repo-store's deploy ledger, as it gives it.
type Deploy struct {
	CommitSHA  string `json:"commit_sha"`
	DeployedBy string `json:"deployed_by"`
	CreatedAt  int64  `json:"created_at"`
}

// Rollup is a project with everything worked out from what it owns.
type Rollup struct {
	Project  Project       `json:"project"`
	Children []Project     `json:"children"`
	Links    []Link        `json:"links"`
	Cards    []ProjectCard `json:"cards"`
	// CardsByWorkState counts the open cards by shared state; "unmapped" counts
	// those whose column maps to none.
	CardsByWorkState map[string]int `json:"cards_by_work_state"`
	Repos            []RepoRollup   `json:"repos"`
}

// BuildRollup works out a project's rollup.
func BuildRollup(ctx context.Context, store *Store, owners *Owners, id string) (Rollup, error) {
	project, err := store.Project(id)
	if err != nil {
		return Rollup{}, err
	}
	rollup := Rollup{Project: project, CardsByWorkState: map[string]int{}, Repos: []RepoRollup{}}
	if rollup.Children, err = store.Projects(ProjectFilter{ParentID: id}); err != nil {
		return rollup, err
	}
	if rollup.Links, err = store.Links(LinkFilter{ProjectID: id}); err != nil {
		return rollup, err
	}
	if rollup.Cards, err = owners.ProjectCards(ctx, id); err != nil {
		return rollup, err
	}
	for _, card := range rollup.Cards {
		if card.Status != "open" {
			continue
		}
		state := card.WorkState
		if state == "" {
			state = "unmapped"
		}
		rollup.CardsByWorkState[state]++
	}
	for _, link := range rollup.Links {
		if link.EntityType != EntityRepo {
			continue
		}
		repoStoreID, err := strconv.ParseInt(link.EntityRef, 10, 64)
		if err != nil {
			return rollup, fmt.Errorf("repo link %d holds %q, not a repo-store id", link.ID, link.EntityRef)
		}
		repo, err := owners.RepoRollup(ctx, repoStoreID, link.Label)
		if err != nil {
			return rollup, err
		}
		rollup.Repos = append(rollup.Repos, repo)
	}
	return rollup, nil
}

// ProjectCards asks kanban-store for every card linked to the project.
func (owners *Owners) ProjectCards(ctx context.Context, projectID string) ([]ProjectCard, error) {
	var rows []struct {
		CardID string `json:"card_id"`
		Item   *struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"item"`
		WorkState *string `json:"work_state"`
	}
	if err := owners.getJSON(ctx, owners.KanbanStoreURL+"/api/entities/project/"+url.PathEscape(projectID)+"/cards", owners.kanbanHeaders(), &rows); err != nil {
		return nil, fmt.Errorf("cards linked to %s: %w", projectID, err)
	}
	cards := []ProjectCard{}
	for _, row := range rows {
		// A link whose noteboard item is gone is kanban-store's orphan; it is
		// no card of the project's.
		if row.Item == nil {
			continue
		}
		card := ProjectCard{CardID: row.CardID, Title: row.Item.Title, Status: row.Item.Status}
		if row.WorkState != nil {
			card.WorkState = *row.WorkState
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// RepoRollup reads a repo's unmerged branches from work-graph-store and its
// latest deploy from repo-store's ledger.
func (owners *Owners) RepoRollup(ctx context.Context, repoStoreID int64, name string) (RepoRollup, error) {
	rollup := RepoRollup{RepoStoreID: repoStoreID, Name: name, UnmergedBranches: []UnmergedBranch{}}
	var graph struct {
		DefaultBranch string `json:"default_branch"`
		Branches      []struct {
			Name              string `json:"name"`
			IsRemote          bool   `json:"is_remote"`
			HeadSHA           string `json:"head_sha"`
			CommittedAt       int64  `json:"committed_at"`
			MergedIntoDefault bool   `json:"merged_into_default"`
			WorktreePath      string `json:"worktree_path"`
			Sessions          []struct {
				SessionID string `json:"session_id"`
			} `json:"sessions"`
		} `json:"branches"`
	}
	if err := owners.getJSON(ctx, owners.WorkGraphStoreURL+"/repos/"+strconv.FormatInt(repoStoreID, 10)+"/graph?max_commits=1", nil, &graph); err != nil {
		return rollup, fmt.Errorf("branches of repo %d: %w", repoStoreID, err)
	}
	for _, branch := range graph.Branches {
		if branch.IsRemote || branch.MergedIntoDefault || branch.Name == graph.DefaultBranch {
			continue
		}
		unmerged := UnmergedBranch{Name: branch.Name, HeadSHA: branch.HeadSHA, CommittedAt: branch.CommittedAt, WorktreePath: branch.WorktreePath, SessionIDs: []string{}}
		for _, session := range branch.Sessions {
			unmerged.SessionIDs = append(unmerged.SessionIDs, session.SessionID)
		}
		rollup.UnmergedBranches = append(rollup.UnmergedBranches, unmerged)
	}
	sort.Slice(rollup.UnmergedBranches, func(left, right int) bool {
		return rollup.UnmergedBranches[left].CommittedAt > rollup.UnmergedBranches[right].CommittedAt
	})

	var deploys []Deploy
	if err := owners.getJSON(ctx, owners.RepoStoreURL+"/deployments?repo_id="+strconv.FormatInt(repoStoreID, 10)+"&limit=1", nil, &deploys); err != nil {
		return rollup, fmt.Errorf("deploys of repo %d: %w", repoStoreID, err)
	}
	if len(deploys) > 0 {
		rollup.LatestDeploy = &deploys[0]
	}
	return rollup, nil
}

func (handlers *Handlers) projectRollup(writer http.ResponseWriter, request *http.Request) {
	rollup, err := BuildRollup(request.Context(), handlers.Store, handlers.Owners, request.PathValue("id"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, rollup)
}

// digest serves every live project's rollup as text for an agent to read,
// parents before their children.
func (handlers *Handlers) digest(writer http.ResponseWriter, request *http.Request) {
	projects, err := handlers.Store.Projects(ProjectFilter{})
	if err != nil {
		writeError(writer, err)
		return
	}
	rollups := map[string]Rollup{}
	for _, project := range projects {
		rollup, err := BuildRollup(request.Context(), handlers.Store, handlers.Owners, project.ID)
		if err != nil {
			writeError(writer, err)
			return
		}
		rollups[project.ID] = rollup
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(writer, DigestText(projects, rollups, time.Now()))
}

// DigestText renders rollups, one block per project, children indented under
// their parent.
func DigestText(projects []Project, rollups map[string]Rollup, now time.Time) string {
	var text strings.Builder
	fmt.Fprintf(&text, "Projects (project-store, %d live). Cards: open cards by shared state. Branches: not merged into the default branch.\n", len(projects))
	live := map[string]bool{}
	for _, project := range projects {
		live[project.ID] = true
	}
	var write func(project Project, depth int)
	write = func(project Project, depth int) {
		rollup := rollups[project.ID]
		indent := strings.Repeat("    ", depth)
		fmt.Fprintf(&text, "\n%s%s  %s  [%s, %s]", indent, project.Name, project.ID, project.Kind, project.Stage)
		if project.SpendTargetShare != nil {
			fmt.Fprintf(&text, "  spend target %.0f%%", *project.SpendTargetShare*100)
		}
		text.WriteString("\n")
		if project.Goal != "" {
			fmt.Fprintf(&text, "%s  goal: %s\n", indent, project.Goal)
		}
		if project.DoneWhen != "" {
			fmt.Fprintf(&text, "%s  done when: %s\n", indent, project.DoneWhen)
		}
		if len(rollup.CardsByWorkState) > 0 {
			var states []string
			for state, count := range rollup.CardsByWorkState {
				states = append(states, fmt.Sprintf("%s %d", state, count))
			}
			sort.Strings(states)
			fmt.Fprintf(&text, "%s  cards: %s\n", indent, strings.Join(states, ", "))
		}
		owned := map[string][]string{}
		filedSessions := 0
		for _, link := range rollup.Links {
			if link.EntityType == EntitySession {
				filedSessions++
				continue
			}
			if link.EntityType == EntityRepo {
				continue
			}
			shown := link.EntityRef
			if link.Label != "" && link.Label != link.EntityRef {
				shown = fmt.Sprintf("%s (%s)", link.Label, link.EntityRef)
			}
			owned[link.EntityType] = append(owned[link.EntityType], shown)
		}
		for _, entityType := range []string{EntityBoard, EntitySchedulerJob, EntityService, EntityPrincipal, EntityNote} {
			if len(owned[entityType]) > 0 {
				fmt.Fprintf(&text, "%s  %ss: %s\n", indent, strings.ReplaceAll(entityType, "_", " "), strings.Join(owned[entityType], ", "))
			}
		}
		if filedSessions > 0 {
			// Only the count: GET /projects/{id}/links?entity_type=session names them.
			fmt.Fprintf(&text, "%s  filed sessions: %d\n", indent, filedSessions)
		}
		for _, repo := range rollup.Repos {
			fmt.Fprintf(&text, "%s  repo %s (%d)", indent, repo.Name, repo.RepoStoreID)
			if repo.LatestDeploy != nil {
				fmt.Fprintf(&text, ", deployed %s ago", roundedAge(now.Sub(time.Unix(repo.LatestDeploy.CreatedAt, 0))))
			} else {
				text.WriteString(", never deployed")
			}
			if len(repo.UnmergedBranches) == 0 {
				text.WriteString(", no unmerged branches\n")
				continue
			}
			fmt.Fprintf(&text, ", %d unmerged:\n", len(repo.UnmergedBranches))
			for _, branch := range repo.UnmergedBranches {
				fmt.Fprintf(&text, "%s    %s, last commit %s ago", indent, branch.Name, roundedAge(now.Sub(time.Unix(branch.CommittedAt, 0))))
				if branch.WorktreePath != "" {
					fmt.Fprintf(&text, ", worktree %s", branch.WorktreePath)
				}
				if len(branch.SessionIDs) > 0 {
					fmt.Fprintf(&text, ", sessions %s", strings.Join(branch.SessionIDs, " "))
				}
				text.WriteString("\n")
			}
		}
		for _, child := range rollup.Children {
			write(child, depth+1)
		}
	}
	for _, project := range projects {
		// A project under an archived parent is written at the top level.
		if project.ParentID == "" || !live[project.ParentID] {
			write(project, 0)
		}
	}
	return text.String()
}

func roundedAge(age time.Duration) string {
	switch {
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 48*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	}
}
