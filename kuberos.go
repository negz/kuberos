package kuberos

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	oidc "github.com/coreos/go-oidc"
	"github.com/negz/kuberos/extractor"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

const (
	// DefaultKubeCfgEndpoint is the default endpoint to which clients should
	// be redirected after authentication.
	DefaultKubeCfgEndpoint = "/kubecfg"

	schemeHTTP  = "http"
	schemeHTTPS = "https"

	urlParamState            = "state"
	urlParamCode             = "code"
	urlParamError            = "error"
	urlParamErrorDescription = "error_description"
	urlParamErrorURI         = "error_uri"
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
)

// A StateFn should take an HTTP request and return a difficult to predict yet
// deterministic state string.
type StateFn func(*http.Request) string

func defaultStateFn(secret []byte) StateFn {
	// Writing to a hash never returns an error.
	// nolint: errcheck, gas
	return func(r *http.Request) string {
		h := sha256.New()
		h.Write(secret)
		h.Write([]byte(r.Host))
		h.Write([]byte(r.RemoteAddr))
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

// KubeCfgEndpoint allows the use of a bespoke endpoint for serving kubecfgs.
func KubeCfgEndpoint(e *url.URL) Option {
	return func(h *Handlers) error {
		h.endpoint = e
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

// KubeCfgEndpoint returns the endpoint at which the kubecfg handler must be
// registered.
func (h *Handlers) KubeCfgEndpoint() string {
	return fmt.Sprint(h.endpoint)
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
	u.Host = r.Host
	return fmt.Sprint(u.ResolveReference(endpoint))
}
