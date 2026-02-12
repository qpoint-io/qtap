import java.sql.*;

public class App {
    public static void main(String[] args) throws Exception {
        String host = getEnv("PG_HOST", "localhost");
        String port = getEnv("PG_PORT", "5432");
        String user = getEnv("PG_USER", "postgres");
        String password = getEnv("PG_PASSWORD", "");
        String database = getEnv("PG_DATABASE", "testdb");
        boolean tlsEnabled = "true".equals(getEnv("PG_TLS_ENABLED", "false"));
        String caCertPath = getEnv("PG_CA_CERT", "");
        int maxIterations = Integer.parseInt(getEnv("MAX_ITERATIONS", "0"));
        double sleepDuration = Double.parseDouble(getEnv("SLEEP_DURATION", "1"));

        String url = String.format("jdbc:postgresql://%s:%s/%s", host, port, database);

        if (tlsEnabled) {
            System.out.println("[Java] TLS enabled");
            url += "?ssl=true&sslmode=verify-ca";
            if (!caCertPath.isEmpty()) {
                url += "&sslrootcert=" + caCertPath;
            }
        } else {
            System.out.println("[Java] TLS disabled");
            url += "?sslmode=disable";
        }

        int iteration = 0;
        while (true) {
            iteration++;
            System.out.printf("%n[Java] === Iteration %d ===%n", iteration);

            try (Connection conn = DriverManager.getConnection(url, user, password)) {
                // Check connection
                try (Statement stmt = conn.createStatement();
                     ResultSet rs = stmt.executeQuery("SELECT 1 AS ok")) {
                    if (rs.next()) {
                        System.out.println("[Java] Connected successfully");
                    }
                }

                // 1. Create table
                System.out.println("[Java] Creating table...");
                try (Statement stmt = conn.createStatement()) {
                    stmt.execute("""
                        CREATE TABLE IF NOT EXISTS test_items (
                            id SERIAL PRIMARY KEY,
                            name TEXT,
                            value FLOAT,
                            created_at TIMESTAMP DEFAULT NOW()
                        )
                    """);
                }

                // 2. Insert with parameterized values
                String name = "java-item-" + iteration;
                double val = Math.random() * 100;
                System.out.printf("[Java] INSERT: name=%s, value=%.2f%n", name, val);
                try (PreparedStatement pstmt = conn.prepareStatement(
                        "INSERT INTO test_items (name, value) VALUES (?, ?)")) {
                    pstmt.setString(1, name);
                    pstmt.setDouble(2, val);
                    pstmt.executeUpdate();
                }

                // 3. Select with LIMIT
                System.out.println("[Java] SELECT (latest 5)...");
                try (Statement stmt = conn.createStatement();
                     ResultSet rs = stmt.executeQuery("SELECT id, name, value, created_at FROM test_items ORDER BY id DESC LIMIT 5")) {
                    int count = 0;
                    while (rs.next()) {
                        count++;
                        System.out.printf("[Java]   Row: id=%d name=%s value=%.2f%n",
                            rs.getInt("id"), rs.getString("name"), rs.getDouble("value"));
                    }
                    System.out.printf("[Java] Selected %d rows%n", count);
                }

                // 4. Update with WHERE
                System.out.println("[Java] UPDATE...");
                try (PreparedStatement pstmt = conn.prepareStatement(
                        "UPDATE test_items SET value = ? WHERE name = ?")) {
                    pstmt.setDouble(1, val + 1);
                    pstmt.setString(2, name);
                    pstmt.executeUpdate();
                }

                // 5. Delete old rows
                System.out.println("[Java] DELETE (old rows)...");
                try (Statement stmt = conn.createStatement()) {
                    stmt.execute("DELETE FROM test_items WHERE id NOT IN (SELECT id FROM test_items ORDER BY id DESC LIMIT 100)");
                }

                // 6. Prepared statement (explicit)
                System.out.println("[Java] Prepared statement...");
                try (PreparedStatement pstmt = conn.prepareStatement(
                        "INSERT INTO test_items (name, value) VALUES (?, ?)")) {
                    pstmt.setString(1, "prepared-" + iteration);
                    pstmt.setDouble(2, val * 2);
                    pstmt.executeUpdate();
                }

                // 7. Transaction
                System.out.println("[Java] Transaction...");
                conn.setAutoCommit(false);
                try (PreparedStatement pstmt = conn.prepareStatement(
                        "INSERT INTO test_items (name, value) VALUES (?, ?)")) {
                    pstmt.setString(1, "tx-" + iteration);
                    pstmt.setDouble(2, val * 3);
                    pstmt.executeUpdate();
                }
                conn.commit();
                conn.setAutoCommit(true);
                System.out.println("[Java] Transaction committed");

                // 8. Intentional error
                System.out.println("[Java] Intentional error (bad query)...");
                try (Statement stmt = conn.createStatement()) {
                    stmt.executeQuery("SELECT * FROM nonexistent_table");
                } catch (SQLException e) {
                    System.out.printf("[Java] Expected error: %s%n", e.getMessage());
                }

            } catch (SQLException e) {
                System.out.printf("[Java] Error: %s%n", e.getMessage());
            }

            if (maxIterations > 0 && iteration >= maxIterations) {
                break;
            }

            // 9. Sleep
            System.out.printf("[Java] Sleeping %.1fs...%n", sleepDuration);
            Thread.sleep((long) (sleepDuration * 1000));
        }

        System.out.printf("[Java] Completed %d iterations%n", iteration);
    }

    private static String getEnv(String key, String defaultValue) {
        String value = System.getenv(key);
        return value != null && !value.isEmpty() ? value : defaultValue;
    }
}
