const Redis = require('ioredis');
const fs = require('fs');

const host = process.env.REDIS_HOST || 'localhost';
const port = parseInt(process.env.REDIS_PORT || '6379', 10);
const tlsEnabled = process.env.REDIS_TLS_ENABLED === 'true';
const caCertPath = process.env.REDIS_CA_CERT || '';
const maxIterations = parseInt(process.env.MAX_ITERATIONS || '0', 10);
const sleepDuration = parseFloat(process.env.SLEEP_DURATION || '1') * 1000;

const redisOptions = { host, port };

if (tlsEnabled) {
    console.log('[Node] TLS enabled');
    redisOptions.tls = {};
    if (caCertPath && fs.existsSync(caCertPath)) {
        redisOptions.tls.ca = fs.readFileSync(caCertPath);
    }
}

async function run() {
    let iteration = 0;

    // sleep for 5 seconds
    await new Promise(resolve => setTimeout(resolve, 5000));

    const redis = new Redis(redisOptions);

    while (true) {
        iteration++;
        console.log(`[Node] Iteration ${iteration}`);

        // Basic operations
        const key = `node:key:${iteration}`;
        await redis.set(key, `value-${iteration}`);
        await redis.get(key);

        // List operations
        await redis.lpush('node:list', `item-${iteration}`);
        await redis.lrange('node:list', 0, -1);

        // Hash operations
        await redis.hset('node:hash', `field-${iteration}`, `value-${iteration}`);
        await redis.hgetall('node:hash');

        // Pub/sub (publish only)
        await redis.publish('node:channel', `message-${iteration}`);

        if (maxIterations > 0 && iteration >= maxIterations) {
            break;
        }
        await new Promise(resolve => setTimeout(resolve, sleepDuration));
    }

    console.log(`[Node] Completed ${iteration} iterations`);
    await redis.quit();
}

run().catch(console.error);
