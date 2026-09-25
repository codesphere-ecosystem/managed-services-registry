// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	harbormodels "github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

func newTestClient(t *testing.T, baseURL string) *apiClient {
	t.Helper()

	client, err := NewClient(Config{
		BaseURL:  baseURL,
		AuthMode: AuthModeBasic,
		Username: fakeUsername,
		Password: fakePassword,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// recordingServer answers every request with the given status and body and records the last request.
func recordingServer(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()

	var last http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = *r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server, &last
}

func TestClientUsesAPIBasePath(t *testing.T) {
	for _, suffix := range []string{"", "/"} {
		server, last := recordingServer(t, http.StatusOK, "[]")
		client := newTestClient(t, server.URL+suffix)

		if _, err := client.ListProjects(context.Background()); err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if last.URL.Path != "/api/v2.0/projects" {
			t.Fatalf("base %q: path = %q, want %q", server.URL+suffix, last.URL.Path, "/api/v2.0/projects")
		}
	}
}

func TestClientAuthModes(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		server, last := recordingServer(t, http.StatusOK, "[]")
		client := newTestClient(t, server.URL)

		if _, err := client.ListProjects(context.Background()); err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		user, pass, ok := last.BasicAuth()
		if !ok || user != fakeUsername || pass != fakePassword {
			t.Fatalf("basic auth = %q/%q (ok=%v)", user, pass, ok)
		}
	})

	t.Run("bearer", func(t *testing.T) {
		server, last := recordingServer(t, http.StatusOK, "[]")
		client, err := NewClient(Config{BaseURL: server.URL, AuthMode: AuthModeBearer, Token: "tok", Timeout: time.Second})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}

		if _, err := client.ListProjects(context.Background()); err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if got := last.Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("Authorization = %q, want %q", got, "Bearer tok")
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		if _, err := NewClient(Config{BaseURL: "https://harbor.example.com", AuthMode: "oauth"}); err == nil {
			t.Fatal("expected error for unsupported auth mode")
		}
	})
}

func TestNewClientRejectsInvalidURL(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "://bad", AuthMode: AuthModeBasic}); err == nil {
		t.Fatal("expected error for invalid url")
	}
}

func TestClientMapsHarborErrors(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{http.StatusNotFound, errHarborNotFound},
		{http.StatusConflict, errHarborConflict},
		{http.StatusBadRequest, errHarborInvalid},
		{http.StatusUnprocessableEntity, errHarborInvalid},
		{http.StatusPreconditionFailed, errHarborInvalid},
		{http.StatusUnauthorized, errHarborRequestFailed},
		{http.StatusForbidden, errHarborRequestFailed},
		{http.StatusInternalServerError, errHarborRequestFailed},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			server, _ := recordingServer(t, tt.status, `{"errors":[{"code":"X","message":"boom"}]}`)
			client := newTestClient(t, server.URL)

			_, err := client.GetProject(context.Background(), "p")
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if !strings.Contains(err.Error(), "boom") {
				t.Fatalf("err = %v, want harbor message included", err)
			}
		})
	}
}

func TestHarborErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"envelope messages joined", `{"errors":[{"message":"a"},null,{"code":"B"}]}`, "a; B"},
		{"plain body", "gateway timeout", "status 500: gateway timeout"},
		{"empty body", "", "status 500"},
		{"long body trimmed", strings.Repeat("x", 600), "status 500: " + strings.Repeat("x", 512) + "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := recordingServer(t, http.StatusInternalServerError, tt.body)
			client := newTestClient(t, server.URL)

			_, err := client.GetProject(context.Background(), "p")
			if err == nil || !strings.HasSuffix(err.Error(), tt.want) {
				t.Fatalf("err = %v, want suffix %q", err, tt.want)
			}
		})
	}
}

func TestClientReportsUndecodableResponse(t *testing.T) {
	// Harbor serves its HTML UI for unknown paths, which must surface as a decode error.
	server, _ := recordingServer(t, http.StatusOK, "<!doctype html><html></html>")
	client := newTestClient(t, server.URL)

	_, err := client.ListProjects(context.Background())
	if !errors.Is(err, errHarborRequestFailed) || !strings.Contains(err.Error(), "failed to decode response body") {
		t.Fatalf("err = %v, want decode failure", err)
	}
}

func TestClientReportsTransportError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	client := newTestClient(t, server.URL)

	if _, err := client.ListProjects(context.Background()); !errors.Is(err, errHarborRequestFailed) {
		t.Fatalf("err = %v, want %v", err, errHarborRequestFailed)
	}
}

