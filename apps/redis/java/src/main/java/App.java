import redis.clients.jedis.Jedis;
import redis.clients.jedis.DefaultJedisClientConfig;
import redis.clients.jedis.HostAndPort;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.TrustManagerFactory;
import java.io.FileInputStream;
import java.security.KeyStore;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;

public class App {
    public static void main(String[] args) throws Exception {
        String host = System.getenv().getOrDefault("REDIS_HOST", "localhost");
        int port = Integer.parseInt(System.getenv().getOrDefault("REDIS_PORT", "6379"));
        boolean tlsEnabled = "true".equals(System.getenv().getOrDefault("REDIS_TLS_ENABLED", "false"));
        String caCertPath = System.getenv().getOrDefault("REDIS_CA_CERT", "");
        int maxIterations = Integer.parseInt(System.getenv().getOrDefault("MAX_ITERATIONS", "0"));
        double sleepDuration = Double.parseDouble(System.getenv().getOrDefault("SLEEP_DURATION", "1"));

        SSLSocketFactory sslSocketFactory = null;
        if (tlsEnabled) {
            System.out.println("[Java] TLS enabled");
            sslSocketFactory = createSSLSocketFactory(caCertPath);
        }

        int iteration = 0;

        while (true) {
            iteration++;
            System.out.printf("[Java] Iteration %d%n", iteration);

            Jedis jedis;
            if (tlsEnabled) {
                DefaultJedisClientConfig config = DefaultJedisClientConfig.builder()
                    .ssl(true)
                    .sslSocketFactory(sslSocketFactory)
                    .build();
                jedis = new Jedis(new HostAndPort(host, port), config);
            } else {
                jedis = new Jedis(host, port);
            }

            try {
                // Basic operations
                String key = "java:key:" + iteration;
                jedis.set(key, "value-" + iteration);
                jedis.get(key);

                // List operations
                jedis.lpush("java:list", "item-" + iteration);
                jedis.lrange("java:list", 0, -1);

                // Hash operations
                jedis.hset("java:hash", "field-" + iteration, "value-" + iteration);
                jedis.hgetAll("java:hash");

                // Pub/sub (publish only)
                jedis.publish("java:channel", "message-" + iteration);
            } finally {
                jedis.close();
            }

            if (maxIterations > 0 && iteration >= maxIterations) {
                break;
            }
            Thread.sleep((long) (sleepDuration * 1000));
        }

        System.out.printf("[Java] Completed %d iterations%n", iteration);
    }

    private static SSLSocketFactory createSSLSocketFactory(String caCertPath) throws Exception {
        CertificateFactory cf = CertificateFactory.getInstance("X.509");
        X509Certificate caCert;
        try (FileInputStream fis = new FileInputStream(caCertPath)) {
            caCert = (X509Certificate) cf.generateCertificate(fis);
        }

        KeyStore trustStore = KeyStore.getInstance(KeyStore.getDefaultType());
        trustStore.load(null, null);
        trustStore.setCertificateEntry("ca", caCert);

        TrustManagerFactory tmf = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
        tmf.init(trustStore);

        SSLContext sslContext = SSLContext.getInstance("TLS");
        sslContext.init(null, tmf.getTrustManagers(), null);

        return sslContext.getSocketFactory();
    }
}
