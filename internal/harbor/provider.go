// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package harbor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strings"

	msclient "github.com/codesphere-cloud/managed-services-lib/client"
	"github.com/codesphere-cloud/managed-services-lib/model"
	"github.com/codesphere-cloud/managed-services-lib/provider"
	harbormodels "github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

// ProviderType is the API path segment and logical provider name.
const ProviderType = "harbor"
const managedRobotName = "ms-admin"

// Client is the subset of the Harbor API used by the provider.
type Client interface {
	CreateProject(ctx context.Context, name string, public bool, storageLimit int64) error
	GetProject(ctx context.Context, name string) (*harbormodels.Project, error)
	ListProjects(ctx context.Context) ([]*harbormodels.Project, error)
	UpdateProject(ctx context.Context, name string, public bool, storageLimit int64) error
	GetProjectSummary(ctx context.Context, name string) (*harbormodels.ProjectSummary, error)
	DeleteProject(ctx context.Context, name string) error
	CreateProjectRobot(ctx context.Context, projectName string, req *harbormodels.RobotCreate) (*harbormodels.RobotCreated, error)
	ListProjectRobots(ctx context.Context, projectID int64) ([]*harbormodels.Robot, error)
	GetProjectRobot(ctx context.Context, robotID int64) (*harbormodels.Robot, error)
	UpdateProjectRobotPassword(ctx context.Context, robotID int64, password string) error
	DeleteProjectRobot(ctx context.Context, robotID int64) error
	UpdateProjectStorageQuota(ctx context.Context, projectID int64, storageLimit int64) error
}

// Provider manages Harbor projects and project-scoped robot accounts.
type Provider struct {
	cfg    Config
	client Client
	logger *slog.Logger
}

// NewProvider creates a Harbor provider.
func NewProvider(cfg Config, client Client, logger *slog.Logger) *Provider {
	return &Provider{
		cfg:    cfg,
		client: client,
		logger: logger,
	}
}

// Create creates a Harbor project and a project-scoped robot account.
func (p *Provider) Create(ctx context.Context, params provider.CreateRequest[PlanParameters, ServiceConfig, ServiceSecrets]) error {
	if err := p.validateCreate(&params); err != nil {
		return err
	}

	projectName := p.projectName(params.ID)
	p.logger.Info(
		"create harbor service invoked",
		"id", params.ID,
		"project", projectName,
		"public", params.Config.Public,
		"storageMiB", params.Plan.StorageMiB,
	)

	projectCreated, err := p.reconcileService(ctx, reconcileServiceArgs{
		id:              params.ID,
		public:          &params.Config.Public,
		storageMiB:      intPtr(params.Plan.StorageMiB),
		robotPassword:   stringPtr(params.Secrets.SuperuserPassword),
		allowCreate:     true,
		createErrorPath: true,
	})
	if err != nil {
		if projectCreated {
			if rollbackErr := p.client.DeleteProject(ctx, projectName); rollbackErr != nil {
				p.logger.Warn("rollback failed after create reconciliation error", "project", projectName, "error", rollbackErr)
			}
		}
		return err
	}

	return nil
}

// List returns the managed service IDs derived from Harbor project names.
func (p *Provider) List(ctx context.Context) ([]model.ServiceID, error) {
	projects, err := p.client.ListProjects(ctx)
	if err != nil {
		return nil, p.mapUpstreamError("list projects", err)
	}

	ids := make([]model.ServiceID, 0, len(projects))
	for _, project := range projects {
		if project == nil || !strings.HasPrefix(project.Name, p.cfg.ProjectPrefix) {
			continue
		}
		ids = append(ids, model.ServiceID(strings.TrimPrefix(project.Name, p.cfg.ProjectPrefix)))
	}

	return ids, nil
}

