const { Client } = require('pg');
const fs = require('fs');

const host = process.env.PG_HOST || 'localhost';
const port = parseInt(process.env.PG_PORT || '5432', 10);
const user = process.env.PG_USER || 'postgres';
const password = process.env.PG_PASSWORD || '';
const database = process.env.PG_DATABASE || 'testdb';
const tlsEnabled = process.env.PG_TLS_ENABLED === 'true';
const caCertPath = process.env.PG_CA_CERT || '';
const maxIterations = parseInt(process.env.MAX_ITERATIONS || '0', 10);
const sleepDuration = parseFloat(process.env.SLEEP_DURATION || '1') * 1000;

async function main() {
  const config = {
    host,
    port,
    user,
    password,
    database,
  };

  if (tlsEnabled) {
    console.log('[Node] TLS enabled');
    config.ssl = {
      rejectUnauthorized: true,
    };
    if (caCertPath && fs.existsSync(caCertPath)) {
      config.ssl.ca = fs.readFileSync(caCertPath);
    }
  } else {
    console.log('[Node] TLS disabled');
    config.ssl = false;
  }

  let iteration = 0;

  while (true) {
    iteration++;
    console.log(`\n[Node] === Iteration ${iteration} ===`);

    const client = new Client(config);
    try {
      await client.connect();

      // Check connection
      await client.query('SELECT 1 AS ok');
      console.log('[Node] Connected successfully');

      // 1. Create table
      console.log('[Node] Creating table...');
      await client.query(`
        CREATE TABLE IF NOT EXISTS test_items (
          id SERIAL PRIMARY KEY,
          name TEXT,
          value FLOAT,
          created_at TIMESTAMP DEFAULT NOW()
        )
      `);

      // 2. Insert with parameterized values
      const name = `node-item-${iteration}`;
      const val = Math.random() * 100;
      console.log(`[Node] INSERT: name=${name}, value=${val.toFixed(2)}`);
      await client.query('INSERT INTO test_items (name, value) VALUES ($1, $2)', [name, val]);

      // 3. Select with LIMIT
      console.log('[Node] SELECT (latest 5)...');
      const selectResult = await client.query('SELECT id, name, value, created_at FROM test_items ORDER BY id DESC LIMIT 5');
      for (const row of selectResult.rows) {
        console.log(`[Node]   Row: id=${row.id} name=${row.name} value=${parseFloat(row.value).toFixed(2)}`);
      }
      console.log(`[Node] Selected ${selectResult.rows.length} rows`);

      // 4. Update with WHERE
      console.log('[Node] UPDATE...');
      await client.query('UPDATE test_items SET value = $1 WHERE name = $2', [val + 1, name]);

      // 5. Delete old rows
      console.log('[Node] DELETE (old rows)...');
      await client.query('DELETE FROM test_items WHERE id NOT IN (SELECT id FROM test_items ORDER BY id DESC LIMIT 100)');

      // 6. Prepared statement (named query)
      console.log('[Node] Prepared statement...');
      await client.query({
        name: 'node_insert',
        text: 'INSERT INTO test_items (name, value) VALUES ($1, $2)',
        values: [`prepared-${iteration}`, val * 2],
      });

      // 7. Transaction
      console.log('[Node] Transaction...');
      await client.query('BEGIN');
      await client.query('INSERT INTO test_items (name, value) VALUES ($1, $2)', [`tx-${iteration}`, val * 3]);
      await client.query('COMMIT');
      console.log('[Node] Transaction committed');

      // 8. Intentional error
      console.log('[Node] Intentional error (bad query)...');
      try {
        await client.query('SELECT * FROM nonexistent_table');
      } catch (err) {
        console.log(`[Node] Expected error: ${err.message}`);
      }

      await client.end();
    } catch (err) {
      console.log(`[Node] Error: ${err.message}`);
      await client.end().catch(() => {});
    }

    if (maxIterations > 0 && iteration >= maxIterations) {
      break;
    }

    // 9. Sleep
    console.log(`[Node] Sleeping ${sleepDuration / 1000}s...`);
    await new Promise(resolve => setTimeout(resolve, sleepDuration));
  }

  console.log(`[Node] Completed ${iteration} iterations`);
}

main().catch(console.error);
