// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codesphere-cloud/managed-services-lib/model"
	"github.com/codesphere-cloud/managed-services-lib/provider"
)

func newTestRouter(t *testing.T) (*gin.Engine, *fakeHarbor) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	p, fake := newTestProvider(t)
	router := gin.New()
	provider.RegisterRoutes(router.Group("/api/v1/"+ProviderType), p)

	return router, fake
}

func serve(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRoutesLifecycle(t *testing.T) {
	router, fake := newTestRouter(t)
	const base = "/api/v1/harbor"

	rec := serve(t, router, http.MethodGet, base, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty list: %d %s", rec.Code, rec.Body)
	}

	rec = serve(t, router, http.MethodPost, base, `{
		"id": "team-a",
		"teamId": 42,
		"config": {"public": false},
		"plan": {"parameters": {"cpu": 0, "memory": 0, "storage": 10240}},
		"secrets": {"superuserPassword": "pw"}
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}

	rec = serve(t, router, http.MethodGet, base, "")
	var ids []model.ServiceID
	if err := json.Unmarshal(rec.Body.Bytes(), &ids); err != nil || len(ids) != 1 || ids[0] != "team-a" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}

	rec = serve(t, router, http.MethodGet, base+"?id=team-a&id=unknown", "")
	var statuses map[model.ServiceID]Status
	if err := json.Unmarshal(rec.Body.Bytes(), &statuses); err != nil {
		t.Fatalf("status decode: %v (%s)", err, rec.Body)
	}
	status, ok := statuses["team-a"]
	if rec.Code != http.StatusOK || len(statuses) != 1 || !ok {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	if !status.Details.Ready || status.Plan.Parameters.StorageMiB != 10240 || status.Details.ProjectName != "ms-team-a" {
		t.Fatalf("status = %+v", status)
	}

	rec = serve(t, router, http.MethodPatch, base+"/team-a", `{"id": "team-a", "teamId": 42, "config": {"public": true}}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if !fake.project("ms-team-a").public {
		t.Fatal("update did not make the project public")
	}

	rec = serve(t, router, http.MethodDelete, base+"/team-a", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if fake.project("ms-team-a") != nil {
		t.Fatal("project still exists after delete")
	}
}

func TestRoutesErrorStatuses(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"create without password", http.MethodPost, "/api/v1/harbor", `{"id":"a","teamId":1,"secrets":{}}`, http.StatusBadRequest},
		{"update unknown service", http.MethodPatch, "/api/v1/harbor/a", `{"id":"a","config":{"public":true}}`, http.StatusNotFound},
		{"delete unknown service", http.MethodDelete, "/api/v1/harbor/a", "", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newTestRouter(t)

			if rec := serve(t, router, tt.method, tt.path, tt.body); rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestRoutesReturn500WhenHarborServesHTML(t *testing.T) {
	// What the provider saw before the /api/v2.0 fix: Harbor's UI instead of JSON.
	router, fake := newTestRouter(t)
	fake.respondRaw("GET /api/v2.0/projects", http.StatusOK, "<!doctype html><html></html>")

	if rec := serve(t, router, http.MethodGet, "/api/v1/harbor", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
