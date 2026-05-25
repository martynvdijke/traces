export {};
declare const L: any;

interface TimelineEvent {
  id: number;
  title: string;
  description: string;
  date: string;
  location: string;
  media_type: string;
  media_url: string;
  thumbnail: string;
  media_caption: string;
  tags: string;
  sort_order: number;
  is_public: boolean;
  is_favorite: boolean;
  created_at: string;
  person_id?: number;
  latitude?: number;
  longitude?: number;
  recurring: string;
  weather_data: string;
  start_time: string;
  end_time: string;
  user_id: number;
  person?: {
    id: number;
    name: string;
    avatar_url: string;
    bio: string;
    birth_date: string;
    color: string;
    created_at: string;
  };
  user?: {
    id: number;
    username: string;
    display_name: string;
    color: string;
    avatar_url: string;
  };
}

interface CalendarDay {
  date: string;
  events: TimelineEvent[];
  count: number;
}

interface ContributionMap {
  [date: string]: number;
}

interface Weather {
  temperature: number;
  condition: string;
  icon: string;
  wind_speed: number;
}

type Theme = 'light' | 'dark';

let currentYear: number = new Date().getFullYear();
let currentMonth: number = 0;
let events: TimelineEvent[] = [];
let contributions: ContributionMap = {};
let galleryPage: number = 1;
const galleryLimit: number = 12;
let mapInstance: any = null;
let mapMarkers: any[] = [];
let mapPathLine: any = null;
let users: any[] = [];

const monthNames: string[] = [
  'January', 'February', 'March', 'April', 'May', 'June',
  'July', 'August', 'September', 'October', 'November', 'December'
];

const themeToggle = document.getElementById('theme-toggle') as HTMLButtonElement;
const themeIcon = themeToggle?.querySelector('i');

function setTheme(theme: Theme): void {
  document.documentElement.setAttribute('data-theme', theme);
  localStorage.setItem('theme', theme);
  if (themeIcon) {
    themeIcon.className = theme === 'dark' ? 'fa-solid fa-sun' : 'fa-solid fa-moon';
  }
}

function initTheme(): void {
  const savedTheme = (localStorage.getItem('theme') || (
    window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  )) as Theme;
  setTheme(savedTheme);
  themeToggle?.addEventListener('click', () => {
    const current = document.documentElement.getAttribute('data-theme') as Theme;
    setTheme(current === 'dark' ? 'light' : 'dark');
  });
}

function changeYear(year: number): void {
  currentYear = year;
  const yearEl = document.getElementById('current-year');
  if (yearEl) yearEl.textContent = String(year);
  document.querySelectorAll('.year-selector .btn').forEach(b => b.classList.remove('btn-primary'));
  document.querySelectorAll('.year-selector .btn').forEach(b => b.classList.add('btn-outline-primary'));
  const btn = document.querySelector(`.year-selector .btn[data-year="${year}"]`);
  if (btn) { btn.classList.remove('btn-outline-primary'); btn.classList.add('btn-primary'); }
  const icsLink = document.getElementById('ics-download') as HTMLAnchorElement;
  if (icsLink) icsLink.href = '/api/events/ics?year=' + year;
  loadData();
  loadStatsDist();
}

function searchEvents(): void {
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  if (input?.value) {
    applyAdvancedFilters();
  }
  hideGlobalDropdown();
}

function globalSearchInput(): void {
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  const q = input?.value?.trim();
  if (!q || q.length < 2) {
    hideGlobalDropdown();
    return;
  }
  fetch('/api/events/search/global?q=' + encodeURIComponent(q) + '&limit=8')
    .then(r => r.json())
    .then((results: any[]) => {
      const dropdown = document.getElementById('global-search-dropdown');
      if (!dropdown) return;
      if (!Array.isArray(results) || results.length === 0) {
        dropdown.innerHTML = '<div class="global-search-no-results">No matches found</div>';
      } else {
        dropdown.innerHTML = results.map((e: any, i: number) => {
          const year = e.date ? e.date.slice(0, 4) : '';
          const loc = e.location ? ' <span class="search-year"><i class="fa-solid fa-location-dot"></i> ' + escapeHtml(e.location) + '</span>' : '';
          return '<div class="global-search-item" data-id="' + e.id + '" onclick="selectGlobalResult(' + e.id + ')" onmouseenter="highlightGlobalItem(' + i + ')">' +
            '<div class="fw-bold">' + escapeHtml(e.title) + '</div>' +
            '<div class="search-year">' + year + loc + '</div>' +
            '</div>';
        }).join('');
      }
      dropdown.style.display = 'block';
    })
    .catch(() => { hideGlobalDropdown(); });
}

let globalSearchIndex = -1;

function globalSearchKeydown(e: KeyboardEvent): void {
  const dropdown = document.getElementById('global-search-dropdown');
  if (!dropdown || dropdown.style.display === 'none') return;
  const items = dropdown.querySelectorAll('.global-search-item');
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    globalSearchIndex = Math.min(globalSearchIndex + 1, items.length - 1);
    updateGlobalHighlight(items);
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    globalSearchIndex = Math.max(globalSearchIndex - 1, 0);
    updateGlobalHighlight(items);
  } else if (e.key === 'Enter') {
    e.preventDefault();
    if (globalSearchIndex >= 0 && items[globalSearchIndex]) {
      const el = items[globalSearchIndex] as HTMLElement;
      selectGlobalResult(parseInt(el.dataset.id || '0'));
    }
  } else if (e.key === 'Escape') {
    hideGlobalDropdown();
  }
}

function updateGlobalHighlight(items: NodeListOf<Element>): void {
  items.forEach((item, i) => {
    item.classList.toggle('active', i === globalSearchIndex);
  });
}

function highlightGlobalItem(index: number): void {
  const dropdown = document.getElementById('global-search-dropdown');
  if (!dropdown) return;
  const items = dropdown.querySelectorAll('.global-search-item');
  items.forEach((item, i) => {
    item.classList.toggle('active', i === index);
  });
  globalSearchIndex = index;
}

function globalSearchFocus(): void {
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  if (input?.value?.trim() && input.value.trim().length >= 2) {
    globalSearchInput();
  }
}

function selectGlobalResult(id: number): void {
  hideGlobalDropdown();
  if (id) showMedia(id);
}

function hideGlobalDropdown(): void {
  const dropdown = document.getElementById('global-search-dropdown');
  if (dropdown) dropdown.style.display = 'none';
  globalSearchIndex = -1;
}

document.addEventListener('click', (e: Event) => {
  const target = e.target as HTMLElement;
  if (!target.closest('.global-search-wrapper')) {
    hideGlobalDropdown();
  }
});

