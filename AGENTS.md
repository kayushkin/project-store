# About project-store

## What it owns

`127.0.0.1:8320`, unit `project-store.service`. Projects: what each is for (name, kind `project` or `stream`, goal, what done means, stage, spend target, parent project) and what it owns in other stores — repos, boards, scheduler jobs, services, people, notes, filed sessions — each by the id its own store gave it. Ids are `project_000001`. `CONTRACT.md` is the route table.

It stores nothing that happens in a project. Cards, branches, sessions, deploys and spend are worked out on request (`/projects/{id}/rollup`, `/digest`) from what the project owns and from the cards kanban-store links to it, so they are never stale here.

# How it works

## Boards are views, projects are scope

A project may own several boards and a board may show cards from several projects: a team board keeps its own columns, and each column maps to kanban-store's shared `work_state` (`not_started`, `working`, `waiting`, `done`, `dropped`), which is what a rollup counts. A card joins a project through a kanban-store card link with `entity_type` `project`, never through a table here. Nesting belongs to projects (`parent_id`) and to cards (noteboard's `parent_id`); boards stay flat.

## Every link is checked with its owner

`POST /projects/{id}/links` asks the owning store for the id before writing it (`owners.go`) and keeps the store's name as `label`, for display only: nothing reads a label back to find a row. The owner saying no is 400, the owner not answering is 502, and nothing is written in either case. kanban-store needs `KANBAN_STORE_SERVICE_TOKEN` and llm-bridge-server `LLMBRIDGE_SERVICE_TOKEN`, which the service reads from a host-local drop-in (`~/.config/systemd/user/project-store.service.d/`) and refuses to start without.

# Working in this repo

## Tests and deploys

The wire types are rendered to TypeScript in `ts/model.ts` (`@kayushkin/project-store-types`, which bridge-ui links) by `./generate-ts.sh` from the files `tygo.yaml` names; run it after changing a wire type and commit the result. The list answers (`{"projects":[…]}`, `{"links":[…]}`) are unnamed maps in `server.go`, so they are not rendered; a caller wraps the named element type. `store.go` holds some internals too, and tygo renders them; nothing reads them.

`go test ./...`; `server_test.go` runs every route against fake owners. Deploy only with `./deploy.sh`, which refuses when the token drop-in is missing and smoke-checks `/digest`, the call that reaches every owner.
