import { test, expect } from '@playwright/test';

let adminSessionCookie: string;

async function getAdminCookie(request: any): Promise<string> {
  if (adminSessionCookie) return adminSessionCookie;

  const setupResp = await request.get('/api/check-setup');
  const { setup } = await setupResp.json();

  const loginData = setup
    ? { username: 'admin', password: 'admin123' }
    : { username: 'admin', password: 'admin123', setup: true };

  const loginResp = await request.post('/api/login', { data: loginData });
  expect(loginResp.ok()).toBeTruthy();

  const cookies = loginResp.headers()['set-cookie'];
  expect(cookies).toBeTruthy();

  const match = cookies.match(/session=([^;]+)/);
  expect(match).toBeTruthy();
  adminSessionCookie = match![1];
  return adminSessionCookie;
}

async function setAuthCookie(page: any) {
  await page.context().addCookies([{
    name: 'session',
    value: adminSessionCookie,
    domain: 'localhost',
    path: '/',
  }]);
}

test.describe('TypeScript Build Output', () => {
  test.beforeAll(async ({ request }) => {
    await getAdminCookie(request);
  });

  test('should serve compiled index.js', async ({ request }) => {
    const resp = await request.get('/static/js/index.js');
    expect(resp.ok()).toBeTruthy();
    const body = await resp.text();
    expect(body).toContain('changeYear');
    expect(body).toContain('searchEvents');
    expect(body).toContain('export {}');
  });

  test('should serve compiled admin.js', async ({ request }) => {
    const resp = await request.get('/static/js/admin.js');
    expect(resp.ok()).toBeTruthy();
    const body = await resp.text();
    expect(body).toContain('logout');
    expect(body).toContain('loadEvents');
    expect(body).toContain('export {}');
  });

  test('should serve compiled login.js', async ({ request }) => {
    const resp = await request.get('/static/js/login.js');
    expect(resp.ok()).toBeTruthy();
    const body = await resp.text();
    expect(body).toContain('login-form');
    expect(body).toContain('export {}');
  });

  test('should serve compiled setup.js', async ({ request }) => {
    const resp = await request.get('/static/js/setup.js');
    expect(resp.ok()).toBeTruthy();
    const body = await resp.text();
    expect(body).toContain('setup-form');
    expect(body).toContain('export {}');
  });

  test('should serve compiled map.js', async ({ request }) => {
    const resp = await request.get('/static/js/map.js');
    expect(resp.ok()).toBeTruthy();
    const body = await resp.text();
    expect(body).toContain('initMap');
    expect(body).toContain('focusEvent');
    expect(body).toContain('export {}');
  });

  test('should serve source maps for all JS files', async ({ request }) => {
    const files = ['index.js.map', 'admin.js.map', 'login.js.map', 'setup.js.map', 'map.js.map'];
    for (const file of files) {
      const resp = await request.get(`/static/js/${file}`);
      expect(resp.ok()).toBeTruthy();
    }
  });

  test('should not have old app.js', async ({ request }) => {
    const resp = await request.get('/static/app.js');
    expect(resp.status()).toBe(404);
  });

  test('should not have inline JavaScript in HTML files', async ({ page }) => {
    await page.goto('/login.html');
    const html = await page.content();
    expect(html).not.toContain('document.getElementById(\'login-form\')');
    expect(html).toContain('type="module"');
    expect(html).toContain('/static/js/login.js');
  });

  test('should have type="module" on main page script', async ({ page }) => {
    await page.goto('/');
    const html = await page.content();
    expect(html).toContain('type="module" src="/static/js/index.js"');
  });

  test('should have type="module" on admin page script', async ({ request }) => {
    const resp = await request.get('/admin.html', {
      headers: { Cookie: `session=${adminSessionCookie}` }
    });
    const html = await resp.text();
    expect(html).toContain('type="module" src="/static/js/admin.js"');
  });

  test('should have type="module" on map page script', async ({ page }) => {
    await page.goto('/map.html');
    const html = await page.content();
    expect(html).toContain('type="module" src="/static/js/map.js"');
  });
});

test.describe('Module Script Loading - No Errors', () => {
  test.beforeAll(async ({ request }) => {
    await getAdminCookie(request);
  });

  test('main page loads without JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', err => errors.push(err.message));
    await page.goto('/');
    await page.waitForTimeout(1000);
    expect(errors).toHaveLength(0);
  });

  test('login page loads without JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', err => errors.push(err.message));
    await page.goto('/login.html');
    await page.waitForTimeout(1000);
    expect(errors).toHaveLength(0);
  });

  test('setup page loads without JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', err => errors.push(err.message));
    await page.goto('/setup.html');
    await page.waitForTimeout(1000);
    expect(errors).toHaveLength(0);
  });

  test('map page loads without JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', err => errors.push(err.message));
    await page.goto('/map.html');
    await page.waitForTimeout(1000);
    expect(errors).toHaveLength(0);
  });

  test('admin page loads without JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', err => errors.push(err.message));
    await setAuthCookie(page);
    await page.goto('/admin.html');
    await page.waitForTimeout(1000);
    expect(errors).toHaveLength(0);
  });
});

