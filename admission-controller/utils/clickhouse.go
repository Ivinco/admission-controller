package utils

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go"

	"github.com/sirupsen/logrus"
)

type ClickHouseHook struct {
	db *sql.DB
}

func NewClickHouseHook(db *sql.DB) *ClickHouseHook {
	return &ClickHouseHook{db: db}
}

func (hook *ClickHouseHook) Fire(entry *logrus.Entry) error {
	// Создаем контекст для операций с базой данных
	ctx := context.Background()

	tx, err := hook.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
        INSERT INTO ADMISSION_TABLE (
            event_date, event_time, k8s_id, level, message, user_id, user_name, user_groups,
            request_id, request_type, target_namespace, target_kind, target_name,
            admission_result, admission_reason, processing_time, observer_mode
        ) VALUES (toDate(?), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	logTimeString, ok := entry.Data["@timestamp"].(string)
	if !ok {
		logTimeString = time.Now().Format(time.RFC3339) // Fallback if timestamp is missing
	}
	logTime, _ := time.Parse(time.RFC3339, logTimeString)
	logTime = logTime.UTC() // Convert to UTC if not already

	// Fields preparation before sending to Clickhouse. Using log.Printf instead of InfoLog, to avoid cyclical InfoLog execution
	// log.Printf("User groups in Entry Data: %v", entry.Data["user_groups"])
	var user_groups_str interface{} // Var for user_groups array

	// check if user_groups is slice of strings
	if userGroups, ok := entry.Data["user_groups"].([]string); ok {
		// log.Printf("User groups array: %v", userGroups)
		user_groups_str = clickhouse.Array(userGroups) // Prepare Clickhouse compatible array
		// log.Printf("User groups clickhouse array: %v", user_groups_str)
	} else {
		// log.Printf("User groups data is not a slice of strings")
		user_groups_str = clickhouse.Array([]string{}) // init as empty
	}

	// Preparation for  user_name, request_id & observer_mode
	// log.Printf("username in Entry Data: %v", entry.Data["user_name"]) // check before extract
	user_name := safeString(entry.Data, "user_name")
	// log.Printf("Extracted username: %s", user_name) // log after extraction

	// log.Printf("Request ID in Entry Data: %v", entry.Data["request_id"]) // check before extract
	request_id := safeString(entry.Data, "request_id")
	// log.Printf("Extracted Request ID: %s", request_id) // log after extraction

	// Get observer mode from env var
	observerModeEnv := os.Getenv("OBSERVER_MODE")
	observer_mode := false
	if observerModeEnv == "true" {
		observer_mode = true
	}
	observer_mode_str := strconv.FormatBool(observer_mode)

	// Other fields
	//request_id := entry.Data["request_id"].(string)
	user_id, _ := entry.Data["user_id"].(string)
	request_type, _ := entry.Data["request_type"].(string)
	target_namespace, _ := entry.Data["target_namespace"].(string)
	target_kind, _ := entry.Data["target_kind"].(string)
	target_name, _ := entry.Data["target_name"].(string)
	admission_result, _ := entry.Data["admission_result"].(string)
	admission_reason, _ := entry.Data["admission_reason"].(string)
	processing_time, _ := entry.Data["processing_time"].(string)
	k8s_id, _ := entry.Data["k8s_id"].(string)

	// Request generation and send
	_, err = stmt.ExecContext(ctx,
		logTime, logTime, k8s_id, entry.Level.String(), entry.Message,
		user_id, user_name, user_groups_str,
		request_id, request_type, target_namespace, target_kind, target_name,
		admission_result, admission_reason, processing_time, observer_mode_str,
	)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (hook *ClickHouseHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.InfoLevel,
		logrus.ErrorLevel,
	}
}

func init() {
	connectToClickHouse()

}

func connectToClickHouse() {
	clickhouseHost := getEnv("CLICKHOUSE_HOST", "")
	if clickhouseHost == "" {
		ErrorLog("CLICKHOUSE_HOST is not set")
		return
	}

	clickhousePort := getEnv("CLICKHOUSE_PORT", "9000")
	clickhouseUser := getEnv("CLICKHOUSE_USER", "admission-controller")
	clickhousePassword := getEnv("CLICKHOUSE_PASSWORD", "none")

	dataSourceName := fmt.Sprintf("tcp://%s:%s?username=%s&password=%s&database=default", clickhouseHost, clickhousePort, clickhouseUser, clickhousePassword)

	InfoLog("Connecting to Clickhouse")
	clickhouseConnection, err := sql.Open("clickhouse", dataSourceName)
	if err != nil {
		ErrorLog("Failed to connect to ClickHouse: %v", err)
		return
	}

	if err = clickhouseConnection.Ping(); err != nil {
		ErrorLog("Failed to ping ClickHouse: %v", err)
		clickhouseConnection = nil // Очистка в случае ошибки
		return
	}

	EnsureAdmissionTableExists(clickhouseConnection)

	Log.AddHook(NewClickHouseHook(clickhouseConnection))
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func EnsureAdmissionTableExists(db *sql.DB) {
	InfoLog("Creating ADMISSION_TABLE in clickhouse") // TODO Add mode for separate CH tables for different k8s_id's (or databases)
	createTableSQL := `
    CREATE TABLE IF NOT EXISTS ADMISSION_TABLE (
        event_date Date DEFAULT toDate(event_time),
        event_time DateTime,
		k8s_id String,
        level String,
        message String,
        user_id String,
        user_name String,
        user_groups Array(String),
        request_id String,
        request_type String,
        target_namespace String,
        target_kind String,
        target_name String,
        admission_result String,
        admission_reason String,
        processing_time String,
        observer_mode String
    ) ENGINE = MergeTree(event_date, (event_time, level), 8192)
    `

	if _, err := db.Exec(createTableSQL); err != nil {
		ErrorLog("Failed to create table ADMISSION_TABLE: %v", err)
	} else {
		InfoLog("Table ADMISSION_TABLE created or already exists")
	}

}
