// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	harbormodels "github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

const (
	fakeUsername = "admin"
	fakePassword = "Harbor12345"
)

type fakeProject struct {
	id      int32
	name    string
	public  bool
	quotaID int64
	hard    int64
	used    int64
}

type fakeRobot struct {
	id          int64
	name        string
	projectID   int32
	secret      string
	permissions []*harbormodels.RobotPermission
}

type fakeFailure struct {
	status int
	body   string
	// remaining limits how many requests fail; zero means every request.
	remaining int
}

// fakeHarbor is an in-memory Harbor v2.0 API covering the endpoints the client uses.
type fakeHarbor struct {
	t      *testing.T
	server *httptest.Server

	mu            sync.Mutex
	nextProjectID int32
	nextRobotID   int64
	projects      map[string]*fakeProject
	robots        map[int64]*fakeRobot
	failures      map[string]fakeFailure
	calls         []string
}

func newFakeHarbor(t *testing.T) *fakeHarbor {
	t.Helper()

	f := &fakeHarbor{
		t:             t,
		nextProjectID: 1,
		nextRobotID:   1,
		projects:      map[string]*fakeProject{},
		robots:        map[int64]*fakeRobot{},
		failures:      map[string]fakeFailure{},
	}

	mux := http.NewServeMux()
	f.handle(mux, "GET /api/v2.0/projects", f.listProjects)
	f.handle(mux, "POST /api/v2.0/projects", f.createProject)
	f.handle(mux, "GET /api/v2.0/projects/{name}", f.getProject)
	f.handle(mux, "PUT /api/v2.0/projects/{name}", f.updateProject)
	f.handle(mux, "DELETE /api/v2.0/projects/{name}", f.deleteProject)
	f.handle(mux, "GET /api/v2.0/projects/{name}/summary", f.projectSummary)
	f.handle(mux, "GET /api/v2.0/robots", f.listRobots)
	f.handle(mux, "POST /api/v2.0/robots", f.createRobot)
	f.handle(mux, "GET /api/v2.0/robots/{id}", f.getRobot)
	f.handle(mux, "PUT /api/v2.0/robots/{id}", f.updateRobot)
	f.handle(mux, "PATCH /api/v2.0/robots/{id}", f.refreshRobotSecret)
	f.handle(mux, "DELETE /api/v2.0/robots/{id}", f.deleteRobot)
	f.handle(mux, "GET /api/v2.0/quotas", f.listQuotas)
	f.handle(mux, "PUT /api/v2.0/quotas/{id}", f.updateQuota)

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	return f
}

func (f *fakeHarbor) URL() string {
	return f.server.URL
}

// failOn makes every request matching the route pattern (e.g. "POST /api/v2.0/robots") fail.
func (f *fakeHarbor) failOn(pattern string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[pattern] = fakeFailure{status: status}
}

// failOnce makes only the next request matching the route pattern fail.
func (f *fakeHarbor) failOnce(pattern string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[pattern] = fakeFailure{status: status, remaining: 1}
}

// respondRaw makes every request matching the route pattern return a raw body.
func (f *fakeHarbor) respondRaw(pattern string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[pattern] = fakeFailure{status: status, body: body}
}

func (f *fakeHarbor) callCount(pattern string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	count := 0
	for _, call := range f.calls {
		if call == pattern {
			count++
		}
	}
	return count
}

func (f *fakeHarbor) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeHarbor) project(name string) *fakeProject {
	f.mu.Lock()
	defer f.mu.Unlock()

	project, ok := f.projects[name]
	if !ok {
		return nil
	}
	copied := *project
	return &copied
}

func (f *fakeHarbor) robotsFor(projectName string) []fakeRobot {
	f.mu.Lock()
	defer f.mu.Unlock()

	project, ok := f.projects[projectName]
	if !ok {
		return nil
	}

	var robots []fakeRobot
	for _, robot := range f.sortedRobots() {
		if robot.projectID == project.id {
			robots = append(robots, *robot)
		}
	}
	return robots
}

// seedProject adds a project directly, bypassing the API.
func (f *fakeHarbor) seedProject(name string, public bool, hard, used int64) *fakeProject {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addProject(name, public, hard, used)
}

// seedRobot adds a project robot directly, bypassing the API.
func (f *fakeHarbor) seedRobot(projectName, fullName, secret string) *fakeRobot {
	f.mu.Lock()
	defer f.mu.Unlock()

	project := f.projects[projectName]
	if project == nil {
		f.t.Fatalf("seedRobot: unknown project %q", projectName)
	}
	return f.addRobot(project.id, fullName, secret, nil)
}

func (f *fakeHarbor) addProject(name string, public bool, hard, used int64) *fakeProject {
	project := &fakeProject{
		id:      f.nextProjectID,
		name:    name,
		public:  public,
		quotaID: int64(f.nextProjectID) + 1000,
		hard:    hard,
		used:    used,
	}
	f.nextProjectID++
	f.projects[name] = project
	return project
}

