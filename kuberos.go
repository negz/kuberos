package kuberos

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/negz/kuberos/extractor"

	oidc "github.com/coreos/go-oidc"
	"github.com/gorilla/schema"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	// DefaultKubeCfgEndpoint is the default endpoint to which clients should
	// be redirected after authentication.
	DefaultKubeCfgEndpoint = "/ui"

	schemeHTTP  = "http"
	schemeHTTPS = "https"

	elbHeaderForwardedProto = "X-Forwarded-Proto"
	elbHeaderForwardedFor   = "X-Forwarded-For"

	urlParamState            = "state"
	urlParamCode             = "code"
	urlParamError            = "error"
	urlParamErrorDescription = "error_description"
	urlParamErrorURI         = "error_uri"

	templateUser             = "kuberos"
	templateAuthProvider     = "oidc"
	templateOIDCClientID     = "client-id"
	templateOIDCClientSecret = "client-secret"
	templateOIDCIDToken      = "id-token"
	templateOIDCIssuer       = "idp-issuer-url"
	templateOIDCRefreshToken = "refresh-token"

	templateFormParseMemory = 32 << 20 // 32MB
)

var (
	// DefaultScopes are the minimum required oauth2 scopes for every
	// authentication request.
	DefaultScopes = []string{oidc.ScopeOpenID, "profile", "email"}

	// ErrInvalidKubeCfgEndpoint indicates an unparseable redirect endpoint.
	ErrInvalidKubeCfgEndpoint = errors.New("invalid redirect endpoint")

	// ErrInvalidState indicates the provided state param was not as expected.
	ErrInvalidState = errors.New("invalid state parameter")

	// ErrMissingCode indicates a response without an OAuth 2.0 authorization
	// code
	ErrMissingCode = errors.New("response missing authorization code")

	// ErrNoYAMLSerializer indicates we're unable to serialize Kubernetes
	// objects as YAML.
	ErrNoYAMLSerializer = errors.New("no YAML serializer registered")

	decoder = schema.NewDecoder()
)

// A StateFn should take an HTTP request and return a difficult to predict yet
// deterministic state string.
type StateFn func(*http.Request) string

func defaultStateFn(secret []byte) StateFn {
	// Writing to a hash never returns an error.
	// nolint: errcheck, gas
	return func(r *http.Request) string {
		remote := r.RemoteAddr
		// Use the forwarded for header instead of the remote address if it is
		// supplied.
		for h, v := range r.Header {
			if h == elbHeaderForwardedFor {
				for _, host := range v {
					remote = host
				}
			}
		}

		h := sha256.New()
		h.Write(secret)
		h.Write([]byte(r.Host))
		h.Write([]byte(remote))
		h.Write([]byte(r.UserAgent()))
		return fmt.Sprintf("%x", h.Sum(nil))
	}
}

// OfflineAsScope determines whether an offline refresh token is requested via
// a scope per the spec or via Google's custom access_type=offline method.
//
// See http://openid.net/specs/openid-connect-core-1_0.html#OfflineAccess and
// https://developers.google.com/identity/protocols/OAuth2WebServer#offline
func OfflineAsScope(p *oidc.Provider) bool {
	var s struct {
		Scopes []string `json:"scopes_supported"`
	}
	if err := p.Claims(&s); err != nil {
		return true
	}
	if len(s.Scopes) == 0 {
		return true
	}
	for _, scope := range s.Scopes {
		if scope == oidc.ScopeOfflineAccess {
			return true
		}
	}
	return false
}

// ScopeRequests configures the oauth2 scopes to request during authentication.
type ScopeRequests struct {
	OfflineAsScope bool
	ExtraScopes    []string
}

// Get the scopes to request during authentication.
func (r *ScopeRequests) Get() []string {
	scopes := DefaultScopes
	if r.OfflineAsScope {
		scopes = append(scopes, oidc.ScopeOfflineAccess)
	}
	return append(scopes, r.ExtraScopes...)
}

// Handlers provides HTTP handlers for the Kubernary service.
type Handlers struct {
	cfg        *oauth2.Config
	e          extractor.OIDC
	oo         []oauth2.AuthCodeOption
	state      StateFn
	httpClient *http.Client
	endpoint   *url.URL
}

// An Option represents a Handlers option.
type Option func(*Handlers) error

// StateFunction allows the use of a bespoke state generator function.
func StateFunction(fn StateFn) Option {
	return func(h *Handlers) error {
		h.state = fn
		return nil
	}
}

// HTTPClient allows the use of a bespoke HTTP client for OIDC requests.
func HTTPClient(c *http.Client) Option {
	return func(h *Handlers) error {
		h.httpClient = c
		return nil
	}
}

// AuthCodeOptions allows the use of bespoke OAuth2 options.
func AuthCodeOptions(oo []oauth2.AuthCodeOption) Option {
	return func(h *Handlers) error {
		h.oo = oo
		return nil
	}
}

