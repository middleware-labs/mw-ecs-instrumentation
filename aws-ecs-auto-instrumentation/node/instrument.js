const { randomUUID: uuid } = require('crypto');
const tracker = require('@middleware.io/node-apm');
const dotenv = require('dotenv');
dotenv.config();

tracker.track({
    target: process.env.MW_TARGET,
    accessToken: process.env.MW_API_KEY,
    projectName: process.env.MW_PROJECT_NAME,
    serviceName: process.env.MW_SERVICE_NAME || 'node-mw-' + uuid(),
    pauseTraces: process.env.MW_APM_TRACES_ENABLED || false,
    pauseMetrics: process.env.MW_APM_METRICS_ENABLED || false,
    consoleExporter: process.env.MW_CONSOLE_EXPORTER || false,
    disabledInstrumentations: process.env.MW_DISABLED_INSTRUMENTATIONS
})