func TestClientSendsResourceNameHeader(t *testing.T) {
	server, last := recordingServer(t, http.StatusOK, `{"name":"my project"}`)
	client := newTestClient(t, server.URL)

	if _, err := client.GetProject(context.Background(), "my project"); err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if last.Header.Get("X-Is-Resource-Name") != "true" {
		t.Fatal("missing X-Is-Resource-Name header")
	}
	if last.URL.EscapedPath() != "/api/v2.0/projects/my%20project" {
		t.Fatalf("path = %q, want escaped project name", last.URL.EscapedPath())
	}
}

func TestClientListProjectsPaginates(t *testing.T) {
	fake := newFakeHarbor(t)
	for i := range 250 {
		fake.seedProject(fmt.Sprintf("p-%03d", i), false, -1, 0)
	}
	client := newTestClient(t, fake.URL())

	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 250 {
		t.Fatalf("got %d projects, want 250", len(projects))
	}
	if got := fake.callCount("GET /api/v2.0/projects"); got != 3 {
		t.Fatalf("list calls = %d, want 3", got)
	}
}

func TestDonePaging(t *testing.T) {
	withTotal := func(total string) *http.Response {
		return &http.Response{Header: http.Header{"X-Total-Count": []string{total}}}
	}

	tests := []struct {
		name     string
		header   *http.Response
		batch    int
		fetched  int
		expected bool
	}{
		{"no response short batch", nil, 5, 5, true},
		{"no response full batch", nil, 10, 10, false},
		{"total reached", withTotal("20"), 10, 20, true},
		{"total not reached", withTotal("21"), 10, 20, false},
		{"invalid total falls back to batch size", withTotal("abc"), 3, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := donePaging(restyResponse(tt.header), tt.batch, tt.fetched, 10); got != tt.expected {
				t.Fatalf("donePaging = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClientRobotLifecycle(t *testing.T) {
	fake := newFakeHarbor(t)
	fake.seedProject("proj", false, -1, 0)
	client := newTestClient(t, fake.URL())
	ctx := context.Background()

	created, err := client.CreateProjectRobot(ctx, "proj", &harbormodels.RobotCreate{
		Name:        "bot",
		Level:       "project",
		Secret:      "s1",
		Permissions: projectRobotPermissions("proj"),
	})
	if err != nil {
		t.Fatalf("CreateProjectRobot: %v", err)
	}
	if created.Name != "robot$proj+bot" {
		t.Fatalf("robot name = %q", created.Name)
	}

	robots, err := client.ListProjectRobots(ctx, 1)
	if err != nil || len(robots) != 1 || robots[0].ID != created.ID {
		t.Fatalf("ListProjectRobots = %v, %v", robots, err)
	}

	if _, err := client.UpdateProjectRobot(ctx, created.ID, &harbormodels.RobotCreate{Permissions: projectRobotPermissions("proj")}); err != nil {
		t.Fatalf("UpdateProjectRobot: %v", err)
	}
	if err := client.UpdateProjectRobotPassword(ctx, created.ID, "s2"); err != nil {
		t.Fatalf("UpdateProjectRobotPassword: %v", err)
	}
	if got := fake.robotsFor("proj")[0].secret; got != "s2" {
		t.Fatalf("secret = %q, want s2", got)
	}

	if err := client.DeleteProjectRobot(ctx, created.ID); err != nil {
		t.Fatalf("DeleteProjectRobot: %v", err)
	}
	if _, err := client.GetProjectRobot(ctx, created.ID); !errors.Is(err, errHarborNotFound) {
		t.Fatalf("GetProjectRobot after delete: %v", err)
	}
}

func TestClientUpdateProjectStorageQuota(t *testing.T) {
	fake := newFakeHarbor(t)
	project := fake.seedProject("proj", false, 100, 0)
	client := newTestClient(t, fake.URL())

	if err := client.UpdateProjectStorageQuota(context.Background(), int64(project.id), 2048); err != nil {
		t.Fatalf("UpdateProjectStorageQuota: %v", err)
	}
	if got := fake.project("proj").hard; got != 2048 {
		t.Fatalf("hard = %d, want 2048", got)
	}

	if err := client.UpdateProjectStorageQuota(context.Background(), 999, 1); !errors.Is(err, errHarborNotFound) {
		t.Fatalf("missing quota err = %v, want %v", err, errHarborNotFound)
	}
}

func restyResponse(raw *http.Response) *resty.Response {
	if raw == nil {
		return nil
	}
	return &resty.Response{RawResponse: raw}
}
