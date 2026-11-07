package main

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/SynologyOpenSource/synology-csi/pkg/driver"
	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/webapi"
)

// The metrics page serves the driver's build and the process's own numbers.
func TestTheMetricsPageServesTheBuildAndTheProcess(t *testing.T) {
	server := httptest.NewServer(metricsHandler(newRegistry()))
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	for _, want := range []string{
		`synology_csi_build_info{version="` + driverVersion() + `"} 1`,
		"process_cpu_seconds_total",
		"go_goroutines",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the metrics page has no %q", want)
		}
	}
}

// The registry holds every metric the CSI servers and the DSM client record.
// A vector shows on the page only once it has a series, so registration is
// what this checks.
func TestTheRegistryHoldsTheDriversMetrics(t *testing.T) {
	registry := newRegistry()
	var already prometheus.AlreadyRegisteredError

	for _, collector := range append(driver.Collectors(), webapi.Collectors()...) {
		if err := registry.Register(collector); !errors.As(err, &already) {
			t.Errorf("registering %T again returned %v, want AlreadyRegisteredError", collector, err)
		}
	}
}
