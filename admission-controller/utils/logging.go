package utils

import (
	"os"

	"github.com/sirupsen/logrus"
)

var (
	Log   = logrus.New() // Новый экземпляр logrus
	k8sID string
)

func init() {
	// Formatting JSON with custom fields
	Log.Formatter = &logrus.JSONFormatter{
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "@timestamp",
			logrus.FieldKeyLevel: "@level", // loglevel
			logrus.FieldKeyMsg:   "@message",
		},
	}

	// Debug loglevel
	if os.Getenv("DEBUG") == "true" {
		Log.Level = logrus.DebugLevel
	} else {
		Log.Level = logrus.InfoLevel
	}
	Log.AddHook(&K8sIDHook{})
}

// K8sIDHook добавляет k8s_id ко всем записям лога
type K8sIDHook struct{}

func (hook *K8sIDHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (hook *K8sIDHook) Fire(entry *logrus.Entry) error {
	entry.Data["k8s_id"] = k8sID
	return nil
}
func DebugLog(format string, args ...interface{}) {
	Log.Debugf(format, args...)
}

func InfoLog(format string, args ...interface{}) {
	Log.Infof(format, args...)
}

func ErrorLog(format string, args ...interface{}) {
	Log.Errorf(format, args...)
}

// Функция для безопасного получения строкового значения из logrus.Entry
func safeString(data map[string]interface{}, key string) string {
	if val, exists := data[key]; exists && val != nil {
		// fmt.Printf("Type of value for %s: %T\n", key, val) // log data type
		switch v := val.(type) {
		case string:
			return v
		default:
			// fmt.Printf("Failed to assert type for key %s with value %v of type %T\n", key, val, val)
		}
	}
	return ""
}
