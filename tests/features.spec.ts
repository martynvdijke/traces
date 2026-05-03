import { test, expect } from '@playwright/test';

test.describe('TRACES Search', () => {
  test('should have search input', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#search-input')).toBeVisible();
  });
});

test.describe('TRACES API New Features', () => {
  test('should return tags', async ({ request }) => {
    const resp = await request.get('/api/tags?year=2026');
    expect(resp.ok()).toBeTruthy();
  });
});