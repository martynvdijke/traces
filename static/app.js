"use strict";
let currentYear = new Date().getFullYear();
let currentMonth = 0;
let events = [];
let contributions = {};
let galleryPage = 1;
const galleryLimit = 12;
const monthNames = [
    'January', 'February', 'March', 'April', 'May', 'June',
    'July', 'August', 'September', 'October', 'November', 'December'
];
const themeToggle = document.getElementById('theme-toggle');
const themeIcon = themeToggle?.querySelector('i');
function setTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);
    if (themeIcon) {
        themeIcon.className = theme === 'dark' ? 'fa-solid fa-sun' : 'fa-solid fa-moon';
    }
}
function initTheme() {
    const savedTheme = (localStorage.getItem('theme') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'));
    setTheme(savedTheme);
    themeToggle?.addEventListener('click', () => {
        const current = document.documentElement.getAttribute('data-theme');
        setTheme(current === 'dark' ? 'light' : 'dark');
    });
}
function changeYear(year) {
    currentYear = year;
    const yearEl = document.getElementById('current-year');
    if (yearEl)
        yearEl.textContent = String(year);
    loadData();
}
function searchEvents() {
    const input = document.getElementById('search-input');
    if (input?.value) {
        window.location.href = '?q=' + encodeURIComponent(input.value);
    }
}
function filterMonth(month) {
    currentMonth = month;
    const buttons = document.querySelectorAll('.month-filter .btn');
    buttons.forEach((btn, i) => {
        btn.classList.toggle('active', i === month);
        btn.classList.toggle('btn-dark', i === month);
        btn.classList.toggle('btn-outline-dark', i !== month);
    });
    renderTimeline();
    renderGallery();
}
async function loadContributions() {
    try {
        const res = await fetch('/api/contributions?year=' + currentYear);
        contributions = await res.json();
        renderContributionGraph();
        updateStats();
    }
    catch (err) {
        console.error('Failed to load contributions:', err);
    }
}
function renderContributionGraph() {
    const graph = document.getElementById('contribution-graph');
    if (!graph)
        return;
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
function updateStats() {
    const filtered = events && events.length > 0
        ? (currentMonth > 0 ? events.filter(e => new Date(e.date).getMonth() + 1 === currentMonth) : events)
        : [];
    const totalEl = document.getElementById('total-events');
    if (totalEl)
        totalEl.textContent = filtered.length + ' events';
    const images = filtered.filter(e => e.media_type === 'image').length;
    const videos = filtered.filter(e => e.media_type === 'video').length;
    const audio = filtered.filter(e => e.media_type === 'audio').length;
    const locations = new Set(filtered.map(e => e.location).filter(l => l)).size;
    const statImages = document.getElementById('stat-images');
    const statVideos = document.getElementById('stat-videos');
    const statAudio = document.getElementById('stat-audio');
    const statLocations = document.getElementById('stat-locations');
    if (statImages)
        statImages.textContent = String(images);
    if (statVideos)
        statVideos.textContent = String(videos);
    if (statAudio)
        statAudio.textContent = String(audio);
    if (statLocations)
        statLocations.textContent = String(locations);
}
async function loadEvents() {
    try {
        let url = '/api/events?year=' + currentYear;
        if (currentMonth > 0) {
            url += '&month=' + String(currentMonth).padStart(2, '0');
        }
        const res = await fetch(url);
        events = await res.json();
        renderTimeline();
        renderGallery();
    }
    catch (err) {
        console.error('Failed to load events:', err);
    }
}
async function loadData() {
    await Promise.all([loadEvents(), loadContributions()]);
    galleryPage = 1;
}
function renderTimeline() {
    const container = document.getElementById('timeline-container');
    if (!container)
        return;
    if (!events || events.length === 0) {
        container.innerHTML = `
      <div class="text-center text-muted py-5">
        <i class="fa-solid fa-clock fa-3x mb-3"></i>
        <p>No events found for ${currentYear}.</p>
      </div>
    `;
        return;
    }
    const filteredEvents = currentMonth > 0
        ? events.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
        : events;
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
        const peopleList = e.people ? e.people.split(',').map(p => p.trim()).filter(p => p) : [];
        html += `
      <div class="timeline-item ${index % 2 === 0 ? 'left' : 'right'}">
        <div class="timeline-content" onclick="${hasMedia ? 'showMedia(' + e.id + ')' : ''}">
          <div class="timeline-date">${formatDate(e.date)}</div>
          <div class="timeline-title">${escapeHtml(e.title)}</div>
          <div class="timeline-location"><i class="fa-solid fa-location-dot me-1"></i>${escapeHtml(e.location)}</div>
          ${peopleList.length > 0 ? '<div class="timeline-people mb-2"><i class="fa-solid fa-users me-1"></i>' + peopleList.map(p => '<span class="badge bg-secondary me-1">' + escapeHtml(p) + '</span>').join('') + '</div>' : ''}
          ${e.description ? '<p class="timeline-desc">' + escapeHtml(e.description) + '</p>' : ''}
          ${hasMedia ? '<div class="timeline-media-badge"><i class="' + mediaIcon + '"></i> ' + e.media_type + '</div>' : ''}
        </div>
      </div>
    `;
    });
    container.innerHTML = html;
}
function getMediaIcon(mediaType) {
    switch (mediaType) {
        case 'video': return 'fa-solid fa-video';
        case 'audio': return 'fa-solid fa-music';
        default: return 'fa-solid fa-image';
    }
}
function formatDate(dateStr) {
    const date = new Date(dateStr);
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}
function renderGallery() {
    const container = document.getElementById('gallery-container');
    if (!container)
        return;
    if (!events || events.length === 0) {
        container.innerHTML = `
      <div class="col-12 text-center text-muted py-5">
        <i class="fa-solid fa-images fa-3x mb-3"></i>
        <p>No media found for ${currentYear}.</p>
      </div>
    `;
        return;
    }
    const filtered = currentMonth > 0
        ? events.filter(e => new Date(e.date).getMonth() + 1 === currentMonth)
        : events;
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
        const peopleList = e.people ? e.people.split(',').map(p => p.trim()).filter(p => p) : [];
        return `
      <div class="col-md-4 col-lg-3">
        <div class="gallery-item" onclick="showMedia(${e.id})">
          ${e.media_type === 'video'
            ? '<video src="' + e.media_url + '" muted></video>'
            : e.media_type === 'audio'
                ? '<div class="audio-placeholder"><i class="fa-solid fa-music fa-3x"></i></div>'
                : '<img src="' + (e.thumbnail || e.media_url) + '" alt="' + escapeHtml(e.title) + '">'}
          <div class="gallery-overlay">
            <i class="${getMediaIcon(e.media_type)}"></i>
          </div>
          ${peopleList.length > 0 ? '<div class="gallery-people"><i class="fa-solid fa-users"></i> ' + peopleList.length + '</div>' : ''}
        </div>
        <div class="mt-2 text-center small">${escapeHtml(e.title)}</div>
      </div>
    `;
    }).join('');
}
function showMedia(id) {
    const event = events.find(e => e.id === id);
    if (!event)
        return;
    const titleEl = document.getElementById('mediaModalTitle');
    const descEl = document.getElementById('mediaModalDesc');
    const bodyEl = document.getElementById('mediaModalBody');
    if (titleEl)
        titleEl.textContent = event.title;
    if (descEl) {
        descEl.innerHTML = `
      ${event.description || ''}
      <div class="mt-2 small opacity-75">
        <i class="fa-solid fa-calendar me-1"></i>${event.date}
        ${event.location ? '<i class="fa-solid fa-location-dot ms-2 me-1"></i>' + event.location : ''}
      </div>
    `;
    }
    let mediaHtml = '';
    if (event.media_url) {
        if (event.media_type === 'video') {
            mediaHtml = '<video controls class="w-100" src="' + event.media_url + '"></video>';
        }
        else if (event.media_type === 'audio') {
            mediaHtml = '<audio controls class="w-100" src="' + event.media_url + '"></audio>';
        }
        else {
            mediaHtml = '<img src="' + event.media_url + '" class="img-fluid" alt="' + event.title + '">';
        }
    }
    if (bodyEl)
        bodyEl.innerHTML = mediaHtml || '<p>No media available</p>';
    new window.bootstrap.Modal(document.getElementById('mediaModal')).show();
}
function escapeHtml(text) {
    if (!text)
        return '';
    return text.replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}
