import redis.clients.jedis.Jedis;

public class App {
    public static void main(String[] args) throws InterruptedException {
        String host = System.getenv().getOrDefault("REDIS_HOST", "localhost");
        int port = Integer.parseInt(System.getenv().getOrDefault("REDIS_PORT", "6379"));
        int maxIterations = Integer.parseInt(System.getenv().getOrDefault("MAX_ITERATIONS", "0"));
        double sleepDuration = Double.parseDouble(System.getenv().getOrDefault("SLEEP_DURATION", "1"));

        try (Jedis jedis = new Jedis(host, port)) {
            int iteration = 0;

            while (true) {
                iteration++;
                System.out.printf("[Java] Iteration %d%n", iteration);

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

                if (maxIterations > 0 && iteration >= maxIterations) {
                    break;
                }
                Thread.sleep((long) (sleepDuration * 1000));
            }

            System.out.printf("[Java] Completed %d iterations%n", iteration);
        }
    }
}
