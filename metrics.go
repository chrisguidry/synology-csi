package main

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"

	"github.com/SynologyOpenSource/synology-csi/pkg/driver"
	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/webapi"
)

// metricsDeadline bounds a scrape's headers and the listener's stop.
const metricsDeadline = 30 * time.Second

func driverVersion() string { return driver.DriverVersion }

// newRegistry holds the process's own numbers, the build, and every metric
// the CSI servers and the DSM client record.
func newRegistry() *prometheus.Registry {
	registry := prometheus.NewRegistry()
	build := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "synology_csi_build_info",
		Help: "The release the driver was built from, value 1.",
	}, []string{"version"})
	build.WithLabelValues(driverVersion()).Set(1)
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		build,
	)
	registry.MustRegister(driver.Collectors()...)
	registry.MustRegister(webapi.Collectors()...)
	return registry
}

// metricsHandler serves the registry at /metrics and nothing else.
func metricsHandler(registry *prometheus.Registry) http.Handler {
	served := http.NewServeMux()
	served.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	return served
}

// serveMetrics serves the metrics on address until ctx ends. An empty
// address serves none.
func serveMetrics(ctx context.Context, address string) error {
	if address == "" {
		return nil
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           metricsHandler(newRegistry()),
		ReadHeaderTimeout: metricsDeadline,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), metricsDeadline)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Errorf("The metrics listener stopped: %v", err)
		}
	}()
	log.Infof("Serving metrics on %s", listener.Addr())
	return nil
}
