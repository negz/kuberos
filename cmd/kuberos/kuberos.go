package main

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/negz/kuberos"
	"github.com/negz/kuberos/extractor"
	"github.com/rakyll/statik/fs"

	_ "github.com/negz/kuberos/statik"

	oidc "github.com/coreos/go-oidc"
	"github.com/julienschmidt/httprouter"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
	"k8s.io/client-go/tools/clientcmd"
)

const indexPath = "/index.html"

func logRequests(h http.Handler, log *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Info("request",
			zap.String("host", r.Host),
			zap.String("method", r.Method),
			zap.String("url", r.URL.String()),
			zap.String("agent", r.UserAgent()),
			zap.String("addr", r.RemoteAddr))
		log.Debug("request", zap.Any("headers", r.Header))
		h.ServeHTTP(w, r)
	})
}

func main() {
	var (
		app         = kingpin.New(filepath.Base(os.Args[0]), "Provides OIDC authentication configuration for kubectl.").DefaultEnvars()
		listen      = app.Flag("listen", "Address at which to expose HTTP webhook.").Default(":10003").String()
		debug       = app.Flag("debug", "Run with debug logging.").Short('d').Bool()
		scopes      = app.Flag("scopes", "List of additional scopes to provide in token.").Default("profile", "email").Strings()
		emailDomain = app.Flag("email-domain", "The eamil domain to restrict access to.").String()

		grace            = app.Flag("shutdown-grace-period", "Wait this long for sessions to end before shutting down.").Default("1m").Duration()
		shutdownEndpoint = app.Flag("shutdown-endpoint", "Insecure HTTP endpoint path (e.g., /quitquitquit) that responds to a GET to shut down kuberos.").String()

		issuerURL        = app.Arg("oidc-issuer-url", "OpenID Connect issuer URL.").URL()
		clientID         = app.Arg("client-id", "OAuth2 client ID.").String()
		clientSecretFile = app.Arg("client-secret-file", "File containing OAuth2 client secret.").ExistingFile()
		templateFile     = app.Arg("kubecfg-template", "A kubecfg file containing clusters to populate with a user and contexts.").ExistingFile()
	)

	kingpin.MustParse(app.Parse(os.Args[1:]))

	var log *zap.Logger
	log, err := zap.NewProduction()
	if *debug {
		log, err = zap.NewDevelopment()
	}
	kingpin.FatalIfError(err, "cannot create log")

	clientSecret, err := ioutil.ReadFile(*clientSecretFile)
	kingpin.FatalIfError(err, "cannot read client secret file")

	ctx := oidc.ClientContext(context.Background(), http.DefaultClient)
	provider, err := oidc.NewProvider(ctx, (*issuerURL).String())
	kingpin.FatalIfError(err, "cannot create OIDC provider from issuer %v", *issuerURL)
	log.Debug("established OIDC provider", zap.String("url", provider.Endpoint().TokenURL))

	sr := kuberos.ScopeRequests{OfflineAsScope: kuberos.OfflineAsScope(provider), Scopes: *scopes}
	cfg := &oauth2.Config{
		ClientID:     *clientID,
		ClientSecret: strings.TrimSpace(string(clientSecret)),
		Endpoint:     provider.Endpoint(),
		Scopes:       sr.Get(),
	}
	e, err := extractor.NewOIDC(provider.Verifier(&oidc.Config{ClientID: *clientID}), extractor.Logger(log), extractor.EmailDomain(*emailDomain))
	kingpin.FatalIfError(err, "cannot setup OIDC extractor")

	h, err := kuberos.NewHandlers(cfg, e, kuberos.Logger(log))
	kingpin.FatalIfError(err, "cannot setup HTTP handlers")

	tmpl, err := clientcmd.LoadFromFile(*templateFile)
	kingpin.FatalIfError(err, "cannot load kubecfg template %s", *templateFile)

	r := httprouter.New()
	s := &http.Server{Addr: *listen, Handler: logRequests(r, log)}

	ctx, cancel := context.WithTimeout(context.Background(), *grace)
	done := make(chan struct{})
	shutdown := func() {
		log.Info("shutdown", zap.Error(s.Shutdown(ctx)))
		close(done)
	}

	go func() {
		sigterm := make(chan os.Signal, 1)
		signal.Notify(sigterm, syscall.SIGTERM)
		<-sigterm
		shutdown()
	}()

	frontend, err := fs.New()
	kingpin.FatalIfError(err, "cannot load frontend")

	index, err := frontend.Open(indexPath)
	kingpin.FatalIfError(err, "cannot open frontend index %s", indexPath)

	r.ServeFiles("/dist/*filepath", frontend)
	r.HandlerFunc("GET", "/ui", content(index, filepath.Base(indexPath)))
	r.HandlerFunc("GET", "/", h.Login)
	r.HandlerFunc("GET", "/kubecfg", h.KubeCfg)
	r.HandlerFunc("GET", "/kubecfg.yaml", kuberos.Template(tmpl))
	r.HandlerFunc("GET", "/healthz", ping())

	if *shutdownEndpoint != "" {
		r.HandlerFunc("GET", *shutdownEndpoint, run(shutdown))
	}

	log.Info("shutdown", zap.Error(s.ListenAndServe()))
	<-done
	cancel()
}

func content(c io.ReadSeeker, filename string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, filename, time.Unix(0, 0), c)
		r.Body.Close()
	}
}

func run(fn func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		go fn()
		w.WriteHeader(http.StatusOK)
		r.Body.Close()
	}
}

func ping() http.HandlerFunc {
	// TODO(negz): Check kuberos health?
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		r.Body.Close()
	}
}
