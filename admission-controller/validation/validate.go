package validation

import (
	"admissioncontroller"

	"admissioncontroller/utils"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
)

func updateTimeMetrics(startTime time.Time, r *v1.AdmissionRequest, status string) {
	labels := prometheus.Labels{
		"operation": string(r.Operation),
		"kind":      r.Kind.Kind,
		"status":    status,
		"namespace": r.Namespace,
	}
	utils.UpdateProcessingTimeMetrics(startTime, labels, status)
}

// Parallel processing with goroutines take twice as much time on local cluster and basically the same time on remote one. Maybe will be useful later though.
//func processDeployment(deployment *appsv1.Deployment, logFields log.Fields) []string {
//	var errorMessages []string
//	var wg sync.WaitGroup
//	errorChan := make(chan string, 4) // Channel for error messages. Should be equal to the number of checks!
//
//	// Probes
//	wg.Add(1)
//	go func() {
//		defer wg.Done()
//		if !hasProbes(deployment.Spec.Template.Spec, logFields) {
//			errorChan <- fmt.Sprintf("At least one container of Deployment %s does not have required probes", deployment.ObjectMeta.Name)
//		}
//	}()
//
//	// ':latest' tag
//	wg.Add(1)
//	go func() {
//		defer wg.Done()
//		if !checkImageLatest(deployment.Spec.Template.Spec, logFields) {
//			errorChan <- fmt.Sprintf("At least one container of Deployment %s uses tag `Latest`", deployment.ObjectMeta.Name)
//		}
//	}()
//
//	// ImagePullPolicy != Always
//	wg.Add(1)
//	go func() {
//		defer wg.Done()
//		if !checkImagePullPolicy(deployment.Spec.Template.Spec, logFields) {
//			errorChan <- fmt.Sprintf("At least one container of Deployment %s uses imagePullPolicy `Always`", deployment.ObjectMeta.Name)
//		}
//	}()
//
//	// runAsUser !=0
//	wg.Add(1)
//	go func() {
//		defer wg.Done()
//		if !hasValidRunAsUser(deployment.Spec.Template.Spec, logFields) {
//			errorChan <- fmt.Sprintf("At least one container of Deployment %s has its RunAsUser set to 0. This is forbidden", deployment.ObjectMeta.Name)
//		}
//	}()
//
//	// Close error Channel after gorutine completion
//	go func() {
//		wg.Wait()
//		close(errorChan)
//	}()
//
//	// collect error messages from channel
//	for errMsg := range errorChan {
//		errorMessages = append(errorMessages, errMsg)
//	}
//
//	return errorMessages
//}