function filterMonth(month: number): void {
  currentMonth = month;
  const buttons = document.querySelectorAll('.month-filter .btn');
  buttons.forEach((btn, i) => {
    btn.classList.toggle('active', i === month);
    btn.classList.toggle('btn-dark', i === month);
    btn.classList.toggle('btn-outline-dark', i !== month);
  });
  renderTimeline();
  renderGallery();
  renderMapInstance();
}

async function loadContributions(): Promise<void> {
  try {
    const res = await fetch('/api/contributions?year=' + currentYear);
    if (!res.ok) {
      contributions = {};
    } else {
      contributions = await res.json() as ContributionMap;
    }
    renderContributionGraph();
    updateStats();
  } catch (err) {
    console.error('Failed to load contributions:', err);
  }
}

function renderContributionGraph(): void {
  const graph = document.getElementById('contribution-graph');
  if (!graph) return;

  const firstDay = new Date(currentYear, 0, 1);
  const startDay = firstDay.getDay();
  const daysInYear = (currentYear % 4 === 0 && currentYear % 100 !== 0) || currentYear % 400 === 0 ? 366 : 365;
  const maxCount = Math.max(...Object.values(contributions), 1);

  let html = '<div class="graph-months">';
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  months.forEach((m, i) => {
    if (i === 0 || new Date(currentYear, i, 1).getDay() === 0) {
      html += '<span class="month-label">' + m + '</span>';
    }
  });
  html += '</div>';

  html += '<div class="graph-grid"><div class="graph-days">';
  for (let i = 0; i < startDay; i++) {
    html += '<div class="day-cell empty"></div>';
  }

  for (let day = 1; day <= daysInYear; day++) {
    const dateObj = new Date(currentYear, 0, day);
    const dateStr = dateObj.toISOString().split('T')[0];
    const count = contributions[dateStr] || 0;
    const level = count > 0 ? Math.ceil((count / maxCount) * 4) : 0;

    html += '<div class="day-cell level-' + level + '" data-date="' + dateStr + '" data-count="' + count + '" title="' + dateStr + ': ' + count + ' event(s)"></div>';

    if (dateObj.getDay() === 6 && day < daysInYear) {
      html += '</div><div class="graph-days">';
    }
  }
  html += '</div></div>';

  graph.innerHTML = html;
}

function updateStats(): void {
  const eventList = Array.isArray(events) ? events : [];
  const filtered = eventList.length > 0
    ? (currentMonth > 0 ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth) : eventList)
    : [];

  const totalEl = document.getElementById('total-events');
  if (totalEl) totalEl.textContent = filtered.length + ' events';

  const images = filtered.filter(e => e.media_type === 'image').length;
  const videos = filtered.filter(e => e.media_type === 'video').length;
  const audio = filtered.filter(e => e.media_type === 'audio').length;
  const locations = new Set(filtered.map(e => e.location).filter(l => l)).size;

  const statImages = document.getElementById('stat-images');
  const statVideos = document.getElementById('stat-videos');
  const statAudio = document.getElementById('stat-audio');
  const statLocations = document.getElementById('stat-locations');

  if (statImages) statImages.textContent = String(images);
  if (statVideos) statVideos.textContent = String(videos);
  if (statAudio) statAudio.textContent = String(audio);
  if (statLocations) statLocations.textContent = String(locations);
}

let selectedFilterTags: string[] = [];
let filterLocationTimeout: any = null;
let showFavoritesOnly: boolean = false;
let selectedCollectionId: string = '';
let collections: any[] = [];

function toggleFilters(): void {
  const panel = document.getElementById('advanced-filters');
  if (!panel) return;
  const isHidden = panel.style.display === 'none' || !panel.style.display;
  panel.style.display = isHidden ? 'block' : 'none';
}

function applyAdvancedFilters(): void {
  const q = (document.getElementById('search-input') as HTMLInputElement | null)?.value?.trim() || '';
  const personId = (document.getElementById('filter-person') as HTMLSelectElement | null)?.value || '';
  const location = (document.getElementById('filter-location') as HTMLInputElement | null)?.value?.trim() || '';
  const collectionId = (document.getElementById('filter-collection') as HTMLSelectElement | null)?.value || '';
  const mediaTypes: string[] = [];
  if ((document.getElementById('filter-media-image') as HTMLInputElement | null)?.checked) mediaTypes.push('image');
  if ((document.getElementById('filter-media-video') as HTMLInputElement | null)?.checked) mediaTypes.push('video');
  if ((document.getElementById('filter-media-audio') as HTMLInputElement | null)?.checked) mediaTypes.push('audio');

  selectedCollectionId = collectionId;

  // If a collection is selected, load collection events directly
  if (collectionId) {
    fetch('/api/collections/' + collectionId + '/events')
      .then(r => r.json())
      .then((data: any[]) => {
        if (Array.isArray(data)) {
          events = data;
          if (showFavoritesOnly) {
            events = events.filter(e => e.is_favorite);
          }
          renderTimeline();
          renderGallery();
          renderMapInstance();
          renderCalendar();
          updateStats();
          const status = document.getElementById('filter-status');
          if (status) status.textContent = events.length + ' results';
        }
      })
      .catch(() => {});
    return;
  }

  let url = '/api/events/search?year=' + currentYear;
  if (q) url += '&q=' + encodeURIComponent(q);
  if (personId) url += '&person_id=' + encodeURIComponent(personId);
  if (location) url += '&location=' + encodeURIComponent(location);

  if (selectedFilterTags.length > 0) {
    url += '&tag=' + encodeURIComponent(selectedFilterTags.join(','));
  }
  if (mediaTypes.length === 1) {
    url += '&media_type=' + encodeURIComponent(mediaTypes[0]);
  }

  const statusEl = document.getElementById('filter-status');
  if (statusEl) statusEl.textContent = 'Filtering...';

  fetch(url)
    .then(r => r.json())
    .then((data: any[]) => {
      if (Array.isArray(data)) {
        events = data;
        if (showFavoritesOnly) {
          events = events.filter(e => e.is_favorite);
        }
        renderTimeline();
        renderGallery();
        renderMapInstance();
        renderCalendar();
        updateStats();
        const status = document.getElementById('filter-status');
        if (status) status.textContent = events.length + ' results';
      }
    })
    .catch(() => {
      const status = document.getElementById('filter-status');
      if (status) status.textContent = 'Filter error';
    });
}

function clearAllFilters(): void {
  (document.getElementById('search-input') as HTMLInputElement).value = '';
  (document.getElementById('filter-person') as HTMLSelectElement).value = '';
  (document.getElementById('filter-location') as HTMLInputElement).value = '';
  (document.getElementById('filter-media-image') as HTMLInputElement).checked = false;
  (document.getElementById('filter-media-video') as HTMLInputElement).checked = false;
  (document.getElementById('filter-media-audio') as HTMLInputElement).checked = false;
  selectedFilterTags = [];
  renderSelectedFilterTags();
  loadData();
  const status = document.getElementById('filter-status');
  if (status) status.textContent = '';
}

