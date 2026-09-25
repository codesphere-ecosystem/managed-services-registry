// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/codesphere-cloud/managed-services-lib/model"
	harbormodels "github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

// mockClient embeds the interface so unexpected calls panic.
type mockClient struct {
	harborClient
	projects       []*harbormodels.Project
	robots         []*harbormodels.Robot
	deletedProject string
}

func (m *mockClient) ListProjects(context.Context) ([]*harbormodels.Project, error) {
	return m.projects, nil
}

func (m *mockClient) GetProject(_ context.Context, name string) (*harbormodels.Project, error) {
	return &harbormodels.Project{Name: name, ProjectID: 1}, nil
}

func (m *mockClient) ListProjectRobots(context.Context, int64) ([]*harbormodels.Robot, error) {
	return m.robots, nil
}

func (m *mockClient) DeleteProject(_ context.Context, name string) error {
	m.deletedProject = name
	return nil
}

func newMockProvider(client *mockClient) *Provider {
	return NewProvider(Config{ProjectPrefix: "ms-"}, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestListReturnsServiceIDs(t *testing.T) {
	client := &mockClient{projects: []*harbormodels.Project{{Name: "ms-a"}, {Name: "library"}}}

	ids, err := newMockProvider(client).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []model.ServiceID{"a"}) {
		t.Fatalf("ids = %v, want [a]", ids)
	}
}

func TestDeleteRemovesProjectWithoutRobot(t *testing.T) {
	client := &mockClient{}

	if err := newMockProvider(client).Delete(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if client.deletedProject != "ms-a" {
		t.Fatalf("deleted project = %q, want ms-a", client.deletedProject)
	}
}
