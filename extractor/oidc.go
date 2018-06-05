package extractor

import (
	"context"
	"net/http"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/oauth2"

	oidc "github.com/coreos/go-oidc"
	"github.com/pkg/errors"
)

const tokenFieldIDToken = "id_token"

// ErrMissingIDToken indicates a response that does not contain an id_token.
var ErrMissingIDToken = errors.New("response missing ID token")

// OIDCAuthenticationParams are the parameters required for kubectl to
// authenticate to Kubernetes via OIDC.
type OIDCAuthenticationParams struct {
	Username     string `json:"email" schema:"email"` // TODO(negz): Support other claims.
	ClientID     string `json:"clientID" schema:"clientID"`
	ClientSecret string `json:"clientSecret" schema:"clientSecret"`
	IDToken      string `json:"idToken" schema:"idToken"`
	RefreshToken string `json:"refreshToken" schema:"refreshToken"`
	IssuerURL    string `json:"issuer" schema:"issuer"`
}

// An OIDC extractor performs OIDC validation, extracting and storing the
// information required for Kubernetes authentication along the way.
type OIDC interface {
	Process(ctx context.Context, cfg *oauth2.Config, code string) (*OIDCAuthenticationParams, error)
}

type oidcExtractor struct {
	log         *zap.Logger
	v           *oidc.IDTokenVerifier
	h           *http.Client
	emailDomain string
}

// An Option represents a OIDC extractor option.
type Option func(*oidcExtractor) error

// HTTPClient allows the use of a bespoke context.
func HTTPClient(h *http.Client) Option {
	return func(o *oidcExtractor) error {
		o.h = h
		return nil
	}
}

// Logger allows the use of a bespoke Zap logger.
func Logger(l *zap.Logger) Option {
	return func(o *oidcExtractor) error {
		o.log = l
		return nil
	}
}

// EmailDomain adds the given email domain to an OIDC extractor
func EmailDomain(domain string) Option {
	return func(o *oidcExtractor) error {
		o.emailDomain = domain
		return nil
	}
}

// NewOIDC creates a new OIDC extractor.
func NewOIDC(v *oidc.IDTokenVerifier, oo ...Option) (OIDC, error) {
	l, err := zap.NewProduction()
	if err != nil {
		return nil, errors.Wrap(err, "cannot create default logger")
	}

	oe := &oidcExtractor{log: l, v: v, h: http.DefaultClient}

	for _, o := range oo {
		if err := o(oe); err != nil {
			return nil, errors.Wrap(err, "cannot apply OIDC option")
		}
	}
	return oe, nil
}

func (o *oidcExtractor) Process(ctx context.Context, cfg *oauth2.Config, code string) (*OIDCAuthenticationParams, error) {
	o.log.Debug("exchange ", zap.String("code", code))
	octx := oidc.ClientContext(ctx, o.h)
	token, err := cfg.Exchange(octx, code)
	if err != nil {
		return nil, errors.Wrap(err, "cannot exchange code for token")
	}

	id, ok := token.Extra(tokenFieldIDToken).(string)
	if !ok {
		return nil, ErrMissingIDToken
	}
	o.log.Debug("token", zap.String("id", id), zap.Any("token", token))

	idt, err := o.v.Verify(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, "cannot verify ID token")
	}

	params := &OIDCAuthenticationParams{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		IDToken:      id,
		RefreshToken: token.RefreshToken,
		IssuerURL:    idt.Issuer,
	}
	if err := idt.Claims(params); err != nil {
		return nil, errors.Wrap(err, "cannot extract claims from ID token")
	}

	if o.emailDomain != "" && !strings.HasSuffix(params.Username, "@"+o.emailDomain) {
		return nil, errors.New("Invalid email domain, expecting " + o.emailDomain)
	}

	return params, nil
}