// GetStatus returns Harbor project and robot-account state for the requested service IDs.
func (p *Provider) GetStatus(ctx context.Context, ids []model.ServiceID) (map[model.ServiceID]Status, error) {
	result := make(map[model.ServiceID]Status, len(ids))

	for _, id := range ids {
		project, robot, err := p.lookupState(ctx, id)
		if err != nil {
			if errors.Is(err, provider.ErrServiceNotFound) {
				p.logger.Debug("state not found", "id", id, "err", err)
				continue
			}
			return nil, err
		}

		summary, err := p.client.GetProjectSummary(ctx, project.Name)
		if err != nil {
			return nil, p.mapUpstreamError("get project summary", err)
		}

		paused := false
		status := provider.NewServiceStatus(
			PlanParameters{
				StorageMiB: projectSummaryStorageMiB(summary),
			},
			ServiceConfig{
				Public: parseProjectVisibility(project),
			},
			Details{
				Ready:              true,
				ProjectName:        project.Name,
				Username:           robot.Name,
				HarborURL:          p.cfg.BaseURL,
				RegistryURL:        projectRegistryURL(p.cfg.BaseURL, project.Name),
				UsedStorage:        projectSummaryUsedStorageMiB(summary),
				UsedStoragePercent: projectSummaryUsedStoragePercent(summary),
			},
			&paused,
			nil,
		)

		result[id] = status
	}

	return result, nil
}

// Update updates Harbor project visibility, storage quota, and the managed robot secret.
func (p *Provider) Update(ctx context.Context, req provider.UpdateRequest[UpdateArgs]) error {
	id := req.ID
	args := req.Params
	if id == "" {
		return fmt.Errorf("%w: missing service id", provider.ErrInvalidArgument)
	}

	p.logger.Info(
		"update harbor service invoked",
		"id", id,
		"hasConfigUpdate", args.Config != nil,
		"hasPlanUpdate", args.Plan != nil,
		"hasSecretsUpdate", args.Secrets != nil,
		"hasSuperuserPasswordUpdate", args.Secrets != nil && strings.TrimSpace(args.Secrets.SuperuserPassword) != "",
	)

	var public *bool
	if args.Config != nil {
		public = &args.Config.Public
	}

	var storageMiB *int
	if args.Plan != nil {
		if args.Plan.Parameters.StorageMiB < 0 {
			return fmt.Errorf("%w: plan.parameters.storage must not be negative", provider.ErrInvalidArgument)
		}
		storageMiB = intPtr(args.Plan.Parameters.StorageMiB)
	}

	var robotPassword *string
	if args.Secrets != nil {
		if strings.TrimSpace(args.Secrets.SuperuserPassword) == "" {
			return fmt.Errorf("%w: secrets.superuserPassword must not be empty", provider.ErrInvalidArgument)
		}
		robotPassword = &args.Secrets.SuperuserPassword
	}

	_, err := p.reconcileService(ctx, reconcileServiceArgs{
		id:            id,
		public:        public,
		storageMiB:    storageMiB,
		robotPassword: robotPassword,
		allowCreate:   false,
	})
	return err
}

// Delete removes the Harbor project and managed Harbor robot account.
func (p *Provider) Delete(ctx context.Context, id model.ServiceID) error {
	p.logger.Info("delete harbor service invoked", "id", id)

	project, err := p.findProjectByServiceID(ctx, id)
	if err != nil {
		return err
	}

	projectName := project.Name
	robot, err := p.findProjectRobot(ctx, int64(project.ProjectID), projectName, p.robotName(id))
	switch {
	case err == nil:
		if err := p.client.DeleteProjectRobot(ctx, robot.ID); err != nil && !errors.Is(err, errHarborNotFound) {
			return p.mapUpstreamError("delete robot account", err)
		}
	case !errors.Is(err, errHarborNotFound):
		return p.mapUpstreamError("find project robot", err)
	}

	if err := p.client.DeleteProject(ctx, projectName); err != nil {
		if errors.Is(err, errHarborNotFound) {
			return fmt.Errorf("%w: %s", provider.ErrServiceNotFound, id)
		}
		return p.mapUpstreamError("delete project", err)
	}

	return nil
}

