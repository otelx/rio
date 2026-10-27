package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/otelx/rio/internal/operator"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	namespace := flag.String("namespace", "rio-system", "operator namespace")
	cert := flag.String("tls-cert", "/tls/tls.crt", "webhook serving certificate")
	key := flag.String("tls-key", "/tls/tls.key", "webhook serving key")
	kubeconfig := flag.String("kubeconfig", "", "explicit kubeconfig for local development")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	var cfg *rest.Config
	var err error
	if *kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	must(err)
	cfg.Timeout = 15 * time.Second
	kube, err := kubernetes.NewForConfig(cfg)
	must(err)
	dyn, err := dynamic.NewForConfig(cfg)
	must(err)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	authority, err := operator.LoadAuthority(ctx, kube, *namespace)
	must(err)
	store := &operator.Store{}
	c := &operator.Controller{Authority: authority, Kube: kube, Dynamic: dyn, Namespace: *namespace, Store: store, Capabilities: &operator.Capabilities{Kube: kube, Namespace: *namespace}}
	go c.Run(ctx)
	mux := http.NewServeMux()
	mux.Handle("/mutate", &operator.Webhook{Authority: authority, Store: store, Kube: kube, Namespace: *namespace})
	webhook := &http.Server{Addr: ":9443", Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	healthMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !store.Get().Synced {
			http.Error(w, "synchronizing", 503)
			return
		}
		w.WriteHeader(200)
	})
	health := &http.Server{Addr: ":8080", Handler: healthMux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if e := health.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			slog.Error("health server", "error", e)
			stop()
		}
	}()
	go func() {
		if e := webhook.ListenAndServeTLS(*cert, *key); e != nil && e != http.ErrServerClosed {
			slog.Error("webhook server", "error", e)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = webhook.Shutdown(shutdown)
	_ = health.Shutdown(shutdown)
}

func must(err error) {
	if err != nil {
		slog.Error("startup", "error", err)
		os.Exit(1)
	}
}
