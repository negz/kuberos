package main

import (
	"context"
	"crypto/tls"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/negz/kuberos"
	"github.com/negz/kuberos/extractor"

	oidc "github.com/coreos/go-oidc"
	"github.com/facebookgo/httpdown"
	"github.com/julienschmidt/httprouter"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
	"k8s.io/client-go/tools/clientcmd"
)

func logReq(fn http.HandlerFunc, log *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Info("request",
			zap.String("host", r.Host),
			zap.String("method", r.Method),
			zap.String("url", r.URL.String()),
			zap.String("agent", r.UserAgent()),
			zap.String("addr", r.RemoteAddr))
		fn(w, r)
	}
}

func main() {
	var (
		app    = kingpin.New(filepath.Base(os.Args[0]), "Provides OIDC authentication configuration for kubectl.").DefaultEnvars()
		ui     = app.Flag("ui", "Directory from which to serve Javascript UI.").Default("/kuberos/frontend").ExistingDir()
		listen = app.Flag("listen", "Address at which to expose HTTP webhook.").Default(":10003").String()
		debug  = app.Flag("debug", "Run with debug logging.").Short('d').Bool()
		stop   = app.Flag("close-after", "Wait this long at shutdown before closing HTTP connections.").Default("1m").Duration()
		kill   = app.Flag("kill-after", "Wait this long at shutdown before exiting.").Default("2m").Duration()
		tlsCrt = app.Flag("tls-cert", "TLS certificate file.").ExistingFile()
		tlsKey = app.Flag("tls-key", "TLS private key file.").ExistingFile()

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

	scopes := kuberos.ScopeRequests{OfflineAsScope: kuberos.OfflineAsScope(provider)}
	cfg := &oauth2.Config{
		ClientID:     *clientID,
		ClientSecret: strings.TrimSpace(string(clientSecret)),
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes.Get(),
	}
	e, err := extractor.NewOIDC(provider.Verifier(&oidc.Config{ClientID: *clientID}), extractor.Logger(log))
	kingpin.FatalIfError(err, "cannot setup OIDC extractor")

	h, err := kuberos.NewHandlers(cfg, e, kuberos.Logger(log))
	kingpin.FatalIfError(err, "cannot setup HTTP handlers")

	lr := clientcmd.ClientConfigLoadingRules{ExplicitPath: *templateFile}
	tmpl, err := lr.Load()
	kingpin.FatalIfError(err, "cannot load kubecfg template %s", *templateFile)

	r := httprouter.New()
	// TODO(negz): Log static asset requests.
	r.ServeFiles("/ui/*filepath", http.Dir(*ui))
	r.HandlerFunc("GET", "/", logReq(h.Login, log))
	r.HandlerFunc("GET", "/kubecfg", logReq(h.KubeCfg, log))
	r.HandlerFunc("GET", "/kubecfg.yaml", logReq(kuberos.Template(tmpl), log))
	r.HandlerFunc("GET", "/quitquitquit", logReq(func(_ http.ResponseWriter, _ *http.Request) { os.Exit(0) }, log))

	hd := &httpdown.HTTP{StopTimeout: *stop, KillTimeout: *kill}
	http := &http.Server{Addr: *listen, Handler: r}
	if *tlsCrt != "" && *tlsKey != "" {
		crt, err := tls.LoadX509KeyPair(*tlsCrt, *tlsKey)
		kingpin.FatalIfError(err, "cannot parse TLS certificate and private key")
		http.TLSConfig = &tls.Config{Certificates: []tls.Certificate{crt}}
	}

	kingpin.FatalIfError(httpdown.ListenAndServe(http, hd), "HTTP server error")
}
