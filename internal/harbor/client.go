package harbor

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-resty/resty/v2"
	harbormodels "github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

var (
	errHarborNotFound      = errors.New("harbor resource not found")
	errHarborConflict      = errors.New("harbor resource conflict")
	errHarborInvalid       = errors.New("harbor resource invalid")
	errHarborRequestFailed = errors.New("harbor request failed")
)

type apiClient struct {
	client  *resty.Client
	timeout timeouts
}

type timeouts struct {
	requestTimeoutSeconds int64
}

type harborErrorEnvelope struct {
	Errors []*harborErrorItem `json:"errors"`
}

type harborErrorItem struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type harborQuota struct {
	ID int64 `json:"id,omitempty"`
}

func NewClient(cfg Config) (*apiClient, error) {
	baseURL, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse harbor url: %w", err)
	}

	client := resty.New().
		SetBaseURL(baseURL.String()).
		// Required as harbor sends a csrf error which is not required for the api.
		SetCookieJar(nil).
		SetTimeout(cfg.Timeout).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json").
		SetTLSClientConfig(&tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		})

	switch cfg.AuthMode {
	case AuthModeBasic:
		client.SetBasicAuth(cfg.Username, cfg.Password)
	case AuthModeBearer:
		client.SetAuthToken(cfg.Token)
	default:
		return nil, fmt.Errorf("unsupported auth mode %q", cfg.AuthMode)
	}

	return &apiClient{
		client: client,
		timeout: timeouts{
			requestTimeoutSeconds: int64(cfg.Timeout.Seconds()),
		},
	}, nil
}

func (c *apiClient) CreateProject(ctx context.Context, name string, public bool, storageLimit int64) error {
	project := &harbormodels.ProjectReq{
		ProjectName: name,
		Metadata: &harbormodels.ProjectMetadata{
			Public: formatHarborBool(public),
		},
	}
	if storageLimit > 0 {
		project.StorageLimit = int64Ptr(storageLimit)
	}

	_, err := c.do(ctx, http.MethodPost, "/projects", requestOptions{
		body: project,
	})
	return err
}

func (c *apiClient) GetProject(ctx context.Context, name string) (*harbormodels.Project, error) {
	var project harbormodels.Project
	_, err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(name), requestOptions{
		headers: resourceNameHeaders(),
		result:  &project,
	})
	if err != nil {
		return nil, err
	}

	return &project, nil
}

func (c *apiClient) ListProjects(ctx context.Context) ([]*harbormodels.Project, error) {
	const pageSize = 100

	var projects []*harbormodels.Project
	for page := 1; ; page++ {
		var batch []*harbormodels.Project
		resp, err := c.do(ctx, http.MethodGet, "/projects", requestOptions{
			query: map[string]string{
				"page":        strconv.Itoa(page),
				"page_size":   strconv.Itoa(pageSize),
				"with_detail": "true",
			},
			result: &batch,
		})
		if err != nil {
			return nil, err
		}

		projects = append(projects, batch...)
		if donePaging(resp, len(batch), len(projects), pageSize) {
			return projects, nil
		}
	}
}

func (c *apiClient) UpdateProject(ctx context.Context, name string, public bool, storageLimit int64) error {
	project := &harbormodels.ProjectReq{
		Metadata: &harbormodels.ProjectMetadata{
			Public: formatHarborBool(public),
		},
	}
	if storageLimit > 0 {
		project.StorageLimit = int64Ptr(storageLimit)
	}

	_, err := c.do(ctx, http.MethodPut, "/projects/"+url.PathEscape(name), requestOptions{
		headers: resourceNameHeaders(),
		body:    project,
	})
	return err
}

func (c *apiClient) GetProjectSummary(ctx context.Context, name string) (*harbormodels.ProjectSummary, error) {
	var summary harbormodels.ProjectSummary
	_, err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(name)+"/summary", requestOptions{
		headers: resourceNameHeaders(),
		result:  &summary,
	})
	if err != nil {
		return nil, err
	}

	return &summary, nil
}

func (c *apiClient) DeleteProject(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodDelete, "/projects/"+url.PathEscape(name), requestOptions{
		headers: resourceNameHeaders(),
	})
	return err
}