func validateCreate() admissioncontroller.AdmitFunc {
	return func(r *v1.AdmissionRequest) (*admissioncontroller.Result, error) {
		var username string
		if usernames, ok := r.UserInfo.Extra["username"]; ok && len(usernames) > 0 {
			username = usernames[0]
		}
		startTime := time.Now()
		logFields := log.Fields{
			"k8s_id":           utils.GetK8SId(),
			"user_id":          r.UserInfo.Username,
			"user_name":        username,
			"user_groups":      r.UserInfo.Groups,
			"request_id":       string(r.UID),
			"request_type":     "create",
			"target_namespace": r.Namespace,
			"target_kind":      r.Kind.Kind,
			"target_name":      r.Name,
		}

		utils.Log.WithFields(log.Fields{
			"k8s_id":           utils.GetK8SId(),
			"user_id":          r.UserInfo.Username,
			"user_name":        username,
			"user_groups":      r.UserInfo.Groups,
			"request_id":       string(r.UID),
			"request_type":     "create",
			"target_namespace": r.Namespace,
			"target_kind":      r.Kind.Kind,
			"target_name":      r.Name,
			"admission_result": "processing", // Start position
			"admission_reason": "processing start",
			"processing_time":  "", // Placeholder value
			"observer_mode":    observerMode,
		}).Debug("GOT CREATE REQUEST")

		receivedObject, err := parseObject(r.Object.Raw)
		if err != nil {
			utils.ErrorLog("Error parsing object: %s", err)
			return &admissioncontroller.Result{Msg: err.Error(), Allowed: false}, err
		}

		unstructuredObj, ok := receivedObject.(*unstructured.Unstructured)
		if !ok {
			utils.ErrorLog("Error asserting object as unstructured: %v", receivedObject)
			return &admissioncontroller.Result{Msg: "Internal error: object type assertion failed", Allowed: false}, nil
		}

		kind := unstructuredObj.GetKind()
		errorMessages := []string{}

		switch kind {
		case "Deployment":
			deployment := &appsv1.Deployment{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, deployment)
			if err != nil {
				utils.InfoLog("Error converting unstructured to deployment: %s", err)
				return &admissioncontroller.Result{Msg: "Failed to convert object to deployment", Allowed: false}, nil
			}
			utils.DebugLog("Processing a Deployment named %s", deployment.ObjectMeta.Name)
			if !hasProbes(deployment.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("At least one container of Deployment %s does not have required probes", deployment.ObjectMeta.Name))
			}
			if !checkImageLatest(deployment.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("At least one container of Deployment %s uses tag `Latest`", deployment.ObjectMeta.Name))
			}

			if !checkImagePullPolicy(deployment.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("At least one container of Deployment %s uses imagePullPolicy `Always`", deployment.ObjectMeta.Name))
			}

		case "StatefulSet":
			statefulSet := &appsv1.StatefulSet{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, statefulSet)
			if err != nil {
				utils.ErrorLog("Error converting unstructured to statefulSet: %s", err)
				return &admissioncontroller.Result{Msg: "Failed to convert object to statefulSet", Allowed: false}, nil
			}
			utils.DebugLog("Processing a StatefulSet named %s", statefulSet.ObjectMeta.Name)
			if !hasProbes(statefulSet.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("At least one container of StatefulSet %s does not have required probes", statefulSet.ObjectMeta.Name))
			}
			if !checkImageLatest(statefulSet.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("At least one container of StatefulSet %s uses tag `Latest`", statefulSet.ObjectMeta.Name))
			}

			if !checkImagePullPolicy(statefulSet.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("At least one container of StatefulSet %s uses imagePullPolicy `Always`", statefulSet.ObjectMeta.Name))
			}
			if !hasValidRunAsUser(statefulSet.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("At least one container of StatefulSet %s has its RunAsUser set to 0. This is forbidden", statefulSet.ObjectMeta.Name))
			}

		case "Service":
			service := &corev1.Service{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, service)
			if err != nil {
				utils.ErrorLog("Error converting unstructured to service: %s", err)
				return &admissioncontroller.Result{Msg: "Failed to convert object to service", Allowed: false}, nil
			}
			allowed, err := checkServiceType(service, logFields) // example for function which returns bool and err. For a standardized way of adding new ones
			if err != nil {
				return nil, err
			}
			if !allowed {
				errorMessages = append(errorMessages, fmt.Sprintf("Service %s is of a type NodePort, which is restricted", service.ObjectMeta.Name))
			}

			// Example
			// allowed, err = checkServiceAnnotations(service)
			// if err != nil {
			// 	return nil, err
			// }
			// if !allowed {
			// 	errorMessages = append(errorMessages, fmt.Sprintf("Service %s does not meet the required annotations", service.ObjectMeta.Name))
			// }

		default:
			utils.ErrorLog("Unhandled or unknown resource type: %s", kind)
			return &admissioncontroller.Result{Msg: "Unhandled resource type", Allowed: false}, nil
		}
		elapsedTime := time.Since(startTime)

		if len(errorMessages) > 0 {
			formattedMessages := "- " + strings.Join(errorMessages, ";\n- ")

			updateTimeMetrics(startTime, r, "denied")

			utils.Log.WithFields(log.Fields{
				"k8s_id":           utils.GetK8SId(),
				"user_id":          r.UserInfo.Username,
				"user_name":        username,
				"user_groups":      r.UserInfo.Groups,
				"request_id":       string(r.UID),
				"request_type":     "create",
				"target_namespace": r.Namespace,
				"target_kind":      r.Kind.Kind,
				"target_name":      r.Name,
				"admission_result": "denied",
				"admission_reason": strings.Join(errorMessages, "; "),
				"processing_time":  elapsedTime.String(),
				"observer_mode":    observerMode,
			}).Error("Admission denied")
			return &admissioncontroller.Result{
				Msg:     "\n" + formattedMessages,
				Allowed: false,
			}, nil
		}
		updateTimeMetrics(startTime, r, "allowed")
		utils.Log.WithFields(log.Fields{
			"k8s_id":           utils.GetK8SId(),
			"user_id":          r.UserInfo.Username,
			"user_name":        username,
			"user_groups":      r.UserInfo.Groups,
			"request_id":       string(r.UID),
			"request_type":     "create",
			"target_namespace": r.Namespace,
			"target_kind":      r.Kind.Kind,
			"target_name":      r.Name,
			"admission_result": "allowed",
			"admission_reason": "all checks passed or observer mode is on",
			"processing_time":  elapsedTime.String(),
			"observer_mode":    observerMode,
		}).Info("Admission allowed")

		return &admissioncontroller.Result{Allowed: true}, nil
	}
}

