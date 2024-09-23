/*
Copyright 2024 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	opsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "The total number of processed requests",
	})
	rawScale = promauto.NewGauge(prometheus.GaugeOpts{ // want "promauto.NewGauge should be assigned to a variable"
		Name: "raw_scale",
		Help: "Exact number of pods to scale to",
	})
)

func hello(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	s := params.Get("scale")
	if s != "" {
		log.Println("Found scale header")
		parsed, _ := strconv.Atoi(s)
		defer rawScale.Set(float64(parsed))
		fmt.Fprintf(w, "Scaling to %d", parsed)
	} else {
		defer opsProcessed.Inc()
		fmt.Fprint(w, "Hello!")
	}
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

	<-ctx.Done()
	if err := mainServer.Shutdown(context.Background()); err != nil {
		fmt.Printf("failed to shutdown main server: %s\n", err)
	}

	if err := promServer.Shutdown(context.Background()); err != nil {
		fmt.Printf("failed to shutdown Prometheus server: %s\n", err)
	}
}
