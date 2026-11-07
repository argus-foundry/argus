import { defineConfig } from '@playwright/test';
import workspace from './playwright.workspace.config';

// This suite owns a synthetic upstream and proxy on the same isolated ports.
export default defineConfig({
  ...workspace,
  testIgnore: [],
  testMatch: '**/server-security.spec.ts',
  webServer: undefined,
});
