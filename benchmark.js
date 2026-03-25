import { buildIncidents } from './src/lib/incidents.js';

function generateLogs(count) {
  const logs = [];
  const services = ['auth', 'gateway', 'payments', 'users', 'billing'];
  const errorCodes = ['HTTP_500', 'HTTP_502', 'HTTP_503', 'HTTP_504'];

  for (let i = 0; i < count; i++) {
    logs.push({
      service: services[i % services.length],
      errorCode: errorCodes[i % errorCodes.length],
      level: i % 5 === 0 ? 'error' : 'info',
      timestamp: new Date(Date.now() - Math.random() * 10000).toISOString()
    });
  }
  return logs;
}

const logs = generateLogs(10000);

const start = performance.now();
for (let i = 0; i < 100; i++) {
  buildIncidents(logs);
}
const end = performance.now();

console.log(`Baseline benchmark: ${(end - start).toFixed(2)} ms for 100 iterations of buildIncidents with 10000 logs`);
