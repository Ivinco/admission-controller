package utils

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func CheckCertExpiry(certPath string) (time.Time, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return time.Time{}, fmt.Errorf("failed to parse certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}

	return cert.NotAfter, nil
}

func UpdateCertExpiryMetric(certPath string) {
	expiryTime, err := CheckCertExpiry(certPath)
	if err != nil {
		ErrorLog("Error checking certificate expiry: %v", err)
		return
	}
	InfoLog("TLS certificate expires on: %v", expiryTime.Format("January 2, 2006 15:04:05"))

	secondsUntilExpiry := time.Until(expiryTime).Seconds()
	labels := prometheus.Labels{"k8s_id": k8sID}
	certExpiryMetric.With(labels).Set(secondsUntilExpiry)

	// If less than 30 days till expiration, flood logs every hour (from main.go)
	if time.Until(expiryTime) <= 30*24*time.Hour {
		for i := 0; i < 30; i++ {
			ErrorLog("Certificate expires in less than a month!!!")
		}
	}
}
