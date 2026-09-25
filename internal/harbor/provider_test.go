// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	msclient "github.com/codesphere-cloud/managed-services-lib/client"
	"github.com/codesphere-cloud/managed-services-lib/model"
	"github.com/codesphere-cloud/managed-services-lib/provider"
)

const mib = 1024 * 1024

func newTestProvider(t *testing.T) (*Provider, *fakeHarbor) {
	t.Helper()

	fake := newFakeHarbor(t)
	cfg := Config{
		BaseURL:       fake.URL(),
		AuthMode:      AuthModeBasic,
		Username:      fakeUsername,
		Password:      fakePassword,
		Timeout:       5 * time.Second,
		ProjectPrefix: defaultProjectPrefix,
		DefaultRoleID: defaultRoleID,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	return NewProvider(cfg, client, slog.New(slog.NewTextHandler(io.Discard, nil))), fake
}

func createRequest(id string, public bool, storageMiB int, password string) provider.CreateRequest[PlanParameters, ServiceConfig, ServiceSecrets] {
	return provider.CreateRequest[PlanParameters, ServiceConfig, ServiceSecrets]{
		ID:      model.ServiceID(id),
		TeamID:  42,
		Plan:    PlanParameters{StorageMiB: storageMiB},
		Config:  ServiceConfig{Public: public},
		Secrets: ServiceSecrets{SuperuserPassword: password},
	}
}

func updateRequest(id string, args UpdateArgs) provider.UpdateRequest[UpdateArgs] {
	return provider.UpdateRequest[UpdateArgs]{ID: model.ServiceID(id), TeamID: 42, Params: args}
}

func mustCreate(t *testing.T, p *Provider, id string) {
	t.Helper()
	if err := p.Create(context.Background(), createRequest(id, false, 100, "pw")); err != nil {
		t.Fatalf("Create(%s): %v", id, err)
	}
}

func TestCreateProvisionsProjectAndRobot(t *testing.T) {
	p, fake := newTestProvider(t)

	if err := p.Create(context.Background(), createRequest("Team_A", true, 10240, "s3cret")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	project := fake.project("ms-team-a")
	if project == nil {
		t.Fatal("project ms-team-a not created")
	}
	if !project.public {
		t.Error("project should be public")
	}
	if project.hard != 10240*mib {
		t.Errorf("quota = %d, want %d", project.hard, 10240*mib)
	}

	robots := fake.robotsFor("ms-team-a")
	if len(robots) != 1 {
		t.Fatalf("got %d robots, want 1", len(robots))
	}
	robot := robots[0]
	if robot.name != "robot$ms-team-a+ms-admin" {
		t.Errorf("robot name = %q", robot.name)
	}
	if robot.secret != "s3cret" {
		t.Errorf("robot secret = %q, want s3cret", robot.secret)
	}
	if len(robot.permissions) != 1 || robot.permissions[0].Namespace != "ms-team-a" {
		t.Errorf("robot permissions = %+v", robot.permissions)
	}
}

func TestCreateWithoutStorageLeavesQuotaUnlimited(t *testing.T) {
	p, fake := newTestProvider(t)

	if err := p.Create(context.Background(), createRequest("a", false, 0, "pw")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := fake.project("ms-a").hard; got != -1 {
		t.Fatalf("quota = %d, want unlimited (-1)", got)
	}
}

func TestCreateIsIdempotent(t *testing.T) {
	p, fake := newTestProvider(t)
	ctx := context.Background()

	if err := p.Create(ctx, createRequest("a", false, 100, "first")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := p.Create(ctx, createRequest("a", true, 200, "second")); err != nil {
		t.Fatalf("second Create: %v", err)
	}

	project := fake.project("ms-a")
	if !project.public || project.hard != 200*mib {
		t.Errorf("project not reconciled: %+v", project)
	}
	robots := fake.robotsFor("ms-a")
	if len(robots) != 1 || robots[0].secret != "second" {
		t.Errorf("robots = %+v, want one robot with rotated secret", robots)
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name string
		req  provider.CreateRequest[PlanParameters, ServiceConfig, ServiceSecrets]
	}{
		{"missing id", createRequest("", false, 1, "pw")},
		{"missing password", createRequest("a", false, 1, "")},
		{"blank password", createRequest("a", false, 1, "   ")},
		{"negative storage", createRequest("a", false, -1, "pw")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, fake := newTestProvider(t)

			err := p.Create(context.Background(), tt.req)
			if !errors.Is(err, provider.ErrInvalidArgument) {
				t.Fatalf("err = %v, want %v", err, provider.ErrInvalidArgument)
			}
			if fake.totalCalls() != 0 {
				t.Fatalf("made %d harbor calls, want none", fake.totalCalls())
			}
		})
	}
}

func TestCreateRollsBackProjectWhenRobotFails(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.failOn("POST /api/v2.0/robots", http.StatusInternalServerError)

	err := p.Create(context.Background(), createRequest("a", false, 100, "pw"))
	if err == nil {
		t.Fatal("expected error")
	}
	if fake.project("ms-a") != nil {
		t.Fatal("project should have been rolled back")
	}
}

func TestCreateRollsBackProjectWhenPasswordUpdateFails(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.failOn("PATCH /api/v2.0/robots/{id}", http.StatusInternalServerError)

	if err := p.Create(context.Background(), createRequest("a", false, 100, "pw")); err == nil {
		t.Fatal("expected error")
	}
	if fake.project("ms-a") != nil {
		t.Fatal("project should have been rolled back")
	}
}

func TestCreateKeepsPreexistingProjectOnFailure(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.seedProject("ms-a", false, -1, 0)
	fake.failOn("POST /api/v2.0/robots", http.StatusInternalServerError)

	if err := p.Create(context.Background(), createRequest("a", false, 100, "pw")); err == nil {
		t.Fatal("expected error")
	}
	if fake.project("ms-a") == nil {
		t.Fatal("pre-existing project must not be deleted")
	}
}

func TestCreateErrorMapping(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		status  int
		want    error
	}{
		{"project lookup conflict", "GET /api/v2.0/projects/{name}", http.StatusConflict, msclient.ErrResourceConflict},
		{"project create invalid", "POST /api/v2.0/projects", http.StatusBadRequest, provider.ErrInvalidArgument},
		{"robot create not found", "POST /api/v2.0/robots", http.StatusNotFound, provider.ErrServiceNotFound},
		{"password update conflict", "PATCH /api/v2.0/robots/{id}", http.StatusConflict, msclient.ErrResourceConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, fake := newTestProvider(t)
			fake.failOn(tt.pattern, tt.status)

			err := p.Create(context.Background(), createRequest("a", false, 100, "pw"))
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCreateUpstreamFailureIsNotMappedToClientError(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.failOn("POST /api/v2.0/projects", http.StatusInternalServerError)

	err := p.Create(context.Background(), createRequest("a", false, 100, "pw"))
	if err == nil || errors.Is(err, provider.ErrInvalidArgument) || errors.Is(err, provider.ErrServiceNotFound) {
		t.Fatalf("err = %v, want generic upstream error", err)
	}
	if !errors.Is(err, errHarborRequestFailed) {
		t.Fatalf("err = %v, want wrapped %v", err, errHarborRequestFailed)
	}
}

func TestCreateToleratesProjectCreateRace(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.seedProject("ms-a", false, -1, 0)

	// The project exists, but the first lookup reports it missing, so the create call hits a conflict.
	fake.failOnce("GET /api/v2.0/projects/{name}", http.StatusNotFound)

	if err := p.Create(context.Background(), createRequest("a", false, 100, "pw")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(fake.robotsFor("ms-a")) != 1 {
		t.Fatal("robot should be created on the existing project")
	}
}

func TestListReturnsServiceIDs(t *testing.T) {
	p, fake := newTestProvider(t)
	mustCreate(t, p, "a")
	mustCreate(t, p, "b")
	fake.seedProject("library", true, -1, 0)
	fake.seedProject("other-ms-x", false, -1, 0)

	ids, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []model.ServiceID{"a", "b"}) {
		t.Fatalf("ids = %v, want [a b]", ids)
	}
}

func TestListIDsRoundTripThroughGetStatus(t *testing.T) {
	p, _ := newTestProvider(t)
	mustCreate(t, p, "3f2b9c1e-8a4d-4f6b-9e21-7c5d0a1b2c3d")

	ids, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	statuses, err := p.GetStatus(context.Background(), ids)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if len(statuses) != len(ids) || len(ids) != 1 {
		t.Fatalf("listed %v but got statuses for %d services", ids, len(statuses))
	}
}

func TestListPaginates(t *testing.T) {
	p, fake := newTestProvider(t)
	for i := range 150 {
		fake.seedProject("ms-svc-"+strconv.Itoa(i), false, -1, 0)
	}

	ids, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 150 {
		t.Fatalf("got %d ids, want 150", len(ids))
	}
}

func TestListUpstreamError(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.failOn("GET /api/v2.0/projects", http.StatusInternalServerError)

	if _, err := p.List(context.Background()); !errors.Is(err, errHarborRequestFailed) {
		t.Fatalf("err = %v, want %v", err, errHarborRequestFailed)
	}
}

func TestGetStatus(t *testing.T) {
	p, fake := newTestProvider(t)
	if err := p.Create(context.Background(), createRequest("a", true, 400, "pw")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	fake.mu.Lock()
	fake.projects["ms-a"].used = 100 * mib
	fake.mu.Unlock()

	statuses, err := p.GetStatus(context.Background(), []model.ServiceID{"a", "missing"})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if _, ok := statuses["missing"]; ok {
		t.Error("missing service should be omitted")
	}

	status, ok := statuses["a"]
	if !ok {
		t.Fatal("status for a missing")
	}
	if status.Plan.Parameters.StorageMiB != 400 {
		t.Errorf("storage = %d, want 400", status.Plan.Parameters.StorageMiB)
	}
	if !status.Config.Public {
		t.Error("public = false, want true")
	}
	if status.Pause == nil || *status.Pause {
		t.Errorf("pause = %v, want false", status.Pause)
	}

	want := Details{
		Ready:              true,
		ProjectName:        "ms-a",
		Username:           "robot$ms-a+ms-admin",
		HarborURL:          fake.URL(),
		RegistryURL:        fake.URL() + "/ms-a",
		UsedStorage:        100,
		UsedStoragePercent: 25,
	}
	if status.Details != want {
		t.Errorf("details = %+v, want %+v", status.Details, want)
	}
}

func TestGetStatusOmitsProjectWithoutRobot(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.seedProject("ms-a", false, -1, 0)

	statuses, err := p.GetStatus(context.Background(), []model.ServiceID{"a"})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("statuses = %v, want none", statuses)
	}
}

func TestGetStatusRejectsDuplicateRobots(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.seedProject("ms-a", false, -1, 0)
	fake.seedRobot("ms-a", "robot$ms-a+ms-admin", "x")
	fake.seedRobot("ms-a", "robot$ms-a+ms-admin", "y")

	if _, err := p.GetStatus(context.Background(), []model.ServiceID{"a"}); !errors.Is(err, provider.ErrInvalidArgument) {
		t.Fatalf("err = %v, want %v", err, provider.ErrInvalidArgument)
	}
}

func TestGetStatusUpstreamErrors(t *testing.T) {
	for _, pattern := range []string{
		"GET /api/v2.0/projects/{name}",
		"GET /api/v2.0/robots",
		"GET /api/v2.0/projects/{name}/summary",
	} {
		t.Run(pattern, func(t *testing.T) {
			p, fake := newTestProvider(t)
			mustCreate(t, p, "a")
			fake.failOn(pattern, http.StatusInternalServerError)

			if _, err := p.GetStatus(context.Background(), []model.ServiceID{"a"}); !errors.Is(err, errHarborRequestFailed) {
				t.Fatalf("err = %v, want %v", err, errHarborRequestFailed)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	p, fake := newTestProvider(t)
	mustCreate(t, p, "a")

	err := p.Update(context.Background(), updateRequest("a", UpdateArgs{
		Config:  &ServiceConfig{Public: true},
		Plan:    &model.PlanSpec[PlanParameters]{Parameters: PlanParameters{StorageMiB: 2048}},
		Secrets: &ServiceSecrets{SuperuserPassword: "rotated"},
	}))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	project := fake.project("ms-a")
	if !project.public || project.hard != 2048*mib {
		t.Errorf("project = %+v, want public with 2048 MiB quota", project)
	}
	if got := fake.robotsFor("ms-a")[0].secret; got != "rotated" {
		t.Errorf("secret = %q, want rotated", got)
	}
}

func TestUpdatePartial(t *testing.T) {
	p, fake := newTestProvider(t)
	if err := p.Create(context.Background(), createRequest("a", true, 100, "pw")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := p.Update(context.Background(), updateRequest("a", UpdateArgs{
		Plan: &model.PlanSpec[PlanParameters]{Parameters: PlanParameters{StorageMiB: 0}},
	})); err != nil {
		t.Fatalf("Update: %v", err)
	}

	project := fake.project("ms-a")
	if !project.public || project.hard != 100*mib {
		t.Errorf("project = %+v, want unchanged", project)
	}
	if got := fake.robotsFor("ms-a")[0].secret; got != "pw" {
		t.Errorf("secret = %q, want unchanged", got)
	}
	if got := fake.callCount("PUT /api/v2.0/projects/{name}"); got != 0 {
		t.Errorf("project updates = %d, want 0", got)
	}
	if got := fake.callCount("PUT /api/v2.0/quotas/{id}"); got != 0 {
		t.Errorf("quota updates = %d, want 0", got)
	}
}

func TestUpdateValidation(t *testing.T) {
	tests := []struct {
		name string
		req  provider.UpdateRequest[UpdateArgs]
	}{
		{"missing id", updateRequest("", UpdateArgs{})},
		{"negative storage", updateRequest("a", UpdateArgs{Plan: &model.PlanSpec[PlanParameters]{Parameters: PlanParameters{StorageMiB: -1}}})},
		{"empty password", updateRequest("a", UpdateArgs{Secrets: &ServiceSecrets{SuperuserPassword: " "}})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, fake := newTestProvider(t)

			if err := p.Update(context.Background(), tt.req); !errors.Is(err, provider.ErrInvalidArgument) {
				t.Fatalf("err = %v, want %v", err, provider.ErrInvalidArgument)
			}
			if fake.totalCalls() != 0 {
				t.Fatalf("made %d harbor calls, want none", fake.totalCalls())
			}
		})
	}
}

func TestUpdateMissingService(t *testing.T) {
	p, fake := newTestProvider(t)

	err := p.Update(context.Background(), updateRequest("a", UpdateArgs{Config: &ServiceConfig{Public: true}}))
	if !errors.Is(err, provider.ErrServiceNotFound) {
		t.Fatalf("err = %v, want %v", err, provider.ErrServiceNotFound)
	}
	if fake.project("ms-a") != nil {
		t.Fatal("update must not create a project")
	}
}

func TestUpdatePasswordWithoutRobot(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.seedProject("ms-a", false, -1, 0)

	err := p.Update(context.Background(), updateRequest("a", UpdateArgs{Secrets: &ServiceSecrets{SuperuserPassword: "x"}}))
	if !errors.Is(err, provider.ErrServiceNotFound) {
		t.Fatalf("err = %v, want %v", err, provider.ErrServiceNotFound)
	}
	if len(fake.robotsFor("ms-a")) != 0 {
		t.Fatal("update must not create a robot")
	}
}

func TestUpdateUpstreamErrors(t *testing.T) {
	tests := []struct {
		pattern string
		status  int
		want    error
	}{
		{"PUT /api/v2.0/projects/{name}", http.StatusBadRequest, provider.ErrInvalidArgument},
		{"GET /api/v2.0/quotas", http.StatusInternalServerError, errHarborRequestFailed},
		{"PUT /api/v2.0/quotas/{id}", http.StatusBadRequest, provider.ErrInvalidArgument},
		{"PATCH /api/v2.0/robots/{id}", http.StatusInternalServerError, errHarborRequestFailed},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			p, fake := newTestProvider(t)
			mustCreate(t, p, "a")
			fake.failOn(tt.pattern, tt.status)

			err := p.Update(context.Background(), updateRequest("a", UpdateArgs{
				Config:  &ServiceConfig{Public: true},
				Plan:    &model.PlanSpec[PlanParameters]{Parameters: PlanParameters{StorageMiB: 10}},
				Secrets: &ServiceSecrets{SuperuserPassword: "x"},
			}))
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	p, fake := newTestProvider(t)
	mustCreate(t, p, "a")
	mustCreate(t, p, "b")

	if err := p.Delete(context.Background(), "a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fake.project("ms-a") != nil {
		t.Error("project ms-a should be deleted")
	}
	if got := fake.callCount("DELETE /api/v2.0/robots/{id}"); got != 1 {
		t.Errorf("robot deletes = %d, want 1", got)
	}
	if fake.project("ms-b") == nil || len(fake.robotsFor("ms-b")) != 1 {
		t.Error("service b must be untouched")
	}
}

func TestDeleteMissingService(t *testing.T) {
	p, _ := newTestProvider(t)

	if err := p.Delete(context.Background(), "a"); !errors.Is(err, provider.ErrServiceNotFound) {
		t.Fatalf("err = %v, want %v", err, provider.ErrServiceNotFound)
	}
}

func TestDeleteRemovesProjectWithoutRobot(t *testing.T) {
	p, fake := newTestProvider(t)
	fake.seedProject("ms-a", false, -1, 0)

	if err := p.Delete(context.Background(), "a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fake.project("ms-a") != nil {
		t.Fatal("project without robot should still be deleted")
	}
}

func TestDeleteToleratesRobotAlreadyGone(t *testing.T) {
	p, fake := newTestProvider(t)
	mustCreate(t, p, "a")
	fake.failOn("DELETE /api/v2.0/robots/{id}", http.StatusNotFound)

	if err := p.Delete(context.Background(), "a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fake.project("ms-a") != nil {
		t.Fatal("project should be deleted")
	}
}

func TestDeleteUpstreamErrors(t *testing.T) {
	tests := []struct {
		pattern string
		status  int
		want    error
	}{
		{"DELETE /api/v2.0/robots/{id}", http.StatusInternalServerError, errHarborRequestFailed},
		{"DELETE /api/v2.0/projects/{name}", http.StatusPreconditionFailed, provider.ErrInvalidArgument},
		{"DELETE /api/v2.0/projects/{name}", http.StatusNotFound, provider.ErrServiceNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+" "+http.StatusText(tt.status), func(t *testing.T) {
			p, fake := newTestProvider(t)
			mustCreate(t, p, "a")
			fake.failOn(tt.pattern, tt.status)

			if err := p.Delete(context.Background(), "a"); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
