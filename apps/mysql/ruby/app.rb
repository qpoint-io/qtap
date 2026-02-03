require 'mysql2'

$stdout.sync = true

host = ENV.fetch('MYSQL_HOST', 'localhost')
port = ENV.fetch('MYSQL_PORT', 3306).to_i
user = ENV.fetch('MYSQL_USER', 'root')
password = ENV.fetch('MYSQL_PASSWORD', '')
database = ENV.fetch('MYSQL_DATABASE', 'testdb')
tls_enabled = ENV.fetch('MYSQL_TLS_ENABLED', 'false') == 'true'
ca_cert_path = ENV.fetch('MYSQL_CA_CERT', nil)
max_iterations = ENV.fetch('MAX_ITERATIONS', 0).to_i
sleep_duration = ENV.fetch('SLEEP_DURATION', 1).to_f

mysql_options = {
  host: host,
  port: port,
  username: user,
  password: password,
  database: database,
  reconnect: true
}

if tls_enabled
  puts "[Ruby] TLS enabled"
  mysql_options[:ssl_mode] = :required
  if ca_cert_path && File.exist?(ca_cert_path)
    mysql_options[:sslca] = ca_cert_path
  end
else
  puts "[Ruby] TLS disabled"
end

iteration = 0

loop do
  iteration += 1
  puts "[Ruby] Iteration #{iteration}"

  begin
    client = Mysql2::Client.new(mysql_options)

    # Check TLS status
    result = client.query("SHOW STATUS LIKE 'Ssl_cipher'")
    ssl_cipher = result.first
    if ssl_cipher && !ssl_cipher['Value'].empty?
      puts "[Ruby] Connected with TLS: #{ssl_cipher['Value']}"
    else
      puts "[Ruby] Connected without TLS"
    end

    # Create test table if not exists
    client.query(<<-SQL)
      CREATE TABLE IF NOT EXISTS ruby_test (
        id INT AUTO_INCREMENT PRIMARY KEY,
        iteration INT,
        value VARCHAR(255),
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
      )
    SQL

    # Insert data
    client.query("INSERT INTO ruby_test (iteration, value) VALUES (#{iteration}, 'ruby-value-#{iteration}')")

    # Select data
    results = client.query("SELECT * FROM ruby_test ORDER BY id DESC LIMIT 5")
    puts "[Ruby] Latest rows: #{results.count}"

    # Update data
    client.query("UPDATE ruby_test SET value = 'updated-#{iteration}' WHERE iteration = #{iteration}")

    # Delete old data (keep last 100)
    client.query("DELETE FROM ruby_test WHERE id NOT IN (SELECT id FROM (SELECT id FROM ruby_test ORDER BY id DESC LIMIT 100) t)")

    client.close
  rescue => e
    puts "[Ruby] Error: #{e.message}"
  end

  break if max_iterations > 0 && iteration >= max_iterations
  sleep(sleep_duration)
end

puts "[Ruby] Completed #{iteration} iterations"
