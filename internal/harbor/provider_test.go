package harbor

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Provider.Delete", func() {
	It("removes repositories before deleting the project", func() {
		var deleteRepositoryCalls []string
		deleteRobotCalled := false
		deleteProjectCalled := false

		client := newTestAPIClient(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/projects/ms-demo":
				return jsonResponse(http.StatusOK, `{"project_id":42,"name":"ms-demo","metadata":{"public":"false"}}`, nil), nil
			case r.Method == http.MethodGet && r.URL.Path == "/projects/ms-demo/repositories":
				return jsonResponse(http.StatusOK, `[{"name":"ms-demo/backend"},{"name":"ms-demo/nested/api"}]`, map[string]string{"X-Total-Count": "2"}), nil
			case r.Method == http.MethodDelete && r.URL.Path == "/projects/ms-demo/repositories/backend":
				deleteRepositoryCalls = append(deleteRepositoryCalls, r.URL.String())
				return jsonResponse(http.StatusOK, "", nil), nil
			case r.Method == http.MethodDelete && r.URL.Path == "/projects/ms-demo/repositories/nested/api":
				deleteRepositoryCalls = append(deleteRepositoryCalls, r.URL.String())
				return jsonResponse(http.StatusOK, "", nil), nil
			case r.Method == http.MethodGet && r.URL.Path == "/robots":
				return jsonResponse(http.StatusOK, `[{"id":7,"name":"robot$ms-demo+ms-admin"}]`, nil), nil
			case r.Method == http.MethodGet && r.URL.Path == "/robots/7":
				return jsonResponse(http.StatusOK, `{"id":7,"name":"robot$ms-demo+ms-admin"}`, nil), nil
			case r.Method == http.MethodDelete && r.URL.Path == "/robots/7":
				deleteRobotCalled = true
				return jsonResponse(http.StatusOK, "", nil), nil
			case r.Method == http.MethodDelete && r.URL.Path == "/projects/ms-demo":
				deleteProjectCalled = true
				return jsonResponse(http.StatusOK, "", nil), nil
			default:
				Fail("unexpected request: " + r.Method + " " + r.URL.String())
				return nil, nil
			}
		})

		provider := NewProvider(Config{
			BaseURL:       "http://harbor.test",
			ProjectPrefix: "ms-",
		}, client, slog.New(slog.NewTextHandler(io.Discard, nil)))

		err := provider.Delete(context.Background(), "demo")

		Expect(err).NotTo(HaveOccurred())
		Expect(deleteRepositoryCalls).To(Equal([]string{
			"http://harbor.test/projects/ms-demo/repositories/backend",
			"http://harbor.test/projects/ms-demo/repositories/nested%2Fapi",
		}))
		Expect(deleteRobotCalled).To(BeTrue())
		Expect(deleteProjectCalled).To(BeTrue())
	})

	It("continues when the managed robot is missing", func() {
		deleteProjectCalled := false

		client := newTestAPIClient(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/projects/ms-demo":
				return jsonResponse(http.StatusOK, `{"project_id":42,"name":"ms-demo","metadata":{"public":"false"}}`, nil), nil
			case r.Method == http.MethodGet && r.URL.Path == "/projects/ms-demo/repositories":
				return jsonResponse(http.StatusOK, `[]`, map[string]string{"X-Total-Count": "0"}), nil
			case r.Method == http.MethodGet && r.URL.Path == "/robots":
				return jsonResponse(http.StatusOK, `[]`, nil), nil
			case r.Method == http.MethodDelete && r.URL.Path == "/projects/ms-demo":
				deleteProjectCalled = true
				return jsonResponse(http.StatusOK, "", nil), nil
			default:
				Fail("unexpected request: " + r.Method + " " + r.URL.String())
				return nil, nil
			}
		})

		provider := NewProvider(Config{
			BaseURL:       "http://harbor.test",
			ProjectPrefix: "ms-",
		}, client, slog.New(slog.NewTextHandler(io.Discard, nil)))

		err := provider.Delete(context.Background(), "demo")

		Expect(err).NotTo(HaveOccurred())
		Expect(deleteProjectCalled).To(BeTrue())
	})
})

func newTestAPIClient(roundTrip func(*http.Request) (*http.Response, error)) *apiClient {
	client := resty.New().
		SetBaseURL("http://harbor.test").
		SetTransport(roundTripperFunc(roundTrip)).
		SetTimeout(time.Second).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json")

	return &apiClient{
		client: client,
		timeout: timeouts{
			requestTimeoutSeconds: 1,
		},
	}
}

func jsonResponse(statusCode int, body string, headers map[string]string) *http.Response {
	responseHeaders := make(http.Header, len(headers)+1)
	responseHeaders.Set("Content-Type", "application/json")
	for key, value := range headers {
		responseHeaders.Set(key, value)
	}

	return &http.Response{
		StatusCode: statusCode,
		Header:     responseHeaders,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
