import { test, expect } from '@playwright/test';

test('login with dev admin', async ({ page, baseURL }) => {
  await page.goto('/');

  // Fill the login form
  await page.fill('#username', process.env.E2E_USERNAME || 'testadmin');
  await page.fill('#password', process.env.E2E_PASSWORD || 'secret123');
  await page.click('button[type="submit"]');

  // Wait for navigation to the dashboard root and verify dashboard content
  await page.waitForURL('/', { timeout: 10000 });
  await page.waitForSelector('text=Dashboard', { timeout: 10000 });
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
});
