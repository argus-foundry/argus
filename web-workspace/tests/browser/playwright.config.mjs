import {defineConfig} from '../../../web-tests/node_modules/@playwright/test/index.mjs';
export default defineConfig({
 outputDir:new URL('../../../.context/theia-browser-results',import.meta.url).pathname,
 testDir:'.',testMatch:'*.spec.mjs',workers:1,reporter:'list',
 use:{baseURL:'http://127.0.0.1:17750',headless:true},
 webServer:{command:'node web-workspace/dev-server.mjs --port 17750 --api http://127.0.0.1:17744',cwd:new URL('../../../',import.meta.url).pathname,port:17750,reuseExistingServer:false},
});
