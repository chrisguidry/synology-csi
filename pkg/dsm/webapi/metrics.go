package webapi

import (
	"context"
	"errors"
	"net"

	"github.com/prometheus/client_golang/prometheus"
)

// dsmRequests counts every request the driver sends a DSM, so the cost of a
// lookup or a creation reads straight off the counter, and failures show by
// kind.
var dsmRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "synology_csi_dsm_requests_total",
	Help: "Requests sent to a DSM, by WebAPI, method, and outcome.",
}, []string{"dsm", "api", "method", "outcome"})

// dsmReachable reports whether a DSM answered the last request sent to it.
// DSM health is a metric rather than part of Probe, because whatever calls
// Probe treats a failure as the plugin being down: a NAS that reboots would
// restart or stall every node's plugin, which cannot bring the DSM back. As
// a metric it raises an alert instead.
var dsmReachable = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "synology_csi_dsm_reachable",
	Help: "1 when the DSM answered the last request sent to it, 0 when it did not.",
}, []string{"dsm"})

// Collectors returns the metrics the DSM client records.
func Collectors() []prometheus.Collector {
	return []prometheus.Collector{dsmRequests, dsmReachable}
}

// The outcomes a request is counted under.
const (
	outcomeSuccess     = "success"
	outcomeDsmError    = "dsm_error"
	outcomeHttpError   = "http_error"
	outcomeBadResponse = "bad_response"
	outcomeTimeout     = "timeout"
	outcomeCanceled    = "canceled"
	outcomeUnreachable = "unreachable"
)

// transportOutcome names a request that got no answer from the DSM.
func transportOutcome(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return outcomeCanceled
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return outcomeTimeout
	default:
		return outcomeUnreachable
	}
}

// bodyOutcome names a response whose body could not be read or decoded. The
// client's timeout and the caller's context also cut a body short.
func bodyOutcome(err error) string {
	if outcome := transportOutcome(err); outcome != outcomeUnreachable {
		return outcome
	}
	return outcomeBadResponse
}

// recordRequest counts a request. A canceled request says nothing about the
// DSM, because its caller stopped waiting, so it leaves the gauge as it was.
func (dsm *DSM) recordRequest(api, method, outcome string) {
	dsmRequests.WithLabelValues(dsm.Ip, api, method, outcome).Inc()
	switch outcome {
	case outcomeCanceled:
	case outcomeTimeout, outcomeUnreachable:
		dsmReachable.WithLabelValues(dsm.Ip).Set(0)
	default:
		dsmReachable.WithLabelValues(dsm.Ip).Set(1)
	}
}
