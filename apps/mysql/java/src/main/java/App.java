import java.sql.*;
import java.util.Properties;

public class App {
    public static void main(String[] args) throws Exception {
        String host = getEnv("MYSQL_HOST", "localhost");
        String port = getEnv("MYSQL_PORT", "3306");
        String user = getEnv("MYSQL_USER", "root");
        String password = getEnv("MYSQL_PASSWORD", "");
        String database = getEnv("MYSQL_DATABASE", "testdb");
        boolean tlsEnabled = "true".equals(getEnv("MYSQL_TLS_ENABLED", "false"));
        String caCertPath = getEnv("MYSQL_CA_CERT", "");
        int maxIterations = Integer.parseInt(getEnv("MAX_ITERATIONS", "0"));
        double sleepDuration = Double.parseDouble(getEnv("SLEEP_DURATION", "1"));

        String url = String.format("jdbc:mysql://%s:%s/%s", host, port, database);
        
        Properties props = new Properties();
        props.setProperty("user", user);
        props.setProperty("password", password);
        
        if (tlsEnabled) {
            System.out.println("[Java] TLS enabled");
            props.setProperty("useSSL", "true");
            props.setProperty("requireSSL", "true");
            props.setProperty("verifyServerCertificate", "false");
            if (!caCertPath.isEmpty()) {
                // For production, you'd configure truststore properly
                props.setProperty("trustCertificateKeyStoreUrl", "file:" + caCertPath);
            }
        } else {
            System.out.println("[Java] TLS disabled");
            props.setProperty("useSSL", "false");
        }

        int iteration = 0;
        while (true) {
            iteration++;
            System.out.printf("[Java] Iteration %d%n", iteration);

            try (Connection conn = DriverManager.getConnection(url, props)) {
                // Check TLS status
                try (Statement stmt = conn.createStatement();
                     ResultSet rs = stmt.executeQuery("SHOW STATUS LIKE 'Ssl_cipher'")) {
                    if (rs.next()) {
                        String cipher = rs.getString("Value");
                        if (cipher != null && !cipher.isEmpty()) {
                            System.out.printf("[Java] Connected with TLS: %s%n", cipher);
                        } else {
                            System.out.println("[Java] Connected without TLS");
                        }
                    }
                }

                // Create test table
                try (Statement stmt = conn.createStatement()) {
                    stmt.execute("""
                        CREATE TABLE IF NOT EXISTS java_test (
                            id INT AUTO_INCREMENT PRIMARY KEY,
                            iteration INT,
                            value VARCHAR(255),
                            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
                        )
                    """);
                }

                // Insert data
                try (PreparedStatement pstmt = conn.prepareStatement(
                        "INSERT INTO java_test (iteration, value) VALUES (?, ?)")) {
                    pstmt.setInt(1, iteration);
                    pstmt.setString(2, "java-value-" + iteration);
                    pstmt.executeUpdate();
                }

                // Select data
                try (Statement stmt = conn.createStatement();
                     ResultSet rs = stmt.executeQuery("SELECT * FROM java_test ORDER BY id DESC LIMIT 5")) {
                    int count = 0;
                    while (rs.next()) count++;
                    System.out.printf("[Java] Latest rows: %d%n", count);
                }

                // Update data
                try (PreparedStatement pstmt = conn.prepareStatement(
                        "UPDATE java_test SET value = ? WHERE iteration = ?")) {
                    pstmt.setString(1, "updated-" + iteration);
                    pstmt.setInt(2, iteration);
                    pstmt.executeUpdate();
                }

                // Delete old data
                try (Statement stmt = conn.createStatement()) {
                    stmt.execute("DELETE FROM java_test WHERE id NOT IN (SELECT id FROM (SELECT id FROM java_test ORDER BY id DESC LIMIT 100) t)");
                }

            } catch (SQLException e) {
                System.out.printf("[Java] Error: %s%n", e.getMessage());
            }

            if (maxIterations > 0 && iteration >= maxIterations) {
                break;
            }
            Thread.sleep((long) (sleepDuration * 1000));
        }

        System.out.printf("[Java] Completed %d iterations%n", iteration);
    }

    private static String getEnv(String key, String defaultValue) {
        String value = System.getenv(key);
        return value != null && !value.isEmpty() ? value : defaultValue;
    }
}
