export {};
declare const L: any;
import { escapeHtml, getMediaIcon, renderMarkdown, formatDate, weatherIconClass } from "./shared/format.js";
import { ensureCSRF } from "./shared/api.js";
import { loadAnalytics } from "./shared/analytics.js";
import { MAP_TILE_URL, MAP_TILE_OPTS } from "./shared/map.js";
import type { TimelineEvent, CalendarDay, ContributionMap, Weather } from "./shared/types.js";

type Theme = 'light' | 'dark';

let currentYear: number = new Date().getFullYear();
let currentMonth: number = 0;
// Phase 4: split view-vs-filter state — viewedYear follows scroll, filterYear/Month drive what is shown
let viewedYear: number = currentYear;
let filterYear: number | null = null;
let activeFilterMonth: number = 0;
// `allEvents` holds the full multi-year story; `events` is the currently visible (filtered) set.
let allEvents: TimelineEvent[] = [];
let events: TimelineEvent[] = [];
let contributions: ContributionMap = {};
let storyChunk: number = 1;
const storyChunkSize: number = 60;
let storyYearObserver: IntersectionObserver | null = null;
let storySentinelObserver: IntersectionObserver | null = null;
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
  if (themeToggle) themeToggle.setAttribute('aria-pressed', theme === 'dark' ? 'true' : 'false');
}

let trapOpener: HTMLElement | null = null;
let trapContainer: HTMLElement | null = null;
let trapHandler: ((e: KeyboardEvent) => void) | null = null;

function getFocusable(container: HTMLElement): HTMLElement[] {
  const sel = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
  return Array.from(container.querySelectorAll(sel) as NodeListOf<HTMLElement>).filter(el => {
    const style = window.getComputedStyle(el);
    return (style.display !== 'none' && style.visibility !== 'hidden' && (el as HTMLElement).offsetParent !== null) || el === document.activeElement;
  }) as HTMLElement[];
}

function trapFocus(container: HTMLElement): void {
  releaseFocus();
  trapOpener = document.activeElement as HTMLElement;
  trapContainer = container;
  const focusable = getFocusable(container);
  const toFocus = focusable[0] || container;
  // ensure container can receive focus if no child
  if (!focusable.length) {
    container.setAttribute('tabindex', '-1');
  }
  toFocus.focus();
  trapHandler = (e: KeyboardEvent) => {
    if (e.key !== 'Tab' || !trapContainer) return;
    const els = getFocusable(trapContainer);
    if (els.length === 0) { e.preventDefault(); return; }
    const first = els[0];
    const last = els[els.length - 1];
    if (e.shiftKey) {
      if (document.activeElement === first) { e.preventDefault(); last.focus(); }
    } else {
      if (document.activeElement === last) { e.preventDefault(); first.focus(); }
    }
  };
  container.addEventListener('keydown', trapHandler);
}

