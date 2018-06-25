package kuberos

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/negz/kuberos/extractor"

	oidc "github.com/coreos/go-oidc"
	"github.com/gorilla/schema"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	// DefaultKubeCfgEndpoint is the default endpoint to which clients should
	// be redirected after authentication.
	DefaultKubeCfgEndpoint = "ui"

	// DefaultAPITokenMountPath is the default mount path for API tokens
	DefaultAPITokenMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"

	schemeHTTP  = "http"
	schemeHTTPS = "https"

	headerForwardedProto  = "X-Forwarded-Proto"
	headerForwardedFor    = "X-Forwarded-For"
	headerForwardedPrefix = "X-Forwarded-Prefix"

	urlParamState            = "state"
	urlParamCode             = "code"
	urlParamError            = "error"
	urlParamErrorDescription = "error_description"
	urlParamErrorURI         = "error_uri"

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
	DefaultScopes = []string{oidc.ScopeOpenID}

	// ErrInvalidKubeCfgEndpoint indicates an unparseable redirect endpoint.
	ErrInvalidKubeCfgEndpoint = errors.New("invalid redirect endpoint")

	// ErrInvalidState indicates the provided state param was not as expected.
	ErrInvalidState = errors.New("invalid state parameter: user agent or IP address changed between requests")

	// ErrMissingCode indicates a response without an OAuth 2.0 authorization
	// code
	ErrMissingCode = errors.New("response missing authorization code")

	// ErrNoYAMLSerializer indicates we're unable to serialize Kubernetes
	// objects as YAML.
	ErrNoYAMLSerializer = errors.New("no YAML serializer registered")

	decoder = schema.NewDecoder()

	appFs = afero.NewOsFs()

	approvalConsent = oauth2.SetAuthURLParam("prompt", "consent")
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
	Scopes         []string
}

// Get the scopes to request during authentication.
func (r *ScopeRequests) Get() []string {
	scopes := DefaultScopes
	if r.OfflineAsScope {
		scopes = append(scopes, oidc.ScopeOfflineAccess)
	}
	return append(scopes, r.Scopes...)
}

// Handlers provides HTTP handlers for the Kubernary service.
type Handlers struct {
	log        *zap.Logger
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

// Logger allows the use of a bespoke Zap logger.
func Logger(l *zap.Logger) Option {
	return func(h *Handlers) error {
		h.log = l
		return nil
	}
}

// NewHandlers returns a new set of Kuberos HTTP handlers.
func NewHandlers(c *oauth2.Config, e extractor.OIDC, ho ...Option) (*Handlers, error) {
	l, err := zap.NewProduction()
	if err != nil {
		return nil, errors.Wrap(err, "cannot create default logger")
	}

	h := &Handlers{
		log:        l,
		cfg:        c,
		e:          e,
		oo:         []oauth2.AuthCodeOption{oauth2.AccessTypeOffline, approvalConsent},
		state:      defaultStateFn([]byte(c.ClientSecret)),
		httpClient: http.DefaultClient,
		endpoint:   &url.URL{Path: DefaultKubeCfgEndpoint},
	}

	// Assume we're using a Googley request for offline access.
	for _, s := range c.Scopes {
		// ...Unless we find an offline scope
		if s == oidc.ScopeOfflineAccess {
			h.oo = []oauth2.AuthCodeOption{approvalConsent}
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

	u := c.AuthCodeURL(h.state(r), h.oo...)
	h.log.Debug("redirect", zap.String("url", u))
	http.Redirect(w, r, u, http.StatusSeeOther)
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

	for h, v := range r.Header {
		switch h {
		case headerForwardedProto:
			// Redirect to HTTPS if we're listening on HTTP behind an HTTPS ELB.
			for _, proto := range v {
				if proto == schemeHTTPS {
					u.Scheme = schemeHTTPS
				}
			}
		case headerForwardedPrefix:
			// Redirect includes X-Forwarded-Prefix if exists
			for _, prefix := range v {
				u.Path = prefix
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

		y, err := clientcmd.Write(populateUser(cfg, p))
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

func populateUser(cfg *api.Config, p *extractor.OIDCAuthenticationParams) api.Config {
	c := api.Config{}
	c.AuthInfos = make(map[string]*api.AuthInfo)
	c.Clusters = make(map[string]*api.Cluster)
	c.Contexts = make(map[string]*api.Context)
	c.CurrentContext = cfg.CurrentContext
	c.AuthInfos[p.Username] = &api.AuthInfo{
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
		// If the cluster definition does not come with certificate-authority-data nor
		// certificate-authority, then check if kuberos has access to the cluster's CA
		// certificate and include it when possible. Assume all errors are non-fatal.
		if len(cluster.CertificateAuthorityData) == 0 && cluster.CertificateAuthority == "" {
			caPath := filepath.Join(DefaultAPITokenMountPath, v1.ServiceAccountRootCAKey)
			if caFile, err := appFs.Open(caPath); err == nil {
				if caCert, err := ioutil.ReadAll(caFile); err == nil {
					cluster.CertificateAuthorityData = caCert
				}
			} else {
				fmt.Printf("Error: %+v\n", err)
			}
		}
		c.Clusters[name] = cluster
		c.Contexts[name] = &api.Context{
			Cluster:  name,
			AuthInfo: p.Username,
		}
	}
	return c
}
