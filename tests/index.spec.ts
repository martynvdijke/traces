import { test, expect } from '@playwright/test';

test.describe('TRACES Timeline', () => {
  test('should load the main page', async ({ page }) => {
    await page.goto('/');
    
    await expect(page).toHaveTitle(/TRACES/);
    await expect(page.locator('text=TRACES')).toBeVisible();
  });

  test('should display year selector', async ({ page }) => {
    await page.goto('/');
    
    await expect(page.locator('text=Year')).toBeVisible();
    await expect(page.locator('button:has-text("2026")')).toBeVisible();
  });

  test('should have a timeline section', async ({ page }) => {
    await page.goto('/');
    
    await expect(page.locator('#timeline')).toBeVisible();
  });

  test('should have contribution graph section', async ({ page }) => {
    await page.goto('/');
    
    await expect(page.locator('#contributions')).toBeVisible();
    await expect(page.locator('.contribution-graph')).toBeVisible();
  });

  test('should have a gallery section', async ({ page }) => {
    await page.goto('/');
    
    await expect(page.locator('#gallery')).toBeVisible();
  });

  test('should have navigation links', async ({ page }) => {
    await page.goto('/');
    
    await expect(page.locator('nav >> text=Timeline')).toBeVisible();
    await expect(page.locator('nav >> text=Activity')).toBeVisible();
    await expect(page.locator('nav >> text=Gallery')).toBeVisible();
    await expect(page.locator('nav >> text=Admin')).toBeVisible();
  });

  test('should have version in footer', async ({ page }) => {
    await page.goto('/');
    
    await expect(page.locator('#version-display')).toBeVisible();
    await expect(page.locator('#version-display')).not.toHaveText('loading...');
  });

  test('should toggle theme', async ({ page }) => {
    await page.goto('/');
    
    const themeToggle = page.locator('#theme-toggle');
    await themeToggle.click();
    
    const theme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
    expect(theme).toBeDefined();
  });

  test('should filter by year', async ({ page }) => {
    await page.goto('/');
    
    await page.locator('button:has-text("2025")').click();
    await expect(page.locator('#current-year')).toHaveText('2025');
  });

  test('should filter by month', async ({ page }) => {
    await page.goto('/');
    
    await page.locator('button:has-text("Jan")').click();
    await expect(page.locator('.month-filter button.btn-dark')).toHaveText('Jan');
  });

  test('should have API docs link', async ({ page }) => {
    await page.goto('/');
    
    await expect(page.locator('nav >> text=API Docs')).toBeVisible();
  });
});

test.describe('TRACES API', () => {
  test('should return version', async ({ request }) => {
    const resp = await request.get('/api/version');
    const data = await resp.json();
    
    expect(resp.ok()).toBeTruthy();
    expect(data.version).toBeDefined();
  });

  test('should return events', async ({ request }) => {
    const resp = await request.get('/api/events?year=2026');
    
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should return contributions', async ({ request }) => {
    const resp = await request.get('/api/contributions?year=2026');
    
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data).toBeDefined();
  });

  test('should check setup status', async ({ request }) => {
    const resp = await request.get('/api/check-setup');
    const data = await resp.json();
    
    expect(resp.ok()).toBeTruthy();
    expect(data.setup).toBeDefined();
  });
});

test.describe('TRACES Login', () => {
  test('should load login page', async ({ page }) => {
    await page.goto('/login.html');
    
    await expect(page).toHaveTitle(/TRACES/);
    await expect(page.locator('text=Sign in')).toBeVisible();
  });

  test('should redirect to setup if no admin', async ({ page }) => {
    await page.goto('/setup.html');
    
    await expect(page).toHaveTitle(/TRACES/);
  });
});

test.describe('TRACES Admin', () => {
  test('should redirect to login without auth', async ({ page }) => {
    await page.goto('/admin.html');
    
    await expect(page).toHaveURL(/login.html/);
  });
});