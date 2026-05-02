import { test, expect } from '@playwright/test';

test.describe('TRACES Admin Panel', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
  });

  test('should load admin page after login', async ({ page }) => {
    await expect(page).toHaveURL(/admin.html/);
    await expect(page.locator('text=TRACES ADMIN')).toBeVisible();
  });

  test('should have events tab', async ({ page }) => {
    await expect(page.locator('text=Events')).toBeVisible();
  });

  test('should have upload tab', async ({ page }) => {
    await expect(page.locator('text=Upload Media')).toBeVisible();
  });

  test('should display events table', async ({ page }) => {
    await page.locator('text=Events').click();
    await expect(page.locator('table')).toBeVisible();
  });

  test('should have logout button', async ({ page }) => {
    await expect(page.locator('text=Logout')).toBeVisible();
  });

  test('should logout and redirect', async ({ page }) => {
    await page.click('text=Logout');
    await expect(page).toHaveURL(/login.html/);
  });
});

test.describe('TRACES Event Management', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.locator('text=Events').click();
  });

  test('should have add event button', async ({ page }) => {
    await expect(page.locator('text=Add Event')).toBeVisible();
  });

  test('should open event modal', async ({ page }) => {
    await page.locator('text=Add Event').click();
    await expect(page.locator('text=Edit Event')).toBeVisible();
  });

  test('should have form fields', async ({ page }) => {
    await page.locator('text=Add Event').click();
    
    await expect(page.locator('input[name="title"]')).toBeVisible();
    await expect(page.locator('textarea[name="description"]')).toBeVisible();
    await expect(page.locator('input[name="date"]')).toBeVisible();
    await expect(page.locator('input[name="location"]')).toBeVisible();
    await expect(page.locator('select[name="media_type"]')).toBeVisible();
  });
});

test.describe('TRACES Media Upload', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.locator('text=Upload Media').click();
  });

  test('should have media type selector', async ({ page }) => {
    await expect(page.locator('select#media-type')).toBeVisible();
  });

  test('should have file input', async ({ page }) => {
    await expect(page.locator('input#media-file')).toBeVisible();
  });

  test('should switch file accept based on media type', async ({ page }) => {
    await page.locator('select#media-type').selectOption('video');
    await expect(page.locator('input#media-file')).toHaveAttribute('accept', 'video/mp4,video/webm,video/quicktime');
  });
});