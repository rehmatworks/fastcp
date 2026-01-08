import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 30_000,
  expect: { timeout: 5000 },
  use: {
    headless: true,
    ignoreHTTPSErrors: true,
    viewport: { width: 1280, height: 800 },
    baseURL: process.env.BASE_URL || 'https://localhost:8080',
  },
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
});
