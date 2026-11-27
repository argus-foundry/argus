import {defineConfig} from '@playwright/test';
export default defineConfig({testDir:'./tests',testMatch:'decide-defer.spec.ts',workers:1,timeout:30000,reporter:'list',projects:[1200,390].map(width=>({name:String(width),use:{browserName:'chromium' as const,viewport:{width,height:844},isMobile:width===390,hasTouch:width===390}}))});