func (f *fakeHarbor) addRobot(projectID int32, name, secret string, permissions []*harbormodels.RobotPermission) *fakeRobot {
	robot := &fakeRobot{
		id:          f.nextRobotID,
		name:        name,
		projectID:   projectID,
		secret:      secret,
		permissions: permissions,
	}
	f.nextRobotID++
	f.robots[robot.id] = robot
	return robot
}

func (f *fakeHarbor) handle(mux *http.ServeMux, pattern string, fn func(http.ResponseWriter, *http.Request)) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, pattern)
		failure, failed := f.failures[pattern]
		if failed && failure.remaining > 0 {
			failure.remaining--
			if failure.remaining == 0 {
				delete(f.failures, pattern)
			} else {
				f.failures[pattern] = failure
			}
		}
		f.mu.Unlock()

		if user, pass, ok := r.BasicAuth(); !ok || user != fakeUsername || pass != fakePassword {
			writeHarborError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
			return
		}
		if failed {
			if failure.body != "" {
				w.WriteHeader(failure.status)
				_, _ = w.Write([]byte(failure.body))
				return
			}
			writeHarborError(w, failure.status, "INJECTED", fmt.Sprintf("injected failure for %s", pattern))
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()
		fn(w, r)
	})
}

func (f *fakeHarbor) projectByPath(w http.ResponseWriter, r *http.Request) *fakeProject {
	if r.Header.Get("X-Is-Resource-Name") != "true" {
		writeHarborError(w, http.StatusBadRequest, "BAD_REQUEST", "expected X-Is-Resource-Name header")
		return nil
	}
	project, ok := f.projects[r.PathValue("name")]
	if !ok {
		writeHarborError(w, http.StatusNotFound, "NOT_FOUND", "project not found")
		return nil
	}
	return project
}

func (f *fakeHarbor) robotByPath(w http.ResponseWriter, r *http.Request) *fakeRobot {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeHarborError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid robot id")
		return nil
	}
	robot, ok := f.robots[id]
	if !ok {
		writeHarborError(w, http.StatusNotFound, "NOT_FOUND", "robot not found")
		return nil
	}
	return robot
}

func (f *fakeHarbor) listProjects(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(f.projects))
	for name := range f.projects {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]*harbormodels.Project, 0, len(names))
	for _, name := range names {
		items = append(items, f.projects[name].model())
	}
	writePage(w, r, items)
}

func (f *fakeHarbor) createProject(w http.ResponseWriter, r *http.Request) {
	var req harbormodels.ProjectReq
	if !decodeBody(w, r, &req) {
		return
	}
	if req.ProjectName == "" {
		writeHarborError(w, http.StatusBadRequest, "BAD_REQUEST", "project_name is required")
		return
	}
	if _, exists := f.projects[req.ProjectName]; exists {
		writeHarborError(w, http.StatusConflict, "CONFLICT", "project already exists")
		return
	}

	hard := int64(-1)
	if req.StorageLimit != nil && *req.StorageLimit > 0 {
		hard = *req.StorageLimit
	}
	public := req.Metadata != nil && req.Metadata.Public == "true"
	f.addProject(req.ProjectName, public, hard, 0)
	w.WriteHeader(http.StatusCreated)
}

func (f *fakeHarbor) getProject(w http.ResponseWriter, r *http.Request) {
	if project := f.projectByPath(w, r); project != nil {
		writeJSON(w, http.StatusOK, project.model())
	}
}

func (f *fakeHarbor) updateProject(w http.ResponseWriter, r *http.Request) {
	project := f.projectByPath(w, r)
	if project == nil {
		return
	}
	var req harbormodels.ProjectReq
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Metadata != nil && req.Metadata.Public != "" {
		project.public = req.Metadata.Public == "true"
	}
	w.WriteHeader(http.StatusOK)
}

func (f *fakeHarbor) deleteProject(w http.ResponseWriter, r *http.Request) {
	project := f.projectByPath(w, r)
	if project == nil {
		return
	}
	for id, robot := range f.robots {
		if robot.projectID == project.id {
			delete(f.robots, id)
		}
	}
	delete(f.projects, project.name)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeHarbor) projectSummary(w http.ResponseWriter, r *http.Request) {
	project := f.projectByPath(w, r)
	if project == nil {
		return
	}
	writeJSON(w, http.StatusOK, &harbormodels.ProjectSummary{
		Quota: &harbormodels.ProjectSummaryQuota{
			Hard: harbormodels.ResourceList{"storage": project.hard},
			Used: harbormodels.ResourceList{"storage": project.used},
		},
	})
}

