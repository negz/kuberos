package kuberos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	oidc "github.com/coreos/go-oidc"
	"github.com/go-test/deep"
	"golang.org/x/oauth2"

	"github.com/negz/kuberos/extractor"

	"k8s.io/client-go/tools/clientcmd/api"
)

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
func TestPopulateUser(t *testing.T) {
	cases := []struct {
		name   string
		cfg    *api.Config
		params *extractor.OIDCAuthenticationParams
		want   api.Config
	}{
		{
			name: "MultiCluster",
			cfg: &api.Config{
				Clusters: map[string]*api.Cluster{
					"a": &api.Cluster{Server: "https://example.org", CertificateAuthorityData: []byte("PAM")},
					"b": &api.Cluster{Server: "https://example.net", CertificateAuthorityData: []byte("PAM")},
				},
			},
			params: &extractor.OIDCAuthenticationParams{
				Username:     "example@example.org",
				ClientID:     "id",
				ClientSecret: "secret",
				IDToken:      "token",
				RefreshToken: "refresh",
				IssuerURL:    "https://example.org",
			},
			want: api.Config{
				Clusters: map[string]*api.Cluster{
					"a": &api.Cluster{Server: "https://example.org", CertificateAuthorityData: []byte("PAM")},
					"b": &api.Cluster{Server: "https://example.net", CertificateAuthorityData: []byte("PAM")},
				},
				Contexts: map[string]*api.Context{
					"a": &api.Context{AuthInfo: "example@example.org", Cluster: "a"},
					"b": &api.Context{AuthInfo: "example@example.org", Cluster: "b"},
				},
				AuthInfos: map[string]*api.AuthInfo{
					"example@example.org": &api.AuthInfo{
						AuthProvider: &api.AuthProviderConfig{
							Name: templateAuthProvider,
							Config: map[string]string{
								templateOIDCClientID:     "id",
								templateOIDCClientSecret: "secret",
								templateOIDCIDToken:      "token",
								templateOIDCRefreshToken: "refresh",
								templateOIDCIssuer:       "https://example.org",
							},
						},
					},
				},
			},
		},
		{
			name: "MultiClusterWithContext",
			cfg: &api.Config{
				Clusters: map[string]*api.Cluster{
					"a": &api.Cluster{Server: "https://example.org", CertificateAuthorityData: []byte("PAM")},
					"b": &api.Cluster{Server: "https://example.net", CertificateAuthorityData: []byte("PAM")},
				},
				CurrentContext: "a",
			},
			params: &extractor.OIDCAuthenticationParams{
				Username:     "example@example.org",
				ClientID:     "id",
				ClientSecret: "secret",
				IDToken:      "token",
				RefreshToken: "refresh",
				IssuerURL:    "https://example.org",
			},
			want: api.Config{
				Clusters: map[string]*api.Cluster{
					"a": &api.Cluster{Server: "https://example.org", CertificateAuthorityData: []byte("PAM")},
					"b": &api.Cluster{Server: "https://example.net", CertificateAuthorityData: []byte("PAM")},
				},
				Contexts: map[string]*api.Context{
					"a": &api.Context{AuthInfo: "example@example.org", Cluster: "a"},
					"b": &api.Context{AuthInfo: "example@example.org", Cluster: "b"},
				},
				AuthInfos: map[string]*api.AuthInfo{
					"example@example.org": &api.AuthInfo{
						AuthProvider: &api.AuthProviderConfig{
							Name: templateAuthProvider,
							Config: map[string]string{
								templateOIDCClientID:     "id",
								templateOIDCClientSecret: "secret",
								templateOIDCIDToken:      "token",
								templateOIDCRefreshToken: "refresh",
								templateOIDCIssuer:       "https://example.org",
							},
						},
					},
				},
				CurrentContext: "a",
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := populateUser(tt.cfg, tt.params)
			if diff := deep.Equal(got, tt.want); diff != nil {
				t.Errorf("populateUser(...): got != want: %v", diff)
			}
		})
	}
}
