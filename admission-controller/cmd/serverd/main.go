package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/ClickHouse/clickhouse-go"

	"admissioncontroller/http"
	"admissioncontroller/utils"

	log "k8s.io/klog/v2"
)

var (
	tlscert, tlskey, port, metricsPort, k8sID string
)

func main() {

	tlscert = getEnv("TLS_CERT_PATH", "/etc/certs/tls.crt")
	tlskey = getEnv("TLS_KEY_PATH", "/etc/certs/tls.key")
	port = getEnv("SERVER_PORT", "8443")
	metricsPort = getEnv("METRICS_PORT", "9090")
	k8sID = getEnv("K8S_ID", "default-cluster")

	flag.StringVar(&tlscert, "tlscert", tlscert, "Path to the TLS certificate")
	flag.StringVar(&tlskey, "tlskey", tlskey, "Path to the TLS key")
	flag.StringVar(&port, "port", port, "The port for validation endpoint")
	flag.StringVar(&metricsPort, "metrics-port", metricsPort, "The port for Prometheus metrics")
	flag.StringVar(&k8sID, "k8s-id", k8sID, "K8S Cluster ID")
	flag.Parse()

	utils.SetK8SId(k8sID) // Global K8S_ID

	utils.UpdateCertExpiryMetric(tlscert)

	// Запускаем таймер для регулярного обновления метрики
	ticker := time.NewTicker(24 * time.Hour) // Обновляем раз в день
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			utils.DebugLog("Scheduled check for TLS certificate expiry")
			utils.UpdateCertExpiryMetric(tlscert)
		}
	}()

	// Validation server start
	server := http.NewServer(port)
	go func() {
		utils.InfoLog("Starting HTTPS server on port: %s", port)
		if err := server.ListenAndServeTLS(tlscert, tlskey); err != nil {
			utils.ErrorLog("Failed to listen and serve HTTPS: %v", err)
		}
	}()

	// Metrics server start
	metricsServer := http.NewMetricsServer(metricsPort)
	go func() {
		utils.InfoLog("Starting metrics server on port: %s", metricsPort)
		if err := metricsServer.ListenAndServe(); err != nil {
			utils.ErrorLog("Failed to listen and serve metrics: %v", err)
		}
	}()

	// Sys call / Signals processing
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-signalChan
	log.Errorf("Received %s signal; shutting down...", sig)

	if err := server.Shutdown(context.Background()); err != nil {
		log.Error(err)
	}
	if err := metricsServer.Shutdown(context.Background()); err != nil {
		log.Error(err)
	}
}

// getEnv gets an environment variable by name and if it doesn't exist, returns a default value
func getEnv(name string, defaultValue string) string {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue
	}
	return value
}
