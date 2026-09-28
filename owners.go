package projectstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ErrOwnerUnavailable means the store that owns an entity could not answer, so
// the HTTP layer answers 502 and writes nothing.
var ErrOwnerUnavailable = errors.New("owner unavailable")

// Owners asks each entity's own store about it. Every id a project links to is
// checked with its owner before it is written, and the owner's name for it is
// kept beside it for display.
type Owners struct {
	HTTP                    *http.Client
	RepoStoreURL            string
	KanbanStoreURL          string
	KanbanStoreServiceToken string
	SchedulerURL            string
	HealthcheckURL          string
	PrincipalStoreURL       string
	NoteboardURL            string
	WorkGraphStoreURL       string
}

// KanbanServiceTokenHeader is how kanban-store recognises an internal service.
const KanbanServiceTokenHeader = "X-Kanban-Store-Service-Token"

// getJSON fetches a URL into target. A 404 is ErrNotFound; anything else that
// is not 200 is ErrOwnerUnavailable.
func (owners *Owners) getJSON(ctx context.Context, rawURL string, headers map[string]string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := owners.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("%w: GET %s: %v", ErrOwnerUnavailable, rawURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 300))
		return fmt.Errorf("%w: GET %s: %s: %s", ErrOwnerUnavailable, rawURL, response.Status, body)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("%w: GET %s: %v", ErrOwnerUnavailable, rawURL, err)
	}
	return nil
}

func (owners *Owners) kanbanHeaders() map[string]string {
	return map[string]string{KanbanServiceTokenHeader: owners.KanbanStoreServiceToken}
}

// Label checks that the entity exists in its store and returns the store's
// name for it. An entity its store does not have is ErrInvalid.
func (owners *Owners) Label(ctx context.Context, entityType, entityRef string) (string, error) {
	escaped := url.PathEscape(entityRef)
	var err error
	var label string
	switch entityType {
	case EntityRepo:
		var repo struct {
			Name string `json:"name"`
		}
		err = owners.getJSON(ctx, owners.RepoStoreURL+"/repos/"+escaped, nil, &repo)
		label = repo.Name
	case EntityBoard:
		var board struct {
			Name string `json:"name"`
		}
		err = owners.getJSON(ctx, owners.KanbanStoreURL+"/api/boards/"+escaped, owners.kanbanHeaders(), &board)
		label = board.Name
	case EntitySchedulerJob:
		var job struct {
			Name string `json:"name"`
		}
		err = owners.getJSON(ctx, owners.SchedulerURL+"/api/jobs/"+escaped, nil, &job)
		label = job.Name
	case EntityService:
		var status struct {
			Services []struct {
				Name string `json:"name"`
				Unit string `json:"unit"`
			} `json:"services"`
		}
		if err = owners.getJSON(ctx, owners.HealthcheckURL+"/api/status", nil, &status); err == nil {
			err = ErrNotFound
			for _, service := range status.Services {
				if service.Name == entityRef {
					err, label = nil, service.Name
					if service.Unit != "" {
						label = service.Unit
					}
				}
			}
		}
	case EntityPrincipal:
		var principal struct {
			DisplayName string `json:"display_name"`
		}
		err = owners.getJSON(ctx, owners.PrincipalStoreURL+"/principals/"+escaped, nil, &principal)
		label = principal.DisplayName
	case EntityNote:
		var item struct {
			Title string `json:"title"`
		}
		err = owners.getJSON(ctx, owners.NoteboardURL+"/api/items/"+escaped, nil, &item)
		label = item.Title
	default:
		return "", fmt.Errorf("%w: entity_type %q is not one a project can own", ErrInvalid, entityType)
	}
	if errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("%w: %s has no %s %q", ErrInvalid, serviceOf(entityType), entityType, entityRef)
	}
	return label, err
}

func serviceOf(entityType string) string {
	for _, info := range EntityTypes {
		if info.Type == entityType {
			return info.Service
		}
	}
	return "no store"
}