func (c *apiClient) ListProjectRepositories(ctx context.Context, projectName string) ([]*harbormodels.Repository, error) {
	const pageSize = 100

	var repositories []*harbormodels.Repository
	for page := 1; ; page++ {
		var batch []*harbormodels.Repository
		resp, err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(projectName)+"/repositories", requestOptions{
			headers: resourceNameHeaders(),
			query: map[string]string{
				"page":      strconv.Itoa(page),
				"page_size": strconv.Itoa(pageSize),
			},
			result: &batch,
		})
		if err != nil {
			return nil, err
		}

		repositories = append(repositories, batch...)
		if donePaging(resp, len(batch), len(repositories), pageSize) {
			return repositories, nil
		}
	}
}

func (c *apiClient) DeleteProjectRepository(ctx context.Context, projectName, repositoryName string) error {
	_, err := c.do(ctx, http.MethodDelete, "/projects/"+url.PathEscape(projectName)+"/repositories/"+url.PathEscape(repositoryPathName(projectName, repositoryName)), requestOptions{
		headers: resourceNameHeaders(),
	})
	return err
}

func (c *apiClient) CreateProjectRobot(ctx context.Context, projectName string, req *harbormodels.RobotCreate) (*harbormodels.RobotCreated, error) {
	var robot harbormodels.RobotCreated
	_, err := c.do(ctx, http.MethodPost, "/robots", requestOptions{
		body:   req,
		result: &robot,
	})
	if err != nil {
		return nil, err
	}

	return &robot, nil
}

func (c *apiClient) ListProjectRobots(ctx context.Context, projectID int64) ([]*harbormodels.Robot, error) {
	const pageSize = 100

	var robots []*harbormodels.Robot
	for page := 1; ; page++ {
		var batch []*harbormodels.Robot
		resp, err := c.do(ctx, http.MethodGet, "/robots", requestOptions{
			query: map[string]string{
				"page":      strconv.Itoa(page),
				"page_size": strconv.Itoa(pageSize),
				"q":         "Level=project,ProjectID=" + strconv.FormatInt(projectID, 10),
			},
			result: &batch,
		})
		if err != nil {
			return nil, err
		}

		robots = append(robots, batch...)
		if donePaging(resp, len(batch), len(robots), pageSize) {
			return robots, nil
		}
	}
}

func (c *apiClient) GetProjectRobot(ctx context.Context, robotID int64) (*harbormodels.Robot, error) {
	var robot harbormodels.Robot
	_, err := c.do(ctx, http.MethodGet, "/robots/"+strconv.FormatInt(robotID, 10), requestOptions{
		result: &robot,
	})
	if err != nil {
		return nil, err
	}

	return &robot, nil
}

func (c *apiClient) UpdateProjectRobot(ctx context.Context, robotID int64, req *harbormodels.RobotCreate) (*harbormodels.RobotSec, error) {
	var secret harbormodels.RobotSec
	_, err := c.do(ctx, http.MethodPut, "/robots/"+strconv.FormatInt(robotID, 10), requestOptions{
		body:   req,
		result: &secret,
	})
	if err != nil {
		return nil, err
	}

	return &secret, nil
}

func (c *apiClient) UpdateProjectRobotPassword(ctx context.Context, robotID int64, password string) error {
	var secret harbormodels.RobotSec
	_, err := c.do(ctx, http.MethodPatch, "/robots/"+strconv.FormatInt(robotID, 10), requestOptions{
		body: &harbormodels.RobotSec{
			Secret: password,
		},
		result: &secret,
	})
	return err
}

func (c *apiClient) DeleteProjectRobot(ctx context.Context, robotID int64) error {
	_, err := c.do(ctx, http.MethodDelete, "/robots/"+strconv.FormatInt(robotID, 10), requestOptions{})
	return err
}

func (c *apiClient) UpdateProjectStorageQuota(ctx context.Context, projectID int64, storageLimit int64) error {
	var quotas []*harborQuota
	_, err := c.do(ctx, http.MethodGet, "/quotas", requestOptions{
		query: map[string]string{
			"reference":    "project",
			"reference_id": strconv.FormatInt(projectID, 10),
			"page":         "1",
			"page_size":    "1",
		},
		result: &quotas,
	})
	if err != nil {
		return err
	}

	if len(quotas) == 0 || quotas[0] == nil {
		return fmt.Errorf("%w: missing quota for project %d", errHarborNotFound, projectID)
	}

	_, err = c.do(ctx, http.MethodPut, "/quotas/"+strconv.FormatInt(quotas[0].ID, 10), requestOptions{
		body: &harbormodels.QuotaUpdateReq{
			Hard: harbormodels.ResourceList{
				"storage": storageLimit,
			},
		},
	})
	return err
}