test.describe('Window Global Function Exports', () => {
  test.beforeAll(async ({ request }) => {
    await getAdminCookie(request);
  });

  test('index page exports functions on window', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(500);
    const fns = await page.evaluate(() => ({
      changeYear: typeof (window as any).changeYear,
      searchEvents: typeof (window as any).searchEvents,
      filterMonth: typeof (window as any).filterMonth,
      showMedia: typeof (window as any).showMedia,
      loadMoreGallery: typeof (window as any).loadMoreGallery,
      calendarPrevMonth: typeof (window as any).calendarPrevMonth,
      calendarNextMonth: typeof (window as any).calendarNextMonth,
      calendarToday: typeof (window as any).calendarToday,
      showCalendarDay: typeof (window as any).showCalendarDay,
      updateCompare: typeof (window as any).updateCompare,
    }));
    expect(fns.changeYear).toBe('function');
    expect(fns.searchEvents).toBe('function');
    expect(fns.filterMonth).toBe('function');
    expect(fns.showMedia).toBe('function');
    expect(fns.loadMoreGallery).toBe('function');
    expect(fns.calendarPrevMonth).toBe('function');
    expect(fns.calendarNextMonth).toBe('function');
    expect(fns.calendarToday).toBe('function');
    expect(fns.showCalendarDay).toBe('function');
    expect(fns.updateCompare).toBe('function');
  });

  test('admin page exports functions on window', async ({ page }) => {
    await setAuthCookie(page);
    await page.goto('/admin.html');
    await page.waitForTimeout(500);
    const fns = await page.evaluate(() => ({
      logout: typeof (window as any).logout,
      loadEvents: typeof (window as any).loadEvents,
      loadPersons: typeof (window as any).loadPersons,
      openEventModal: typeof (window as any).openEventModal,
      editEvent: typeof (window as any).editEvent,
      deleteEvent: typeof (window as any).deleteEvent,
      showPersonEvents: typeof (window as any).showPersonEvents,
      clearPersonFilter: typeof (window as any).clearPersonFilter,
      openPersonModal: typeof (window as any).openPersonModal,
      deletePerson: typeof (window as any).deletePerson,
      useMyLocation: typeof (window as any).useMyLocation,
      openCamera: typeof (window as any).openCamera,
      sendMemoriesNow: typeof (window as any).sendMemoriesNow,
      testGotify: typeof (window as any).testGotify,
      testEmailConfig: typeof (window as any).testEmailConfig,
      createBackup: typeof (window as any).createBackup,
      openUserModal: typeof (window as any).openUserModal,
      deleteAdminUser: typeof (window as any).deleteAdminUser,
      fetchEventWeather: typeof (window as any).fetchEventWeather,
      autoTagEvent: typeof (window as any).autoTagEvent,
      addTag: typeof (window as any).addTag,
      removeTag: typeof (window as any).removeTag,
      applyFilters: typeof (window as any).applyFilters,
      updateUploadField: typeof (window as any).updateUploadField,
      loadTags: typeof (window as any).loadTags,
      filterEventsByTag: typeof (window as any).filterEventsByTag,
    }));
    expect(fns.logout).toBe('function');
    expect(fns.loadEvents).toBe('function');
    expect(fns.loadPersons).toBe('function');
    expect(fns.openEventModal).toBe('function');
    expect(fns.editEvent).toBe('function');
    expect(fns.deleteEvent).toBe('function');
    expect(fns.showPersonEvents).toBe('function');
    expect(fns.clearPersonFilter).toBe('function');
    expect(fns.openPersonModal).toBe('function');
    expect(fns.deletePerson).toBe('function');
    expect(fns.useMyLocation).toBe('function');
    expect(fns.openCamera).toBe('function');
    expect(fns.sendMemoriesNow).toBe('function');
    expect(fns.testGotify).toBe('function');
    expect(fns.testEmailConfig).toBe('function');
    expect(fns.createBackup).toBe('function');
    expect(fns.openUserModal).toBe('function');
    expect(fns.deleteAdminUser).toBe('function');
    expect(fns.fetchEventWeather).toBe('function');
    expect(fns.autoTagEvent).toBe('function');
    expect(fns.addTag).toBe('function');
    expect(fns.removeTag).toBe('function');
    expect(fns.applyFilters).toBe('function');
    expect(fns.updateUploadField).toBe('function');
    expect(fns.loadTags).toBe('function');
    expect(fns.filterEventsByTag).toBe('function');
  });

  test('map page exports focusEvent on window', async ({ page }) => {
    await page.goto('/map.html');
    await page.waitForTimeout(500);
    const fn = await page.evaluate(() => typeof (window as any).focusEvent);
    expect(fn).toBe('function');
  });
});

test.describe('Service Worker', () => {
  test('should reference correct JS path', async ({ request }) => {
    const resp = await request.get('/sw.js');
    expect(resp.ok()).toBeTruthy();
    const body = await resp.text();
    expect(body).toContain('/static/js/index.js');
    expect(body).not.toContain('/static/app.js');
  });
});
