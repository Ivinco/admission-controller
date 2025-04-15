package http

import (
	"admissioncontroller/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"admissioncontroller"

	"github.com/containerd/containerd/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	admissionv1 "k8s.io/api/admission/v1" // Теперь используем псевдоним admissionv1 для admission API
	v1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"

	appsv1 "k8s.io/api/apps/v1"                 // Для Deployment, StatefulSet
	coordinationv1 "k8s.io/api/coordination/v1" // Для Lease
	corev1 "k8s.io/api/core/v1"                 // Для Pod, Service
	discoveryv1 "k8s.io/api/discovery/v1"       // Для EndpointSlice
)

var observerMode bool

func init() {
	observerModeValue := getEnv("OBSERVER_MODE", "false")
	var err error
	observerMode, err = strconv.ParseBool(observerModeValue)
	if err != nil {
		observerMode = false
		utils.ErrorLog("Error parsing OBSERVER_MODE value: %s", err)
	}
	v1.AddToScheme(scheme.Scheme)
	appsv1.AddToScheme(scheme.Scheme)
	discoveryv1.AddToScheme(scheme.Scheme)
	coordinationv1.AddToScheme(scheme.Scheme)

}

// admissionHandler represents the HTTP handler for an admission webhook
type admissionHandler struct {
	decoder runtime.Decoder
}

// newAdmissionHandler returns an instance of AdmissionHandler
func newAdmissionHandler() *admissionHandler {
	sch := runtime.NewScheme()
	corev1.AddToScheme(sch)
	appsv1.AddToScheme(sch)
	discoveryv1.AddToScheme(sch)
	coordinationv1.AddToScheme(sch)
	admissionv1.AddToScheme(sch)

	// Добавляем стандартные типы ресурсов Kubernetes
	scheme.AddToScheme(sch)

	return &admissionHandler{
		decoder: serializer.NewCodecFactory(sch).UniversalDeserializer(),
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

// Serve returns a http.HandlerFunc for an admission webhook
func (h *admissionHandler) Serve(hook admissioncontroller.Hook) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, fmt.Sprintf("invalid method only POST requests are allowed current method is %v", r.Method), http.StatusMethodNotAllowed)
			return
		}

		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			http.Error(w, fmt.Sprintf("only content type 'application/json' is supported. Current content type is %v", contentType), http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not read request body: %v", err), http.StatusBadRequest)
			return
		}

		var review v1.AdmissionReview

		if _, _, err := h.decoder.Decode(body, nil, &review); err != nil {
			http.Error(w, fmt.Sprintf("could not deserialize request: %v", err), http.StatusBadRequest)
			return
		}

		if review.Request == nil {
			http.Error(w, "malformed admission review: request is nil", http.StatusBadRequest)
			return
		}

		// Get DEBUG env var
		debugMode, err := strconv.ParseBool(getEnv("DEBUG", "false"))
		if err != nil {
			utils.ErrorLog("Error parsing DEBUG value: %s", err)
			debugMode = false // в случае ошибки считаем, что DEBUG=false
		}

		// log everything if debug=true
		if debugMode {
			requestLog, err := json.Marshal(review.Request)
			if err != nil {
				utils.ErrorLog("Failed to serialize request: %v", err)
			} else {
				utils.DebugLog("DEBUG mode active: Received request: %s", string(requestLog))
			}
		}

		result, err := hook.Execute(review.Request)
		if err != nil {
			utils.ErrorLog("Internal Server Error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		responseLabel := "deny"
		if result.Allowed {
			responseLabel = "allow"
		}
		namespace := review.Request.Namespace
		resourceKind := review.Request.Kind.Kind
		username := review.Request.UserInfo.Username
		operation := review.Request.Operation

		utils.TotalRequests.WithLabelValues(
			responseLabel,
			namespace,
			resourceKind,
			username,
			string(operation),
			strconv.FormatBool(observerMode),
			utils.GetK8SId(), // Добавляем k8s_id
		).Inc()

		admissionResponse := v1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AdmissionReview",
				APIVersion: "admission.k8s.io/v1",
			},
			Response: &v1.AdmissionResponse{
				UID:     review.Request.UID,
				Allowed: result.Allowed,
				Result:  &metav1.Status{Message: result.Msg},
			},
		}

		res, err := json.Marshal(admissionResponse)
		if err != nil {
			utils.ErrorLog("could not marshal response: %v", err)
			http.Error(w, fmt.Sprintf("could not marshal response: %v", err), http.StatusInternalServerError)
			return
		}

		utils.DebugLog("Webhook [%s - %s] - Allowed: %t", r.URL.Path, review.Request.Operation, result.Allowed)
		w.WriteHeader(http.StatusOK)
		w.Write(res)
	}
}

func healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}

func (h *admissionHandler) ServeTrack() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Could not read request body: %v", err), http.StatusBadRequest)
			return
		}

		var review admissionv1.AdmissionReview
		if _, _, err := h.decoder.Decode(body, nil, &review); err != nil {
			http.Error(w, fmt.Sprintf("Could not deserialize request: %v", err), http.StatusBadRequest)
			return
		}

		if review.Request == nil {
			http.Error(w, "Malformed admission review: request is nil", http.StatusBadRequest)
			return
		}

		// Filter by kind
		allowedKinds := map[string]bool{
			"Deployment":            false,
			"DaemonSet":             true,
			"StatefulSet":           false,
			"Service":               false,
			"Endpoints":             false,
			"Ingress":               true,
			"Secret":                true,
			"Pods":                  true,
			"ConfigMap":             false,
			"PersistentVolume":      true,
			"PersistentVolumeClaim": true,
			"Role":                  true,
			"RoleBinding":           true,
			"ClusterRole":           true,
			"ClusterRoleBinding":    true,
			"Namespace":             true,
			"NetworkPolicy":         true,
			"ResourceQuota":         false,
		}

		if _, allowed := allowedKinds[review.Request.Kind.Kind]; !allowed {
			// Allow
			respondWithAllowed(w, review.Request.UID)
			return
		}

		var username string
		if usernames, ok := review.Request.UserInfo.Extra["username"]; ok && len(usernames) > 0 {
			username = usernames[0]
		}

		// Log request
		utils.Log.WithFields(log.Fields{
			"user_name":        username,
			"user_id":          review.Request.UserInfo.Username,
			"user_groups":      review.Request.UserInfo.Groups,
			"request_type":     string(review.Request.Operation),
			"request_id":       string(review.Request.UID),
			"target_namespace": review.Request.Namespace,
			"target_kind":      review.Request.Kind.Kind,
			"target_name":      review.Request.Name,
			"k8s_id":           utils.GetK8SId(),
		}).Info("Received tracked request")

		// Always allow
		respondWithAllowed(w, review.Request.UID)
	}
}

func respondWithAllowed(w http.ResponseWriter, uid types.UID) {
	response := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     uid,
			Allowed: true,
		},
	}
	res, err := json.Marshal(response)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not marshal response: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(res)
}
