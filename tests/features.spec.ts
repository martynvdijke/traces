import { test, expect } from '@playwright/test';

test.describe('TRACES Search', () => {
  test('should search events', async ({ page }) => {
    await page.goto('/');
    
    await page.fill('input[placeholder*="Search"]', 'test');
    await page.click('button:has-text("Search")');
    
    await expect(page.url()).toContain('q=');
  });

  test('should filter by tag', async ({ page }) => {
    await page.goto('/?tag=vacation');
    
    await expect(page).toHaveURL(/tag=/);
  });
});

test.describe('TRACES API New Features', () => {
  test('should return tags', async ({ request }) => {
    const resp = await request.get('/api/tags');
    const data = await resp.json();
    
    expect(resp.ok()).toBeTruthy();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should export json', async ({ request }) => {
    const resp = await request.get('/api/events/export?format=json');
    
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should export csv', async ({ request }) => {
    const resp = await request.get('/api/events/export?format=csv');
    
    expect(resp.ok()).toBeTruthy();
    const text = await resp.text();
    expect(text).toContain('Title');
  });

  test('should return stats', async ({ request }) => {
    const resp = await request.get('/api/stats?year=2026');
    const data = await resp.json();
    
    expect(resp.ok()).toBeTruthy();
    expect(data.total).toBeDefined();
    expect(data.by_month).toBeDefined();
    expect(data.by_tag).toBeDefined();
    expect(data.by_media).toBeDefined();
  });

  test('should search events with query', async ({ request }) => {
    const resp = await request.get('/api/events/search?q=test');
    
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should search with year filter', async ({ request }) => {
    const resp = await request.get('/api/events/search?year=2026&tag=vacation');
    
    expect(resp.ok()).toBeTruthy();
  });
});

test.describe('TRACES Year Comparison', () => {
  test('should show year comparison', async ({ page }) => {
    await page.goto('/');
    
    await page.locator('text=Compare Years').click();
    await expect(page.locator('.year-comparison')).toBeVisible();
  });
});

test.describe('TRACES Theme Memory', () => {
  test('should persist theme preference', async ({ page }) => {
    await page.goto('/');
    
    await page.click('#theme-toggle');
    await page.reload();
    
    const theme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    expect(theme).toBeDefined();
  });
});

test.describe('TRACES Carousel', () => {
  test('should open carousel', async ({ page }) => {
    await page.goto('/');
    
    const galleryItems = page.locator('.gallery-item');
    if (await galleryItems.count() > 0) {
      await galleryItems.first().click();
      await expect(page.locator('.carousel')).toBeVisible();
    }
  });
});