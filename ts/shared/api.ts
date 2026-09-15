let csrfToken: string = (window as any).__csrfToken || '';

export async function ensureCSRF(): Promise<string> {
  if (csrfToken) return csrfToken;
  if ((window as any).__csrfToken) {
    csrfToken = (window as any).__csrfToken;
    return csrfToken;
  }
  try {
    const res = await fetch('/api/csrf-token');
    if (res.ok) {
      const data = await res.json();
      csrfToken = data.token;
      (window as any).__csrfToken = csrfToken;
    }
  } catch (e) {}
  return csrfToken;
}

export function csrfHeaders(contentType?: string): Record<string, string> {
  const h: Record<string, string> = {};
  const token = csrfToken || (window as any).__csrfToken || '';
  if (token) h['X-CSRF-Token'] = token;
  if (contentType) h['Content-Type'] = contentType;
  return h;
}
