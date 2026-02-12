require 'pg'

$stdout.sync = true

host = ENV.fetch('PG_HOST', 'localhost')
port = ENV.fetch('PG_PORT', 5432).to_i
user = ENV.fetch('PG_USER', 'postgres')
password = ENV.fetch('PG_PASSWORD', '')
database = ENV.fetch('PG_DATABASE', 'testdb')
tls_enabled = ENV.fetch('PG_TLS_ENABLED', 'false') == 'true'
ca_cert_path = ENV.fetch('PG_CA_CERT', nil)
max_iterations = ENV.fetch('MAX_ITERATIONS', 0).to_i
sleep_duration = ENV.fetch('SLEEP_DURATION', 1).to_f

pg_options = {
  host: host,
  port: port,
  user: user,
  password: password,
  dbname: database
}

if tls_enabled
  puts "[Ruby] TLS enabled"
  pg_options[:sslmode] = 'verify-ca'
  if ca_cert_path && File.exist?(ca_cert_path)
    pg_options[:sslrootcert] = ca_cert_path
  end
else
  puts "[Ruby] TLS disabled"
  pg_options[:sslmode] = 'disable'
end

iteration = 0

loop do
  iteration += 1
  puts "\n[Ruby] === Iteration #{iteration} ==="

  begin
    conn = PG.connect(pg_options)

    # Check connection
    result = conn.exec("SELECT 1 AS ok")
    puts "[Ruby] Connected successfully"

    # 1. Create table
    puts "[Ruby] Creating table..."
    conn.exec(<<-SQL)
      CREATE TABLE IF NOT EXISTS test_items (
        id SERIAL PRIMARY KEY,
        name TEXT,
        value FLOAT,
        created_at TIMESTAMP DEFAULT NOW()
      )
    SQL

    # 2. Insert with parameterized values
    name = "ruby-item-#{iteration}"
    val = rand * 100
    puts "[Ruby] INSERT: name=#{name}, value=#{'%.2f' % val}"
    conn.exec_params("INSERT INTO test_items (name, value) VALUES ($1, $2)", [name, val])

    # 3. Select with LIMIT
    puts "[Ruby] SELECT (latest 5)..."
    results = conn.exec("SELECT id, name, value, created_at FROM test_items ORDER BY id DESC LIMIT 5")
    results.each do |row|
      puts "[Ruby]   Row: id=#{row['id']} name=#{row['name']} value=#{row['value']}"
    end
    puts "[Ruby] Selected #{results.ntuples} rows"

    # 4. Update with WHERE
    puts "[Ruby] UPDATE..."
    conn.exec_params("UPDATE test_items SET value = $1 WHERE name = $2", [val + 1, name])

    # 5. Delete old rows
    puts "[Ruby] DELETE (old rows)..."
    conn.exec("DELETE FROM test_items WHERE id NOT IN (SELECT id FROM test_items ORDER BY id DESC LIMIT 100)")

    # 6. Prepared statement
    puts "[Ruby] Prepared statement..."
    conn.prepare('ruby_insert', 'INSERT INTO test_items (name, value) VALUES ($1, $2)')
    conn.exec_prepared('ruby_insert', ["prepared-#{iteration}", val * 2])
    conn.exec("DEALLOCATE ruby_insert")

    # 7. Transaction
    puts "[Ruby] Transaction..."
    conn.transaction do |c|
      c.exec_params("INSERT INTO test_items (name, value) VALUES ($1, $2)", ["tx-#{iteration}", val * 3])
    end
    puts "[Ruby] Transaction committed"

    # 8. Intentional error
    puts "[Ruby] Intentional error (bad query)..."
    begin
      conn.exec("SELECT * FROM nonexistent_table")
    rescue PG::Error => e
      puts "[Ruby] Expected error: #{e.message.strip}"
    end

    conn.close
  rescue => e
    puts "[Ruby] Error: #{e.message}"
  end

  break if max_iterations > 0 && iteration >= max_iterations

  # 9. Sleep
  puts "[Ruby] Sleeping #{sleep_duration}s..."
  sleep(sleep_duration)
end

puts "[Ruby] Completed #{iteration} iterations"
