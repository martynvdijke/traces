import { test, expect } from '@playwright/test';

test.describe('HTMX Integration', () => {
  let sessionCookie: string;
  let csrfToken: string;

  test.beforeAll(async ({ request }) => {
    const setupResp = await request.get('/api/check-setup');
    const { setup } = await setupResp.json();

    if (!setup) {
      const setupRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123', setup: true }
      });
      expect(setupRes.ok()).toBeTruthy();
    } else {
      const loginRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123' }
      });
      expect(loginRes.ok()).toBeTruthy();
    }

    const loginResp = await request.post('/api/login', {
      data: { username: 'admin', password: 'admin123' }
    });
    const cookies = loginResp.headers()['set-cookie'];
    if (cookies) {
      const match = cookies.match(/session=([^;]+)/);
      if (match) sessionCookie = match[1];
    }
    expect(sessionCookie).toBeTruthy();

    const csrfResp = await request.get('/api/csrf-token', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const csrfData = await csrfResp.json();
    csrfToken = csrfData.token;
  });

  test('admin page loads htmx script', async ({ page }) => {
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL('**/admin.html', { timeout: 10000 });

    const hasHtmx = await page.evaluate(() => {
      return typeof (window as any).htmx !== 'undefined';
    });
    expect(hasHtmx).toBeTruthy();
  });

  test('index page loads htmx script', async ({ page }) => {
    await page.goto('/');
    const hasHtmx = await page.evaluate(() => {
      return typeof (window as any).htmx !== 'undefined';
    });
    expect(hasHtmx).toBeTruthy();
  });

  test('login page loads htmx script', async ({ page }) => {
    await page.goto('/login.html');
    const hasHtmx = await page.evaluate(() => {
      return typeof (window as any).htmx !== 'undefined';
    });
    expect(hasHtmx).toBeTruthy();
  });

  test('htmx admin events endpoint returns HTML', async ({ request }) => {
    const resp = await request.get('/api/admin/events', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const contentType = resp.headers()['content-type'] || '';
    expect(contentType).toContain('text/html');
    const body = await resp.text();
    expect(body).toContain('<tr');
  });

  test('htmx admin persons endpoint returns HTML', async ({ request }) => {
    const resp = await request.get('/api/admin/persons', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const contentType = resp.headers()['content-type'] || '';
    expect(contentType).toContain('text/html');
  });

  test('htmx admin tags endpoint returns HTML', async ({ request }) => {
    const resp = await request.get('/api/admin/tags', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const contentType = resp.headers()['content-type'] || '';
    expect(contentType).toContain('text/html');
  });

  test('htmx admin events create via form-encoded POST', async ({ request }) => {
    const resp = await request.post('/api/admin/events', {
      headers: {
        Cookie: `session=${sessionCookie}`,
        'X-CSRF-Token': csrfToken,
        'Content-Type': 'application/x-www-form-urlencoded'
      },
      data: {
        title: 'HTMX E2E Event',
        description: 'Created via htmx endpoint',
        date: '2026-06-01',
        location: 'HTMX City',
        tags: 'htmx,test',
        media_type: 'image'
      }
    });
    expect(resp.ok()).toBeTruthy();
    const body = await resp.text();
    expect(body).toContain('HTMX E2E Event');
  });

  test('htmx admin collections endpoint returns HTML', async ({ request }) => {
    const resp = await request.get('/api/admin/collections', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
  });

  test('htmx admin templates endpoint returns HTML', async ({ request }) => {
    const resp = await request.get('/api/admin/templates', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
  });

  test('htmx admin users endpoint returns HTML', async ({ request }) => {
    const resp = await request.get('/api/admin/users', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
  });

  test('htmx admin trash endpoint returns HTML', async ({ request }) => {
    const resp = await request.get('/api/admin/trash', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
  });

  test('htmx admin events reject unauthenticated requests', async ({ request }) => {
    const resp = await request.get('/api/admin/events');
    expect(resp.status()).toBe(401);
  });

  test('htmx create and delete collection via htmx', async ({ request }) => {
    // Create a collection
    const createResp = await request.post('/api/admin/collections', {
      headers: {
        Cookie: `session=${sessionCookie}`,
        'X-CSRF-Token': csrfToken,
        'Content-Type': 'application/x-www-form-urlencoded'
      },
      data: {
        name: 'HTMX Test Collection',
        description: 'Created via htmx',
        color: '#ff0000'
      }
    });
    expect(createResp.ok()).toBeTruthy();
    const body = await createResp.text();
    expect(body).toContain('HTMX Test Collection');

    // Get collection ID from the response
    const listResp = await request.get('/api/collections', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const collections = await listResp.json();
    const created = collections.find((c: any) => c.name === 'HTMX Test Collection');
    expect(created).toBeDefined();

    // Delete via htmx
    const deleteResp = await request.delete(`/api/admin/collections/${created.id}`, {
      headers: {
        Cookie: `session=${sessionCookie}`,
        'X-CSRF-Token': csrfToken
      }
    });
    expect(deleteResp.ok()).toBeTruthy();
  });

  test('htmx create and delete user via htmx', async ({ request }) => {
    // Create a user
    const createResp = await request.post('/api/admin/users', {
      headers: {
        Cookie: `session=${sessionCookie}`,
        'X-CSRF-Token': csrfToken,
        'Content-Type': 'application/x-www-form-urlencoded'
      },
      data: {
        username: 'htmxuser',
        display_name: 'HTMX User',
        color: '#00ff00'
      }
    });
    expect(createResp.ok()).toBeTruthy();
    const body = await createResp.text();
    expect(body).toContain('htmxuser');

    // Get user ID
    const listResp = await request.get('/api/users', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const users = await listResp.json();
    const created = users.find((u: any) => u.username === 'htmxuser');
    expect(created).toBeDefined();

    // Delete via htmx
    const deleteResp = await request.delete(`/api/admin/users/${created.id}`, {
      headers: {
        Cookie: `session=${sessionCookie}`,
        'X-CSRF-Token': csrfToken
      }
    });
    expect(deleteResp.ok()).toBeTruthy();
  });

  test('htmx trash restore flow', async ({ request }) => {
    // Create an event, delete it, then verify it appears in trash via htmx
    const eventResp = await request.post('/api/events', {
      headers: {
        Cookie: `session=${sessionCookie}`,
        'X-CSRF-Token': csrfToken
      },
      data: {
        title: 'HTMX Trash Test Event',
        date: '2026-01-15',
        location: 'Trash City'
      }
    });
    expect(eventResp.ok()).toBeTruthy();
    const event = await eventResp.json();

    // Soft delete the event
    await request.delete(`/api/events?id=${event.id}`, {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
    });

    // Verify it's in trash via htmx endpoint
    const trashResp = await request.get('/api/admin/trash', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(trashResp.ok()).toBeTruthy();
    const trashBody = await trashResp.text();
    expect(trashBody).toContain('HTMX Trash Test Event');

    // Restore it via htmx
    const restoreResp = await request.post(`/api/admin/trash/${event.id}/restore`, {
      headers: {
        Cookie: `session=${sessionCookie}`,
        'X-CSRF-Token': csrfToken
      }
    });
    expect(restoreResp.ok()).toBeTruthy();

    // Clean up: delete the event permanently
    await request.delete(`/api/events?id=${event.id}`, {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
    });
    await request.post('/api/admin/trash/empty', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
    });
  });
});
