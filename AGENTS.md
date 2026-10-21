# About project-store

## What it owns

`127.0.0.1:8320`, unit `project-store.service`. Projects: what each is for (name, kind `project` or `stream`, goal, what done means, stage, spend target, parent project) and what it owns in other stores — repos, boards, scheduler jobs, services, people, notes, filed sessions — each by the id its own store gave it. Ids are `project_000001`. `CONTRACT.md` is the route table.

It stores nothing that happens in a project. Cards, branches, commits, pull requests, sessions, deploys and spend are worked out on request (`/projects/{id}/rollup`, `/projects/{id}/activity`, `/digest`) from what the project owns and from the cards kanban-store links to it, so they are never stale here. Pull requests come from repo-store, which syncs them from GitHub; this service never calls GitHub.

**Next steps are noteboard todos linked to the project** with a `note` link, the same way a decision note is: noteboard holds what a step says and whether it is done, and `POST /projects/{id}/next-steps` makes the todo (tag `next-step`) and the link in one call. The rollup reads every note link's item back in one `POST /api/items/query`. **Suggested next steps are the one thing stored here that a project did not decide**: `POST /projects/{id}/suggestions` asks llm-bridge-server's one-shot router (`POST /oneshot`, role `PROJECT_STORE_SUGGESTION_MODEL_ROLE`, default `balanced`, caller `project-store-next-step-suggestions`) to read the project's digest, open todos, cards, pull requests and last 14 days of activity, and keeps each proposal (`suggestion_000001`) until a person accepts it — it becomes a next step — or dismisses it. A new round supersedes the undecided proposals of the last.

# How it works

## Boards are views, projects are scope

A project may own several boards and a board may show cards from several projects: a team board keeps its own columns, and each column maps to kanban-store's shared `work_state` (`not_started`, `working`, `waiting`, `done`, `dropped`), which is what a rollup counts. A card joins a project through a kanban-store card link with `entity_type` `project`, never through a table here. Nesting belongs to projects (`parent_id`) and to cards (noteboard's `parent_id`); boards stay flat.

## Every link is checked with its owner

`POST /projects/{id}/links` asks the owning store for the id before writing it (`owners.go`) and keeps the store's name as `label`, for display only: nothing reads a label back to find a row. The owner saying no is 400, the owner not answering is 502, and nothing is written in either case. kanban-store needs `KANBAN_STORE_SERVICE_TOKEN` and llm-bridge-server `LLMBRIDGE_SERVICE_TOKEN`, which the service reads from a host-local drop-in (`~/.config/systemd/user/project-store.service.d/`) and refuses to start without.

# Working in this repo

## Tests and deploys

The wire types are rendered to TypeScript in `ts/model.ts` (`@kayushkin/project-store-types`, which bridge-ui links) by `./generate-ts.sh` from the files `tygo.yaml` names; run it after changing a wire type and commit the result. The list answers (`{"projects":[…]}`, `{"links":[…]}`) are unnamed maps in `server.go`, so they are not rendered; a caller wraps the named element type. `store.go` holds some internals too, and tygo renders them; nothing reads them.

`go test ./...`; `server_test.go` runs every route against fake owners. Deploy only with `./deploy.sh`, which refuses when the token drop-in is missing and smoke-checks `/digest`, the call that reaches every owner.
