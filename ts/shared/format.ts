export function escapeHtml(text: string): string {
  if (!text) return '';
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
}

export function getMediaIcon(mediaType: string): string {
  switch (mediaType) {
    case 'video': return 'fa-solid fa-video';
    case 'audio': return 'fa-solid fa-music';
    case 'boardgame': return 'fa-solid fa-dice';
    default: return 'fa-solid fa-image';
  }
}

export function renderMarkdown(text: string): string {
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

export function formatDate(dateStr: string, includeYear?: boolean): string {
  const date = new Date(dateStr);
  if (includeYear) return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / 1048576).toFixed(1) + ' MB';
}

export function weatherIconClass(code: string): string {
  const m: Record<string, string> = {
    '01d': 'sun', '01n': 'moon',
    '02d': 'cloud-sun', '02n': 'cloud-moon',
    '03d': 'cloud', '03n': 'cloud',
    '04d': 'cloud', '04n': 'cloud',
    '09d': 'cloud-showers-heavy', '09n': 'cloud-showers-heavy',
    '10d': 'cloud-rain', '10n': 'cloud-rain',
    '11d': 'cloud-bolt', '11n': 'cloud-bolt',
    '13d': 'snowflake', '13n': 'snowflake',
    '50d': 'smog', '50n': 'smog',
  };
  if (m[code]) return m[code];
  if (!code) return 'cloud-sun';
  const known = new Set(['sun','moon','cloud','cloud-sun','cloud-moon','cloud-rain','cloud-showers-heavy','cloud-bolt','snowflake','smog','wind','clouds']);
  if (known.has(code)) return code;
  return 'cloud-sun';
}