function addFilterTag(tag: string): void {
  if (!selectedFilterTags.includes(tag)) {
    selectedFilterTags.push(tag);
    renderSelectedFilterTags();
    applyAdvancedFilters();
  }
  (document.getElementById('filter-tag-input') as HTMLInputElement).value = '';
}

function removeFilterTag(tag: string): void {
  selectedFilterTags = selectedFilterTags.filter(t => t !== tag);
  renderSelectedFilterTags();
  applyAdvancedFilters();
}

function renderSelectedFilterTags(): void {
  const container = document.getElementById('selected-filter-tags');
  if (!container) return;
  container.innerHTML = selectedFilterTags.map(t =>
    '<span class="filter-tag-badge" onclick="removeFilterTag(\'' + escapeHtml(t) + '\')">' + escapeHtml(t) + ' <i class="fa-solid fa-xmark"></i></span>'
  ).join('');
}

function filterTagInput(): void {
  const input = (document.getElementById('filter-tag-input') as HTMLInputElement).value;
  if (input.endsWith(',') || input.endsWith(' ')) {
    const tag = input.replace(/[, ]+$/, '').trim();
    if (tag) addFilterTag(tag);
  }
}

function filterLocationDebounce(): void {
  clearTimeout(filterLocationTimeout);
  filterLocationTimeout = setTimeout(applyAdvancedFilters, 400);
}

