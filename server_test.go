package projectstore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeOwners answers like the real stores: repo 15 is "dash", board b1 is
// "Support Desk" (and only with the service token), anything else is 404.
func fakeOwners(t *testing.T) *Owners {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/15":
			json.NewEncoder(writer).Encode(map[string]any{"id": 15, "name": "dash"})
		case "/api/boards/b1":
			if request.Header.Get(KanbanServiceTokenHeader) != "service-token" {
				http.Error(writer, "no token", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(writer).Encode(map[string]any{"id": "b1", "name": "Support Desk"})
		case "/api/status":
			json.NewEncoder(writer).Encode(map[string]any{"services": []map[string]any{{"name": "dash-server", "unit": "dash-server"}}})
		case "/api/entities/project/project_000001/cards":
			writer.Write([]byte(`[{"card_id":"c1","item":{"title":"Waiting on a reply","status":"open"},"work_state":"waiting"},
				{"card_id":"c2","item":{"title":"Shipped","status":"done"},"work_state":"done"},
				{"card_id":"c3","item":{"title":"Column maps to nothing","status":"open","work_state":null}},
				{"card_id":"c4","item":null}]`))
		case "/repos/15/graph":
			writer.Write([]byte(`{"default_branch":"main","branches":[
				{"name":"main","head_sha":"a","merged_into_default":true},
				{"name":"feature","head_sha":"b","committed_at":100,"merged_into_default":false,"worktree_path":"/w","sessions":[{"session_id":"br_1790000000000000001"}]},
				{"name":"origin/feature","is_remote":true,"merged_into_default":false},
				{"name":"old","head_sha":"c","merged_into_default":true}]}`))
		case "/deployments":
			writer.Write([]byte(`[{"commit_sha":"abc","deployed_by":"someone","created_at":50}]`))
		case "/api/jobs/500":
			http.Error(writer, "boom", http.StatusInternalServerError)
		default:
			if strings.HasPrefix(request.URL.Path, "/api/entities/project/") {
				writer.Write([]byte(`[]`))
				return
			}
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	return &Owners{HTTP: server.Client(), RepoStoreURL: server.URL, KanbanStoreURL: server.URL, KanbanStoreServiceToken: "service-token",
		SchedulerURL: server.URL, HealthcheckURL: server.URL, PrincipalStoreURL: server.URL, NoteboardURL: server.URL, WorkGraphStoreURL: server.URL}
}

type client struct {
	t   *testing.T
	mux *http.ServeMux
}

func newClient(t *testing.T) client {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	mux := http.NewServeMux()
	RegisterHandlers(mux, &Handlers{Store: store, Owners: fakeOwners(t)})
	return client{t, mux}
}

func (c client) do(method, path, body string, wantStatus int) map[string]any {
	c.t.Helper()
	recorder := httptest.NewRecorder()
	c.mux.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	if recorder.Code != wantStatus {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, recorder.Code, wantStatus, recorder.Body)
	}
	var decoded map[string]any
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
			c.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return decoded
}

func TestProjectsNestAndRecordEveryChange(t *testing.T) {
	c := newClient(t)
	parent := c.do("POST", "/projects", `{"name":"LLM bridge","kind":"project","stage":"building","created_by":"test"}`, http.StatusCreated)
	if parent["id"] != "project_000001" || parent["spend_target_share"] != nil {
		t.Fatalf("parent = %v", parent)
	}
	child := c.do("POST", "/projects", `{"name":"Access and gating","parent_id":"project_000001","spend_target_share":0.1}`, http.StatusCreated)
	if child["id"] != "project_000002" || child["stage"] != "idea" || child["kind"] != "project" || child["spend_target_share"] != 0.1 {
		t.Fatalf("child = %v", child)
	}

	c.do("POST", "/projects", `{"name":"x","stage":"someday"}`, http.StatusBadRequest)
	c.do("POST", "/projects", `{"name":"x","parent_id":"project_000099"}`, http.StatusBadRequest)
	c.do("POST", "/projects", `{"name":"x","colour":"red"}`, http.StatusBadRequest)
	c.do("POST", "/projects", `{"name":"   "}`, http.StatusBadRequest)
	c.do("POST", "/projects", `{"name":"x","spend_target_share":1.5}`, http.StatusBadRequest)
	// A project cannot become its own ancestor.
	c.do("PATCH", "/projects/project_000001", `{"parent_id":"project_000002"}`, http.StatusBadRequest)

	c.do("PATCH", "/projects/project_000002", `{"stage":"shipping","spend_target_share":null,"changed_by":"claxon"}`, http.StatusOK)
	c.do("PATCH", "/projects/project_000002", `{"archived":true}`, http.StatusOK)
	changes := c.do("GET", "/projects/project_000002/changes", "", http.StatusOK)["changes"].([]any)
	var summary []string
	for _, raw := range changes {
		change := raw.(map[string]any)
		summary = append(summary, change["field"].(string)+":"+change["old_value"].(string)+">"+change["new_value"].(string)+"@"+change["changed_by"].(string))
	}
	if got := strings.Join(summary, " "); got != "stage:idea>shipping@claxon spend_target_share:0.1>@claxon archived:false>true@" {
		t.Errorf("changes = %s", got)
	}

	listed := c.do("GET", "/projects", "", http.StatusOK)["projects"].([]any)
	if len(listed) != 1 {
		t.Errorf("an archived project is listed by default: %v", listed)
	}
	listed = c.do("GET", "/projects?parent_id=project_000001&include_archived=true", "", http.StatusOK)["projects"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["name"] != "Access and gating" {
		t.Errorf("children = %v", listed)
	}
	c.do("GET", "/projects?colour=red", "", http.StatusBadRequest)
	c.do("GET", "/projects/project_000404", "", http.StatusNotFound)
}

func TestALinkIsCheckedWithItsOwnerAndCarriesItsName(t *testing.T) {
	c := newClient(t)
	c.do("POST", "/projects", `{"name":"Ticketing"}`, http.StatusCreated)
	c.do("POST", "/projects", `{"name":"Chat UI"}`, http.StatusCreated)

	repo := c.do("POST", "/projects/project_000001/links", `{"entity_type":"repo","entity_ref":"15","note":"serves the desk"}`, http.StatusCreated)
	if repo["label"] != "dash" {
		t.Errorf("repo link label = %v", repo["label"])
	}
	board := c.do("POST", "/projects/project_000001/links", `{"entity_type":"board","entity_ref":"b1"}`, http.StatusCreated)
	if board["label"] != "Support Desk" {
		t.Errorf("board link label = %v", board["label"])
	}
	c.do("POST", "/projects/project_000001/links", `{"entity_type":"service","entity_ref":"dash-server"}`, http.StatusCreated)
	// One repo, two projects.
	c.do("POST", "/projects/project_000002/links", `{"entity_type":"repo","entity_ref":"15"}`, http.StatusCreated)

	c.do("POST", "/projects/project_000001/links", `{"entity_type":"repo","entity_ref":"15"}`, http.StatusConflict)
	c.do("POST", "/projects/project_000001/links", `{"entity_type":"repo","entity_ref":"999"}`, http.StatusBadRequest)
	c.do("POST", "/projects/project_000001/links", `{"entity_type":"service","entity_ref":"nope"}`, http.StatusBadRequest)
	c.do("POST", "/projects/project_000001/links", `{"entity_type":"scheduler_job","entity_ref":"500"}`, http.StatusBadGateway)
	c.do("POST", "/projects/project_000001/links", `{"entity_type":"spaceship","entity_ref":"1"}`, http.StatusBadRequest)
	c.do("POST", "/projects/project_000404/links", `{"entity_type":"repo","entity_ref":"15"}`, http.StatusNotFound)

	owners := c.do("GET", "/links?entity_type=repo&entity_ref=15", "", http.StatusOK)["links"].([]any)
	if len(owners) != 2 {
		t.Errorf("repo 15 is owned by %d projects, want 2", len(owners))
	}
	c.do("DELETE", "/projects/project_000001/links/1", "", http.StatusNoContent)
	c.do("DELETE", "/projects/project_000001/links/1", "", http.StatusNotFound)
	links := c.do("GET", "/projects/project_000001/links", "", http.StatusOK)["links"].([]any)
	if len(links) != 2 {
		t.Errorf("links after delete = %v", links)
	}
}

func TestTheRollupIsWorkedOutFromWhatTheProjectOwns(t *testing.T) {
	c := newClient(t)
	c.do("POST", "/projects", `{"name":"Ticketing","goal":"a help desk","spend_target_share":0.2}`, http.StatusCreated)
	c.do("POST", "/projects", `{"name":"Email to ticket","parent_id":"project_000001","stage":"building"}`, http.StatusCreated)
	c.do("POST", "/projects/project_000001/links", `{"entity_type":"repo","entity_ref":"15"}`, http.StatusCreated)
	c.do("POST", "/projects/project_000001/links", `{"entity_type":"board","entity_ref":"b1"}`, http.StatusCreated)

	rollup := c.do("GET", "/projects/project_000001/rollup", "", http.StatusOK)
	counts, _ := json.Marshal(rollup["cards_by_work_state"])
	if string(counts) != `{"unmapped":1,"waiting":1}` {
		t.Errorf("cards_by_work_state = %s", counts)
	}
	if cards := rollup["cards"].([]any); len(cards) != 3 {
		t.Errorf("cards = %v, want the orphan dropped", cards)
	}
	repos := rollup["repos"].([]any)
	branches := repos[0].(map[string]any)["unmerged_branches"].([]any)
	if len(branches) != 1 || branches[0].(map[string]any)["name"] != "feature" {
		t.Errorf("unmerged branches = %v", branches)
	}
	if children := rollup["children"].([]any); len(children) != 1 {
		t.Errorf("children = %v", children)
	}

	recorder := httptest.NewRecorder()
	c.mux.ServeHTTP(recorder, httptest.NewRequest("GET", "/digest", nil))
	text := recorder.Body.String()
	for _, want := range []string{"Ticketing  project_000001  [project, idea]  spend target 20%", "goal: a help desk", "cards: unmapped 1, waiting 1",
		"boards: Support Desk (b1)", "repo dash (15), deployed", "feature, last commit", "worktree /w, sessions br_1790000000000000001",
		"\n    Email to ticket  project_000002  [project, building]"} {
		if !strings.Contains(text, want) {
			t.Errorf("digest lacks %q:\n%s", want, text)
		}
	}
}
