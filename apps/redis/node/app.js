const Redis = require('ioredis');

const host = process.env.REDIS_HOST || 'localhost';
const port = parseInt(process.env.REDIS_PORT || '6379', 10);
const maxIterations = parseInt(process.env.MAX_ITERATIONS || '0', 10);
const sleepDuration = parseFloat(process.env.SLEEP_DURATION || '1') * 1000;

const redis = new Redis({ host, port });

async function run() {
    let iteration = 0;

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
