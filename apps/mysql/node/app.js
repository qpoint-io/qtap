const mysql = require('mysql2/promise');
const fs = require('fs');

const host = process.env.MYSQL_HOST || 'localhost';
const port = parseInt(process.env.MYSQL_PORT || '3306', 10);
const user = process.env.MYSQL_USER || 'root';
const password = process.env.MYSQL_PASSWORD || '';
const database = process.env.MYSQL_DATABASE || 'testdb';
const tlsEnabled = process.env.MYSQL_TLS_ENABLED === 'true';
const caCertPath = process.env.MYSQL_CA_CERT || '';
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
    config.ssl = {};
    if (caCertPath && fs.existsSync(caCertPath)) {
      config.ssl.ca = fs.readFileSync(caCertPath);
    }
  } else {
    console.log('[Node] TLS disabled');
  }

  let iteration = 0;

  while (true) {
    iteration++;
    console.log(`[Node] Iteration ${iteration}`);

    let connection;
    try {
      connection = await mysql.createConnection(config);

      // Check TLS status
      const [rows] = await connection.execute("SHOW STATUS LIKE 'Ssl_cipher'");
      if (rows.length > 0 && rows[0].Value) {
        console.log(`[Node] Connected with TLS: ${rows[0].Value}`);
      } else {
        console.log('[Node] Connected without TLS');
      }

      // Create test table
      await connection.execute(`
        CREATE TABLE IF NOT EXISTS node_test (
          id INT AUTO_INCREMENT PRIMARY KEY,
          iteration INT,
          value VARCHAR(255),
          created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
      `);

      // Insert data
      await connection.execute(
        'INSERT INTO node_test (iteration, value) VALUES (?, ?)',
        [iteration, `node-value-${iteration}`]
      );

      // Select data
      const [selectRows] = await connection.execute(
        'SELECT * FROM node_test ORDER BY id DESC LIMIT 5'
      );
      console.log(`[Node] Latest rows: ${selectRows.length}`);

      // Update data
      await connection.execute(
        'UPDATE node_test SET value = ? WHERE iteration = ?',
        [`updated-${iteration}`, iteration]
      );

      // Delete old data
      await connection.execute(
        'DELETE FROM node_test WHERE id NOT IN (SELECT id FROM (SELECT id FROM node_test ORDER BY id DESC LIMIT 100) t)'
      );

      await connection.end();
    } catch (err) {
      console.log(`[Node] Error: ${err.message}`);
      if (connection) await connection.end().catch(() => {});
    }

    if (maxIterations > 0 && iteration >= maxIterations) {
      break;
    }
    await new Promise(resolve => setTimeout(resolve, sleepDuration));
  }

  console.log(`[Node] Completed ${iteration} iterations`);
}

main().catch(console.error);