async function loadStatsDist(): Promise<void> {
  const container = document.getElementById('stats-distribution-container');
  if (!container) return;
  container.innerHTML = '<div class="text-center text-muted py-5"><i class="fa-solid fa-spinner fa-spin me-2"></i>Loading statistics...</div>';

  try {
    const res = await fetch('/api/stats/distribution?year=' + currentYear);
    if (!res.ok) throw new Error('Failed');
    const dist = await res.json();

    const monthNames_short = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    const maxMonth = Math.max(...(Object.values(dist.by_month) as number[]), 1);
    const monthBars = monthNames_short.map((m, i) => {
      const idx = String(i + 1).padStart(2, '0');
      const val = dist.by_month[idx] || 0;
      const pct = (val / maxMonth * 100).toFixed(0);
      return '<div class="d-flex flex-column align-items-center" style="flex:1">' +
        '<div class="stats-bar-value">' + val + '</div>' +
        '<div class="stats-bar" style="height:' + pct + '%" title="' + m + ': ' + val + ' events"></div>' +
        '<div class="stats-bar-label">' + m + '</div></div>';
    }).join('');

    const wdNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    const maxWD = Math.max(...(Object.values(dist.by_weekday) as number[]), 1);
    const wdBars = wdNames.map((d, i) => {
      const val = dist.by_weekday[String(i)] || 0;
      const pct = (val / maxWD * 100).toFixed(0);
      return '<div class="d-flex flex-column align-items-center" style="flex:1">' +
        '<div class="weekday-bar" style="height:' + pct + '%" title="' + d + ': ' + val + ' events"></div>' +
        '<div class="weekday-bar-label">' + d + '</div></div>';
    }).join('');

    let tagHTML = '';
    if (dist.by_tag && dist.by_tag.length > 0) {
      const totalTags = dist.by_tag.reduce((s: number, t: any) => s + t.count, 0);
      tagHTML = '<div class="tag-cloud">' + dist.by_tag.slice(0, 20).map((t: any) => {
        const pct = totalTags > 0 ? (t.count / totalTags * 100).toFixed(1) : '0';
        return '<span class="tag-cloud-item" title="' + t.count + ' events">' + escapeHtml(t.name) + ' (' + pct + '%)</span>';
      }).join('') + '</div>';
    }

    let personHTML = '';
    if (dist.by_person && dist.by_person.length > 0) {
      personHTML = dist.by_person.map((p: any) =>
        '<div class="d-flex justify-content-between align-items-center py-1"><span>' + escapeHtml(p.name) + '</span><span class="badge bg-primary">' + p.count + '</span></div>'
      ).join('');
    }

    let userHTML = '';
    if (dist.by_user && dist.by_user.length > 0) {
      userHTML = dist.by_user.map((u: any) =>
        '<div class="d-flex justify-content-between align-items-center py-1"><span>' + escapeHtml(u.display_name) + '</span><span class="badge bg-primary">' + u.count + '</span></div>'
      ).join('');
    }

    let locHTML = '';
    if (dist.by_location && dist.by_location.length > 0) {
      locHTML = dist.by_location.map((l: any) =>
        '<div class="d-flex justify-content-between align-items-center py-1"><span><i class="fa-solid fa-location-dot me-1"></i>' + escapeHtml(l.location) + '</span><span class="badge bg-secondary">' + l.count + '</span></div>'
      ).join('');
    }

    const topDayFormatted = dist.top_day ? new Date(dist.top_day + 'T12:00:00').toLocaleDateString('en-US', { month: 'long', day: 'numeric' }) : 'N/A';

    container.innerHTML =
      '<div class="row g-3">' +
      '<div class="col-12 mb-2"><div class="d-flex gap-3 flex-wrap">' +
      '<div class="dist-card" style="flex:1;min-width:150px"><h5><i class="fa-solid fa-calendar me-1"></i>Events</h5><div class="fs-4 fw-bold">' + dist.event_count + '</div><div class="text-muted small">' + dist.monthly_avg.toFixed(1) + '/mo · ' + dist.daily_avg.toFixed(2) + '/day</div></div>' +
      '<div class="dist-card" style="flex:1;min-width:150px"><h5><i class="fa-solid fa-star me-1"></i>Busiest Day</h5><div class="top-day-badge">' + topDayFormatted + '</div></div>' +
      '<div class="dist-card" style="flex:1;min-width:150px"><h5><i class="fa-solid fa-globe me-1"></i>Geo Spread</h5><div class="fs-5 fw-bold">' + (dist.geo_spread > 0 ? dist.geo_spread.toFixed(0) + ' km' : 'N/A') + '</div><div class="geo-spread">avg distance between locations</div></div>' +
      '</div></div>' +
      '<div class="col-12"><div class="dist-card"><h5><i class="fa-solid fa-chart-column me-1"></i>Events by Month</h5><div class="stats-bar-chart">' + monthBars + '</div></div></div>' +
      '<div class="col-12"><div class="dist-card"><h5><i class="fa-solid fa-calendar-week me-1"></i>Events by Day of Week</h5><div class="weekday-chart">' + wdBars + '</div></div></div>' +
      (tagHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-tags me-1"></i>Tag Distribution</h5>' + tagHTML + '</div></div>' : '') +
      (personHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-user-group me-1"></i>People</h5>' + personHTML + '</div></div>' : '') +
      (userHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-users me-1"></i>Family Members</h5>' + userHTML + '</div></div>' : '') +
      (locHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-map-pin me-1"></i>Top Locations</h5>' + locHTML + '</div></div>' : '') +
      '</div>';
  } catch (err) {
    container.innerHTML = '<div class="text-center text-muted py-5">Failed to load statistics</div>';
  }
}

async function loadEvents(): Promise<void> {
  try {
    let url = '/api/events?year=' + currentYear;
    if (currentMonth > 0) {
      url += '&month=' + String(currentMonth).padStart(2, '0');
    }
    const res = await fetch(url);
    if (res.status === 401) {
      // Not authenticated — fall back to public endpoint
      let pubUrl = '/api/public?year=' + currentYear;
      if (currentMonth > 0) {
        pubUrl += '&month=' + String(currentMonth).padStart(2, '0');
      }
      const pubRes = await fetch(pubUrl);
      if (pubRes.ok) {
        events = await pubRes.json() as TimelineEvent[];
      } else {
        events = [];
      }
    } else if (res.ok) {
      events = await res.json() as TimelineEvent[];
    } else {
      events = [];
    }
    if (!Array.isArray(events)) events = [];
    renderTimeline();
    renderGallery();
    renderMapInstance();
    renderCalendar();
  } catch (err) {
    console.error('Failed to load events:', err);
  }
}

async function loadUsers(): Promise<void> {
  try {
    const res = await fetch('/api/users');
    users = await res.json();
    if (!Array.isArray(users)) users = [];
  } catch (e) { users = []; }
}

async function toggleFav(id: number): Promise<void> {
  const csrf = await ensureCSRF();
  await fetch('/api/events/favorite', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' },
    body: JSON.stringify({ id })
  });
  await loadData();
  loadWrapped();
}

async function loadCollections(): Promise<void> {
  try {
    const res = await fetch('/api/collections');
    collections = await res.json();
    const sel = document.getElementById('filter-collection') as HTMLSelectElement;
    if (sel) {
      sel.innerHTML = '<option value="">All Collections</option>' + collections.map((c: any) => `<option value="${c.id}">${escapeHtml(c.name)}</option>`).join('');
    }
  } catch (e) { collections = []; }
}

function toggleFavFilter(): void {
  showFavoritesOnly = !showFavoritesOnly;
  const btn = document.getElementById('fav-filter-btn');
  if (btn) btn.classList.toggle('btn-primary', showFavoritesOnly);
  if (btn) btn.classList.toggle('btn-outline-primary', !showFavoritesOnly);
  applyAdvancedFilters();
}

async function filterByCollection(): Promise<void> {
  const sel = document.getElementById('filter-collection') as HTMLSelectElement;
  selectedCollectionId = sel?.value || '';
  if (!selectedCollectionId) {
    await loadData();
    return;
  }
  try {
    const res = await fetch('/api/collections/' + selectedCollectionId + '/events');
    events = await res.json();
    if (!Array.isArray(events)) events = [];
    renderTimeline();
    renderGallery();
    renderMapInstance();
    renderCalendar();
    updateStats();
  } catch (e) { console.error('Filter by collection failed', e); }
}

async function ensureCSRF(): Promise<string> {
  try {
    const res = await fetch('/api/csrf-token');
    if (res.ok) {
      const data = await res.json();
      return data.token;
    }
  } catch (e) {}
  return '';
}

async function loadData(): Promise<void> {
  await Promise.all([loadEvents(), loadContributions(), loadUsers(), loadCollections()]);
  populateYearButtons();
  galleryPage = 1;
}

function populateYearButtons(): void {
  const container = document.getElementById('year-buttons');
  if (!container) return;
  const years = new Set<number>();
  events.forEach(e => {
    const y = parseInt(e.date.slice(0, 4));
    if (!isNaN(y)) years.add(y);
  });
  years.add(currentYear);
  const sorted = Array.from(years).sort((a, b) => b - a);
  container.innerHTML = sorted.map(y =>
    `<button class="btn ${y === currentYear ? 'btn-primary' : 'btn-outline-primary'}" data-year="${y}" onclick="changeYear(${y})">${y}</button>`
  ).join('');
}

function renderTimeline(): void {
  const container = document.getElementById('timeline-container');
  if (!container) return;
  const eventList = Array.isArray(events) ? events : [];

  if (eventList.length === 0) {
    container.innerHTML = `
      <div class="text-center text-muted py-5">
        <i class="fa-solid fa-clock fa-3x mb-3"></i>
        <p>No events found for ${currentYear}.</p>
      </div>
    `;
    return;
  }

  const filteredEvents = currentMonth > 0
    ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
    : eventList;

  if (filteredEvents.length === 0) {
    container.innerHTML = `
      <div class="text-center text-muted py-5">
        <i class="fa-solid fa-clock fa-3x mb-3"></i>
        <p>No events found for ${currentYear}.</p>
      </div>
    `;
    return;
  }

  let currentMonthIdx = -1;
  let html = '';

  filteredEvents.forEach((e, index) => {
    const eventDate = new Date(e.date);
    const month = eventDate.getMonth();

    if (month !== currentMonthIdx) {
      currentMonthIdx = month;
      html += `
        <div class="timeline-month-label">
          <span class="badge bg-primary">${monthNames[month]}</span>
        </div>
      `;
    }

    const mediaIcon = getMediaIcon(e.media_type);
    const hasMedia = e.media_url;
    const tagList = e.tags ? e.tags.split(',').map(t => t.trim()).filter(t => t) : [];

    let thumbHtml = '';
    if (hasMedia) {
      const thumbUrl = e.thumbnail || e.media_url;
      if (e.media_type === 'video') {
        thumbHtml = '<div class="timeline-thumb"><video src="' + thumbUrl + '" muted></video></div>';
      } else if (e.media_type === 'audio') {
        thumbHtml = '<div class="timeline-thumb"><div class="audio-placeholder-sm"><i class="fa-solid fa-music fa-2x"></i></div></div>';
      } else {
        thumbHtml = '<div class="timeline-thumb"><img src="' + thumbUrl + '" alt="' + escapeHtml(e.title) + '"></div>';
      }
    }

    let weatherHtml = '';
    if (e.weather_data) {
      try {
        const w = JSON.parse(e.weather_data) as Weather;
        weatherHtml = `<span class="weather-badge ms-2"><i class="fa-solid fa-${w.icon}"></i> ${Math.round(w.temperature)}°C ${w.condition}</span>`;
      } catch (_) {}
    }

    let userHtml = '';
    if (e.user_id && users.length) {
      const u = users.find(u => u.id === e.user_id);
      if (u) {
        userHtml = `<span class="user-badge ms-1" style="background:${u.color || '#7c3aed'}"><i class="fa-solid fa-user"></i> ${escapeHtml(u.display_name || u.username)}</span>`;
      }
    }

    const recurringBadge = e.recurring ? `<span class="badge bg-info ms-1"><i class="fa-solid fa-rotate"></i> ${e.recurring}</span>` : '';

    const favStar = `<i class="${e.is_favorite ? 'fa-solid' : 'fa-regular'} fa-star text-warning" style="cursor:pointer;font-size:0.85rem" onclick="event.stopPropagation();toggleFav(${e.id})" title="${e.is_favorite ? 'Unfavorite' : 'Favorite'}"></i>`;

    html += `
      <div class="timeline-item ${index % 2 === 0 ? 'left' : 'right'}" id="event-${e.id}">
        <div class="timeline-content" onclick="${hasMedia ? 'showMedia(' + e.id + ')' : ''}">
          <div class="d-flex justify-content-between align-items-start">
            <div class="timeline-date">${formatDate(e.date)}${e.start_time ? ' <i class="fa-regular fa-clock ms-1"></i>' + e.start_time.substring(0, 5) : ''}${e.end_time ? '–' + e.end_time.substring(0, 5) : ''} ${recurringBadge}</div>
            ${favStar}
          </div>
          <div class="timeline-title">${escapeHtml(e.title)} ${weatherHtml}</div>
          <div class="timeline-location"><i class="fa-solid fa-location-dot me-1"></i>${escapeHtml(e.location)} ${userHtml}</div>
          ${tagList.length > 0 ? '<div class="timeline-people mb-2"><i class="fa-solid fa-tags me-1"></i>' + tagList.map(t => '<span class="badge bg-secondary me-1">' + escapeHtml(t) + '</span>').join('') + '</div>' : ''}
          ${thumbHtml}
          ${e.description ? '<div class="timeline-desc md-content">' + renderMarkdown(e.description) + '</div>' : ''}
          ${hasMedia ? '<div class="timeline-media-badge"><i class="' + mediaIcon + '"></i> ' + e.media_type + '</div>' : ''}
        </div>
      </div>
    `;
  });

  container.innerHTML = html;
}

function renderMarkdown(text: string): string {
  if (!text) return '';
  let html = escapeHtml(text);
  html = html.replace(/### (.+)/g, '<h3>$1</h3>');
  html = html.replace(/## (.+)/g, '<h2>$1</h2>');
  html = html.replace(/# (.+)/g, '<h1>$1</h1>');
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
  html = html.replace(/\*(.+?)\*/g, '<em>$1</em>');
  html = html.replace(/`(.+?)`/g, '<code>$1</code>');
  html = html.replace(/^> (.+)$/gm, '<blockquote>$1</blockquote>');
  html = html.replace(/^- (.+)$/gm, '<li>$1</li>');
  html = html.replace(/(<li>.*<\/li>\n?)/s, '<ul>$1</ul>');
  html = html.replace(/\[(.+?)\]\((.+?)\)/g, '<a href="$2" target="_blank">$1</a>');
  html = html.replace(/\n/g, '<br>');
  return html;
}

function getMediaIcon(mediaType: string): string {
  switch (mediaType) {
    case 'video': return 'fa-solid fa-video';
    case 'audio': return 'fa-solid fa-music';
    default: return 'fa-solid fa-image';
  }
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr);
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

function renderGallery(): void {
  const container = document.getElementById('gallery-container');
  if (!container) return;
  const eventList = Array.isArray(events) ? events : [];

  if (eventList.length === 0) {
    container.innerHTML = `
      <div class="col-12 text-center text-muted py-5">
        <i class="fa-solid fa-images fa-3x mb-3"></i>
        <p>No media found for ${currentYear}.</p>
      </div>
    `;
    return;
  }

  const filtered = currentMonth > 0
    ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
    : eventList;
  const mediaEvents = filtered.filter(e => e.media_url);

  if (mediaEvents.length === 0) {
    container.innerHTML = `
      <div class="col-12 text-center text-muted py-5">
        <i class="fa-solid fa-images fa-3x mb-3"></i>
        <p>No media found for ${currentYear}.</p>
      </div>
    `;
    return;
  }

  container.innerHTML = mediaEvents.slice(0, galleryLimit).map(e => {
    return `
      <div class="col-md-4 col-lg-3">
        <div class="gallery-item" onclick="showMedia(${e.id})">
          ${e.media_type === 'video'
            ? '<video src="' + e.media_url + '" muted></video>'
            : e.media_type === 'audio'
            ? '<div class="audio-placeholder"><i class="fa-solid fa-music fa-3x"></i></div>'
            : '<img src="' + (e.thumbnail || e.media_url) + '" alt="' + escapeHtml(e.title) + '">'
          }
          <div class="gallery-overlay">
            <i class="${getMediaIcon(e.media_type)}"></i>
          </div>
        </div>
        <div class="mt-2 text-center small">${escapeHtml(e.title)}</div>
      </div>
    `;
  }).join('');

  const loadMoreBtn = document.getElementById('load-more-btn');
  if (loadMoreBtn) {
    loadMoreBtn.style.display = mediaEvents.length > galleryLimit ? '' : 'none';
  }
}

function renderMapInstance(): void {
  const eventList = Array.isArray(events) ? events : [];
  const filtered = currentMonth > 0
    ? eventList.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
    : eventList;
  const geoEvents = filtered.filter(e => e.latitude && e.longitude && (e.latitude !== 0 || e.longitude !== 0));

  const placeholder = document.getElementById('map-placeholder');
  if (placeholder) placeholder.style.display = geoEvents.length ? 'none' : 'block';

  if (!geoEvents.length) {
    if (mapInstance) mapInstance.remove();
    mapInstance = null;
    mapMarkers = [];
    return;
  }

  if (!mapInstance) {
    mapInstance = L.map('map-container').setView([20, 0], 2);
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
      maxZoom: 18
    }).addTo(mapInstance);
  }

  mapMarkers.forEach(m => mapInstance.removeLayer(m));
  mapMarkers = [];

  const bounds: [number, number][] = [];
  const markerIcon = L.divIcon({
    className: 'custom-marker',
    html: '<i class="fa-solid fa-map-pin" style="color:#7c3aed;font-size:24px;"></i>',
    iconSize: [24, 24],
    iconAnchor: [12, 24],
    popupAnchor: [0, -24]
  });

  geoEvents.forEach(e => {
    const m = L.marker([e.latitude!, e.longitude!], { icon: markerIcon }).addTo(mapInstance);
    let weatherHtml = '';
    if (e.weather_data) {
      try {
        const w = JSON.parse(e.weather_data);
        weatherHtml = `<br><small><i class="fa-solid fa-${w.icon}"></i> ${Math.round(w.temperature)}°C ${w.condition}</small>`;
      } catch (_) { }
    }
    m.bindPopup(`
      <div class="map-popup">
        <h6>${escapeHtml(e.title)}</h6>
        <p>${formatDate(e.date)} — ${escapeHtml(e.location)}${weatherHtml}</p>
      </div>
    `);
    mapMarkers.push(m);
    bounds.push([e.latitude!, e.longitude!]);
  });

  const showPath = (document.getElementById('show-location-path') as HTMLInputElement)?.checked;
  if (mapPathLine) {
    mapInstance.removeLayer(mapPathLine);
    mapPathLine = null;
  }
  if (showPath && geoEvents.length > 1) {
    const sorted = [...geoEvents].sort((a, b) => a.date.localeCompare(b.date));
    const latlngs: [number, number][] = sorted.map(e => [e.latitude!, e.longitude!]);
    mapPathLine = L.polyline(latlngs, {
      color: '#7c3aed',
      weight: 3,
      opacity: 0.6,
      dashArray: '8, 8'
    }).addTo(mapInstance);
  }

  if (bounds.length > 0) {
    mapInstance.fitBounds(bounds, { padding: [30, 30], maxZoom: 14 });
  }

  setTimeout(() => mapInstance.invalidateSize(), 100);
}

function showMedia(id: number): void {
  const event = Array.isArray(events) ? events.find(e => e.id === id) : null;
  if (!event) return;

  const titleEl = document.getElementById('mediaModalTitle');
  const descEl = document.getElementById('mediaModalDesc');
  const bodyEl = document.getElementById('mediaModalBody');

  if (titleEl) titleEl.textContent = event.title;
  if (descEl) {
    descEl.innerHTML = `
      ${event.description ? renderMarkdown(event.description) : ''}
      <div class="mt-2 small opacity-75">
        <i class="fa-solid fa-calendar me-1"></i>${event.date}${event.start_time ? ' <i class="fa-regular fa-clock ms-2"></i>' + event.start_time.substring(0, 5) : ''}${event.end_time ? '–' + event.end_time.substring(0, 5) : ''}
        ${event.location ? '<i class="fa-solid fa-location-dot ms-2 me-1"></i>' + event.location : ''}
      </div>
    `;
  }

  let mediaHtml = '';
  if (event.media_url) {
    if (event.media_type === 'video') {
      mediaHtml = '<video controls class="w-100" src="' + event.media_url + '"></video>';
    } else if (event.media_type === 'audio') {
      mediaHtml = '<audio controls class="w-100" src="' + event.media_url + '"></audio>';
    } else {
      mediaHtml = '<img src="' + event.media_url + '" class="img-fluid" alt="' + event.title + '">';
    }
  }

  if (bodyEl) bodyEl.innerHTML = mediaHtml || '<p>No media available</p>';
  new (window as any).bootstrap.Modal(document.getElementById('mediaModal')).show();
}

function escapeHtml(text: string): string {
  if (!text) return '';
  return text.replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

async function loadMoreGallery(): Promise<void> {
  galleryPage++;
  const skip = (galleryPage - 1) * galleryLimit;
  try {
    const url = '/api/events?year=' + currentYear + '&limit=' + galleryLimit + '&skip=' + skip;
    const res = await fetch(url);
    let moreEvents: TimelineEvent[];
    if (!res.ok) {
      moreEvents = [];
    } else {
      moreEvents = await res.json() as TimelineEvent[];
      if (!Array.isArray(moreEvents)) moreEvents = [];
    }

    const container = document.getElementById('gallery-container');
    const mediaEvents = moreEvents.filter(e => e.media_url);

    if (mediaEvents.length === 0) {
      const btn = document.getElementById('load-more-btn');
      if (btn) btn.style.display = 'none';
      return;
    }

    if (container) {
      container.innerHTML += mediaEvents.map(e => `
        <div class="col-md-4 col-lg-3">
          <div class="gallery-item" onclick="showMedia(${e.id})">
            ${e.media_type === 'video'
              ? '<video src="' + e.media_url + '" muted></video>'
              : e.media_type === 'audio'
              ? '<div class="audio-placeholder"><i class="fa-solid fa-music fa-3x"></i></div>'
              : '<img src="' + (e.thumbnail || e.media_url) + '" alt="' + escapeHtml(e.title) + '">'
            }
            <div class="gallery-overlay">
              <i class="${getMediaIcon(e.media_type)}"></i>
            </div>
          </div>
          <div class="mt-2 text-center small">${escapeHtml(e.title)}</div>
        </div>
      `).join('');
    }

    events = [...events, ...moreEvents];
  } catch (err) {
    console.error('Failed to load more gallery:', err);
  }
}

async function getYearStats(year: string): Promise<any> {
  try {
    const res = await fetch('/api/events?year=' + year);
    const yearEvents = await res.json() as TimelineEvent[];
    return {
      total: yearEvents.length,
      images: yearEvents.filter((e: TimelineEvent) => e.media_type === 'image').length,
      videos: yearEvents.filter((e: TimelineEvent) => e.media_type === 'video').length,
      audio: yearEvents.filter((e: TimelineEvent) => e.media_type === 'audio').length,
      locations: new Set(yearEvents.map((e: TimelineEvent) => e.location).filter(l => l)).size
    };
  } catch (err) {
    return { total: 0, images: 0, videos: 0, audio: 0, locations: 0 };
  }
}

async function updateCompare(): Promise<void> {
  const year1El = document.getElementById('compare-year-1') as HTMLSelectElement | null;
  const year2El = document.getElementById('compare-year-2') as HTMLSelectElement | null;
  if (!year1El || !year2El) return;

  const year1 = year1El.value;
  const year2 = year2El.value;

  const [stats1, stats2] = await Promise.all([getYearStats(year1), getYearStats(year2)]);

  const stats1El = document.getElementById('compare-stats-1');
  const stats2El = document.getElementById('compare-stats-2');
  const resultEl = document.getElementById('compare-result');

  if (stats1El) stats1El.innerHTML = renderCompareStats(stats1);
  if (stats2El) stats2El.innerHTML = renderCompareStats(stats2);

  const diff = stats2.total - stats1.total;
  const trendColor = diff > 0 ? 'text-success' : diff < 0 ? 'text-danger' : 'text-muted';
  const trendIcon = diff > 0 ? 'fa-arrow-trend-up' : diff < 0 ? 'fa-arrow-trend-down' : 'fa-minus';

  if (resultEl) {
    resultEl.innerHTML = `
      <div class="text-center">
        <div class="mb-2"><i class="fa-solid ${trendIcon} fa-3x ${trendColor}"></i></div>
        <div class="h4 ${trendColor}">${diff > 0 ? '+' : ''}${diff}</div>
        <div class="small text-muted">events difference</div>
        <div class="mt-3">
          <strong>${year1}</strong>: ${stats1.total} events<br>
          <strong>${year2}</strong>: ${stats2.total} events
        </div>
      </div>
    `;
  }
}

function renderCompareStats(stats: any): string {
  return `
    <div class="small">
      <div><strong>Total:</strong> ${stats.total} events</div>
      <div><i class="fa-solid fa-image me-1"></i>${stats.images}</div>
      <div><i class="fa-solid fa-video me-1"></i>${stats.videos}</div>
      <div><i class="fa-solid fa-music me-1"></i>${stats.audio}</div>
      <div><i class="fa-solid fa-location-dot me-1"></i>${stats.locations} locations</div>
    </div>
  `;
}

let calendarYear: number = new Date().getFullYear();
let calendarMonth: number = new Date().getMonth() + 1;
let calendarEventList: TimelineEvent[] = [];

function renderCalendar(): void {
  const filtered = Array.isArray(events) ? events : [];
  calendarEventList = filtered;
}

function renderCalendarView(): void {
  const grid = document.getElementById('calendar-grid');
  const title = document.getElementById('calendar-title');
  if (!grid || !title) return;

  title.textContent = monthNames[calendarMonth - 1] + ' ' + calendarYear;

  const firstDay = new Date(calendarYear, calendarMonth - 1, 1);
  const lastDay = new Date(calendarYear, calendarMonth, 0);
  const startDay = firstDay.getDay();
  const daysInMonth = lastDay.getDate();

  const prevMonth = new Date(calendarYear, calendarMonth - 1, 0);
  const daysInPrevMonth = prevMonth.getDate();

  let html = '';
  const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  dayNames.forEach(d => {
    html += '<div class="calendar-header">' + d + '</div>';
  });

  for (let i = startDay - 1; i >= 0; i--) {
    const day = daysInPrevMonth - i;
    html += '<div class="calendar-day other-month"><span class="day-num">' + day + '</span></div>';
  }

  const today = new Date();
  const todayStr = today.toISOString().split('T')[0];

  for (let day = 1; day <= daysInMonth; day++) {
    const dateStr = calendarYear + '-' + String(calendarMonth).padStart(2, '0') + '-' + String(day).padStart(2, '0');
    const dayEvents = calendarEventList.filter(e => e.date === dateStr);
    const isToday = dateStr === todayStr;

    html += '<div class="calendar-day' + (isToday ? ' today' : '') + '" onclick="showCalendarDay(\'' + dateStr + '\')">';
    html += '<span class="day-num">' + day + '</span>';
    if (dayEvents.length > 0) {
      html += '<div class="event-dots">';
      dayEvents.slice(0, 5).forEach(() => {
        html += '<span class="event-dot"></span>';
      });
      if (dayEvents.length > 5) {
        html += '<span class="event-dot" style="background:var(--text-muted)"></span>';
      }
      html += '</div>';
    }
    html += '</div>';
  }

  const remainingCells = 7 - ((startDay + daysInMonth) % 7);
  if (remainingCells < 7) {
    for (let day = 1; day <= remainingCells; day++) {
      html += '<div class="calendar-day other-month"><span class="day-num">' + day + '</span></div>';
    }
  }

  grid.innerHTML = html;
}

function calendarPrevMonth(): void {
  calendarMonth--;
  if (calendarMonth < 1) { calendarMonth = 12; calendarYear--; }
  renderCalendarView();
}

function calendarNextMonth(): void {
  calendarMonth++;
  if (calendarMonth > 12) { calendarMonth = 1; calendarYear++; }
  renderCalendarView();
}

function calendarToday(): void {
  calendarYear = new Date().getFullYear();
  calendarMonth = new Date().getMonth() + 1;
  renderCalendarView();
}

function showCalendarDay(dateStr: string): void {
  const dayEvents = calendarEventList.filter(e => e.date === dateStr);
  const section = document.getElementById('calendar-selected-day');
  const dateEl = document.getElementById('calendar-selected-date');
  const listEl = document.getElementById('calendar-event-list');
  if (!section || !dateEl || !listEl) return;

  const d = new Date(dateStr + 'T12:00:00');
  dateEl.textContent = d.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' });

  if (dayEvents.length === 0) {
    listEl.innerHTML = '<p class="text-muted text-center py-3">No events on this day</p>';
  } else {
    listEl.innerHTML = dayEvents.map(e => {
      const mediaIcon = getMediaIcon(e.media_type);
      return '<div class="calendar-event-item" onclick="showMedia(' + e.id + ')">'
        + '<div class="fw-bold">' + escapeHtml(e.title) + '</div>'
        + '<div class="text-muted small">' + escapeHtml(e.location)
        + (e.media_url ? ' <i class="' + mediaIcon + ' ms-1"></i>' : '')
        + '</div>'
        + '</div>';
    }).join('');
  }
  section.style.display = 'block';
}

async function loadMemories(): Promise<void> {
  try {
    const res = await fetch('/api/memories');
    const memories = await res.json();
    const section = document.getElementById('memories-section');
    if (!section) return;
    if (!memories || !memories.length) { section.style.display = 'none'; return; }
    memories.sort((a: any, b: any) => a.years_ago - b.years_ago);
    section.style.display = 'block';
    section.innerHTML = '<h5 class="mb-3"><i class="fa-solid fa-clock-rotate-left me-2 text-primary"></i>On This Day</h5>'
      + memories.map((m: any) => '<div class="memories-item"><div class="fw-bold">' + escapeHtml(m.title) + '</div><div class="small text-muted">' + m.years_ago + ' year' + (m.years_ago > 1 ? 's' : '') + ' ago &middot; ' + m.date + '</div></div>').join('');
  } catch (_) { }
}

async function loadWrapped(): Promise<void> {
  const container = document.getElementById('wrapped-content');
  if (!container) return;
  try {
    const res = await fetch('/api/wrapped?year=' + currentYear);
    if (!res.ok) { container.innerHTML = '<div class="text-center text-muted py-5">Failed to load wrapped data</div>'; return; }
    const w = await res.json();

    const monthNames = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    const maxMonth = Math.max(...(Object.values(w.by_month) as number[]), 1);
    const monthBars = monthNames.map((m, i) => {
      const idx = String(i + 1).padStart(2, '0');
      const val = w.by_month[idx] || 0;
      const pct = (val / maxMonth * 100).toFixed(0);
      return '<div class="d-flex flex-column align-items-center" style="flex:1"><div class="stats-bar-value">' + val + '</div><div class="stats-bar" style="height:' + pct + '%;background:var(--primary);border-radius:4px 4px 0 0" title="' + m + ': ' + val + '"></div><div class="stats-bar-label">' + m + '</div></div>';
    }).join('');

    container.innerHTML = `
      <div class="text-center mb-4">
        <h3 class="fw-bold"><i class="fa-solid fa-gift me-2 text-primary"></i>${w.year} Wrapped</h3>
        <p class="text-muted">Your year in review</p>
      </div>
      <div class="row g-3 mb-4">
        <div class="col-md-3"><div class="dist-card text-center"><h5>Total Events</h5><div class="fs-2 fw-bold text-primary">${w.total_events}</div></div></div>
        <div class="col-md-3"><div class="dist-card text-center"><h5>Longest Streak</h5><div class="fs-2 fw-bold text-primary">${w.longest_streak} days</div></div></div>
        <div class="col-md-3"><div class="dist-card text-center"><h5>Favorites</h5><div class="fs-2 fw-bold text-warning">${w.favorite_count}</div></div></div>
        <div class="col-md-3"><div class="dist-card text-center"><h5>Media Items</h5><div class="fs-2 fw-bold text-primary">${w.total_media}</div></div></div>
      </div>
      <div class="row g-3 mb-4">
        <div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-star me-2 text-warning"></i>Busiest Month</h5><div class="fs-4 fw-bold">${w.busiest_month || 'N/A'}</div><div class="text-muted">${w.busiest_month_count || 0} events</div></div></div>
        <div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-pen me-2"></i>Most Detailed Event</h5><div class="fw-bold">${w.top_event ? escapeHtml(w.top_event) : 'N/A'}</div>${w.top_event_date ? '<div class="text-muted small">' + w.top_event_date + '</div>' : ''}</div></div>
      </div>
      ${w.most_tags_title ? '<div class="row g-3 mb-4"><div class="col-12"><div class="dist-card"><h5><i class="fa-solid fa-tags me-2"></i>Most Tagged Event</h5><div class="fw-bold">' + escapeHtml(w.most_tags_title) + '</div><div class="text-muted">' + w.most_tags_count + ' tags</div></div></div></div>' : ''}
      <div class="dist-card">
        <h5><i class="fa-solid fa-chart-column me-2"></i>Events by Month</h5>
        <div class="stats-bar-chart" style="display:flex;align-items:flex-end;gap:6px;height:150px;padding-top:8px">${monthBars}</div>
      </div>
    `;
  } catch (e) {
    container.innerHTML = '<div class="text-center text-muted py-5">Failed to load wrapped data</div>';
  }
}

function initApp(): void {
  initTheme();

  const params = new URLSearchParams(window.location.search);
  const q = params.get('q');
  if (q) {
    const input = document.getElementById('search-input') as HTMLInputElement | null;
    if (input) input.value = q;
  }

  loadData();
  loadMemories();
  updateCompare();
  renderCalendarView();
  loadPersonsForFilter();

  const statsTab = document.getElementById('stats-tab');
  if (statsTab) {
    statsTab.addEventListener('shown.bs.tab', () => { loadStatsDist(); });
    statsTab.addEventListener('click', () => { loadStatsDist(); });
  }

  const wrappedTab = document.getElementById('wrapped-tab');
  if (wrappedTab) {
    wrappedTab.addEventListener('shown.bs.tab', () => { loadWrapped(); });
    wrappedTab.addEventListener('click', () => { loadWrapped(); });
  }

  const compareYear1 = document.getElementById('compare-year-1') as HTMLSelectElement | null;
  const compareYear2 = document.getElementById('compare-year-2') as HTMLSelectElement | null;
  if (compareYear1 && compareYear2) {
    const y = new Date().getFullYear();
    for (let i = y + 1; i >= y - 10; i--) {
      const o1 = document.createElement('option');
      o1.value = String(i); o1.textContent = String(i);
      if (i === y) o1.selected = true;
      compareYear1.appendChild(o1);
      const o2 = document.createElement('option');
      o2.value = String(i); o2.textContent = String(i);
      if (i === y - 1) o2.selected = true;
      compareYear2.appendChild(o2);
    }
  }

  fetch('/api/version').then(r => r.json()).then(d => {
    const versionEl = document.getElementById('version-display');
    if (versionEl) versionEl.textContent = 'v' + d.version;
  }).catch(() => {
    const versionEl = document.getElementById('version-display');
    if (versionEl) versionEl.textContent = 'v1.0.0';
  });
}

async function loadPersonsForFilter(): Promise<void> {
  try {
    const res = await fetch('/api/persons');
    const persons = await res.json();
    if (!Array.isArray(persons)) return;
    const select = document.getElementById('filter-person') as HTMLSelectElement;
    if (!select) return;
    select.innerHTML = '<option value="">Any person</option>';
    persons.forEach((p: any) => {
      const opt = document.createElement('option');
      opt.value = p.id;
      opt.textContent = p.name;
      select.appendChild(opt);
    });
  } catch (_) { }
}

document.addEventListener('DOMContentLoaded', initApp);

(window as any).changeYear = changeYear;
(window as any).searchEvents = searchEvents;
(window as any).filterMonth = filterMonth;
(window as any).showMedia = showMedia;
(window as any).loadMoreGallery = loadMoreGallery;
(window as any).calendarPrevMonth = calendarPrevMonth;
(window as any).calendarNextMonth = calendarNextMonth;
(window as any).calendarToday = calendarToday;
(window as any).showCalendarDay = showCalendarDay;
(window as any).updateCompare = updateCompare;
(window as any).toggleFilters = toggleFilters;
(window as any).applyAdvancedFilters = applyAdvancedFilters;
(window as any).clearAllFilters = clearAllFilters;
(window as any).addFilterTag = addFilterTag;
(window as any).removeFilterTag = removeFilterTag;
(window as any).filterTagInput = filterTagInput;
(window as any).filterLocationDebounce = filterLocationDebounce;
(window as any).loadStatsDist = loadStatsDist;
(window as any).globalSearchInput = globalSearchInput;
(window as any).globalSearchKeydown = globalSearchKeydown;
(window as any).globalSearchFocus = globalSearchFocus;
(window as any).selectGlobalResult = selectGlobalResult;
(window as any).highlightGlobalItem = highlightGlobalItem;
(window as any).toggleFav = toggleFav;
(window as any).toggleFavFilter = toggleFavFilter;
(window as any).filterByCollection = filterByCollection;
(window as any).loadWrapped = loadWrapped;
