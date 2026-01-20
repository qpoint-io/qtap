require 'redis'

$stdout.sync = true

host = ENV.fetch('REDIS_HOST', 'localhost')
port = ENV.fetch('REDIS_PORT', 6379).to_i
max_iterations = ENV.fetch('MAX_ITERATIONS', 0).to_i
sleep_duration = ENV.fetch('SLEEP_DURATION', 1).to_f

redis = Redis.new(host: host, port: port)
iteration = 0

loop do
  iteration += 1
  puts "[Ruby] Iteration #{iteration}"

  # Basic operations
  redis.set("ruby:key:#{iteration}", "value-#{iteration}")
  redis.get("ruby:key:#{iteration}")

  # List operations
  redis.lpush("ruby:list", "item-#{iteration}")
  redis.lrange("ruby:list", 0, -1)

  # Hash operations
  redis.hset("ruby:hash", "field-#{iteration}", "value-#{iteration}")
  redis.hgetall("ruby:hash")

  # Pub/sub (publish only)
  redis.publish("ruby:channel", "message-#{iteration}")

  break if max_iterations > 0 && iteration >= max_iterations
  sleep(sleep_duration)
end

puts "[Ruby] Completed #{iteration} iterations"
