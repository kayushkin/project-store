# project-store routes

Rooted at `/`, JSON in and out unless a route says otherwise. Ids are
`project_000001`, handed out once and never reused. A request body or query
with a field the route does not take is 400. Times are unix seconds.

| Method | Path | What it does |
|---|---|---|
| `GET` | `/health` | `{"status":"ok"}` |
| `GET` | `/settings` | The service's settings, read-only; the kanban token shows only as set or unset |
| `GET` | `/vocabulary` | `{"kinds":[…],"stages":[…],"default_stage":…,"entity_types":[{"type","service","meaning"}]}` |
| `GET` | `/projects` | `{"projects":[…]}` by name. Query: `q` (name, goal or id), `kind`, `parent_id`, `include_archived=true` |
| `POST` | `/projects` | `name` (required), `kind` (`project` default, or `stream`), `parent_id`, `goal`, `done_when`, `stage` (`idea` default), `spend_target_share` (0–1 or null), `created_by`. **201** |
| `GET` | `/projects/{id}` | One project |
| `PATCH` | `/projects/{id}` | Any of the create fields but `created_by`, plus `archived` (true/false) and `changed_by`. `parent_id` `""` makes it top level; one that would make the project its own ancestor is 400. Every change is recorded |
| `GET` | `/projects/{id}/changes` | `{"changes":[{"field","old_value","new_value","changed_by","changed_at"}]}`, oldest first |
| `GET` | `/projects/{id}/links` | `{"links":[…]}` what the project owns; `?entity_type=` narrows |
| `POST` | `/projects/{id}/links` | `{"entity_type":…,"entity_ref":…,"note":…,"created_by":…}`. The ref is checked with its own store first: **400** when that store has no such thing, **502** when it cannot answer, **409** when the project already owns it. `label` is that store's name for it, kept for display |
| `DELETE` | `/projects/{id}/links/{link_id}` | **204** |
| `GET` | `/links?entity_type=…&entity_ref=…` | Reverse lookup: which projects own this. `entity_type` is required; without `entity_ref` it lists every link of that type (every filed session, to group sessions by project in one call) |
| `GET` | `/projects/{id}/rollup` | Worked out on request: `project`, `children`, `links`, `cards` (every card kanban-store links to the project: `card_id`, `title`, noteboard `status`, kanban `work_state`), `cards_by_work_state` (open cards only; `unmapped` when the card's column has no state), and `repos` (each owned repo's `unmerged_branches` from work-graph-store and `latest_deploy` from repo-store's ledger). A store that cannot answer fails the whole call |
| `GET` | `/digest` | Every live project's rollup as `text/plain` for an agent: one block each, children indented under their parent |

## What a project owns

`entity_type` is one of `repo` (repo-store's numeric id), `board` (kanban-store
board uuid), `scheduler_job` (scheduler job id), `service` (healthcheck check
name), `principal` (principal-store id), `note` (noteboard item uuid) and
`session` (llm-bridge-server session id: a chat about the project, or a worker
on it, filed because no card or branch ties it there already). One thing may
belong to several projects.

A **card** is not linked here. kanban-store holds card links, so a card joins a
project with `POST …/kanban/api/cards/{card_id}/links
{"entity_type":"project","entity_ref":"project_000001"}`, and the rollup reads
them back from kanban-store's reverse lookup.