// NewHandlers returns a new set of Kuberos HTTP handlers.
func NewHandlers(c *oauth2.Config, e extractor.OIDC, ho ...Option) (*Handlers, error) {
	h := &Handlers{
		cfg:        c,
		e:          e,
		oo:         []oauth2.AuthCodeOption{oauth2.AccessTypeOffline},
		state:      defaultStateFn([]byte(c.ClientSecret)),
		httpClient: http.DefaultClient,
		endpoint:   &url.URL{Path: DefaultKubeCfgEndpoint},
	}

	// Assume we're using a Googley request for offline access.
	for _, s := range c.Scopes {
		// ...Unless we find an offline scope
		if s == oidc.ScopeOfflineAccess {
			h.oo = []oauth2.AuthCodeOption{}
		}
	}

	for _, o := range ho {
		if err := o(h); err != nil {
			return nil, errors.Wrap(err, "cannot apply handlers option")
		}
	}
	return h, nil
}

// Login redirects to an OIDC provider per the supplied oauth2 config.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	c := &oauth2.Config{
		ClientID:     h.cfg.ClientID,
		ClientSecret: h.cfg.ClientSecret,
		Endpoint:     h.cfg.Endpoint,
		Scopes:       h.cfg.Scopes,
		RedirectURL:  redirectURL(r, h.endpoint),
	}

	http.Redirect(w, r, c.AuthCodeURL(h.state(r), h.oo...), http.StatusSeeOther)
}

// KubeCfg returns a handler that forms helpers for kubecfg authentication.
func (h *Handlers) KubeCfg(w http.ResponseWriter, r *http.Request) {
	if r.FormValue(urlParamState) != h.state(r) {
		http.Error(w, ErrInvalidState.Error(), http.StatusForbidden)
		return
	}

	if e := r.FormValue(urlParamError); e != "" {
		msg := e
		if desc := r.FormValue(urlParamErrorDescription); desc != "" {
			msg = fmt.Sprintf("%s: %s", msg, desc)
		}
		if uri := r.FormValue(urlParamErrorURI); uri != "" {
			msg = fmt.Sprintf("%s (see %s)", msg, uri)
		}
		http.Error(w, msg, http.StatusForbidden)
		return
	}

	code := r.FormValue(urlParamCode)
	if code == "" {
		http.Error(w, ErrMissingCode.Error(), http.StatusBadRequest)
		return
	}

	c := &oauth2.Config{
		ClientID:     h.cfg.ClientID,
		ClientSecret: h.cfg.ClientSecret,
		Endpoint:     h.cfg.Endpoint,
		Scopes:       h.cfg.Scopes,
		RedirectURL:  redirectURL(r, h.endpoint),
	}

	rsp, err := h.e.Process(r.Context(), c, code)
	if err != nil {
		http.Error(w, errors.Wrap(err, "cannot process OAuth2 code").Error(), http.StatusForbidden)
		return
	}

	j, err := json.Marshal(rsp)
	if err != nil {
		http.Error(w, errors.Wrap(err, "cannot marshal JSON").Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := w.Write(j); err != nil {
		http.Error(w, errors.Wrap(err, "cannot write response").Error(), http.StatusInternalServerError)
	}
}

func redirectURL(r *http.Request, endpoint *url.URL) string {
	if r.URL.IsAbs() {
		return fmt.Sprint(r.URL.ResolveReference(endpoint))
	}
	u := &url.URL{}
	u.Scheme = schemeHTTP
	if r.TLS != nil {
		u.Scheme = schemeHTTPS
	}

	// Redirect to HTTPS if we're listening on HTTP behind an HTTPS ELB.
	for h, v := range r.Header {
		if h == elbHeaderForwardedProto {
			for _, proto := range v {
				if proto == schemeHTTPS {
					u.Scheme = schemeHTTPS
				}
			}
		}
	}
	// TODO(negz): Set port if X-Forwarded-Port exists?
	u.Host = r.Host
	return fmt.Sprint(u.ResolveReference(endpoint))
}

// Template returns an HTTP handler that returns a new kubecfg by taking a
// template with existing clusters and adding a user and context for each based
// on the URL parameters passed to it.
func Template(cfg *api.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(templateFormParseMemory) //nolint:errcheck
		p := &extractor.OIDCAuthenticationParams{}

		// TODO(negz): Return an error if any required parameter is absent.
		if err := decoder.Decode(p, r.Form); err != nil {
			http.Error(w, errors.Wrap(err, "cannot parse URL parameter").Error(), http.StatusBadRequest)
			return
		}

		c := &api.Config{}
		c.AuthInfos = make(map[string]*api.AuthInfo)
		c.Clusters = make(map[string]*api.Cluster)
		c.Contexts = make(map[string]*api.Context)
		c.AuthInfos[templateUser] = &api.AuthInfo{
			Username: p.Username,
			AuthProvider: &api.AuthProviderConfig{
				Name: templateAuthProvider,
				Config: map[string]string{
					templateOIDCClientID:     p.ClientID,
					templateOIDCClientSecret: p.ClientSecret,
					templateOIDCIDToken:      p.IDToken,
					templateOIDCRefreshToken: p.RefreshToken,
					templateOIDCIssuer:       p.IssuerURL,
				},
			},
		}
		for name, cluster := range cfg.Clusters {
			c.Clusters[name] = cluster
			c.Contexts[name] = &api.Context{Cluster: name, AuthInfo: templateUser}
		}

		y, err := clientcmd.Write(*c)
		if err != nil {
			http.Error(w, errors.Wrap(err, "cannot marshal template to YAML").Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/x-yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment")
		if _, err := w.Write(y); err != nil {
			http.Error(w, errors.Wrap(err, "cannot write response").Error(), http.StatusInternalServerError)
		}
	}
}
