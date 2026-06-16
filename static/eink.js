(function() {
  'use strict';

  var eink = {
    enabled: function() {
      return document.documentElement.classList.contains('eink-mode');
    },
    enable: function() {
      document.documentElement.classList.add('eink-mode');
      if (!document.querySelector('link[href*="eink.css"]')) {
        var l = document.createElement('link');
        l.rel = 'stylesheet';
        l.href = '/static/eink.css';
        document.head.appendChild(l);
      }
      document.cookie = 'eink=1;path=/;max-age=' + 31536000;
      document.dispatchEvent(new CustomEvent('eink-change', { detail: { enabled: true } }));
    },
    disable: function() {
      document.documentElement.classList.remove('eink-mode');
      document.cookie = 'eink=;path=/;max-age=0';
      document.dispatchEvent(new CustomEvent('eink-change', { detail: { enabled: false } }));
    },
    toggle: function() {
      if (this.enabled()) {
        this.disable();
      } else {
        this.enable();
      }
    }
  };

  window.einkMode = eink;

  // Keyboard shortcut: press E (not in input fields)
  document.addEventListener('keydown', function(e) {
    if (e.key === 'e' || e.key === 'E') {
      var tag = (e.target || {}).tagName;
      if (tag !== 'INPUT' && tag !== 'TEXTAREA' && tag !== 'SELECT') {
        eink.toggle();
      }
    }
  });
})();
