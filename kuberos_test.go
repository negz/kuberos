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

var offlineAsScopeTests = []struct {
	issuer         string
	offlineAsScope bool
}{
	// TODO(negz): Call a mock provider instead of Google. oidc.NewProvider()
	// is the only way to instantiate a provider, and makes a real HTTP call.
	{"https://accounts.google.com", false},
}

func TestOfflineAsScope(t *testing.T) {
	for _, tt := range offlineAsScopeTests {
		p, err := oidc.NewProvider(context.Background(), tt.issuer)
		if err != nil {
			t.Errorf("oidc.NewProvider(context.Background(), %v): %s", tt.issuer, err)
			continue
		}
		actual := OfflineAsScope(p)
		if tt.offlineAsScope != actual {
			t.Errorf("OfflineAsScope(%v): wanted %v, got %v", p.Endpoint(), tt.offlineAsScope, actual)
			continue
		}
	}
}

type predictableExtractor struct {
	p   *extractor.OIDCAuthenticationParams
	err error
}

func (p *predictableExtractor) Process(_ context.Context, _ *oauth2.Config, _ string) (*extractor.OIDCAuthenticationParams, error) {
	return p.p, p.err
}

var authCodeURLTests = []struct {
	c   *oauth2.Config
	s   StateFn
	url string
}{
	{
		c: &oauth2.Config{
			ClientID:     "testClientID",
			ClientSecret: "testClientSecret",
			Endpoint:     oauth2.Endpoint{"https://auth.example.org", "https://token.example.org"},
			Scopes:       DefaultScopes,
			RedirectURL:  "https://example.org/redirect",
		},
		s:   func(_ *http.Request) string { return "state" },
		url: "https://auth.example.org?access_type=offline&client_id=testClientID&redirect_uri=http%3A%2F%2Fexample.com%2Fui&response_type=code&scope=openid+profile+email&state=state",
	},
	{
		c: &oauth2.Config{
			ClientID:     "testClientID",
			ClientSecret: "testClientSecret",
			Endpoint:     oauth2.Endpoint{"https://auth.example.org", "https://token.example.org"},
			Scopes:       []string{oidc.ScopeOpenID, oidc.ScopeOfflineAccess},
			RedirectURL:  "https://example.org/redirect",
		},
		s:   func(_ *http.Request) string { return "state" },
		url: "https://auth.example.org?client_id=testClientID&redirect_uri=http%3A%2F%2Fexample.com%2Fui&response_type=code&scope=openid+offline_access&state=state",
	},
}

func TestAuthCodeURL(t *testing.T) {
	for _, tt := range authCodeURLTests {
		e := &predictableExtractor{}
		h, err := NewHandlers(tt.c, e, StateFunction(tt.s))
		if err != nil {
			t.Errorf("NewHandlers(%v, %v): %v", tt.c, e, err)
			continue
		}

		w := httptest.NewRecorder()
		h.Login(w, httptest.NewRequest("GET", "/", nil))

		if w.Code != http.StatusSeeOther {
			t.Errorf("w.Code: want %v, got %v", http.StatusSeeOther, w.Code)
			continue
		}
		for _, u := range w.Header()["Location"] {
			if u != tt.url {
				t.Errorf("u: want %v, got %v", tt.url, u)
			}
		}
	}
}
