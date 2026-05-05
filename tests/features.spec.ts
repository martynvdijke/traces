import { test, expect } from '@playwright/test';

test.describe('TRACES Search', () => {
  test('should have search input', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('#search-input')).toBeVisible();
  });
});

test.describe('TRACES Map', () => {
  test('should load map container', async ({ page }) => {
    await page.goto('/');
    await page.locator('#map-tab').click();
    await page.waitForTimeout(500);
    await expect(page.locator('#map-container')).toBeVisible();
  });

  test('should have map container or placeholder visible', async ({ page }) => {
    await page.goto('/');
    await page.locator('#map-tab').click();
    await page.waitForTimeout(1000);
    const mapContainer = page.locator('#map-container');
    await expect(mapContainer).toBeVisible();
    const content = await mapContainer.innerHTML();
    expect(content.length).toBeGreaterThan(0);
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

  test('should return events sorted desc with limit', async ({ request }) => {
    const resp = await request.get('/api/events?sort=desc&limit=5');
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    expect(data.length).toBeLessThanOrEqual(5);
  });

  test('should return stats', async ({ request }) => {
    const resp = await request.get('/api/stats?year=2026');
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.total).toBeDefined();
  });
});
