// +build integration
package kuberos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/negz/kuberos/extractor"

	oidc "github.com/coreos/go-oidc"
	"golang.org/x/oauth2"
)

func TestOfflineAsScope(t *testing.T) {
	cases := []struct {
		name           string
		issuer         string
		offlineAsScope bool
	}{
		// TODO(negz): Call a mock provider instead of Google. oidc.NewProvider()
		// is the only way to instantiate a provider, and makes a real HTTP call.
		{
			name:           "Google",
			issuer:         "https://accounts.google.com",
			offlineAsScope: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p, err := oidc.NewProvider(context.Background(), tt.issuer)
			if err != nil {
				t.Fatalf("oidc.NewProvider(context.Background(), %v): %s", tt.issuer, err)
			}
			actual := OfflineAsScope(p)
			if tt.offlineAsScope != actual {
				t.Fatalf("OfflineAsScope(%v): wanted %v, got %v", p.Endpoint(), tt.offlineAsScope, actual)
			}
		})
	}
}

type predictableExtractor struct {
	p   *extractor.OIDCAuthenticationParams
	err error
}

func (p *predictableExtractor) Process(_ context.Context, _ *oauth2.Config, _ string) (*extractor.OIDCAuthenticationParams, error) {
	return p.p, p.err
}

func TestAuthCodeURL(t *testing.T) {
	cases := []struct {
		name string
		c    *oauth2.Config
		s    StateFn
		url  string
	}{
		{
			name: "DefaultScopes",
			c: &oauth2.Config{
				ClientID:     "testClientID",
				ClientSecret: "testClientSecret",
				Endpoint:     oauth2.Endpoint{"https://auth.example.org", "https://token.example.org"},
				Scopes:       DefaultScopes,
				RedirectURL:  "https://example.org/redirect",
			},
			s:   func(_ *http.Request) string { return "state" },
			url: "https://auth.example.org?access_type=offline&client_id=testClientID&prompt=consent&redirect_uri=http%3A%2F%2Fexample.com%2Fui&response_type=code&scope=openid+profile+email&state=state",
		},
		{
			name: "CustomScopes",
			c: &oauth2.Config{
				ClientID:     "testClientID",
				ClientSecret: "testClientSecret",
				Endpoint:     oauth2.Endpoint{"https://auth.example.org", "https://token.example.org"},
				Scopes:       []string{oidc.ScopeOpenID, oidc.ScopeOfflineAccess},
				RedirectURL:  "https://example.org/redirect",
			},
			s:   func(_ *http.Request) string { return "state" },
			url: "https://auth.example.org?client_id=testClientID&prompt=consent&redirect_uri=http%3A%2F%2Fexample.com%2Fui&response_type=code&scope=openid+offline_access&state=state",
		},
	}

	for _, tt := range cases {
		e := &predictableExtractor{}
		t.Run(tt.name, func(t *testing.T) {
			h, err := NewHandlers(tt.c, e, StateFunction(tt.s))
			if err != nil {
				t.Fatalf("NewHandlers(%v, %v): %v", tt.c, e, err)
			}

			w := httptest.NewRecorder()
			h.Login(w, httptest.NewRequest("GET", "/", nil))

			if w.Code != http.StatusSeeOther {
				t.Fatalf("w.Code:\nwant %v\ngot %v\n", http.StatusSeeOther, w.Code)
			}
			for _, u := range w.Header()["Location"] {
				if u != tt.url {
					t.Errorf("u:\nwant %v\ngot %v\n", tt.url, u)
				}
			}
		})
	}
}
