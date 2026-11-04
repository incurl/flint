package agent_test

import (
	"context"
	"net/http"
	"testing"

	"charm.land/fantasy"

	"github.com/incurl/flint/internal/agent"
)

// hostCapture records the host a request goes to.
type hostCapture struct {
	hosts []string
}

// Do implements the HTTP client the provider uses.
func (c *hostCapture) Do(r *http.Request) (*http.Response, error) {
	c.hosts = append(c.hosts, r.URL.Host)

	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       http.NoBody,
		Request:    r,
	}, nil
}

// TestTheProviderReachesItsEndpoint verifies that a provider id reaches
// the endpoint it names.
//
// The two MiniMax platforms have separate accounts, separate keys and
// separate balances. A provider that reaches the wrong endpoint does not
// fail with "unknown provider": it fails with "insufficient balance",
// and the user has no way to tell that the endpoint is the problem.
func TestTheProviderReachesItsEndpoint(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"minimax", "api.minimax.io"},
		{"minimax-global", "api.minimax.io"},
		{"minimax-intl", "api.minimax.io"},
		{"minimax-cn", "api.minimax.cn"},
		{"minimax-china", "api.minimax.cn"},
	}

	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			capture := &hostCapture{}

			// The provider is built by the real code, so the test
			// fails when the mapping is wrong.
			model, err := agent.NewModelWithClient(agent.ProviderConfig{
				Provider: test.provider,
				Model:    "MiniMax-M3.1-Flash",
				APIKey:   "sk-test",
			}, capture)
			if err != nil {
				t.Fatalf("NewModelWithClient() error = %v", err)
			}

			_, _ = model.Generate(context.Background(), fantasy.Call{
				Prompt: fantasy.Prompt{fantasy.NewUserMessage("hi")},
			})

			if len(capture.hosts) == 0 {
				t.Fatal("the model made no request")
			}

			if capture.hosts[0] != test.want {
				t.Errorf("the provider %q reached %q, want %q", test.provider, capture.hosts[0], test.want)
			}
		})
	}
}
