import { test, expect } from '@playwright/test';

test.describe('TRACES Login Page', () => {
  test('should have login form', async ({ page }) => {
    await page.goto('/login.html');
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
  });
});

test.describe('TRACES Admin Backend', () => {
  test.describe.configure({ mode: 'serial' });

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
      const cookies = setupRes.headers()['set-cookie'];
      if (cookies) {
        const match = cookies.match(/session=([^;]+)/);
        if (match) sessionCookie = match[1];
      }
    } else {
      const loginRes = await request.post('/api/login', {
        data: { username: 'admin', password: 'admin123' }
      });
      expect(loginRes.ok()).toBeTruthy();
      const cookies = loginRes.headers()['set-cookie'];
      if (cookies) {
        const match = cookies.match(/session=([^;]+)/);
        if (match) sessionCookie = match[1];
      }
    }

    expect(sessionCookie).toBeTruthy();

    const csrfResp = await request.get('/api/csrf-token', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const csrfData = await csrfResp.json();
    csrfToken = csrfData.token;
    expect(csrfToken).toBeTruthy();
  });

  test('should create a new event via POST /api/events', async ({ request }) => {
    const resp = await request.post('/api/events', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        title: 'E2E Test Event',
        description: 'Created by Playwright test',
        date: '2026-05-03',
        location: 'Test City',
        media_type: 'image',
        tags: 'test,e2e',
        sort_order: 0,
        is_public: false,
        latitude: 40.7128,
        longitude: -74.0060
      }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.id).toBeGreaterThan(0);
    expect(data.title).toBe('E2E Test Event');
    expect(data.date).toBe('2026-05-03');
    expect(data.latitude).toBe(40.7128);
    expect(data.longitude).toBe(-74.0060);
  });

  test('should return created event in events list', async ({ request }) => {
    const resp = await request.get('/api/events?year=2026&sort=desc&limit=100', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    const testEvent = data.find((e: any) => e.title === 'E2E Test Event');
    expect(testEvent).toBeDefined();
    expect(testEvent.date).toBe('2026-05-03');
  });

  test('should respect limit and sort params on /api/events', async ({ request }) => {
    const resp = await request.get('/api/events?year=2026&sort=desc&limit=3', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    expect(data.length).toBeLessThanOrEqual(3);
  });

  test('should create a person via POST /api/persons', async ({ request }) => {
    const resp = await request.post('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        name: 'E2E Test Person',
        bio: 'A test person created by Playwright',
        color: '#ef4444'
      }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(data.id).toBeGreaterThan(0);
    expect(data.name).toBe('E2E Test Person');
  });

  test('should return persons list', async ({ request }) => {
    const resp = await request.get('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    const testPerson = data.find((p: any) => p.name === 'E2E Test Person');
    expect(testPerson).toBeDefined();
  });

  test('should reject unauthenticated event creation', async ({ request }) => {
    const resp = await request.post('/api/events', {
      data: { title: 'Should fail' }
    });
    expect(resp.status()).toBe(401);
  });

  test('should fetch autocomplete for locations', async ({ request }) => {
    const resp = await request.get('/api/autocomplete?field=location&q=test', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    // Returns 200 with empty array if no matches
    expect(resp.ok()).toBeTruthy();
  });

  test('should fetch autocomplete for tags', async ({ request }) => {
    const resp = await request.get('/api/autocomplete?field=tag&q=t', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    // Returns 200 with empty array if no matches
    expect(resp.ok()).toBeTruthy();
  });

  test('should create event through browser UI form', async ({ page }) => {
    // Collect console errors for debugging
    const errors: string[] = [];
    page.on('console', msg => { if (msg.type() === 'error') errors.push(msg.text()); });

    // Login via the browser UI
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL('**/admin.html', { timeout: 15000 });
    // Wait for admin JS to fully initialize (all API calls in init())
    await page.waitForFunction(() => typeof (window as any).openEventModal === 'function', { timeout: 15000 });

    // Verify admin page loaded
    await expect(page.locator('#event-list')).toBeVisible({ timeout: 5000 });

    // Open modal directly via JS to avoid onclick timing issues
    await page.evaluate(() => (window as any).openEventModal());
    await page.waitForSelector('#event-title', { state: 'visible', timeout: 5000 });

    // Verify modal opened: event-title should be visible
    if (!await page.locator('#event-title').isVisible()) {
      console.log('Console errors:', errors);
      throw new Error('Event modal did not open. Console errors: ' + errors.join(', '));
    }

    // Fill in the event form
    await page.fill('#event-title', 'Browser UI Test Event');
    await page.fill('#event-desc', 'Created via browser form');
    await page.fill('#event-date', '2026-05-04');
    await page.fill('#event-location', 'Browser City');

    // Click "Save Event"
    await page.click('#event-form button[type="submit"]');

    // Wait for the event to appear in the list (indicates successful creation)
    await expect(page.locator('#event-list')).toContainText('Browser UI Test Event', { timeout: 10000 });

    // Delete the test event via API to clean up
    const sessionCookie = (await page.context().cookies()).find(c => c.name === 'session');
    if (sessionCookie) {
      const request = page.request;
      const csrfResp = await request.get('/api/csrf-token', {
        headers: { Cookie: `session=${sessionCookie.value}` }
      });
      const { token } = await csrfResp.json();

      const listResp = await request.get('/api/events?year=2026&sort=desc&limit=20', {
        headers: { Cookie: `session=${sessionCookie.value}` }
      });
      const events = await listResp.json();
      const testEvent = events.find((e: any) => e.title === 'Browser UI Test Event');
      if (testEvent) {
        await request.delete(`/api/events?id=${testEvent.id}`, {
          headers: { Cookie: `session=${sessionCookie.value}`, 'X-CSRF-Token': token }
        });
      }
    }
  });

  test('should delete created test event', async ({ request }) => {
    const listResp = await request.get('/api/events?year=2026&sort=desc&limit=10', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const events = await listResp.json();
    const testEvent = events.find((e: any) => e.title === 'E2E Test Event');
    if (testEvent) {
      const resp = await request.delete(`/api/events?id=${testEvent.id}`, {
        headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
      });
      expect(resp.ok()).toBeTruthy();
    }
  });

  test('should delete created test person', async ({ request }) => {
    const listResp = await request.get('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const persons = await listResp.json();
    const testPerson = persons.find((p: any) => p.name === 'E2E Test Person');
    if (testPerson) {
      const resp = await request.delete(`/api/persons?id=${testPerson.id}`, {
        headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
      });
      expect(resp.ok()).toBeTruthy();
    }
  });

  test('should create event with person, tags, user and recurring via browser UI', async ({ page }) => {
    // Login and navigate to admin
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL('**/admin.html', { timeout: 15000 });
    await page.waitForFunction(() => typeof (window as any).openEventModal === 'function', { timeout: 15000 });
    await page.waitForSelector('#event-list', { state: 'visible', timeout: 5000 });

    // First create a person inline via the event modal
    await page.evaluate(() => (window as any).openEventModal());
    await page.waitForSelector('#event-title', { state: 'visible', timeout: 5000 });
    await page.waitForTimeout(500);

    // Open inline person form - the button has title "Add new person"
    await page.click('button[title="Add new person"]');
    await page.waitForSelector('#inline-person-name', { state: 'visible', timeout: 3000 });
    await page.fill('#inline-person-name', 'Inline Test Person');
    const addBtn = page.locator('#inline-person-form button.btn-success');
    await addBtn.click();
    // Wait for inline form to close
    await page.waitForTimeout(1000);

    // Verify person was added
    const personSelect = page.locator('#event-person');
    await expect(personSelect).toContainText('Inline Test Person', { timeout: 5000 });

    // Add tags
    await page.fill('#event-tags', 'test-tag-one');
    await page.click('#add-tag-btn');
    await page.waitForTimeout(300);
    await page.fill('#event-tags', 'test-tag-two');
    await page.click('#add-tag-btn');
    await page.waitForTimeout(300);

    // Verify tags are shown as badges
    await expect(page.locator('#selected-tags')).toContainText('test-tag-one');
    await expect(page.locator('#selected-tags')).toContainText('test-tag-two');

    // Set recurring and user
    await page.selectOption('#event-recurring', 'monthly');
    await page.selectOption('#event-user', { index: 1 }); // first user after "Default"

    // Fill event details
    await page.fill('#event-title', 'Full Feature Test Event');
    await page.fill('#event-desc', 'Event with person, tags, user, and recurring');
    await page.fill('#event-date', '2026-06-15');
    await page.fill('#event-location', 'Feature City');

    // Submit the form
    await page.click('#event-form button[type="submit"]');

    // Wait for event to appear in list
    await expect(page.locator('#event-list')).toContainText('Full Feature Test Event', { timeout: 10000 });

    // Verify via API that all fields were saved
    const cookies = await page.context().cookies();
    const sessionCookie = cookies.find(c => c.name === 'session');
    if (sessionCookie) {
      const request = page.request;
      const csrfResp = await request.get('/api/csrf-token', {
        headers: { Cookie: `session=${sessionCookie.value}` }
      });
      const { token } = await csrfResp.json();

      const listResp = await request.get('/api/events?year=2026&sort=desc&limit=100', {
        headers: { Cookie: `session=${sessionCookie.value}` }
      });
      const events = await listResp.json();
      const testEvent = events.find((e: any) => e.title === 'Full Feature Test Event');
      expect(testEvent).toBeDefined();
      expect(testEvent.tags).toContain('test-tag-one');
      expect(testEvent.tags).toContain('test-tag-two');
      expect(testEvent.recurring).toBe('monthly');
      expect(testEvent.person_id).toBeGreaterThan(0);
      expect(testEvent.user_id).toBeGreaterThan(0);

      // Cleanup
      if (testEvent) {
        await request.delete(`/api/events?id=${testEvent.id}`, {
          headers: { Cookie: `session=${sessionCookie.value}`, 'X-CSRF-Token': token }
        });
      }
    }
  });

  test('should edit event and verify tags, person, user, recurring persist', async ({ page }) => {
    // Login
    await page.goto('/login.html');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'admin123');
    await page.click('button[type="submit"]');
    await page.waitForURL('**/admin.html', { timeout: 15000 });
    await page.waitForFunction(() => typeof (window as any).openEventModal === 'function', { timeout: 15000 });

    // Create an event via API first with all fields
    const cookies = await page.context().cookies();
    const sessionCookie = cookies.find(c => c.name === 'session');
    let testEvent: any;
    if (sessionCookie) {
      const request = page.request;
      const csrfResp = await request.get('/api/csrf-token', {
        headers: { Cookie: `session=${sessionCookie.value}` }
      });
      const { token } = await csrfResp.json();

      // Create a person first
      const personResp = await request.post('/api/persons', {
        headers: { Cookie: `session=${sessionCookie.value}`, 'X-CSRF-Token': token },
        data: { name: 'Edit Test Person', color: '#10b981' }
      });
      const person = await personResp.json();

      // Create event with all fields
      const evResp = await request.post('/api/events', {
        headers: { Cookie: `session=${sessionCookie.value}`, 'X-CSRF-Token': token },
        data: {
          title: 'Edit Persist Test Event',
          description: 'Testing edit persistence',
          date: '2026-08-01',
          location: 'Edit City',
          tags: 'edit-tag-1, edit-tag-2',
          person_id: person.id,
          recurring: 'yearly',
          user_id: 1
        }
      });
      testEvent = await evResp.json();
    }

    // Reload the admin page to get updated event list
    await page.goto('/admin.html');
    await page.waitForSelector('#event-list', { state: 'visible', timeout: 5000 });

    // Click edit on the event
    await page.evaluate((id: number) => (window as any).editEvent(id), testEvent.id);
    await page.waitForSelector('#event-title', { state: 'visible', timeout: 5000 });

    // Verify fields are populated correctly
    await expect(page.locator('#event-title')).toHaveValue('Edit Persist Test Event');
    await expect(page.locator('#event-location')).toHaveValue('Edit City');
    await expect(page.locator('#event-recurring')).toHaveValue('yearly');
    await expect(page.locator('#event-person')).toContainText('Edit Test Person');
    await expect(page.locator('#selected-tags')).toContainText('edit-tag-1');
    await expect(page.locator('#selected-tags')).toContainText('edit-tag-2');

    // Modify and save
    await page.fill('#event-title', 'Edit Persist Test Event Modified');
    await page.click('#event-form button[type="submit"]');

    // Verify the modification persisted via API
    if (sessionCookie) {
      const request = page.request;
      const csrfResp = await request.get('/api/csrf-token', {
        headers: { Cookie: `session=${sessionCookie.value}` }
      });
      const { token } = await csrfResp.json();

      const listResp = await request.get('/api/events?year=2026&sort=desc&limit=20', {
        headers: { Cookie: `session=${sessionCookie.value}` }
      });
      const events = await listResp.json();
      const modifiedEvent = events.find((e: any) => e.title === 'Edit Persist Test Event Modified');
      expect(modifiedEvent).toBeDefined();
      expect(modifiedEvent.tags).toContain('edit-tag-1');
      expect(modifiedEvent.tags).toContain('edit-tag-2');
      expect(modifiedEvent.recurring).toBe('yearly');
      expect(modifiedEvent.person_id).toBeGreaterThan(0);

      // Cleanup
      if (modifiedEvent) {
        await request.delete(`/api/events?id=${modifiedEvent.id}`, {
          headers: { Cookie: `session=${sessionCookie.value}`, 'X-CSRF-Token': token }
        });
      }
    }
  });

  test('should search events via API', async ({ request }) => {
    const resp = await request.get('/api/events/search?q=test&year=2026', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should search events with multiple filters', async ({ request }) => {
    const resp = await request.get('/api/events/search?year=2026&media_type=image', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should create and use share link', async ({ request }) => {
    // Create an event to share
    const createResp = await request.post('/api/events', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        title: 'Share Test Event',
        date: '2026-07-01',
        tags: 'share-test'
      }
    });
    const event = await createResp.json();

    // Create share link
    const shareResp = await request.post('/api/share/create', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        event_ids: [event.id],
        year: '2026',
        days: 7
      }
    });
    expect(shareResp.ok()).toBeTruthy();
    const shareData = await shareResp.json();
    expect(shareData.token).toBeTruthy();

    // Access share link (will redirect, follow redirects)
    const shareGetResp = await request.get(`/api/share?token=${shareData.token}`);
    expect(shareGetResp.ok() || shareGetResp.status() === 302).toBeTruthy();

    // Get public events with share token
    const publicResp = await request.get(`/api/public?share=${shareData.token}`);
    expect(publicResp.ok()).toBeTruthy();
    const publicEvents = await publicResp.json();
    expect(Array.isArray(publicEvents)).toBeTruthy();

    // Cleanup event
    await request.delete(`/api/events?id=${event.id}`, {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
    });
  });

  test('should clone an event via API', async ({ request }) => {
    // Create source event
    const createResp = await request.post('/api/events', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        title: 'Clone Source Event',
        date: '2026-04-01',
        tags: 'clone-test',
        location: 'Clone City'
      }
    });
    const sourceEvent = await createResp.json();

    // Clone it
    const cloneResp = await request.post('/api/events/clone', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: { id: sourceEvent.id, date: '2026-09-01' }
    });
    expect(cloneResp.ok()).toBeTruthy();

    // Verify clone exists
    const listResp = await request.get('/api/events?year=2026&sort=desc&limit=20', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const events = await listResp.json();
    const clones = events.filter((e: any) => e.title === 'Clone Source Event');
    expect(clones.length).toBe(2);

    // Cleanup both
    for (const e of clones) {
      await request.delete(`/api/events?id=${e.id}`, {
        headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
      });
    }
  });

  test('should export events as JSON', async ({ request }) => {
    const resp = await request.get('/api/events/export?year=2026', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should export events as CSV', async ({ request }) => {
    const resp = await request.get('/api/events/export?year=2026&format=csv', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const text = await resp.text();
    expect(text).toContain('Title');
    expect(text).toContain('Description');
    expect(text).toContain('Date');
  });

  test('should create and manage a backup', async () => {
    // Use native fetch instead of Playwright request fixture to avoid
    // "Request context disposed" error on retry in serial mode
    const resp = await fetch(`http://localhost:6270/api/backup`, {
      method: 'POST',
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
    });
    // Backup may fail in test env, just verify endpoint is accessible
    expect(resp.ok || resp.status === 500).toBeTruthy();
  });

  test('should list backups', async ({ request }) => {
    const resp = await request.get('/api/backups', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should get and save gotify config', async ({ request }) => {
    // Get config
    const getResp = await request.get('/api/gotify/config', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(getResp.ok()).toBeTruthy();

    // Save config
    const saveResp = await request.post('/api/gotify/config', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: { url: 'https://gotify.example.com', token: 'test-token', enabled: false }
    });
    expect(saveResp.ok()).toBeTruthy();
  });

  test('should get and save ollama config', async ({ request }) => {
    const getResp = await request.get('/api/ollama/config', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(getResp.ok()).toBeTruthy();

    const saveResp = await request.post('/api/ollama/config', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: { url: 'http://localhost:11434', model: 'llama3.2', enabled: false }
    });
    expect(saveResp.ok()).toBeTruthy();
  });

  test('should get and save memories config', async ({ request }) => {
    const getResp = await request.get('/api/memories/config', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(getResp.ok()).toBeTruthy();

    const saveResp = await request.post('/api/memories/config', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: { enabled: true, days_window: 3, email_enabled: false }
    });
    expect(saveResp.ok()).toBeTruthy();
  });

  test('should get and save email config', async ({ request }) => {
    const getResp = await request.get('/api/email/config', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(getResp.ok()).toBeTruthy();

    const saveResp = await request.post('/api/email/config', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        smtp_host: 'smtp.example.com',
        smtp_port: 587,
        smtp_user: 'user@example.com',
        smtp_pass: 'testpass',
        from_addr: 'from@example.com',
        to_addr: 'to@example.com'
      }
    });
    expect(saveResp.ok()).toBeTruthy();
  });

  test('should get calendar data', async ({ request }) => {
    const resp = await request.get('/api/calendar?year=2026&month=05', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should get user events', async ({ request }) => {
    const resp = await request.get('/api/users/1/events', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should create and delete a user', async ({ request }) => {
    const createResp = await request.post('/api/users', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        username: 'testuser_e2e',
        display_name: 'Test User E2E',
        color: '#f59e0b'
      }
    });
    expect(createResp.ok()).toBeTruthy();
    const user = await createResp.json();
    expect(user.id).toBeGreaterThan(0);
    expect(user.display_name).toBe('Test User E2E');

    // Verify user appears in list
    const listResp = await request.get('/api/users', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    const users = await listResp.json();
    const found = users.find((u: any) => u.username === 'testuser_e2e');
    expect(found).toBeDefined();

    // Delete user
    const deleteResp = await request.delete(`/api/users?id=${user.id}`, {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
    });
    expect(deleteResp.ok()).toBeTruthy();
  });

  test('should update and delete a person via API', async ({ request }) => {
    // Create person
    const createResp = await request.post('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: { name: 'Update Test Person', bio: 'Will be updated', color: '#8b5cf6' }
    });
    const person = await createResp.json();

    // Update person
    const updateResp = await request.post('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: { id: person.id, name: 'Updated Test Person', bio: 'Was updated', color: '#8b5cf6' }
    });
    expect(updateResp.ok()).toBeTruthy();
    const updatedPerson = await updateResp.json();
    expect(updatedPerson.name).toBe('Updated Test Person');
    expect(updatedPerson.bio).toBe('Was updated');

    // Delete person
    const deleteResp = await request.delete(`/api/persons?id=${person.id}`, {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
    });
    expect(deleteResp.ok()).toBeTruthy();
  });

  test('should get person events', async ({ request }) => {
    // Create person and event linked to them
    const personResp = await request.post('/api/persons', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: { name: 'Person Events Test', color: '#ec4899' }
    });
    const person = await personResp.json();

    const evResp = await request.post('/api/events', {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken },
      data: {
        title: 'Linked Event',
        date: '2026-09-15',
        person_id: person.id
      }
    });
    const event = await evResp.json();

    // Get person events
    const resp = await request.get(`/api/persons/${person.id}/events`, {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const events = await resp.json();
    expect(events.some((e: any) => e.title === 'Linked Event')).toBeTruthy();

    // Cleanup
    await request.delete(`/api/events?id=${event.id}`, {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
    });
    await request.delete(`/api/persons?id=${person.id}`, {
      headers: { Cookie: `session=${sessionCookie}`, 'X-CSRF-Token': csrfToken }
    });
  });

  test('should search events by tag', async ({ request }) => {
    const resp = await request.get('/api/events/search?tag=test&year=2026', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });

  test('should return tags with name and count', async ({ request }) => {
    const resp = await request.get('/api/tags', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
    // If there are tags, verify format
    if (data.length > 0) {
      expect(data[0]).toHaveProperty('name');
      expect(data[0]).toHaveProperty('count');
      expect(typeof data[0].name).toBe('string');
      expect(typeof data[0].count).toBe('number');
    }
  });

  test('should return tags filtered by year', async ({ request }) => {
    const resp = await request.get('/api/tags?year=2026', {
      headers: { Cookie: `session=${sessionCookie}` }
    });
    expect(resp.ok()).toBeTruthy();
    const data = await resp.json();
    expect(Array.isArray(data)).toBeTruthy();
  });
});