func (p *Provider) lookupState(
	ctx context.Context,
	id model.ServiceID,
) (*harbormodels.Project, *harbormodels.Robot, error) {
	project, err := p.findProjectByServiceID(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	robotName := p.robotName(id)
	robot, err := p.findProjectRobot(ctx, int64(project.ProjectID), project.Name, robotName)
	if err != nil {
		if errors.Is(err, errHarborNotFound) {
			return nil, nil, fmt.Errorf("%w: missing robot account for %s", provider.ErrServiceNotFound, id)
		}
		return nil, nil, p.mapUpstreamError("find project robot", err)
	}

	return project, robot, nil
}

func (p *Provider) findProjectByServiceID(ctx context.Context, id model.ServiceID) (*harbormodels.Project, error) {
	projectName := p.projectName(id)
	project, err := p.client.GetProject(ctx, projectName)
	if err != nil {
		if errors.Is(err, errHarborNotFound) {
			return nil, fmt.Errorf("%w: %s", provider.ErrServiceNotFound, projectName)
		}
		return nil, p.mapUpstreamError("get project", err)
	}

	return project, nil
}

type reconcileServiceArgs struct {
	id              model.ServiceID
	public          *bool
	storageMiB      *int
	robotPassword   *string
	allowCreate     bool
	createErrorPath bool
}

func (p *Provider) reconcileService(ctx context.Context, args reconcileServiceArgs) (bool, error) {
	project, projectCreated, err := p.ensureProject(ctx, args.id, args.public, args.storageMiB, args.allowCreate, args.createErrorPath)
	if err != nil {
		return projectCreated, err
	}

	if args.robotPassword == nil {
		return projectCreated, nil
	}

	robot, err := p.ensureProjectRobot(ctx, args.id, project, *args.robotPassword, args.allowCreate, args.createErrorPath)
	if err != nil {
		return projectCreated, err
	}

	p.logger.Info(
		"updating harbor robot password",
		"id", args.id,
		"project", project.Name,
		"robotID", robot.ID,
	)
	if err := p.client.UpdateProjectRobotPassword(ctx, robot.ID, *args.robotPassword); err != nil {
		if args.createErrorPath {
			return projectCreated, p.mapCreateError("update robot account password", err)
		}
		return projectCreated, p.mapUpstreamError("update robot secret", err)
	}

	return projectCreated, nil
}

func (p *Provider) ensureProject(ctx context.Context, id model.ServiceID, public *bool, storageMiB *int, allowCreate bool, createErrorPath bool) (*harbormodels.Project, bool, error) {
	projectName := p.projectName(id)
	projectCreated := false

	project, err := p.client.GetProject(ctx, projectName)
	switch {
	case err == nil:
		if public != nil {
			if err := p.client.UpdateProject(ctx, projectName, *public, 0); err != nil {
				return nil, false, p.mapUpstreamError("update project", err)
			}
		}
		if storageMiB != nil && *storageMiB > 0 {
			p.logger.Info(
				"updating harbor storage quota",
				"id", id,
				"project", projectName,
				"projectID", project.ProjectID,
				"storageMiB", *storageMiB,
			)
			if err := p.client.UpdateProjectStorageQuota(ctx, int64(project.ProjectID), storageMiBToHarborQuota(*storageMiB)); err != nil {
				return nil, false, p.mapUpstreamError("update project quota", err)
			}
		}
	case errors.Is(err, errHarborNotFound):
		if !allowCreate {
			return nil, false, fmt.Errorf("%w: %s", provider.ErrServiceNotFound, projectName)
		}
		createPublic := false
		if public != nil {
			createPublic = *public
		}
		createStorageMiB := 0
		if storageMiB != nil {
			createStorageMiB = *storageMiB
		}
		if err := p.client.CreateProject(ctx, projectName, createPublic, storageMiBToHarborQuota(createStorageMiB)); err != nil {
			if !errors.Is(err, errHarborConflict) {
				if createErrorPath {
					return nil, false, p.mapCreateError("create project", err)
				}
				return nil, false, p.mapUpstreamError("create project", err)
			}
		} else {
			projectCreated = true
		}
	default:
		if createErrorPath {
			return nil, false, p.mapCreateError("create project", err)
		}
		return nil, false, p.mapUpstreamError("get project", err)
	}

	project, err = p.client.GetProject(ctx, projectName)
	if err != nil {
		return nil, false, p.mapUpstreamError("get project", err)
	}

	return project, projectCreated, nil
}

func (p *Provider) ensureProjectRobot(ctx context.Context, id model.ServiceID, project *harbormodels.Project, password string, allowCreate bool, createErrorPath bool) (*harbormodels.Robot, error) {
	if project == nil {
		return nil, fmt.Errorf("%w: project is required", provider.ErrInvalidArgument)
	}

	robotName := p.robotName(id)
	robot, err := p.findProjectRobot(ctx, int64(project.ProjectID), project.Name, robotName)
	switch {
	case err == nil:
		return robot, nil
	case !errors.Is(err, errHarborNotFound):
		return nil, p.mapUpstreamError("find project robot", err)
	}
	if !allowCreate {
		return nil, fmt.Errorf("%w: missing robot account for %s", provider.ErrServiceNotFound, id)
	}

	createRobotReq := &harbormodels.RobotCreate{
		Name:        managedRobotName,
		Level:       "project",
		Description: fmt.Sprintf("Managed service robot for %s", project.Name),
		Secret:      password,
		Disable:     false,
		Duration:    -1,
		Permissions: projectRobotPermissions(project.Name),
	}

	createdRobot, err := p.client.CreateProjectRobot(ctx, project.Name, createRobotReq)
	if err != nil && !errors.Is(err, errHarborConflict) {
		if createErrorPath {
			return nil, p.mapCreateError("create robot account", err)
		}
		return nil, p.mapUpstreamError("create robot account", err)
	}

	if createdRobot != nil {
		return p.client.GetProjectRobot(ctx, createdRobot.ID)
	}

	robot, err = p.findProjectRobot(ctx, int64(project.ProjectID), project.Name, robotName)
	if err != nil {
		return nil, p.mapUpstreamError("find project robot", err)
	}

	return robot, nil
}

func (p *Provider) projectName(id model.ServiceID) string {
	return p.cfg.ProjectPrefix + normalizeName(string(id))
}

func (p *Provider) robotName(id model.ServiceID) string {
	return "robot$" + p.projectName(id) + "+" + managedRobotName
}

func (p *Provider) validateCreate(params *provider.CreateRequest[PlanParameters, ServiceConfig, ServiceSecrets]) error {
	if params == nil {
		return fmt.Errorf("%w: request body is required", provider.ErrInvalidArgument)
	}
	if params.ID == "" {
		return fmt.Errorf("%w: id is required", provider.ErrInvalidArgument)
	}
	if strings.TrimSpace(params.Secrets.SuperuserPassword) == "" {
		return fmt.Errorf("%w: secrets.superuserPassword is required", provider.ErrInvalidArgument)
	}
	if params.Plan.StorageMiB < 0 {
		return fmt.Errorf("%w: plan.parameters.storage must not be negative", provider.ErrInvalidArgument)
	}
	return nil
}

func (p *Provider) findProjectRobot(ctx context.Context, projectID int64, projectName, robotName string) (*harbormodels.Robot, error) {
	robots, err := p.client.ListProjectRobots(ctx, projectID)
	if err != nil {
		return nil, err
	}

	var matched *harbormodels.Robot
	for _, robot := range robots {
		if robot == nil || robot.Name != robotName {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("%w: multiple harbor robots matched project %s", provider.ErrInvalidArgument, projectName)
		}
		matched = robot
	}

	if matched == nil {
		return nil, errHarborNotFound
	}

	return p.client.GetProjectRobot(ctx, matched.ID)
}

func storageMiBToHarborQuota(storageMiB int) int64 {
	if storageMiB <= 0 {
		return 0
	}

	return int64(storageMiB) * 1024 * 1024
}

func intPtr(v int) *int {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func registryURL(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}

	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return strings.TrimRight(parsed.String(), "/")
}

func projectRegistryURL(baseURL, projectName string) string {
	registry := registryURL(baseURL)
	if registry == "" {
		return projectName
	}

	return registry + "/" + projectName
}

func projectSummaryStorageMiB(summary *harbormodels.ProjectSummary) int {
	if summary == nil || summary.Quota == nil || summary.Quota.Hard == nil {
		return 0
	}

	storageBytes, ok := summary.Quota.Hard["storage"]
	if !ok || storageBytes <= 0 {
		return 0
	}

	return int(storageBytes / (1024 * 1024))
}

func projectSummaryUsedStorageMiB(summary *harbormodels.ProjectSummary) int {
	if summary == nil || summary.Quota == nil || summary.Quota.Used == nil {
		return 0
	}

	storageBytes, ok := summary.Quota.Used["storage"]
	if !ok || storageBytes <= 0 {
		return 0
	}

	return int(storageBytes / (1024 * 1024))
}

func projectSummaryUsedStoragePercent(summary *harbormodels.ProjectSummary) float64 {
	if summary == nil || summary.Quota == nil || summary.Quota.Hard == nil || summary.Quota.Used == nil {
		return 0
	}

	hardStorageBytes, ok := summary.Quota.Hard["storage"]
	if !ok || hardStorageBytes <= 0 {
		return 0
	}

	usedStorageBytes, ok := summary.Quota.Used["storage"]
	if !ok || usedStorageBytes <= 0 {
		return 0
	}

	percent := float64(usedStorageBytes) / float64(hardStorageBytes) * 100
	return math.Round(percent*100) / 100
}

func projectRobotPermissions(projectName string) []*harbormodels.RobotPermission {
	return []*harbormodels.RobotPermission{
		{
			Kind:      "project",
			Namespace: projectName,
			Access:    projectRobotAccess(),
		},
	}
}

func projectRobotAccess() []*harbormodels.Access {
	entries := []struct {
		resource string
		action   string
	}{
		{"artifact", "create"},
		{"artifact-label", "create"},
		{"export-cve", "create"},
		{"immutable-tag", "create"},
		{"label", "create"},
		{"member", "create"},
		{"metadata", "create"},
		{"notification-policy", "create"},
		{"preheat-policy", "create"},
		{"robot", "create"},
		{"sbom", "create"},
		{"scan", "create"},
		{"scanner", "create"},
		{"tag", "create"},
		{"tag-retention", "create"},
		{"artifact", "delete"},
		{"artifact-label", "delete"},
		{"immutable-tag", "delete"},
		{"label", "delete"},
		{"member", "delete"},
		{"metadata", "delete"},
		{"notification-policy", "delete"},
		{"preheat-policy", "delete"},
		{"project", "delete"},
		{"repository", "delete"},
		{"robot", "delete"},
		{"tag", "delete"},
		{"tag-retention", "delete"},
		{"accessory", "list"},
		{"artifact", "list"},
		{"immutable-tag", "list"},
		{"label", "list"},
		{"log", "list"},
		{"member", "list"},
		{"metadata", "list"},
		{"notification-policy", "list"},
		{"preheat-policy", "list"},
		{"repository", "list"},
		{"robot", "list"},
		{"tag", "list"},
		{"tag-retention", "list"},
		{"repository", "pull"},
		{"repository", "push"},
		{"artifact", "read"},
		{"artifact-addition", "read"},
		{"export-cve", "read"},
		{"label", "read"},
		{"member", "read"},
		{"metadata", "read"},
		{"notification-policy", "read"},
		{"preheat-policy", "read"},
		{"project", "read"},
		{"quota", "read"},
		{"repository", "read"},
		{"robot", "read"},
		{"sbom", "read"},
		{"scan", "read"},
		{"scanner", "read"},
		{"tag-retention", "read"},
		{"sbom", "stop"},
		{"scan", "stop"},
		{"immutable-tag", "update"},
		{"label", "update"},
		{"member", "update"},
		{"metadata", "update"},
		{"notification-policy", "update"},
		{"preheat-policy", "update"},
		{"project", "update"},
		{"repository", "update"},
		{"tag-retention", "update"},
	}

	access := make([]*harbormodels.Access, 0, len(entries))
	for _, entry := range entries {
		access = append(access, &harbormodels.Access{
			Resource: entry.resource,
			Action:   entry.action,
		})
	}

	return access
}

func (p *Provider) mapCreateError(action string, err error) error {
	if errors.Is(err, errHarborConflict) {
		p.logKnownUpstreamError(action, "conflict", err)
		return fmt.Errorf("%w: %s", msclient.ErrResourceConflict, action)
	}
	if errors.Is(err, errHarborNotFound) {
		p.logKnownUpstreamError(action, "notfound", err)
		return fmt.Errorf("%w: %s", provider.ErrServiceNotFound, action)
	}
	return p.mapUpstreamError(action, err)
}

func (p *Provider) mapUpstreamError(action string, err error) error {
	if errors.Is(err, errHarborConflict) {
		p.logKnownUpstreamError(action, "conflict", err)
		return fmt.Errorf("%w: %s", msclient.ErrResourceConflict, action)
	}
	if errors.Is(err, errHarborInvalid) {
		p.logKnownUpstreamError(action, "invalid_argument", err)
		return fmt.Errorf("%w: %s", provider.ErrInvalidArgument, action)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func (p *Provider) logKnownUpstreamError(action string, kind string, err error) {
	if p.logger == nil {
		return
	}

	p.logger.Warn("mapped known upstream error", "action", action, "kind", kind, "error", err)
}

func normalizeName(value string) string {
	var b strings.Builder
	b.Grow(len(value))

	lastWasDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasDash = false
		case r == '-', r == '_', r == '.':
			if !lastWasDash {
				b.WriteRune('-')
				lastWasDash = true
			}
		default:
			if !lastWasDash {
				b.WriteRune('-')
				lastWasDash = true
			}
		}
	}

	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "service"
	}

	return result
}
