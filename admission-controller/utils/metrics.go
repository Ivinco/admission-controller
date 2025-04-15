package utils

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	customRegistry = prometheus.NewRegistry() // Metrics registry

	TotalRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "total_requests",
			Help: "Total number of processed admission requests",
		},
		[]string{"status", "namespace", "kind", "username", "operation", "observer_mode", "k8s_id"},
	)

	AvgProcessingTime = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "avg_processing_time_seconds",
			Help:       "Average time in seconds per admission request",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"operation", "kind", "status", "namespace", "k8s_id"},
	)

	MaxProcessingTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "max_processing_time_seconds",
			Help: "Maximum time in seconds for admission requests",
		},
		[]string{"operation", "kind", "status", "namespace", "k8s_id"},
	)

	certExpiryMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tls_cert_expiry_seconds",
			Help: "Number of seconds until the TLS certificate expires",
		},
		[]string{"k8s_id"},
	)

	maxTimeValues = make(map[string]float64)
	maxTimeLock   sync.Mutex
)

func init() {
	// Creation of a wrapped registrar for adding a prefix to all the metrics
	prefixedRegistry := prometheus.WrapRegistererWithPrefix("admission_controller_", customRegistry)

	// Adding default go collectors via wrapped registrar
	prefixedRegistry.MustRegister(collectors.NewGoCollector())
	prefixedRegistry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// Adding custom metrics via wrapped registrar
	prefixedRegistry.MustRegister(TotalRequests)
	prefixedRegistry.MustRegister(AvgProcessingTime)
	prefixedRegistry.MustRegister(MaxProcessingTime)
	prefixedRegistry.MustRegister(certExpiryMetric)
}

// SetK8SId sets the global K8S_ID value
func SetK8SId(id string) {
	k8sID = id
}

// GetK8SId returns the global K8S_ID value
func GetK8SId() string {
	return k8sID
}

// ServeMetrics creates HTTP handler for Prometheus metrics.
func ServeMetrics() http.Handler {
	return promhttp.HandlerFor(customRegistry, promhttp.HandlerOpts{})
}

// UpdateProcessingTimeMetrics updates metrics of avg & max processing time
func UpdateProcessingTimeMetrics(startTime time.Time, labels prometheus.Labels, status string) {
	labels["k8s_id"] = k8sID
	duration := time.Since(startTime).Seconds()
	AvgProcessingTime.With(labels).Observe(duration)

	labelString := labelsToString(labels)

	maxTimeLock.Lock()
	defer maxTimeLock.Unlock()

	currentMax, exists := maxTimeValues[labelString]
	if !exists || duration > currentMax {
		maxTimeValues[labelString] = duration
		MaxProcessingTime.With(labels).Set(duration)
	}
}

// labelsToString converts labels to a string to use as a map key
func labelsToString(labels prometheus.Labels) string {
	return fmt.Sprintf("%s_%s_%s_%s_%s", labels["operation"], labels["kind"], labels["status"], labels["namespace"], labels["k8s_id"])
}
