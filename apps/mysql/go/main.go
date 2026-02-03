package main

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
)

func main() {
	host := getEnv("MYSQL_HOST", "localhost")
	port := getEnv("MYSQL_PORT", "3306")
	user := getEnv("MYSQL_USER", "root")
	password := getEnv("MYSQL_PASSWORD", "")
	database := getEnv("MYSQL_DATABASE", "testdb")
	tlsEnabled := getEnv("MYSQL_TLS_ENABLED", "false") == "true"
	caCertPath := getEnv("MYSQL_CA_CERT", "")
	maxIterations := getEnvInt("MAX_ITERATIONS", 0)
	sleepDuration := getEnvFloat("SLEEP_DURATION", 1.0)

	// Build DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", user, password, host, port, database)

	if tlsEnabled {
		fmt.Println("[Go] TLS enabled")
		if err := registerTLSConfig(caCertPath); err != nil {
			fmt.Printf("[Go] Failed to configure TLS: %v\n", err)
			os.Exit(1)
		}
		dsn += "&tls=custom"
	} else {
		fmt.Println("[Go] TLS disabled")
	}

	iteration := 0
	for {
		iteration++
		fmt.Printf("[Go] Iteration %d\n", iteration)

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			fmt.Printf("[Go] Connection error: %v\n", err)
			time.Sleep(time.Duration(sleepDuration * float64(time.Second)))
			continue
		}

		// Check TLS status
		var variable, value string
		err = db.QueryRow("SHOW STATUS LIKE 'Ssl_cipher'").Scan(&variable, &value)
		if err == nil && value != "" {
			fmt.Printf("[Go] Connected with TLS: %s\n", value)
		} else {
			fmt.Println("[Go] Connected without TLS")
		}

		// Create test table
		_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS go_test (
				id INT AUTO_INCREMENT PRIMARY KEY,
				iteration INT,
				value VARCHAR(255),
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			)
		`)
		if err != nil {
			fmt.Printf("[Go] Create table error: %v\n", err)
		}

		// Insert data
		_, err = db.Exec("INSERT INTO go_test (iteration, value) VALUES (?, ?)", iteration, fmt.Sprintf("go-value-%d", iteration))
		if err != nil {
			fmt.Printf("[Go] Insert error: %v\n", err)
		}

		// Select data
		rows, err := db.Query("SELECT * FROM go_test ORDER BY id DESC LIMIT 5")
		if err == nil {
			count := 0
			for rows.Next() {
				count++
			}
			rows.Close()
			fmt.Printf("[Go] Latest rows: %d\n", count)
		}

		// Update data
		db.Exec("UPDATE go_test SET value = ? WHERE iteration = ?", fmt.Sprintf("updated-%d", iteration), iteration)

		// Delete old data
		db.Exec("DELETE FROM go_test WHERE id NOT IN (SELECT id FROM (SELECT id FROM go_test ORDER BY id DESC LIMIT 100) t)")

		db.Close()

		if maxIterations > 0 && iteration >= maxIterations {
			break
		}
		time.Sleep(time.Duration(sleepDuration * float64(time.Second)))
	}

	fmt.Printf("[Go] Completed %d iterations\n", iteration)
}

func registerTLSConfig(caCertPath string) error {
	rootCertPool := x509.NewCertPool()

	if caCertPath != "" {
		pem, err := os.ReadFile(caCertPath)
		if err != nil {
			return fmt.Errorf("failed to read CA cert: %w", err)
		}
		if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
			return fmt.Errorf("failed to append CA cert")
		}
	}

	return mysql.RegisterTLSConfig("custom", &tls.Config{
		RootCAs: rootCertPool,
	})
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return defaultValue
}
