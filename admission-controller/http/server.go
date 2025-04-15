package http

import (
	"admissioncontroller/utils"
	"admissioncontroller/validation"
	"fmt"
	"net/http"
)

// NewServer creates and return main http.Server
func NewServer(port string) *http.Server {
	validationHook := validation.NewValidationHook()

	ah := newAdmissionHandler()
	mux := http.NewServeMux()
	mux.Handle("/healthz", healthz())
	mux.Handle("/validate", ah.Serve(validationHook)) // Main validation endpoint
	mux.Handle("/track", ah.ServeTrack())             // Tracking endpoint, no validation

	return &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: mux,
	}
}

// NewMetricsServer creates and return Prometheus metrics server
func NewMetricsServer(metricsPort string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", utils.ServeMetrics()) // Metrics endpoint

	return &http.Server{
		Addr:    fmt.Sprintf(":%s", metricsPort),
		Handler: mux,
	}
}
