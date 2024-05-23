package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	opsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "The total number of processed requests",
	})
)

func hello(w http.ResponseWriter, req *http.Request) {
	defer opsProcessed.Inc()
	fmt.Fprint(w, "Hello!")

}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", hello)

	ctx, cancelCtx := context.WithCancel(context.Background())
	mainServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		err := mainServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("server one closed\n")
		} else if err != nil {
			fmt.Printf("error listening for main server: %s\n", err)
		}
		cancelCtx()
	}()

	promux := http.NewServeMux()
	promux.Handle("/metrics", promhttp.Handler())
	promServer := &http.Server{
		Addr:    ":9096",
		Handler: promux,
	}

	go func() {
		err := promServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("server one closed\n")
		} else if err != nil {
			fmt.Printf("error listening for Prometheus server: %s\n", err)
		}
		cancelCtx()
	}()

	select {
	case <-ctx.Done():
		if err := mainServer.Shutdown(context.Background()); err != nil {
			fmt.Printf("failed to shutdown main server: %s\n", err)
		}

		if err := promServer.Shutdown(context.Background()); err != nil {
			fmt.Printf("failed to shutdown Prometheus server: %s\n", err)
		}
	}
}
