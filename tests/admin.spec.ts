import { test, expect } from '@playwright/test';

test.describe('TRACES Login Page', () => {
  test('should have login form', async ({ page }) => {
    await page.goto('/login.html');
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
  });
});