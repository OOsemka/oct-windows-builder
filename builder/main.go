package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	httpPort := flag.String("http-port", "8080", "HTTP port for local development")
	httpsPort := flag.String("https-port", "8443", "HTTPS port for TLS serving")
	tlsCert := flag.String("tls-cert", "/var/run/secrets/tls/tls.crt", "Path to TLS certificate")
	tlsKey := flag.String("tls-key", "/var/run/secrets/tls/tls.key", "Path to TLS key")
	modifyISO := flag.String("modify-iso", "", "Patch a mounted Windows ISO for unattended EFI (El Torito noprompt) and exit")
	flag.Parse()

	if strings.TrimSpace(*modifyISO) != "" {
		if err := patchISONoprompt(*modifyISO); err != nil {
			log.Fatalf("modify-iso: %v", err)
		}
		return
	}

	workNS := envOr("POD_NAMESPACE", "oct-windows-builder")
	goldenNS := envOr("GOLDEN_IMAGE_NAMESPACE", "openshift-virtualization-os-images")
	templateNS := envOr("TEMPLATE_NAMESPACE", "openshift")

	k8s, k8sErr := NewK8sClient()
	if k8sErr != nil {
		logf("K8s client unavailable: %v — start/list will fail until in-cluster", k8sErr)
	}
	mgr := NewManager(k8s, workNS, goldenNS, templateNS)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", mgr.handleHealthz)
	mux.HandleFunc("/api/v1/builds", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			mgr.handleListBuilds(w, r)
		case http.MethodPost:
			mgr.handleStartBuild(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/builds/", mgr.handleGetBuild)
	mux.HandleFunc("/api/v1/virtio-win", mgr.handleVirtioWin)

	handler := withCORS(mux)

	go func() {
		addr := ":" + *httpPort
		logf("HTTP on %s (workNS=%s goldenNS=%s)", addr, workNS, goldenNS)
		srv := &http.Server{
			Addr:         addr,
			Handler:      handler,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		}
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	if _, err := os.Stat(*tlsCert); err == nil {
		addr := ":" + *httpsPort
		logf("HTTPS on %s", addr)
		srv := &http.Server{
			Addr:    addr,
			Handler: handler,
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		}
		if err := srv.ListenAndServeTLS(*tlsCert, *tlsKey); err != nil {
			log.Fatalf("HTTPS server error: %v", err)
		}
	} else {
		logf("TLS cert not found, HTTP only")
		select {}
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