func validateUpdate() admissioncontroller.AdmitFunc {
	return func(r *v1.AdmissionRequest) (*admissioncontroller.Result, error) {
		var username string
		if usernames, ok := r.UserInfo.Extra["username"]; ok && len(usernames) > 0 {
			username = usernames[0]
		}
		startTime := time.Now()

		logFields := log.Fields{
			"k8s_id":           utils.GetK8SId(),
			"user_id":          r.UserInfo.Username,
			"user_name":        username,
			"user_groups":      r.UserInfo.Groups,
			"request_id":       string(r.UID),
			"request_type":     "create",
			"target_namespace": r.Namespace,
			"target_kind":      r.Kind.Kind,
			"target_name":      r.Name,
		}

		utils.Log.WithFields(log.Fields{
			"k8s_id":           utils.GetK8SId(),
			"user_id":          r.UserInfo.Username,
			"user_name":        username,
			"user_groups":      r.UserInfo.Groups,
			"request_id":       string(r.UID),
			"request_type":     "update",
			"target_namespace": r.Namespace,
			"target_kind":      r.Kind.Kind,
			"target_name":      r.Name,
			"admission_result": "processing",
			"admission_reason": "processing start",
			"processing_time":  "", // TODO: Placeholder value. Can be better?
			"observer_mode":    observerMode,
		}).Debug("GOT UPDATE REQUEST")

		receivedObject, err := parseObject(r.Object.Raw)
		if err != nil {
			utils.ErrorLog("Error parsing object: %s", err)
			return &admissioncontroller.Result{Msg: err.Error(), Allowed: false}, err
		}

		unstructuredObj, ok := receivedObject.(*unstructured.Unstructured)
		if !ok {
			utils.ErrorLog("Error asserting object as unstructured: %v", receivedObject)
			return &admissioncontroller.Result{Msg: "Internal error: object type assertion failed", Allowed: false}, nil
		}

		kind := unstructuredObj.GetKind()
		errorMessages := []string{}

		switch kind {
		case "Deployment":
			deployment := &appsv1.Deployment{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, deployment)
			if err != nil {
				utils.InfoLog("Error converting unstructured to deployment: %s", err)
				return &admissioncontroller.Result{Msg: "Failed to convert object to deployment", Allowed: false}, nil
			}
			utils.DebugLog("Processing a Deployment named %s", deployment.ObjectMeta.Name)
			if !hasProbes(deployment.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("Deployment %s does not have required probes", deployment.ObjectMeta.Name))
			}
			if !checkImageLatest(deployment.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("Deployment %s uses tag Latest", deployment.ObjectMeta.Name))
			}

			if !checkImagePullPolicy(deployment.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("At least one container of Deployment %s uses imagePullPolicy `Always`", deployment.ObjectMeta.Name))
			}

		case "StatefulSet":
			statefulSet := &appsv1.StatefulSet{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, statefulSet)
			if err != nil {
				utils.ErrorLog("Error converting unstructured to statefulSet: %s", err)
				return &admissioncontroller.Result{Msg: "Failed to convert object to statefulSet", Allowed: false}, nil
			}
			utils.DebugLog("Processing a StatefulSet named %s", statefulSet.ObjectMeta.Name)
			if !hasProbes(statefulSet.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("StatefulSet %s does not have required probes", statefulSet.ObjectMeta.Name))
			}
			if !checkImageLatest(statefulSet.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("StatefulSet %s uses tag Latest", statefulSet.ObjectMeta.Name))
			}

			if !checkImagePullPolicy(statefulSet.Spec.Template.Spec, logFields) {
				errorMessages = append(errorMessages, fmt.Sprintf("At least one container of StatefulSet %s uses imagePullPolicy `Always`", statefulSet.ObjectMeta.Name))
			}

		case "Service":
			service := &corev1.Service{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, service)
			if err != nil {
				utils.ErrorLog("Error converting unstructured to service: %s", err)
				return &admissioncontroller.Result{Msg: "Failed to convert object to service", Allowed: false}, nil
			}
			allowed, err := checkServiceType(service, logFields)
			if err != nil {
				return nil, err
			}
			if !allowed {
				errorMessages = append(errorMessages, fmt.Sprintf("Service %s is of a type NodePort, which is restricted", service.ObjectMeta.Name))
			} // TODO Maybe remake all the checks to also provide err (?)

		default:
			utils.ErrorLog("Unhandled or unknown resource type: %s", kind)
			return &admissioncontroller.Result{Msg: "Unhandled resource type", Allowed: false}, nil
		}
		elapsedTime := time.Since(startTime)

		if len(errorMessages) > 0 {
			formattedMessages := "- " + strings.Join(errorMessages, ";\n- ")
			updateTimeMetrics(startTime, r, "denied")

			utils.Log.WithFields(log.Fields{
				"k8s_id":           utils.GetK8SId(),
				"user_id":          r.UserInfo.Username,
				"user_name":        username,
				"user_groups":      r.UserInfo.Groups,
				"request_id":       string(r.UID),
				"request_type":     "update",
				"target_namespace": r.Namespace,
				"target_kind":      r.Kind.Kind,
				"target_name":      r.Name,
				"admission_result": "denied",
				"admission_reason": strings.Join(errorMessages, "; "),
				"processing_time":  elapsedTime.String(),
				"observer_mode":    observerMode,
			}).Error("Admission denied")

			return &admissioncontroller.Result{
				Msg:     "\n" + formattedMessages,
				Allowed: false,
			}, nil
		}
		updateTimeMetrics(startTime, r, "allowed")

		utils.Log.WithFields(log.Fields{
			"k8s_id":           utils.GetK8SId(),
			"user_id":          r.UserInfo.Username,
			"user_name":        username,
			"user_groups":      r.UserInfo.Groups,
			"request_id":       string(r.UID),
			"request_type":     "update",
			"target_namespace": r.Namespace,
			"target_kind":      r.Kind.Kind,
			"target_name":      r.Name,
			"admission_result": "allowed",
			"admission_reason": "all checks passed or observer mode is on",
			"processing_time":  elapsedTime.String(),
			"observer_mode":    observerMode,
		}).Info("Admission allowed")

		return &admissioncontroller.Result{Allowed: true}, nil
	}
}
