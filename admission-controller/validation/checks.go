package validation

import (
	"admissioncontroller/utils"
	"encoding/json"
	"os"
	"regexp"
	"strconv"

	"admissioncontroller"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	observerMode bool
	k8sID        string
)

func init() {
	k8sID = utils.GetK8SId() //TODO Doesn't work, look below
	observerModeValue := getEnv("OBSERVER_MODE", "false")
	var err error
	observerMode, err = strconv.ParseBool(observerModeValue)
	if err != nil {
		observerMode = false
		utils.ErrorLog("Error parsing OBSERVER_MODE value: %s", err)
	}
	if observerMode {
		utils.InfoLog("Observer mode is activated. All requests will be allowed without enforcement.") // This record does not contain k8s_id. Not a huge problem since
	} // Not a huge problem since all the requests have observer_mode: true|false indicator
}

func getEnv(key, defaultValue string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	return value
}

// NewValidationHook creates a new instance of deployment validation hook
func NewValidationHook() admissioncontroller.Hook {
	return admissioncontroller.Hook{
		Create: validateCreate(),
		Update: validateUpdate(),
	}
}

func parseObject(object []byte) (runtime.Object, error) {
	obj := &unstructured.Unstructured{}
	err := json.Unmarshal(object, obj)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func hasProbes(spec corev1.PodSpec, logFields log.Fields) bool {
	for _, container := range spec.Containers {
		if container.ReadinessProbe == nil && container.LivenessProbe == nil && container.StartupProbe == nil {
			utils.Log.WithFields(logFields).Errorf("Container %s doesn't have probes set", container.Name)
			if !observerMode {
				return false
			}
		}
	}
	return true
}

func checkImageLatest(spec corev1.PodSpec, logFields log.Fields) bool {
	pattern := regexp.MustCompile(`:latest$`)
	for _, container := range spec.Containers {
		if pattern.MatchString(container.Image) {
			utils.Log.WithFields(logFields).Errorf("Container %s is created with the following image: %s. `latest` tags are restricted. Please use specific image versions", container.Name, container.Image)
			if !observerMode {
				return false
			}
		}
	}
	return true
}

func checkImagePullPolicy(spec corev1.PodSpec, logFields log.Fields) bool {
	restrictedImagePolicies := regexp.MustCompile(`Always`)
	for _, container := range spec.Containers {
		if restrictedImagePolicies.MatchString(string(container.ImagePullPolicy)) {
			utils.Log.WithFields(logFields).Errorf("Container %s uses forbidden imagePullPolicy `%s`", container.Name, container.ImagePullPolicy)
			if !observerMode {
				return false
			}
		}
	}
	return true
}

func hasValidRunAsUser(spec corev1.PodSpec, logFields log.Fields) bool {
	// Check runAsUser on pod level
	if spec.SecurityContext != nil && spec.SecurityContext.RunAsUser != nil && *spec.SecurityContext.RunAsUser == 0 {
		utils.Log.WithFields(logFields).Error("Pod securityContext has runAsUser set to 0")
		if !observerMode {
			return false
		}
	}

	// Check runAsUser on container level
	for _, container := range spec.Containers {
		if container.SecurityContext != nil && container.SecurityContext.RunAsUser != nil {
			if *container.SecurityContext.RunAsUser == 0 {
				utils.Log.WithFields(logFields).Errorf("Container %s has runAsUser set to 0", container.Name)
				if !observerMode {
					return false
				}
			}
		} else if spec.SecurityContext != nil && spec.SecurityContext.RunAsUser != nil && *spec.SecurityContext.RunAsUser == 0 {
			// Check on container level if pod level is set
			utils.Log.WithFields(logFields).Errorf("Container %s inherits pod's runAsUser set to 0", container.Name)
			if !observerMode {
				return false
			}
		}
	}
	return true
}

func checkServiceType(service *corev1.Service, logFields log.Fields) (bool, error) {
	if service.Spec.Type == corev1.ServiceTypeNodePort {
		utils.Log.WithFields(logFields).Errorf("Service %s is of type NodePort, which is restricted", service.Name) //TODO change loglevel to warning. complication - need warnings to be sent to clickhouse. Done in logging.go
		if !observerMode {
			return false, nil
		}
	}
	return true, nil
}

// func checkServiceAnnotations(service *corev1.Service) (bool, error) {
// 	// Например, проверяем наличие определенной аннотации
// 	if value, ok := service.Annotations["example.io/required-annotation"]; !ok || value != "true" {
// 		return false, fmt.Errorf("required annotation is missing or incorrect")
// 	}
// 	return true, nil
// }
