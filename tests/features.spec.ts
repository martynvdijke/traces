import { test, expect } from '@playwright/test';

test.describe('TRACES Search', () => {
  test('should have search input', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#search-input')).toBeVisible();
  });
});

test.describe('TRACES API Features', () => {
  test('should return persons', async ({ request }) => {
    const resp = await request.get('/api/persons');
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should return full events list', async ({ request }) => {
    const resp = await request.get('/api/events/full');
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });
});