function releaseFocus(): void {
  if (trapContainer && trapHandler) trapContainer.removeEventListener('keydown', trapHandler);
  const opener = trapOpener;
  trapContainer = null;
  trapHandler = null;
  trapOpener = null;
  // restore focus after trap released
  if (opener && typeof opener.focus === 'function') {
    // restore on next tick to avoid focus being stolen by hide animation
    setTimeout(() => opener.focus(), 0);
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

function setViewedYearOnly(year: number): void {
  viewedYear = year;
  currentYear = year;
  const yearEl = document.getElementById('current-year');
  if (yearEl) yearEl.textContent = String(year);
  document.querySelectorAll('#year-buttons .btn[data-year]').forEach(b => {
    const active = b.getAttribute('data-year') === String(year);
    b.classList.toggle('btn-primary', active);
    b.classList.toggle('btn-outline-primary', !active);
    if (active) b.setAttribute('aria-current', 'true');
    else b.removeAttribute('aria-current');
  });
  const icsLink = document.getElementById('ics-download') as HTMLAnchorElement;
  if (icsLink) icsLink.href = '/api/events/ics?year=' + year;
}

function scrollToYear(year: number): void {
  setViewedYearOnly(year);
  const marker = document.getElementById('year-' + year);
  if (marker) {
    marker.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }
  // deliberately no fetch — year chips are scroll-aids (Phase 4 decision)
}

function changeYear(year: number): void {
  // Back-compat: treat as scroll-aid, no filter refetch
  scrollToYear(year);
}

function searchEvents(): void {
  // Unified search: typing already live-filters; lens path is removed. Keep as no-op
  // for back-compat (tests / inline handlers) — just ensure dropdown state is consistent.
  hideGlobalDropdown();
}

function updateGlobalDropdown(): void {
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  const dropdown = document.getElementById('global-search-dropdown');
  if (!dropdown || !input) return;
  const q = input.value.trim();
  if (!q) { hideGlobalDropdown(); return; }
  const matches = filteredEvents();
  if (matches.length === 0) {
    dropdown.innerHTML = '<div class="global-search-no-results" role="option" aria-selected="false">No matches found</div>';
    dropdown.style.display = 'block';
    input.setAttribute('aria-expanded', 'true');
    input.removeAttribute('aria-activedescendant');
    globalSearchIndex = -1;
  } else {
    const items = matches.slice(0, 8);
    dropdown.innerHTML = items.map((e: any, i: number) => {
      const year = e.date ? e.date.slice(0, 4) : '';
      const loc = e.location ? ' <span class="search-year"><i class="fa-solid fa-location-dot"></i> ' + escapeHtml(e.location) + '</span>' : '';
      return '<div class="global-search-item" id="search-option-' + i + '" role="option" aria-selected="false" data-id="' + e.id + '" onclick="selectGlobalResult(' + e.id + ')" onmouseenter="highlightGlobalItem(' + i + ')">' +
        '<div class="fw-bold">' + escapeHtml(e.title) + '</div>' +
        '<div class="search-year">' + year + loc + '</div>' +
        '</div>';
    }).join('');
    dropdown.style.display = 'block';
    input.setAttribute('aria-expanded', 'true');
    globalSearchIndex = -1;
    input.removeAttribute('aria-activedescendant');
  }
}

function globalSearchInput(): void {
  // One input, one model: typing live-filters the story (via existing filter path)
  // and presents the same matching results in the dropdown. No separate fetch.
  applyAdvancedFilters();
  updateGlobalDropdown();
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
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  items.forEach((item, i) => {
    const active = i === globalSearchIndex;
    item.classList.toggle('active', active);
    item.setAttribute('aria-selected', active ? 'true' : 'false');
  });
  if (input) {
    if (globalSearchIndex >= 0 && items[globalSearchIndex]) {
      input.setAttribute('aria-activedescendant', items[globalSearchIndex].id || 'search-option-' + globalSearchIndex);
    } else {
      input.removeAttribute('aria-activedescendant');
    }
  }
}

function highlightGlobalItem(index: number): void {
  const dropdown = document.getElementById('global-search-dropdown');
  if (!dropdown) return;
  const items = dropdown.querySelectorAll('.global-search-item');
  globalSearchIndex = index;
  updateGlobalHighlight(items);
}

function globalSearchFocus(): void {
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  if (input?.value?.trim()) {
    updateGlobalDropdown();
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
  const input = document.getElementById('search-input') as HTMLInputElement | null;
  if (input) { input.setAttribute('aria-expanded', 'false'); input.removeAttribute('aria-activedescendant'); }
  // clear aria-selected
  if (dropdown) dropdown.querySelectorAll('[role="option"]').forEach(el => el.setAttribute('aria-selected', 'false'));
}

document.addEventListener('click', (e: Event) => {
  const target = e.target as HTMLElement;
  if (!target.closest('.global-search-wrapper')) {
    hideGlobalDropdown();
  }
});

function syncFilterURL(): void {
  const params = new URLSearchParams(window.location.search);
  if (filterYear !== null) params.set('year', String(filterYear));
  else params.delete('year');
  if (activeFilterMonth !== 0) params.set('month', String(activeFilterMonth));
  else params.delete('month');
  // preserve q if present
  const q = (document.getElementById('search-input') as HTMLInputElement | null)?.value?.trim() || params.get('q') || '';
  if (q) params.set('q', q); else params.delete('q');
  const qs = params.toString();
  const url = window.location.pathname + (qs ? '?' + qs : '') + window.location.hash;
  history.replaceState(null, '', url);
}

function filterMonth(month: number): void {
  activeFilterMonth = month;
  currentMonth = month;
  const buttons = document.querySelectorAll('.month-filter .btn');
  buttons.forEach((btn, i) => {
    const active = i === month;
    btn.classList.toggle('active', active);
    btn.classList.toggle('btn-dark', active);
    btn.classList.toggle('btn-outline-dark', !active);
    btn.setAttribute('aria-pressed', active ? 'true' : 'false');
  });
  syncFilterURL();
  renderStory();
  renderCalendar();
  updateStats();
  renderMapInstance();
  loadStatsDist();
}

function effectiveStatsYear(): number {
  if (filterYear !== null) return filterYear;
  const fe = filteredEvents();
  if (fe.length) {
    const y = parseInt(fe[0].date.slice(0, 4));
    if (!isNaN(y)) return y;
  }
  return viewedYear;
}

async function loadContributions(): Promise<void> {
  try {
    const yearForFetch = effectiveStatsYear();
    const res = await fetch('/api/contributions?year=' + yearForFetch);
    if (!res.ok) {
      contributions = {};
    } else {
      contributions = await res.json() as ContributionMap;
    }
    // When a month filter is active, contribution data is still year-scoped; stats below reflect month filtering via updateStats
    renderContributionGraph();
    updateStats();
  } catch (err) {
    console.error('Failed to load contributions:', err);
  }
}

function renderContributionGraph(): void {
  const graph = document.getElementById('contribution-graph');
  if (!graph) return;

  const y = effectiveStatsYear();
  const firstDay = new Date(y, 0, 1);
  const startDay = firstDay.getDay();
  const daysInYear = (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0 ? 366 : 365;
  const maxCount = Math.max(...Object.values(contributions), 1);

  let html = '<div class="graph-months">';
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  months.forEach((m, i) => {
    if (i === 0 || new Date(y, i, 1).getDay() === 0) {
      html += '<span class="month-label">' + m + '</span>';
    }
  });
  html += '</div>';

  html += '<div class="graph-grid"><div class="graph-days">';
  for (let i = 0; i < startDay; i++) {
    html += '<div class="day-cell empty"></div>';
  }

  for (let day = 1; day <= daysInYear; day++) {
    const dateObj = new Date(y, 0, day);
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
  const filtered = filteredEvents();
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
  document.getElementById('filters-toggle')?.setAttribute('aria-expanded', isHidden ? 'true' : 'false');
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
  if ((document.getElementById('filter-media-boardgame') as HTMLInputElement | null)?.checked) mediaTypes.push('boardgame');

  selectedCollectionId = collectionId;

  const statusEl = document.getElementById('filter-status');
  if (statusEl) statusEl.textContent = 'Filtering...';

  // The story spans all years, so filtering happens client-side over the loaded events.
  const apply = (base: TimelineEvent[]): void => {
    let result = base;
    if (q) {
      const needle = q.toLowerCase();
      result = result.filter(e =>
        (e.title && e.title.toLowerCase().includes(needle)) ||
        (e.description && e.description.toLowerCase().includes(needle)) ||
        (e.location && e.location.toLowerCase().includes(needle)) ||
        (e.tags && e.tags.toLowerCase().includes(needle))
      );
    }
    if (personId) result = result.filter(e => String(e.person_id || '') === personId);
    if (location) result = result.filter(e => e.location && e.location.toLowerCase().includes(location.toLowerCase()));
    if (selectedFilterTags.length > 0) {
      result = result.filter(e => {
        const tags = e.tags ? e.tags.split(',').map(t => t.trim().toLowerCase()) : [];
        return selectedFilterTags.every(t => tags.includes(t.toLowerCase()));
      });
    }
    if (mediaTypes.length > 0) result = result.filter(e => mediaTypes.includes(e.media_type));
    if (showFavoritesOnly) result = result.filter(e => e.is_favorite);
    events = result;
    storyChunk = 1;
    syncFilterURL();
    renderStory();
    renderCalendar();
    updateStats();
    renderMapInstance();
    loadStatsDist();
    updateResultCount(filteredEvents().length);
    renderActiveFilterChips();
    // Unified search: keep dropdown in sync with composed filters when a query is active
    if (q) updateGlobalDropdown();
    else hideGlobalDropdown();
  };

  if (collectionId) {
    fetch('/api/collections/' + collectionId + '/events')
      .then(r => r.json())
      .then((data: any[]) => {
        if (Array.isArray(data)) apply(data);
        else if (statusEl) statusEl.textContent = 'Filter error';
      })
      .catch(() => {
        const status = document.getElementById('filter-status');
        if (status) status.textContent = 'Filter error';
      });
    return;
  }

  apply(allEvents);
}

function clearAllFilters(): void {
  (document.getElementById('search-input') as HTMLInputElement).value = '';
  hideGlobalDropdown();
  (document.getElementById('filter-person') as HTMLSelectElement).value = '';
  (document.getElementById('filter-location') as HTMLInputElement).value = '';
  (document.getElementById('filter-media-image') as HTMLInputElement).checked = false;
  (document.getElementById('filter-media-video') as HTMLInputElement).checked = false;
  (document.getElementById('filter-media-audio') as HTMLInputElement).checked = false;
  (document.getElementById('filter-media-boardgame') as HTMLInputElement).checked = false;
  const collectionSel = document.getElementById('filter-collection') as HTMLSelectElement | null;
  if (collectionSel) collectionSel.value = '';
  selectedCollectionId = '';
  selectedFilterTags = [];
  renderSelectedFilterTags();
  events = allEvents;
  storyChunk = 1;
  activeFilterMonth = 0;
  currentMonth = 0;
  filterYear = null;
  showFavoritesOnly = false;
  const favBtn = document.getElementById('fav-filter-btn');
  if (favBtn) { favBtn.classList.remove('btn-primary'); favBtn.classList.add('btn-outline-primary'); favBtn.setAttribute('aria-pressed', 'false'); }
  document.querySelectorAll('.month-filter .btn').forEach((btn, i) => {
    const active = i === 0;
    btn.classList.toggle('active', active);
    btn.classList.toggle('btn-dark', active);
    btn.classList.toggle('btn-outline-dark', !active);
    btn.setAttribute('aria-pressed', active ? 'true' : 'false');
  });
  syncFilterURL();
  renderStory();
  renderCalendar();
  updateStats();
  renderMapInstance();
  loadStatsDist();
  renderActiveFilterChips();
  const status = document.getElementById('filter-status');
  if (status) status.textContent = '';
  const rc = document.getElementById('result-count');
  if (rc) rc.textContent = '';
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
  const legacy = document.getElementById('stats-distribution-container');
  const rhythmEl = document.getElementById('stats-rhythm-container');
  const peopleEl = document.getElementById('stats-people-places-container');
  const container = rhythmEl || legacy;
  if (!container && !peopleEl && !legacy) return;
  if (rhythmEl) rhythmEl.innerHTML = '<div class="text-center text-muted py-5"><i class="fa-solid fa-spinner fa-spin me-2"></i>Loading statistics...</div>';
  else if (legacy) legacy.innerHTML = '<div class="text-center text-muted py-5"><i class="fa-solid fa-spinner fa-spin me-2"></i>Loading statistics...</div>';
  if (peopleEl) peopleEl.innerHTML = '';

  try {
    // Phase 4: Stats must reflect FILTER state, not viewedYear. Prefer client aggregation from filteredEvents
    // so month/year filtering is reflected immediately without extra fetch mismatch.
    const list = filteredEvents();
    // If no events loaded yet, fall back to server for effective year (initial load)
    let dist: any;
    if (list.length > 0 || filterYear !== null || activeFilterMonth !== 0) {
      // client-side aggregation
      const byMonth: Record<string, number> = {};
      const byWeekday: Record<string, number> = {};
      for (let i = 1; i <= 12; i++) byMonth[String(i).padStart(2, '0')] = 0;
      for (let i = 0; i < 7; i++) byWeekday[String(i)] = 0;
      const tagCounts: Record<string, number> = {};
      const personCounts: Record<string, number> = {};
      const userCounts: Record<string, number> = {};
      const locCounts: Record<string, number> = {};
      const dayCounts: Record<string, number> = {};
      let geoEvents: TimelineEvent[] = [];
      list.forEach(e => {
        const m = String(new Date(e.date).getMonth() + 1).padStart(2, '0');
        byMonth[m] = (byMonth[m] || 0) + 1;
        const wd = String(new Date(e.date).getDay());
        byWeekday[wd] = (byWeekday[wd] || 0) + 1;
        if (e.tags) e.tags.split(',').map(t => t.trim()).filter(Boolean).forEach(t => { tagCounts[t] = (tagCounts[t] || 0) + 1; });
        if (e.person_id) {
          const pname = (e.person?.name) || String(e.person_id);
          personCounts[pname] = (personCounts[pname] || 0) + 1;
        }
        if (e.user_id) {
          const u = users.find(u => u.id === e.user_id);
          const uname = u ? (u.display_name || u.username) : String(e.user_id);
          userCounts[uname] = (userCounts[uname] || 0) + 1;
        }
        if (e.location) locCounts[e.location] = (locCounts[e.location] || 0) + 1;
        dayCounts[e.date] = (dayCounts[e.date] || 0) + 1;
        if (e.latitude && e.longitude) geoEvents.push(e);
      });
      const topDay = Object.entries(dayCounts).sort((a, b) => b[1] - a[1])[0]?.[0] || '';
      const byTagArr = Object.entries(tagCounts).map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count);
      const byPersonArr = Object.entries(personCounts).map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count);
      const byUserArr = Object.entries(userCounts).map(([display_name, count]) => ({ display_name, count })).sort((a, b) => b.count - a.count);
      const byLocArr = Object.entries(locCounts).map(([location, count]) => ({ location, count })).sort((a, b) => b.count - a.count);
      const eventCount = list.length;
      const monthlyAvg = filterYear !== null ? eventCount / 12 : eventCount / Math.max(1, new Set(list.map(e => e.date.slice(0, 7))).size);
      const dailyAvg = eventCount / 365;
      // crude geo spread: avg haversine between geo points if any
      let geoSpread = 0;
      if (geoEvents.length > 1) {
        const toRad = (d: number) => d * Math.PI / 180;
        const hav = (a: TimelineEvent, b: TimelineEvent) => {
          const R = 6371;
          const dLat = toRad((b.latitude || 0) - (a.latitude || 0));
          const dLon = toRad((b.longitude || 0) - (a.longitude || 0));
          const aa = Math.sin(dLat / 2) ** 2 + Math.cos(toRad(a.latitude || 0)) * Math.cos(toRad(b.latitude || 0)) * Math.sin(dLon / 2) ** 2;
          return 2 * R * Math.asin(Math.sqrt(aa));
        };
        let sum = 0; let n = 0;
        for (let i = 0; i < geoEvents.length; i++) for (let j = i + 1; j < geoEvents.length; j++) { sum += hav(geoEvents[i], geoEvents[j]); n++; }
        geoSpread = n ? sum / n : 0;
      }
      dist = {
        event_count: eventCount,
        by_month: byMonth,
        by_weekday: byWeekday,
        by_tag: byTagArr,
        by_person: byPersonArr,
        by_user: byUserArr,
        by_location: byLocArr,
        top_day: topDay,
        monthly_avg: monthlyAvg,
        daily_avg: dailyAvg,
        geo_spread: geoSpread,
      };
    } else {
      const y = effectiveStatsYear();
      const res = await fetch('/api/stats/distribution?year=' + y);
      if (!res.ok) throw new Error('Failed');
      dist = await res.json();
    }

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

    // 7.3 Rhythm: month + weekday side by side
    const rhythmHTML =
      '<div class="row g-3">' +
      '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-chart-column me-1"></i>Events by Month</h5><div class="stats-bar-chart">' + monthBars + '</div></div></div>' +
      '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-calendar-week me-1"></i>By Day of Week</h5><div class="weekday-chart">' + wdBars + '</div></div></div>' +
      '</div>';
    // 7.3 People & Places
    const peopleHTMLCombined =
      '<div class="row g-3">' +
      (tagHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-tags me-1"></i>Tags</h5>' + tagHTML + '</div></div>' : '') +
      (personHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-user-group me-1"></i>People</h5>' + personHTML + '</div></div>' : '') +
      (userHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-users me-1"></i>Family Members</h5>' + userHTML + '</div></div>' : '') +
      (locHTML ? '<div class="col-md-6"><div class="dist-card"><h5><i class="fa-solid fa-map-pin me-1"></i>Top Locations</h5>' + locHTML + '</div></div>' : '') +
      (!tagHTML && !personHTML && !userHTML && !locHTML ? '<div class="col-12"><p class="text-muted small">No people or places to show for this filter.</p></div>' : '') +
      '</div>';

    if (rhythmEl) rhythmEl.innerHTML = rhythmHTML;
    else if (container) (container as HTMLElement).innerHTML = rhythmHTML;
    if (peopleEl) peopleEl.innerHTML = peopleHTMLCombined;
    // legacy is now wrapper containing the two sections; only populate directly if old markup (no inner containers)
    if (legacy && !rhythmEl && !peopleEl) legacy.innerHTML = rhythmHTML + peopleHTMLCombined;
    else if (!rhythmEl && container && container !== legacy) (container as HTMLElement).innerHTML = rhythmHTML + peopleHTMLCombined;
  } catch (_err) {
    const errHTML = '<div class="error-state"><i class="fa-solid fa-triangle-exclamation"></i><p>Failed to load statistics</p><button class="btn btn-sm btn-outline-primary" onclick="loadStatsDist()">Retry</button></div>';
    if (rhythmEl) rhythmEl.innerHTML = errHTML;
    if (peopleEl) peopleEl.innerHTML = '';
    if (legacy && !rhythmEl && !peopleEl) legacy.innerHTML = errHTML;
    const fallback = document.getElementById('stats-distribution-container');
    if (fallback && fallback !== rhythmEl && fallback !== legacy && !rhythmEl) fallback.innerHTML = errHTML;
  }
}

function renderStoryError(msg: string): void {
  const container = document.getElementById('timeline-container');
  if (!container) return;
  container.innerHTML = '<div class="error-state"><i class="fa-solid fa-triangle-exclamation"></i><p>' + escapeHtml(msg) + '</p><button class="btn btn-sm btn-outline-primary" onclick="loadData()">Retry</button></div>';
  updateResultCount(0);
  updateLoadMoreVisibility();
}

function renderSkeletons(count: number = 4): void {
  const container = document.getElementById('timeline-container');
  if (!container) return;
  let html = '';
  for (let i = 0; i < count; i++) {
    html += '<article class="story-card story-skeleton" aria-hidden="true"><div class="skeleton-media"></div><div class="story-body"><div class="skeleton-bar" style="height:14px;width:28%;margin-bottom:12px"></div><div class="skeleton-line" style="height:18px;width:62%;margin-bottom:10px"></div><div class="skeleton-line" style="height:12px;width:90%"></div><div class="skeleton-line" style="height:12px;width:75%;margin-top:6px"></div></div></article>';
  }
  container.innerHTML = html;
  const sentinel = document.getElementById('story-sentinel');
  if (sentinel) { sentinel.style.display = 'none'; sentinel.classList.remove('is-loading'); }
  const wrap = document.getElementById('load-more-wrap');
  if (wrap) wrap.style.display = 'none';
}

async function loadEvents(): Promise<void> {
  try {
    // The story is one continuous multi-year roll, so load everything at once.
    // Authenticated users get every event; guests fall back to public events.
    const res = await fetch('/api/events/full');
    if (res.status === 401) {
      const pubRes = await fetch('/api/public');
      events = pubRes.ok ? await pubRes.json() as TimelineEvent[] : [];
    } else if (res.ok) {
      events = await res.json() as TimelineEvent[];
    } else {
      throw new Error('Failed to load story');
    }
    if (!Array.isArray(events)) events = [];
    allEvents = events;
    storyChunk = 1;
    renderStory();
    renderCalendar();
    updateStats();
  } catch (_err) {
    renderStoryError('Failed to load story');
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
  const allIdx = allEvents.findIndex(e => e.id === id);
  const evIdx = events.findIndex(e => e.id === id);
  const prevAll = allIdx >= 0 ? allEvents[allIdx].is_favorite : undefined;
  const prevEv = evIdx >= 0 ? events[evIdx].is_favorite : undefined;
  const nextVal = !(prevAll ?? prevEv ?? false);
  if (allIdx >= 0) allEvents[allIdx].is_favorite = nextVal;
  if (evIdx >= 0) events[evIdx].is_favorite = nextVal;
  // optimistic DOM patch
  const card = document.getElementById('event-' + id);
  if (card) {
    const star = card.querySelector('.story-fav');
    if (star) {
      star.classList.toggle('fa-solid', nextVal);
      star.classList.toggle('fa-regular', !nextVal);
      star.setAttribute('title', nextVal ? 'Unfavorite' : 'Favorite');
    }
  }
  try {
    const csrf = await ensureCSRF();
    const res = await fetch('/api/events/favorite', {
      method: 'POST',
      headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' },
      body: JSON.stringify({ id })
    });
    if (!res.ok) throw new Error('fav failed');
  } catch (_e) {
    if (allIdx >= 0 && prevAll !== undefined) allEvents[allIdx].is_favorite = prevAll;
    if (evIdx >= 0 && prevEv !== undefined) events[evIdx].is_favorite = prevEv;
    if (card) {
      const star = card.querySelector('.story-fav');
      if (star) {
        star.classList.toggle('fa-solid', !!prevAll);
        star.classList.toggle('fa-regular', !prevAll);
        star.setAttribute('title', prevAll ? 'Unfavorite' : 'Favorite');
      }
    }
  }
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
  if (btn) {
    btn.classList.toggle('btn-primary', showFavoritesOnly);
    btn.classList.toggle('btn-outline-primary', !showFavoritesOnly);
    btn.setAttribute('aria-pressed', showFavoritesOnly ? 'true' : 'false');
  }
  applyAdvancedFilters();
}

async function filterByCollection(): Promise<void> {
  const sel = document.getElementById('filter-collection') as HTMLSelectElement;
  selectedCollectionId = sel?.value || '';
  if (!selectedCollectionId) {
    events = allEvents;
    storyChunk = 1;
    renderStory();
    renderCalendar();
    updateStats();
    return;
  }
  try {
    const res = await fetch('/api/collections/' + selectedCollectionId + '/events');
    events = await res.json();
    if (!Array.isArray(events)) events = [];
    if (showFavoritesOnly) {
      events = events.filter(e => e.is_favorite);
    }
    storyChunk = 1;
    renderStory();
    renderCalendar();
    updateStats();
  } catch (e) { console.error('Filter by collection failed', e); }
}

async function loadData(): Promise<void> {
  renderSkeletons();
  await Promise.all([loadEvents(), loadContributions(), loadUsers(), loadCollections()]);
  populateYearButtons();
  // loadEvents already rendered; keep chunk at 1 and ensure observers
  storyChunk = 1;
  if (events.length > 0) renderStory();
}

function populateYearButtons(): void {
  const container = document.getElementById('year-buttons');
  if (!container) return;
  const years = new Set<number>();
  allEvents.forEach(e => {
    const y = parseInt(e.date.slice(0, 4));
    if (!isNaN(y)) years.add(y);
  });
  // include viewedYear so indicator highlight exists even if no events that year
  years.add(viewedYear);
  const sorted = Array.from(years).sort((a, b) => b - a);
  container.innerHTML = sorted.map(y =>
    `<button class="btn ${y === viewedYear ? 'btn-primary' : 'btn-outline-primary'}" data-year="${y}" onclick="scrollToYear(${y})"${y === viewedYear ? ' aria-current="true"' : ''} aria-label="Scroll to ${y}" aria-controls="timeline-container">${y}</button>`
  ).join('');
}

// ── Filter chips + live count + load-more helpers ──
function updateResultCount(count: number): void {
  const el = document.getElementById('result-count');
  if (el) el.textContent = count === 0 ? 'No results' : count + ' ' + (count === 1 ? 'result' : 'results');
  const status = document.getElementById('filter-status');
  if (status) status.textContent = count + ' results';
}

function updateLoadMoreVisibility(): void {
  const wrap = document.getElementById('load-more-wrap');
  const btn = document.getElementById('load-more-btn');
  const sentinel = document.getElementById('story-sentinel');
  if (!wrap || !btn) return;
  const total = filteredEvents().length;
  const shown = Math.min(storyChunk * storyChunkSize, total);
  const hasMore = shown < total;
  wrap.style.display = hasMore ? 'flex' : 'none';
  btn.style.display = hasMore ? 'inline-flex' : 'none';
  if (sentinel) {
    sentinel.style.display = hasMore ? '' : 'none';
    if (!hasMore) sentinel.classList.remove('is-loading');
  }
}

function removeOneFilter(kind: string, value?: string): void {
  if (kind === 'search') {
    const inp = document.getElementById('search-input') as HTMLInputElement | null;
    if (inp) inp.value = '';
  } else if (kind === 'tag' && value) {
    selectedFilterTags = selectedFilterTags.filter(t => t !== value);
    renderSelectedFilterTags();
  } else if (kind === 'person') {
    const sel = document.getElementById('filter-person') as HTMLSelectElement | null;
    if (sel) sel.value = '';
  } else if (kind === 'collection') {
    const sel = document.getElementById('filter-collection') as HTMLSelectElement | null;
    if (sel) sel.value = '';
    selectedCollectionId = '';
  } else if (kind === 'location') {
    const inp = document.getElementById('filter-location') as HTMLInputElement | null;
    if (inp) inp.value = '';
  } else if (kind === 'media' && value) {
    const el = document.getElementById('filter-media-' + value) as HTMLInputElement | null;
    if (el) el.checked = false;
  } else if (kind === 'fav') {
    showFavoritesOnly = false;
    const btn = document.getElementById('fav-filter-btn');
    if (btn) { btn.classList.remove('btn-primary'); btn.classList.add('btn-outline-primary'); }
  } else if (kind === 'month') {
    activeFilterMonth = 0;
    currentMonth = 0;
    document.querySelectorAll('.month-filter .btn').forEach((btn, i) => {
      const active = i === 0;
      btn.classList.toggle('active', active);
      btn.classList.toggle('btn-dark', active);
      btn.classList.toggle('btn-outline-dark', !active);
      btn.setAttribute('aria-pressed', active ? 'true' : 'false');
    });
  } else if (kind === 'year' && value) {
    filterYear = null;
  }
  applyAdvancedFilters();
}

function renderActiveFilterChips(): void {
  const bar = document.getElementById('active-filter-bar');
  const chipsEl = document.getElementById('active-filter-chips');
  if (!bar || !chipsEl) return;
  const chips: string[] = [];
  const q = (document.getElementById('search-input') as HTMLInputElement | null)?.value?.trim() || '';
  if (q) chips.push('<button class="filter-chip" onclick="removeOneFilter(\'search\')">Search: ' + escapeHtml(q) + ' <i class="fa-solid fa-xmark"></i></button>');
  selectedFilterTags.forEach(t => chips.push('<button class="filter-chip" onclick="removeOneFilter(\'tag\',\'' + escapeHtml(t) + '\')">Tag: ' + escapeHtml(t) + ' <i class="fa-solid fa-xmark"></i></button>'));
  const personVal = (document.getElementById('filter-person') as HTMLSelectElement | null)?.value || '';
  if (personVal) {
    const sel = document.getElementById('filter-person') as HTMLSelectElement;
    const label = sel.options[sel.selectedIndex]?.text || personVal;
    chips.push('<button class="filter-chip" onclick="removeOneFilter(\'person\')">Person: ' + escapeHtml(label) + ' <i class="fa-solid fa-xmark"></i></button>');
  }
  const collVal = (document.getElementById('filter-collection') as HTMLSelectElement | null)?.value || '';
  if (collVal) {
    const sel = document.getElementById('filter-collection') as HTMLSelectElement;
    const label = sel.options[sel.selectedIndex]?.text || collVal;
    chips.push('<button class="filter-chip" onclick="removeOneFilter(\'collection\')">Collection: ' + escapeHtml(label) + ' <i class="fa-solid fa-xmark"></i></button>');
  }
  const loc = (document.getElementById('filter-location') as HTMLInputElement | null)?.value?.trim() || '';
  if (loc) chips.push('<button class="filter-chip" onclick="removeOneFilter(\'location\')">Location: ' + escapeHtml(loc) + ' <i class="fa-solid fa-xmark"></i></button>');
  ['image', 'video', 'audio', 'boardgame'].forEach(m => {
    const el = document.getElementById('filter-media-' + m) as HTMLInputElement | null;
    if (el?.checked) chips.push('<button class="filter-chip" onclick="removeOneFilter(\'media\',\'' + m + '\')">' + m + ' <i class="fa-solid fa-xmark"></i></button>');
  });
  if (showFavoritesOnly) chips.push('<button class="filter-chip" onclick="removeOneFilter(\'fav\')">Favorites <i class="fa-solid fa-xmark"></i></button>');
  if (activeFilterMonth !== 0) chips.push('<button class="filter-chip" onclick="removeOneFilter(\'month\')">' + monthNames[activeFilterMonth - 1] + ' <i class="fa-solid fa-xmark"></i></button>');
  if (filterYear !== null) chips.push('<button class="filter-chip" onclick="removeOneFilter(\'year\',\'' + filterYear + '\')">Year: ' + filterYear + ' <i class="fa-solid fa-xmark"></i></button>');
  chipsEl.innerHTML = chips.join('');
  bar.style.display = chips.length ? 'flex' : 'none';
}

// ── The Story ──
// One continuous multi-year roll: photos, videos, audio, places (mini-maps),
// and text events (books, quotes, notes) all inline, newest memory first.

function filteredEvents(): TimelineEvent[] {
  let list = events;
  if (filterYear !== null) {
    list = list.filter(e => parseInt(e.date.slice(0, 4)) === filterYear);
  }
  if (activeFilterMonth !== 0) {
    list = list.filter(e => new Date(e.date).getMonth() + 1 === activeFilterMonth);
  }
  return list;
}

function renderStory(): void {
  const container = document.getElementById('timeline-container');
  if (!container) return;
  const list = filteredEvents();

  if (list.length === 0) {
    const hasFilters = selectedFilterTags.length > 0 || showFavoritesOnly || activeFilterMonth !== 0 || filterYear !== null || !!((document.getElementById('search-input') as HTMLInputElement | null)?.value?.trim()) || !!((document.getElementById('filter-person') as HTMLSelectElement | null)?.value) || !!((document.getElementById('filter-location') as HTMLInputElement | null)?.value?.trim()) || !!((document.getElementById('filter-collection') as HTMLSelectElement | null)?.value) || (document.getElementById('filter-media-image') as HTMLInputElement | null)?.checked || (document.getElementById('filter-media-video') as HTMLInputElement | null)?.checked || (document.getElementById('filter-media-audio') as HTMLInputElement | null)?.checked || (document.getElementById('filter-media-boardgame') as HTMLInputElement | null)?.checked;
    const msg = hasFilters ? 'No moments match these filters.' : 'Your story starts here';
    const sub = hasFilters ? 'Try adjusting filters or clearing them.' : 'Add photos, videos, places, books and quotes in the Admin panel — they will appear here, year after year, in one continuous timeline.';
    container.innerHTML = '<div class="empty-state"><i class="fa-regular fa-hourglass-half"></i><p class="empty-title">' + msg + '</p><p>' + sub + '</p><a class="btn btn-sm btn-primary mt-2" href="/admin.html"><i class="fa-solid fa-plus me-1"></i>Add content</a>' + (hasFilters ? ' <button class="btn btn-sm btn-outline-secondary mt-2 ms-2" onclick="clearAllFilters()">Clear filters</button>' : '') + '</div>';
    updateResultCount(0);
    renderActiveFilterChips();
    updateLoadMoreVisibility();
    return;
  }

  // Year marker counts (stable across chunks)
  const yearCounts: Record<number, number> = {};
  list.forEach(e => {
    const y = parseInt(e.date.slice(0, 4));
    if (!isNaN(y)) yearCounts[y] = (yearCounts[y] || 0) + 1;
  });

  const start = 0;
  const end = Math.min(storyChunk * storyChunkSize, list.length);
  let html = '';
  let prevYear = -1;
  let prevMonth = -1;

  list.slice(start, end).forEach(e => {
    const eventDate = new Date(e.date);
    const year = parseInt(e.date.slice(0, 4));
    const month = eventDate.getMonth();

    if (year !== prevYear) {
      prevYear = year;
      prevMonth = -1;
      const count = yearCounts[year] || 0;
      html += `
        <div class="story-year" id="year-${year}" data-year="${year}">
          <div class="story-year-rule"></div>
          <h2 class="story-year-label">${year}</h2>
          <div class="story-year-rule"></div>
          <span class="story-year-count">${count} ${count === 1 ? 'moment' : 'moments'}</span>
        </div>
      `;
    }
    if (month !== prevMonth) {
      prevMonth = month;
      html += `
        <div class="story-month">
          <span class="badge bg-primary">${monthNames[month]}</span>
        </div>
      `;
    }

    html += storyCardHtml(e);
  });

  container.innerHTML = html;
  initStoryObservers();
  initMiniMaps();
  initMediaOrientation();
  updateResultCount(list.length);
  renderActiveFilterChips();
  updateLoadMoreVisibility();
}

function storyCardHtml(e: TimelineEvent): string {
  const hasMedia = !!e.media_url;
  const hasGeo = !!(e.latitude && e.longitude && (e.latitude !== 0 || e.longitude !== 0));
  const isVideo = e.media_type === 'video';
  const isAudio = e.media_type === 'audio';
  const tagList = e.tags ? e.tags.split(',').map(t => t.trim()).filter(t => t) : [];

  let weatherHtml = '';
  if (e.weather_data) {
    try {
      const w = JSON.parse(e.weather_data) as Weather;
      weatherHtml = `<span class="weather-badge ms-2"><i class="fa-solid fa-${weatherIconClass(w.icon)}"></i> ${Math.round(w.temperature)}°C ${w.condition}</span>`;
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
  const favStar = `<i class="${e.is_favorite ? 'fa-solid' : 'fa-regular'} fa-star text-warning story-fav" onclick="event.stopPropagation();toggleFav(${e.id})" title="${e.is_favorite ? 'Unfavorite' : 'Favorite'}"></i>`;

  let mediaHtml = '';
  if (hasMedia) {
    if (isVideo) {
      mediaHtml = `
        <div class="story-media-frame">
          <video class="story-media-el" src="${escapeHtml(e.media_url)}" muted preload="metadata"></video>
          <span class="story-play"><i class="fa-solid fa-play"></i></span>
        </div>
      `;
    } else if (isAudio) {
      mediaHtml = `
        <div class="story-audio">
          <i class="fa-solid fa-music"></i>
          <span>Audio memory — tap to play</span>
        </div>
      `;
    } else {
      mediaHtml = `
        <div class="story-media-frame">
          <img class="story-media-el" src="${escapeHtml(e.thumbnail || e.media_url)}" alt="${escapeHtml(e.title)}" loading="lazy">
        </div>
      `;
    }
  }

  let mapHtml = '';
  if (hasGeo) {
    const locLabel = escapeHtml(e.location || e.title);
    mapHtml = `
      <div class="story-minimap-wrap">
        <div class="story-minimap" data-lat="${e.latitude}" data-lng="${e.longitude}" data-id="${e.id}" aria-hidden="true"></div>
        <button type="button" class="story-minimap-action" onclick="event.stopPropagation();openMapAt(${e.latitude}, ${e.longitude}, ${e.id})" aria-label="View ${locLabel} on map"><i class="fa-solid fa-map-location-dot"></i> View on map</button>
      </div>
    `;
  }

  const cardClass = hasMedia ? 'story-media' : 'story-text';
  const geoClass = hasGeo ? ' story-geo' : '';

  return `
    <article class="story-card ${cardClass}${geoClass}" id="event-${e.id}" onclick="${hasMedia ? 'showMedia(' + e.id + ')' : ''}">
      ${mediaHtml}
      <div class="story-body">
        <div class="d-flex justify-content-between align-items-start gap-2">
          <div class="story-meta">
            <i class="fa-solid fa-calendar-day me-1"></i>${formatDate(e.date, shouldShowYear())}
            ${e.start_time ? '<span class="story-meta-time"><i class="fa-regular fa-clock ms-2 me-1"></i>' + e.start_time.substring(0, 5) + '</span>' : ''}
            ${e.end_time ? '–' + e.end_time.substring(0, 5) : ''}
            ${recurringBadge}
          </div>
          ${favStar}
        </div>
        <div class="story-title">${escapeHtml(e.title)} ${weatherHtml}</div>
        ${e.location ? '<div class="story-location"><i class="fa-solid fa-location-dot me-1"></i>' + escapeHtml(e.location) + userHtml + '</div>' : userHtml ? '<div class="story-location">' + userHtml + '</div>' : ''}
        ${tagList.length > 0 ? '<div class="story-tags"><i class="fa-solid fa-tags me-1"></i>' + tagList.map(t => '<span class="badge bg-secondary me-1">' + escapeHtml(t) + '</span>').join('') + '</div>' : ''}
        ${e.description ? '<div class="story-desc md-content">' + renderMarkdown(e.description) + '</div>' : ''}
        ${mapHtml}
      </div>
    </article>
  `;
}

let miniMaps: Map<number, any> = new Map();
let miniMapObserver: IntersectionObserver | null = null;

function destroyMiniMaps(): void {
  if (miniMapObserver) { miniMapObserver.disconnect(); miniMapObserver = null; }
  miniMaps.forEach(m => { try { m.remove(); } catch (_) {} });
  miniMaps.clear();
}

function createMiniMap(el: HTMLElement): void {
  const id = parseInt(el.dataset.id || '0');
  if (miniMaps.has(id)) return;
  const lat = parseFloat(el.dataset.lat || '0');
  const lng = parseFloat(el.dataset.lng || '0');
  if (!lat && !lng) return;
  // ponytail: OSM tiles kept for Phase 3; Phase 7 unifies to shared map module / Carto
  const map = L.map(el, {
    zoomControl: false,
    attributionControl: false,
    scrollWheelZoom: false,
    dragging: false,
    touchZoom: false,
    doubleClickZoom: false,
    boxZoom: false,
    keyboard: false
  }).setView([lat, lng], 10);
  L.tileLayer(MAP_TILE_URL, MAP_TILE_OPTS).addTo(map);
  L.marker([lat, lng], {
    icon: L.divIcon({
      className: 'custom-marker',
      html: '<i class="fa-solid fa-map-pin" style="color:#7c3aed;font-size:20px;"></i>',
      iconSize: [20, 20],
      iconAnchor: [10, 20],
      popupAnchor: [0, -20]
    })
  }).addTo(map);
  miniMaps.set(id, map);
}

function initMiniMaps(): void {
  destroyMiniMaps();
  const els = document.querySelectorAll<HTMLElement>('.story-minimap[data-lat]');
  if (!els.length) return;
  if (!('IntersectionObserver' in window)) {
    els.forEach(el => createMiniMap(el));
    return;
  }
  miniMapObserver = new IntersectionObserver((entries) => {
    entries.forEach(en => {
      if (en.isIntersecting) {
        createMiniMap(en.target as HTMLElement);
        miniMapObserver!.unobserve(en.target);
      }
    });
  }, { rootMargin: '200px 0px', threshold: 0.01 });
  els.forEach(el => miniMapObserver!.observe(el));
}

function classifyOrientation(img: HTMLImageElement): 'portrait' | 'square' | 'landscape' {
  const w = img.naturalWidth || (img as any).width || 0;
  const h = img.naturalHeight || (img as any).height || 0;
  if (!w || !h) return 'landscape';
  const r = w / h;
  if (r < 0.95) return 'portrait';
  if (r > 1.05) return 'landscape';
  return 'square';
}

function orientFrame(img: HTMLImageElement): void {
  const frame = img.closest('.story-media-frame') as HTMLElement | null;
  if (!frame) return;
  const o = classifyOrientation(img);
  frame.dataset.orientation = o;
  frame.classList.toggle('is-portrait', o === 'portrait');
  frame.classList.toggle('is-square', o === 'square');
  frame.classList.toggle('is-landscape', o === 'landscape');
}

function initMediaOrientation(): void {
  document.querySelectorAll<HTMLImageElement>('.story-media-frame img.story-media-el').forEach(img => {
    if (img.complete && img.naturalWidth) orientFrame(img);
    else img.addEventListener('load', () => orientFrame(img), { once: true });
    img.addEventListener('error', () => {
      const f = img.closest('.story-media-frame') as HTMLElement | null;
      if (f) f.dataset.orientation = 'landscape';
    }, { once: true });
  });
}

function initStoryObservers(): void {
  const container = document.getElementById('timeline-container');

  // Keep the "Viewing YYYY" indicator in the toolbar in sync with the scroll position.
  if (storyYearObserver) storyYearObserver.disconnect();
  const yearEls = container ? container.querySelectorAll('.story-year[data-year]') : [];
  if (yearEls.length > 0) {
    storyYearObserver = new IntersectionObserver((entries) => {
      entries.forEach(en => {
        if (en.isIntersecting) {
          const y = parseInt((en.target as HTMLElement).dataset.year || '0');
          if (y) setViewedYear(y);
        }
      });
    }, { rootMargin: '-35% 0px -55% 0px' });
    yearEls.forEach(el => storyYearObserver!.observe(el));
  }

  // Infinite scroll: sentinel only animates while loading next chunk (2.9)
  const sentinel = document.getElementById('story-sentinel');
  if (!sentinel) return;
  if (storySentinelObserver) storySentinelObserver.disconnect();
  const total = filteredEvents().length;
  const shown = Math.min(storyChunk * storyChunkSize, total);
  if (total === 0 || shown >= total) {
    sentinel.style.display = 'none';
    sentinel.classList.remove('is-loading');
    updateLoadMoreVisibility();
    return;
  }
  sentinel.style.display = '';
  sentinel.classList.remove('is-loading');
  updateLoadMoreVisibility();
  storySentinelObserver = new IntersectionObserver((entries) => {
    if (entries.some(en => en.isIntersecting)) {
      sentinel.classList.add('is-loading');
      loadMoreGallery();
    }
  }, { rootMargin: '300px' });
  storySentinelObserver.observe(sentinel);
}

function setViewedYear(year: number): void {
  if (year === viewedYear) return;
  setViewedYearOnly(year);
}

async function loadMoreGallery(): Promise<void> {
  const total = filteredEvents().length;
  if (storyChunk * storyChunkSize >= total) {
    updateLoadMoreVisibility();
    return;
  }
  storyChunk++;
  renderStory();
  const sentinel = document.getElementById('story-sentinel');
  if (sentinel) setTimeout(() => sentinel.classList.remove('is-loading'), 300);
}

// ponytail: show year when story spans >1 year; Phase 4 will split viewedYear vs filterYear — revisit then
function shouldShowYear(): boolean {
  const list = filteredEvents();
  const years = new Set(list.map(e => e.date.slice(0, 4)));
  return years.size > 1;
}

let mapClusterGroup: any = null;

function ensureMapInstance(): any {
  if (mapInstance) return mapInstance;
  const el = document.getElementById('map-container');
  if (!el) return null;
  // 7.5 gate scroll/zoom conflict: disable scrollWheelZoom until user focuses map
  mapInstance = L.map('map-container', { scrollWheelZoom: false } as any).setView([20, 0], 2);
  L.tileLayer(MAP_TILE_URL, MAP_TILE_OPTS).addTo(mapInstance);
  // focus enables scroll zoom to avoid page-scroll hijack on mobile
  mapInstance.on('focus', () => { try { mapInstance.scrollWheelZoom.enable(); } catch (_) {} });
  mapInstance.on('blur', () => { try { mapInstance.scrollWheelZoom.disable(); } catch (_) {} });
  // also enable on click for touch devices without focus
  if (el) el.addEventListener('click', () => { try { mapInstance.scrollWheelZoom.enable(); } catch (_) {} }, { once: false });
  setTimeout(() => mapInstance.invalidateSize(), 100);
  setTimeout(() => mapInstance.invalidateSize(), 350);
  return mapInstance;
}

function renderMapInstance(): void {
  const filtered = filteredEvents();
  const geoEvents = filtered.filter(e => e.latitude && e.longitude && (e.latitude !== 0 || e.longitude !== 0));

  const placeholder = document.getElementById('map-placeholder');
  if (placeholder) placeholder.style.display = geoEvents.length ? 'none' : 'block';

  const map = ensureMapInstance();
  if (!map) return;

  if (!geoEvents.length) {
    mapMarkers.forEach(m => map.removeLayer(m));
    mapMarkers = [];
    if (mapPathLine) { map.removeLayer(mapPathLine); mapPathLine = null; }
    return;
  }

  // clear previous layers including cluster group
  if (mapClusterGroup) {
    try { mapInstance.removeLayer(mapClusterGroup); } catch (_) {}
    mapClusterGroup = null;
  }
  mapMarkers.forEach(m => { try { mapInstance.removeLayer(m); } catch (_) {} });
  mapMarkers = [];

  const bounds: [number, number][] = [];
  const markerIcon = L.divIcon({
    className: 'custom-marker',
    html: '<i class="fa-solid fa-map-pin" style="color:#7c3aed;font-size:24px;"></i>',
    iconSize: [24, 24],
    iconAnchor: [12, 24],
    popupAnchor: [0, -24]
  });

  const rawMarkers: any[] = [];
  geoEvents.forEach(e => {
    const m = L.marker([e.latitude!, e.longitude!], { icon: markerIcon });
    let weatherHtml = '';
    if (e.weather_data) {
      try {
        const w = JSON.parse(e.weather_data);
        weatherHtml = `<br><small><i class="fa-solid fa-${weatherIconClass(w.icon)}"></i> ${Math.round(w.temperature)}°C ${w.condition}</small>`;
      } catch (_) { }
    }
    m.bindPopup(`
      <div class="map-popup">
        <h6>${escapeHtml(e.title)}</h6>
        <p>${formatDate(e.date)} — ${escapeHtml(e.location)}${weatherHtml}</p>
      </div>
    `);
    (m as any)._eventId = e.id;
    rawMarkers.push(m);
    mapMarkers.push(m);
    bounds.push([e.latitude!, e.longitude!]);
  });
  // 7.1 clustering — use markerClusterGroup when available (shared with ts/map.ts)
  const hasCluster = typeof L !== 'undefined' && typeof (L as any).markerClusterGroup === 'function';
  if (hasCluster && rawMarkers.length) {
    mapClusterGroup = (L as any).markerClusterGroup({ chunkedLoading: true, maxClusterRadius: 40 });
    mapClusterGroup.addLayers(rawMarkers);
    mapInstance.addLayer(mapClusterGroup);
  } else {
    rawMarkers.forEach(m => m.addTo(mapInstance));
  }

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
    // if openMapAt set a pending focus, honor it instead of fitting all bounds
    const pending = (mapInstance as any)._pendingMapFocus as { lat: number; lng: number; id?: number } | null;
    if (pending && typeof pending.lat === 'number' && typeof pending.lng === 'number') {
      const target = mapMarkers.find((m: any) => (m as any)._eventId === pending.id);
      mapInstance.setView([pending.lat, pending.lng], 14, { animate: true });
      if (target) {
        if (mapClusterGroup && typeof mapClusterGroup.zoomToShowLayer === 'function') {
          mapClusterGroup.zoomToShowLayer(target, () => target.openPopup());
        } else target.openPopup();
      }
      (mapInstance as any)._pendingMapFocus = null;
      // highlight story card without scrolling timeline
      if (pending.id) highlightStoryCard(pending.id);
    } else {
      mapInstance.fitBounds(bounds, { padding: [30, 30], maxZoom: 14 });
    }
  }

  setTimeout(() => mapInstance.invalidateSize(), 100);
  setTimeout(() => mapInstance.invalidateSize(), 350);
}

function highlightStoryCard(id: number): void {
  const card = document.getElementById('event-' + id);
  if (!card) return;
  card.classList.add('story-card-flash');
  setTimeout(() => card.classList.remove('story-card-flash'), 1800);
}

// ── Overlays (Calendar / Map / Stats) ──

function openOverlay(id: string): void {
  const overlay = document.getElementById(id);
  if (!overlay) return;
  overlay.style.display = 'flex';
  document.body.style.overflow = 'hidden';
  if (id === 'map-overlay') {
    renderMapInstance();
    // 7.5 ensure Leaflet knows its container size after overlay becomes visible
    setTimeout(() => { if (mapInstance) mapInstance.invalidateSize(); }, 80);
    setTimeout(() => { if (mapInstance) mapInstance.invalidateSize(); }, 220);
    // update hash for deep link (client-side, no Go route)
    try { if (location.hash !== '#map') history.replaceState(null, '', '#map'); } catch (_) {}
  } else if (id === 'stats-overlay') {
    const y = effectiveStatsYear();
    const label = filterYear !== null ? String(filterYear) + (activeFilterMonth ? ' · ' + monthNames[activeFilterMonth - 1] : '') : (filteredEvents().length ? 'Filtered · ' + filteredEvents().length + ' events' : String(y));
    const yearEl = document.getElementById('stats-year');
    if (yearEl) yearEl.textContent = label;
    const cyEl = document.getElementById('contribution-year');
    if (cyEl) cyEl.textContent = '· ' + y;
    loadStatsDist();
    loadContributions();
  } else if (id === 'calendar-overlay') {
    renderCalendarView();
  }
  // focus trap: move focus into overlay and trap Tab
  const panel = overlay.querySelector('.overlay-panel') as HTMLElement | null;
  if (panel) trapFocus(panel);
  else trapFocus(overlay);
}

function closeOverlay(id: string): void {
  const overlay = document.getElementById(id);
  if (!overlay) return;
  overlay.style.display = 'none';
  // release trap before checking body lock
  if (trapContainer && overlay.contains(trapContainer)) releaseFocus();
  // Keep body scroll lock while any overlay is still open.
  const anyOpen = document.querySelectorAll('.overlay[style*="flex"]').length > 0;
  if (!anyOpen) document.body.style.overflow = '';
  if (id === 'map-overlay' && window.location.hash === '#map') {
    try { history.replaceState(null, '', window.location.pathname + window.location.search); } catch (_) {}
  }
}

function openMapAt(lat: number, lng: number, _eventId?: number): void {
  const map = ensureMapInstance();
  if (map) (map as any)._pendingMapFocus = { lat, lng, id: _eventId };
  // update URL for shareable deep link (lat/lng/id, no Go route)
  try {
    const url = new URL(window.location.href);
    url.hash = 'map';
    url.searchParams.set('lat', String(lat));
    url.searchParams.set('lng', String(lng));
    if (_eventId) url.searchParams.set('mapId', String(_eventId));
    history.replaceState(null, '', url.toString());
  } catch (_) {}
  openOverlay('map-overlay');
  // renderMapInstance will consume _pendingMapFocus and open popup + highlight card without scrolling timeline
  // fallback: if map already rendered, set view directly
  if (map && !(map as any)._pendingMapFocus) {
    map.setView([lat, lng], 14);
  }
}

// ── Lightbox State ──
let lightboxEvents: TimelineEvent[] = [];
let lightboxIndex = -1;
let lightboxZoomed = false;
let touchStartX = 0;
let touchStartY = 0;
let lightboxOpen = false;
let lightboxKeyHandler: ((e: KeyboardEvent) => void) | null = null;
let lightboxTouchHandler: ((e: TouchEvent) => void) | null = null;

function showMedia(id: number): void {
  lightboxEvents = Array.isArray(events) ? events.filter(e => e.media_url) : [];
  lightboxIndex = lightboxEvents.findIndex(e => e.id === id);
  if (lightboxIndex === -1) return;
  lightboxZoomed = false;
  renderLightbox();
  openLightbox();
}

function openLightbox(): void {
  const lb = document.getElementById('lightbox');
  if (!lb) return;
  lightboxOpen = true;
  lb.style.display = 'flex';
  document.body.style.overflow = 'hidden';

  lightboxKeyHandler = (e: KeyboardEvent) => {
    if (e.key === 'Escape') { closeLightbox(); return; }
    if (e.key === 'ArrowLeft') { e.preventDefault(); navigateLightbox(-1); }
    if (e.key === 'ArrowRight') { e.preventDefault(); navigateLightbox(1); }
  };
  document.addEventListener('keydown', lightboxKeyHandler);

  lightboxTouchHandler = (e: TouchEvent) => {
    if (!lightboxOpen) return;
    if (e.type === 'touchstart') {
      touchStartX = e.touches[0].clientX;
      touchStartY = e.touches[0].clientY;
    } else if (e.type === 'touchend') {
      const dx = e.changedTouches[0].clientX - touchStartX;
      const dy = e.changedTouches[0].clientY - touchStartY;
      if (Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.5) {
        navigateLightbox(dx > 0 ? -1 : 1);
      }
    }
  };
  lb.addEventListener('touchstart', lightboxTouchHandler);
  lb.addEventListener('touchend', lightboxTouchHandler);
  trapFocus(lb);
}

function closeLightbox(): void {
  const lb = document.getElementById('lightbox');
  if (!lb) return;
  lightboxOpen = false;
  lb.style.display = 'none';
  // only unlock body if no overlay remains open
  const anyOverlayOpen = document.querySelectorAll('.overlay[style*="flex"]').length > 0;
  if (!anyOverlayOpen) document.body.style.overflow = '';
  lightboxZoomed = false;
  if (lightboxKeyHandler) document.removeEventListener('keydown', lightboxKeyHandler);
  if (lightboxTouchHandler) {
    lb.removeEventListener('touchstart', lightboxTouchHandler);
    lb.removeEventListener('touchend', lightboxTouchHandler);
  }
  if (trapContainer === lb) releaseFocus();
}

function navigateLightbox(dir: number): void {
  if (lightboxEvents.length === 0) return;
  lightboxIndex = (lightboxIndex + dir + lightboxEvents.length) % lightboxEvents.length;
  lightboxZoomed = false;
  renderLightbox();
}

function toggleLightboxZoom(img: HTMLImageElement): void {
  if (img.dataset.zoomed === 'true') {
    img.dataset.zoomed = 'false';
    const container = document.getElementById('lightbox-media-container');
    if (container) container.classList.remove('zoomed');
  } else {
    img.dataset.zoomed = 'true';
    const container = document.getElementById('lightbox-media-container');
    if (container) container.classList.add('zoomed');
  }
}

function renderLightbox(): void {
  const event = lightboxEvents[lightboxIndex];
  if (!event) return;

  const container = document.getElementById('lightbox-media-container');
  const titleEl = document.getElementById('lightbox-title');
  const descEl = document.getElementById('lightbox-desc');
  const counterEl = document.getElementById('lightbox-counter');
  const loaderEl = document.getElementById('lightbox-loader');

  if (titleEl) titleEl.textContent = event.title;
  if (counterEl) counterEl.textContent = `${lightboxIndex + 1} / ${lightboxEvents.length}`;

  if (descEl) {
    const dateParts: string[] = [];
    if (event.start_time) {
      dateParts.push(`<i class="fa-regular fa-clock me-1"></i>${event.start_time.substring(0, 5)}${event.end_time ? '–' + event.end_time.substring(0, 5) : ''}`);
    }
    if (event.location) {
      dateParts.push(`<i class="fa-solid fa-location-dot ms-2 me-1"></i>${escapeHtml(event.location)}`);
    }
    const dateInfo = event.date + (dateParts.length ? ' ' + dateParts.join('') : '');
    descEl.innerHTML = `
      <div class="lightbox-date"><i class="fa-solid fa-calendar me-1"></i>${dateInfo}</div>
      ${event.description ? '<div class="lightbox-description">' + renderMarkdown(event.description) + '</div>' : ''}
    `;
  }

  if (loaderEl) loaderEl.style.display = 'flex';

  if (!container) return;
  container.innerHTML = '';
  container.classList.remove('zoomed');

  if (event.media_type === 'video') {
    const video = document.createElement('video');
    video.className = 'lightbox-media lightbox-video';
    video.src = event.media_url;
    video.controls = true;
    video.autoplay = true;
    container.appendChild(video);
    if (loaderEl) loaderEl.style.display = 'none';
  } else if (event.media_type === 'audio') {
    const audio = document.createElement('audio');
    audio.className = 'lightbox-media lightbox-audio';
    audio.src = event.media_url;
    audio.controls = true;
    audio.autoplay = true;
    container.appendChild(audio);
    if (loaderEl) loaderEl.style.display = 'none';
  } else {
    const img = new Image();
    img.onload = () => {
      if (loaderEl) loaderEl.style.display = 'none';
      container!.innerHTML = '';
      img.className = 'lightbox-image';
      img.alt = event.title || 'Event media';
      img.draggable = false;
      img.onclick = () => toggleLightboxZoom(img);
      container!.appendChild(img);
    };
    img.onerror = () => {
      if (loaderEl) loaderEl.style.display = 'none';
      container!.innerHTML = '<p class="text-white-50 mt-5">Failed to load image</p>';
    };
    img.src = event.media_url;
  }
}

let calendarYear: number = new Date().getFullYear();
let calendarMonth: number = new Date().getMonth() + 1;
let calendarEventList: TimelineEvent[] = [];

function renderCalendar(): void {
  calendarEventList = filteredEvents();
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

function restoreFiltersFromURL(): void {
  const params = new URLSearchParams(window.location.search);
  const y = params.get('year');
  const m = params.get('month');
  if (y && /^\d{4}$/.test(y)) {
    const yi = parseInt(y, 10);
    if (!isNaN(yi)) filterYear = yi;
  } else filterYear = null;
  if (m && /^\d+$/.test(m)) {
    const mi = parseInt(m, 10);
    if (mi >= 0 && mi <= 12) { activeFilterMonth = mi; currentMonth = mi; }
  } else { activeFilterMonth = 0; currentMonth = 0; }
  // reflect month pills
  document.querySelectorAll('.month-filter .btn').forEach((btn, i) => {
    const active = i === activeFilterMonth;
    btn.classList.toggle('active', active);
    btn.classList.toggle('btn-dark', active);
    btn.classList.toggle('btn-outline-dark', !active);
    btn.setAttribute('aria-pressed', active ? 'true' : 'false');
  });
}

function initToolbarKeyboard(): void {
  document.querySelectorAll<HTMLElement>('.toolbar-scroll[tabindex="0"]').forEach(el => {
    el.addEventListener('keydown', (e: KeyboardEvent) => {
      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        e.preventDefault();
        const delta = e.key === 'ArrowLeft' ? -80 : 80;
        el.scrollBy({ left: delta, behavior: 'smooth' });
      }
    });
  });
}

function initApp(): void {
  initTheme();
  initToolbarKeyboard();
  loadAnalytics();

  const params = new URLSearchParams(window.location.search);
  const q = params.get('q');
  if (q) {
    const input = document.getElementById('search-input') as HTMLInputElement | null;
    if (input) input.value = q;
  }
  restoreFiltersFromURL();

  loadData();
  loadMemories();
  renderCalendarView();
  loadPersonsForFilter();

  // Escape closes any open overlay.
  document.addEventListener('keydown', (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      document.querySelectorAll<HTMLElement>('.overlay').forEach(ov => {
        if (ov.style.display === 'flex') closeOverlay(ov.id);
      });
    }
  });

  fetch('/api/version').then(r => r.json()).then(d => {
    const versionEl = document.getElementById('version-display');
    if (versionEl) versionEl.textContent = 'v' + d.version;
  }).catch(() => {
    const versionEl = document.getElementById('version-display');
    if (versionEl) versionEl.textContent = 'v1.0.0';
  });

  // 7.2 deep link: /#map or ?lat=&lng= opens canonical map overlay with focus (no Go route)
  const hash = window.location.hash || '';
  const lp = new URLSearchParams(window.location.search);
  const dLat = parseFloat(lp.get('lat') || '');
  const dLng = parseFloat(lp.get('lng') || '');
  const dId = lp.get('mapId') || lp.get('id') || '';
  if (hash === '#map' || (!isNaN(dLat) && !isNaN(dLng))) {
    // wait a tick for data; openMapAt will set pending focus if data not yet there
    setTimeout(() => {
      if (!isNaN(dLat) && !isNaN(dLng)) openMapAt(dLat, dLng, dId ? parseInt(dId, 10) : undefined);
      else openOverlay('map-overlay');
    }, 600);
  }
  window.addEventListener('hashchange', () => {
    if (window.location.hash === '#map') openOverlay('map-overlay');
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
(window as any).scrollToYear = scrollToYear;
(window as any).searchEvents = searchEvents;
(window as any).filterMonth = filterMonth;
(window as any).showMedia = showMedia;
(window as any).loadMoreGallery = loadMoreGallery;
(window as any).removeOneFilter = removeOneFilter;
(window as any).loadData = loadData;
(window as any).calendarPrevMonth = calendarPrevMonth;
(window as any).calendarNextMonth = calendarNextMonth;
(window as any).calendarToday = calendarToday;
(window as any).showCalendarDay = showCalendarDay;
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
(window as any).openOverlay = openOverlay;
(window as any).closeOverlay = closeOverlay;
(window as any).openMapAt = openMapAt;