async function loadMoreGallery() {
    galleryPage++;
    const skip = (galleryPage - 1) * galleryLimit;
    try {
        const url = '/api/events?year=' + currentYear + '&limit=' + galleryLimit + '&skip=' + skip;
        const res = await fetch(url);
        const moreEvents = await res.json();
        const container = document.getElementById('gallery-container');
        const mediaEvents = moreEvents.filter(e => e.media_url);
        if (mediaEvents.length === 0) {
            if (container)
                container.innerHTML += '<div class="col-12 text-center text-muted py-3">No more media to load</div>';
            return;
        }
        if (container) {
            container.innerHTML += mediaEvents.map(e => {
                const peopleList = e.people ? e.people.split(',').map(p => p.trim()).filter(p => p) : [];
                return `
          <div class="col-md-4 col-lg-3">
            <div class="gallery-item" onclick="showMedia(${e.id})">
              ${e.media_type === 'video'
                    ? '<video src="' + e.media_url + '" muted></video>'
                    : e.media_type === 'audio'
                        ? '<div class="audio-placeholder"><i class="fa-solid fa-music fa-3x"></i></div>'
                        : '<img src="' + (e.thumbnail || e.media_url) + '" alt="' + escapeHtml(e.title) + '">'}
              <div class="gallery-overlay">
                <i class="${getMediaIcon(e.media_type)}"></i>
              </div>
              ${peopleList.length > 0 ? '<div class="gallery-people"><i class="fa-solid fa-users"></i> ' + peopleList.length + '</div>' : ''}
            </div>
            <div class="mt-2 text-center small">${escapeHtml(e.title)}</div>
          </div>
        `;
            }).join('');
        }
        events = [...events, ...moreEvents];
    }
    catch (err) {
        console.error('Failed to load more gallery:', err);
    }
}
async function loadRecentActivity() {
    try {
        const res = await fetch('/api/events?limit=5&sort=desc');
        const recentEvents = await res.json();
        renderRecentActivity(recentEvents);
    }
    catch (err) {
        console.error('Failed to load recent activity:', err);
    }
}
function renderRecentActivity(recentEvents) {
    const container = document.getElementById('recent-activity-list');
    if (!container)
        return;
    if (!recentEvents || recentEvents.length === 0) {
        container.innerHTML = '<div class="text-center text-muted py-4"><p>No recent activity</p></div>';
        return;
    }
    container.innerHTML = recentEvents.map(e => `
    <div class="d-flex align-items-center p-2 border-bottom">
      <div class="me-3">
        <i class="${getMediaIcon(e.media_type)} fa-2x text-primary"></i>
      </div>
      <div class="flex-grow-1">
        <div class="fw-bold">${escapeHtml(e.title)}</div>
        <div class="small text-muted">
          <i class="fa-solid fa-calendar me-1"></i>${e.date}
          ${e.location ? '<i class="fa-solid fa-location-dot ms-2 me-1"></i>' + escapeHtml(e.location) : ''}
        </div>
      </div>
      <div>
        <button class="btn btn-sm btn-outline-primary" onclick="showMedia(${e.id})">
          <i class="fa-solid fa-eye"></i>
        </button>
      </div>
    </div>
  `).join('');
}
async function getYearStats(year) {
    try {
        const res = await fetch('/api/events?year=' + year);
        const yearEvents = await res.json();
        return {
            total: yearEvents.length,
            images: yearEvents.filter(e => e.media_type === 'image').length,
            videos: yearEvents.filter(e => e.media_type === 'video').length,
            audio: yearEvents.filter(e => e.media_type === 'audio').length,
            locations: new Set(yearEvents.map(e => e.location).filter(l => l)).size
        };
    }
    catch (err) {
        return { total: 0, images: 0, videos: 0, audio: 0, locations: 0 };
    }
}
async function updateCompare() {
    const year1El = document.getElementById('compare-year-1');
    const year2El = document.getElementById('compare-year-2');
    if (!year1El || !year2El)
        return;
    const year1 = year1El.value;
    const year2 = year2El.value;
    const [stats1, stats2] = await Promise.all([getYearStats(year1), getYearStats(year2)]);
    const stats1El = document.getElementById('compare-stats-1');
    const stats2El = document.getElementById('compare-stats-2');
    const resultEl = document.getElementById('compare-result');
    if (stats1El)
        stats1El.innerHTML = renderCompareStats(stats1);
    if (stats2El)
        stats2El.innerHTML = renderCompareStats(stats2);
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
function renderCompareStats(stats) {
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
function initApp() {
    initTheme();
    loadData();
    loadRecentActivity();
    updateCompare();
    fetch('/api/version').then(r => r.json()).then(d => {
        const versionEl = document.getElementById('version-display');
        if (versionEl)
            versionEl.textContent = 'v' + d.version;
    }).catch(() => {
        const versionEl = document.getElementById('version-display');
        if (versionEl)
            versionEl.textContent = 'v1.0.0';
    });
}
document.addEventListener('DOMContentLoaded', initApp);
window.changeYear = changeYear;
window.searchEvents = searchEvents;
window.filterMonth = filterMonth;
window.showMedia = showMedia;
window.loadMoreGallery = loadMoreGallery;