func (f *fakeHarbor) listRobots(w http.ResponseWriter, r *http.Request) {
	projectID := int32(-1)
	for _, part := range strings.Split(r.URL.Query().Get("q"), ",") {
		key, value, _ := strings.Cut(part, "=")
		if key == "ProjectID" {
			parsed, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				writeHarborError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid ProjectID")
				return
			}
			projectID = int32(parsed)
		}
	}

	items := []*harbormodels.Robot{}
	for _, robot := range f.sortedRobots() {
		if projectID >= 0 && robot.projectID != projectID {
			continue
		}
		items = append(items, robot.model())
	}
	writePage(w, r, items)
}

func (f *fakeHarbor) createRobot(w http.ResponseWriter, r *http.Request) {
	var req harbormodels.RobotCreate
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Level != "project" || len(req.Permissions) != 1 {
		writeHarborError(w, http.StatusBadRequest, "BAD_REQUEST", "expected one project permission")
		return
	}

	project, ok := f.projects[req.Permissions[0].Namespace]
	if !ok {
		writeHarborError(w, http.StatusNotFound, "NOT_FOUND", "project not found")
		return
	}

	fullName := "robot$" + project.name + "+" + req.Name
	for _, robot := range f.robots {
		if robot.name == fullName {
			writeHarborError(w, http.StatusConflict, "CONFLICT", "robot already exists")
			return
		}
	}

	robot := f.addRobot(project.id, fullName, req.Secret, req.Permissions)
	writeJSON(w, http.StatusCreated, &harbormodels.RobotCreated{
		ID:     robot.id,
		Name:   robot.name,
		Secret: robot.secret,
	})
}

func (f *fakeHarbor) getRobot(w http.ResponseWriter, r *http.Request) {
	if robot := f.robotByPath(w, r); robot != nil {
		writeJSON(w, http.StatusOK, robot.model())
	}
}

func (f *fakeHarbor) updateRobot(w http.ResponseWriter, r *http.Request) {
	robot := f.robotByPath(w, r)
	if robot == nil {
		return
	}
	var req harbormodels.RobotCreate
	if !decodeBody(w, r, &req) {
		return
	}
	robot.permissions = req.Permissions
	writeJSON(w, http.StatusOK, &harbormodels.RobotSec{Secret: robot.secret})
}

func (f *fakeHarbor) refreshRobotSecret(w http.ResponseWriter, r *http.Request) {
	robot := f.robotByPath(w, r)
	if robot == nil {
		return
	}
	var req harbormodels.RobotSec
	if !decodeBody(w, r, &req) {
		return
	}
	robot.secret = req.Secret
	writeJSON(w, http.StatusOK, &harbormodels.RobotSec{Secret: robot.secret})
}

func (f *fakeHarbor) deleteRobot(w http.ResponseWriter, r *http.Request) {
	robot := f.robotByPath(w, r)
	if robot == nil {
		return
	}
	delete(f.robots, robot.id)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeHarbor) listQuotas(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("reference") != "project" {
		writeHarborError(w, http.StatusBadRequest, "BAD_REQUEST", "unsupported reference")
		return
	}

	items := []*harbormodels.Quota{}
	for _, project := range f.projects {
		if strconv.Itoa(int(project.id)) == query.Get("reference_id") {
			items = append(items, &harbormodels.Quota{
				ID:   project.quotaID,
				Hard: harbormodels.ResourceList{"storage": project.hard},
			})
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (f *fakeHarbor) updateQuota(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeHarborError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid quota id")
		return
	}
	var req harbormodels.QuotaUpdateReq
	if !decodeBody(w, r, &req) {
		return
	}
	for _, project := range f.projects {
		if project.quotaID == id {
			project.hard = req.Hard["storage"]
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	writeHarborError(w, http.StatusNotFound, "NOT_FOUND", "quota not found")
}

func (f *fakeHarbor) sortedRobots() []*fakeRobot {
	robots := make([]*fakeRobot, 0, len(f.robots))
	for _, robot := range f.robots {
		robots = append(robots, robot)
	}
	sort.Slice(robots, func(i, j int) bool { return robots[i].id < robots[j].id })
	return robots
}

func (p *fakeProject) model() *harbormodels.Project {
	return &harbormodels.Project{
		ProjectID: p.id,
		Name:      p.name,
		Metadata:  &harbormodels.ProjectMetadata{Public: formatHarborBool(p.public)},
	}
}

func (r *fakeRobot) model() *harbormodels.Robot {
	return &harbormodels.Robot{
		ID:          r.id,
		Name:        r.name,
		Level:       "project",
		Permissions: r.permissions,
	}
}

func writePage[T any](w http.ResponseWriter, r *http.Request, items []T) {
	page, pageSize := 1, 10
	if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && value > 0 {
		pageSize = value
	}

	start := min((page-1)*pageSize, len(items))
	end := min(start+pageSize, len(items))

	w.Header().Set("X-Total-Count", strconv.Itoa(len(items)))
	writeJSON(w, http.StatusOK, items[start:end])
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeHarborError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeHarborError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, harborErrorEnvelope{
		Errors: []*harborErrorItem{{Code: code, Message: message}},
	})
}
