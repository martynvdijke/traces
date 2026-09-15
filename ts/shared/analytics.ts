export function loadAnalytics(): void {
  fetch('/api/config').then(function (r) { return r.json(); }).then(function (cfg) {
    if (cfg.umami_url && cfg.umami_site && cfg.umami_enabled) {
      var s = document.createElement('script');
      s.async = true;
      s.defer = true;
      s.src = cfg.umami_url + '/script.js';
      s.setAttribute('data-website-id', cfg.umami_site);
      document.head.appendChild(s);
    }
  }).catch(function () { });
}