type requestOptions struct {
	headers map[string]string
	query   map[string]string
	body    any
	result  any
}

func (c *apiClient) do(ctx context.Context, method, path string, opts requestOptions) (*resty.Response, error) {
	req := c.client.R().SetContext(ctx)
	for key, value := range opts.headers {
		req.SetHeader(key, value)
	}
	for key, value := range opts.query {
		req.SetQueryParam(key, value)
	}
	if opts.body != nil {
		req.SetBody(opts.body)
	}

	resp, err := req.Execute(method, path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errHarborRequestFailed, err)
	}
	if resp.IsError() {
		return nil, mapResponseError(resp)
	}
	if opts.result != nil && len(resp.Body()) > 0 {
		if err := json.Unmarshal(resp.Body(), opts.result); err != nil {
			return nil, fmt.Errorf("%w: %s", errHarborRequestFailed, formatDecodeError(resp, err))
		}
	}

	return resp, nil
}

func mapResponseError(resp *resty.Response) error {
	message := harborErrorMessage(resp)
	switch resp.StatusCode() {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", errHarborNotFound, message)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", errHarborConflict, message)
	case http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusPreconditionFailed:
		return fmt.Errorf("%w: %s", errHarborInvalid, message)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %s", errHarborRequestFailed, message)
	default:
		return fmt.Errorf("%w: %s", errHarborRequestFailed, message)
	}
}

func harborErrorMessage(resp *resty.Response) string {
	var envelope harborErrorEnvelope
	if err := json.Unmarshal(resp.Body(), &envelope); err == nil && len(envelope.Errors) > 0 {
		messages := make([]string, 0, len(envelope.Errors))
		for _, issue := range envelope.Errors {
			if issue == nil {
				continue
			}
			message := strings.TrimSpace(issue.Message)
			if message == "" {
				message = strings.TrimSpace(issue.Code)
			}
			if message != "" {
				messages = append(messages, message)
			}
		}
		if len(messages) > 0 {
			return strings.Join(messages, "; ")
		}
	}

	body := strings.TrimSpace(string(resp.Body()))
	if body != "" {
		return fmt.Sprintf("status %d: %s", resp.StatusCode(), trimForError(body))
	}

	return fmt.Sprintf("status %d", resp.StatusCode())
}

func formatDecodeError(resp *resty.Response, err error) string {
	body := strings.TrimSpace(string(resp.Body()))
	if body == "" {
		return fmt.Sprintf("failed to decode response body: %v", err)
	}

	return fmt.Sprintf("failed to decode response body: %v; status=%d body=%q", err, resp.StatusCode(), trimForError(body))
}

func trimForError(body string) string {
	if len(body) <= 512 {
		return body
	}

	return body[:512] + "..."
}

func donePaging(resp *resty.Response, batchLen, totalFetched, pageSize int) bool {
	if resp == nil {
		return batchLen < pageSize
	}

	totalCountHeader := strings.TrimSpace(resp.Header().Get("X-Total-Count"))
	if totalCountHeader != "" {
		totalCount, err := strconv.Atoi(totalCountHeader)
		if err == nil {
			return totalFetched >= totalCount
		}
	}

	return batchLen < pageSize
}

func resourceNameHeaders() map[string]string {
	return map[string]string{
		"X-Is-Resource-Name": "true",
	}
}

func repositoryPathName(projectName, repositoryName string) string {
	prefix := strings.TrimSpace(projectName) + "/"
	if strings.HasPrefix(repositoryName, prefix) {
		return strings.TrimPrefix(repositoryName, prefix)
	}

	return repositoryName
}

func parseProjectVisibility(project *harbormodels.Project) bool {
	return project != nil && project.Metadata != nil && strings.EqualFold(project.Metadata.Public, "true")
}

func int64Ptr(v int64) *int64 {
	return &v
}

func formatHarborBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
