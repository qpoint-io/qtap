package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	host := getEnv("PG_HOST", "localhost")
	port := getEnv("PG_PORT", "5432")
	user := getEnv("PG_USER", "postgres")
	password := getEnv("PG_PASSWORD", "")
	database := getEnv("PG_DATABASE", "testdb")
	tlsEnabled := getEnv("PG_TLS_ENABLED", "false") == "true"
	caCertPath := getEnv("PG_CA_CERT", "")
	maxIterations := getEnvInt("MAX_ITERATIONS", 0)
	sleepDuration := getEnvFloat("SLEEP_DURATION", 1.0)

	ctx := context.Background()

	// Build connection string
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s", host, port, user, password, database)

	if tlsEnabled {
		fmt.Println("[Go] TLS enabled")
		connStr += " sslmode=verify-ca"
		if caCertPath != "" {
			connStr += " sslrootcert=" + caCertPath
		}
	} else {
		fmt.Println("[Go] TLS disabled")
		connStr += " sslmode=disable"
	}

	iteration := 0
	for {
		iteration++
		fmt.Printf("\n[Go] === Iteration %d ===\n", iteration)

		connConfig, err := pgx.ParseConfig(connStr)
		if err != nil {
			fmt.Printf("[Go] Config error: %v\n", err)
			time.Sleep(time.Duration(sleepDuration * float64(time.Second)))
			continue
		}

		if tlsEnabled && caCertPath != "" {
			tlsConfig, tlsErr := buildTLSConfig(caCertPath)
			if tlsErr != nil {
				fmt.Printf("[Go] TLS config error: %v\n", tlsErr)
				os.Exit(1)
			}
			connConfig.TLSConfig = tlsConfig
		}

		conn, err := pgx.ConnectConfig(ctx, connConfig)
		if err != nil {
			fmt.Printf("[Go] Connection error: %v\n", err)
			time.Sleep(time.Duration(sleepDuration * float64(time.Second)))
			continue
		}

		// Check connection
		var one int
		err = conn.QueryRow(ctx, "SELECT 1").Scan(&one)
		if err != nil {
			fmt.Printf("[Go] Ping error: %v\n", err)
		} else {
			fmt.Println("[Go] Connected successfully")
		}

		// 1. Create table
		fmt.Println("[Go] Creating table...")
		_, err = conn.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS test_items (
				id SERIAL PRIMARY KEY,
				name TEXT,
				value FLOAT,
				created_at TIMESTAMP DEFAULT NOW()
			)
		`)
		if err != nil {
			fmt.Printf("[Go] Create table error: %v\n", err)
		}

		// 2. Insert with parameterized values
		name := fmt.Sprintf("go-item-%d", iteration)
		val := rand.Float64() * 100
		fmt.Printf("[Go] INSERT: name=%s, value=%.2f\n", name, val)
		_, err = conn.Exec(ctx, "INSERT INTO test_items (name, value) VALUES ($1, $2)", name, val)
		if err != nil {
			fmt.Printf("[Go] Insert error: %v\n", err)
		}

		// 3. Select with LIMIT
		fmt.Println("[Go] SELECT (latest 5)...")
		rows, err := conn.Query(ctx, "SELECT id, name, value, created_at FROM test_items ORDER BY id DESC LIMIT 5")
		if err == nil {
			count := 0
			for rows.Next() {
				var id int
				var n string
				var v float64
				var createdAt time.Time
				if err := rows.Scan(&id, &n, &v, &createdAt); err == nil {
					count++
					fmt.Printf("[Go]   Row: id=%d name=%s value=%.2f\n", id, n, v)
				}
			}
			rows.Close()
			fmt.Printf("[Go] Selected %d rows\n", count)
		}

		// 4. Update with WHERE
		fmt.Println("[Go] UPDATE...")
		_, err = conn.Exec(ctx, "UPDATE test_items SET value = $1 WHERE name = $2", val+1, name)
		if err != nil {
			fmt.Printf("[Go] Update error: %v\n", err)
		}

		// 5. Delete with WHERE
		fmt.Println("[Go] DELETE (old rows)...")
		_, err = conn.Exec(ctx, "DELETE FROM test_items WHERE id NOT IN (SELECT id FROM test_items ORDER BY id DESC LIMIT 100)")
		if err != nil {
			fmt.Printf("[Go] Delete error: %v\n", err)
		}

		// 6. Prepared statement (pgx uses named prepared statements natively)
		fmt.Println("[Go] Prepared statement...")
		_, err = conn.Prepare(ctx, "go_insert", "INSERT INTO test_items (name, value) VALUES ($1, $2)")
		if err == nil {
			_, err = conn.Exec(ctx, "go_insert", fmt.Sprintf("prepared-%d", iteration), val*2)
			if err != nil {
				fmt.Printf("[Go] Execute prepared error: %v\n", err)
			}
			conn.Exec(ctx, "DEALLOCATE go_insert")
		} else {
			fmt.Printf("[Go] Prepare error: %v\n", err)
		}

		// 7. Transaction
		fmt.Println("[Go] Transaction...")
		tx, err := conn.Begin(ctx)
		if err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO test_items (name, value) VALUES ($1, $2)", fmt.Sprintf("tx-%d", iteration), val*3)
			if err != nil {
				fmt.Printf("[Go] TX insert error: %v\n", err)
				tx.Rollback(ctx)
			} else {
				err = tx.Commit(ctx)
				if err != nil {
					fmt.Printf("[Go] TX commit error: %v\n", err)
				} else {
					fmt.Println("[Go] Transaction committed")
				}
			}
		}

		// 8. Intentional error
		fmt.Println("[Go] Intentional error (bad query)...")
		_, err = conn.Exec(ctx, "SELECT * FROM nonexistent_table")
		if err != nil {
			fmt.Printf("[Go] Expected error: %v\n", err)
		}

		conn.Close(ctx)

		if maxIterations > 0 && iteration >= maxIterations {
			break
		}

		// 9. Sleep
		fmt.Printf("[Go] Sleeping %.1fs...\n", sleepDuration)
		time.Sleep(time.Duration(sleepDuration * float64(time.Second)))
	}

	fmt.Printf("[Go] Completed %d iterations\n", iteration)
}

func buildTLSConfig(caCertPath string) (*tls.Config, error) {
	tlsConfig := &tls.Config{}
	rootCertPool := x509.NewCertPool()
	pem, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %w", err)
	}
	if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
		return nil, fmt.Errorf("failed to append CA cert")
	}
	tlsConfig.RootCAs = rootCertPool
	tlsConfig.ServerName = "postgres"
	return tlsConfig, nil
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
