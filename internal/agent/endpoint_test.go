package agent_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"charm.land/fantasy"

	"github.com/incurl/flint/internal/agent"
)

// hostCapture records the host a request goes to, and the
// Authorization header it carries.
type hostCapture struct {
	hosts []string
	auth  []string
}

// Do implements the HTTP client the provider uses.
func (c *hostCapture) Do(r *http.Request) (*http.Response, error) {
	c.hosts = append(c.hosts, r.URL.Host)
	c.auth = append(c.auth, r.Header.Get("Authorization"))

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

// TestAModelWithoutAKeyIsRefused verifies that a provider that needs a
// key is refused one that has none.
//
// The request would carry no Authorization header at all, and the
// provider would answer with a message about the header. That message
// says nothing about the variable that holds the key, and nothing
// about the fact that the variable is empty: the user is sent looking
// for a header that was never there.
func TestAModelWithoutAKeyIsRefused(t *testing.T) {
	for _, provider := range []string{"minimax", "minimax-cn", "openai", "openrouter"} {
		t.Run(provider, func(t *testing.T) {
			_, err := agent.NewModel(agent.ProviderConfig{
				Provider: provider,
				Model:    "MiniMax-M3.1-Flash",
				APIKey:   "",
			})
			if !errors.Is(err, agent.ErrNoAPIKey) {
				t.Errorf("NewModel() error = %v, want ErrNoAPIKey", err)
			}
		})
	}
}

// TestALocalProviderNeedsNoKey verifies that a local server is not
// asked for a key.
//
// It runs on the machine, and it usually does not check.
func TestALocalProviderNeedsNoKey(t *testing.T) {
	for _, provider := range []string{"local", "openaicompat", "openai-compatible"} {
		t.Run(provider, func(t *testing.T) {
			_, err := agent.NewModel(agent.ProviderConfig{
				Provider: provider,
				Model:    "llama3.1",
				BaseURL:  "http://127.0.0.1:11434/v1",
			})
			if err != nil {
				t.Errorf("NewModel() error = %v, want no error", err)
			}
		})
	}
}

// TestTheKeyIsOnTheRequest verifies that the key reaches the wire.
//
// The header is what the provider checks, and it is what was missing.
func TestTheKeyIsOnTheRequest(t *testing.T) {
	capture := &hostCapture{}

	model, err := agent.NewModelWithClient(agent.ProviderConfig{
		Provider: "minimax",
		Model:    "MiniMax-M3.1-Flash",
		APIKey:   "sk-the-key",
	}, capture)
	if err != nil {
		t.Fatalf("NewModelWithClient() error = %v", err)
	}

	_, _ = model.Generate(context.Background(), fantasy.Call{
		Prompt: fantasy.Prompt{fantasy.NewUserMessage("hi")},
	})

	if len(capture.auth) == 0 {
		t.Fatal("the model made no request")
	}

	if capture.auth[0] != "Bearer sk-the-key" {
		t.Errorf("the Authorization header is %q, want %q", capture.auth[0], "Bearer sk-the-key")
	}
}
