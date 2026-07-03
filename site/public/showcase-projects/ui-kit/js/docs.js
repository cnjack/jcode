/* ==========================================================================
   Jui docs — js/docs.js
   Documentation-shell behaviour only:
     • Theme toggle (delegates to Jui.toggleTheme)
     • Sidebar quick-filter (live substring narrowing)
     • Copy-to-clipboard buttons (clipboard API + execCommand fallback)
     • Active-section highlight (IntersectionObserver)
     • Accent-hue swatches (override --jui-accent-h/s/l, persisted)
     • Mobile sidebar (hamburger + backdrop + ESC)
   ========================================================================== */
(function () {
  "use strict";

  var doc = document;
  var root = doc.documentElement;

  /* --------------------------- theme toggle ------------------------ */
  function initThemeToggle() {
    var btn = doc.getElementById("themeToggle");
    if (!btn) return;
    btn.addEventListener("click", function () {
      if (window.Jui && window.Jui.toggleTheme) {
        window.Jui.toggleTheme();
      } else {
        var next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
        root.setAttribute("data-theme", next);
      }
    });
  }

  /* --------------------------- swatches ---------------------------- */
  var SWATCH_KEY = "jui-accent";
  function applyAccent(h, s, l) {
    root.style.setProperty("--jui-accent-h", h);
    root.style.setProperty("--jui-accent-s", s);
    root.style.setProperty("--jui-accent-l", l);
  }
  function markActiveSwatch(values) {
    var swatches = doc.querySelectorAll(".docs-swatch");
    swatches.forEach(function (sw) {
      var match =
        sw.dataset.h === values.h &&
        sw.dataset.s === values.s &&
        sw.dataset.l === values.l;
      sw.classList.toggle("is-active", !!match);
    });
  }
  function initSwatches() {
    var swatches = doc.querySelectorAll(".docs-swatch");
    if (!swatches.length) return;

    // Restore saved accent.
    try {
      var saved = localStorage.getItem(SWATCH_KEY);
      if (saved) {
        var parts = JSON.parse(saved);
        if (parts && parts.h && parts.s && parts.l) {
          applyAccent(parts.h, parts.s, parts.l);
          markActiveSwatch(parts);
        }
      }
    } catch (e) { /* ignore */ }

    swatches.forEach(function (sw) {
      sw.addEventListener("click", function () {
        var v = { h: sw.dataset.h, s: sw.dataset.s, l: sw.dataset.l };
        applyAccent(v.h, v.s, v.l);
        markActiveSwatch(v);
        try { localStorage.setItem(SWATCH_KEY, JSON.stringify(v)); } catch (e) {}
      });
    });
  }

  /* ----------------------------- filter ---------------------------- */
  function initFilter() {
    var input = doc.getElementById("navFilter");
    var nav = doc.getElementById("docsNav");
    if (!input || !nav) return;
    var groups = nav.querySelectorAll(".docs-nav__group");
    var links = nav.querySelectorAll(".docs-nav__link");
    var emptyNote = nav.querySelector(".docs-nav__empty");

    input.addEventListener("input", function () {
      var q = input.value.trim().toLowerCase();
      var anyVisible = false;

      groups.forEach(function (group) {
        var groupLinks = group.querySelectorAll(".docs-nav__link");
        var visibleInGroup = 0;
        groupLinks.forEach(function (link) {
          var text = (link.textContent || "").toLowerCase();
          var match = !q || text.indexOf(q) !== -1;
          if (match) {
            link.removeAttribute("hidden");
            visibleInGroup++;
            anyVisible = true;
          } else {
            link.setAttribute("hidden", "");
          }
        });
        group.classList.toggle("is-hidden", q && visibleInGroup === 0);
      });

      if (emptyNote) {
        emptyNote.classList.toggle("is-visible", !!q && !anyVisible);
      }
    });

    // '/' focuses the filter (when not already in a field).
    doc.addEventListener("keydown", function (e) {
      if (e.key === "/" && !/^(INPUT|TEXTAREA|SELECT)$/.test((doc.activeElement || {}).tagName)) {
        e.preventDefault();
        input.focus();
      }
    });
  }

  /* --------------------------- copy buttons ------------------------ */
  function copyText(text, done) {
    function fallback() {
      try {
        var ta = doc.createElement("textarea");
        ta.value = text;
        ta.setAttribute("readonly", "");
        ta.style.position = "absolute";
        ta.style.left = "-9999px";
        doc.body.appendChild(ta);
        ta.select();
        var ok = doc.execCommand("copy");
        doc.body.removeChild(ta);
        return ok;
      } catch (e) { return false; }
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () {
        if (fallback()) done();
      });
    } else {
      if (fallback()) done();
    }
  }

  function initCopyButtons() {
    var buttons = doc.querySelectorAll(".docs-copy");
    buttons.forEach(function (btn) {
      btn.addEventListener("click", function () {
        var block = btn.closest(".docs-code");
        var pre = block && block.querySelector("pre");
        if (!pre) return;
        var text = pre.innerText.replace(/\u00a0/g, " ");
        var label = btn.querySelector(".docs-copy__label");
        var original = label ? label.textContent : "Copy";
        copyText(text, function () {
          btn.classList.add("is-copied");
          if (label) label.textContent = "Copied!";
          setTimeout(function () {
            btn.classList.remove("is-copied");
            if (label) label.textContent = original;
          }, 1500);
        });
      });
    });
  }

  /* -------------------- active-section highlight ------------------- */
  function initScrollSpy() {
    var nav = doc.getElementById("docsNav");
    var links = nav ? nav.querySelectorAll(".docs-nav__link") : [];
    if (!links.length) return;
    var linkMap = {};
    links.forEach(function (l) {
      var id = (l.getAttribute("href") || "").replace("#", "");
      if (id) linkMap[id] = l;
    });

    var sections = [];
    Object.keys(linkMap).forEach(function (id) {
      var sec = doc.getElementById(id);
      if (sec) sections.push(sec);
    });
    if (!sections.length) return;

    function setActive(id) {
      links.forEach(function (l) { l.classList.remove("is-active"); });
      if (linkMap[id]) linkMap[id].classList.add("is-active");
    }

    if (!("IntersectionObserver" in window)) {
      return;
    }

    var current = sections[0] ? sections[0].id : null;
    if (current) setActive(current);

    var observer = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            current = entry.target.id;
            setActive(current);
          }
        });
      },
      { rootMargin: "-72px 0px -68% 0px", threshold: 0 }
    );
    sections.forEach(function (s) { observer.observe(s); });
  }

  /* ------------------------- mobile sidebar ------------------------ */
  function initSidebar() {
    var burger = doc.getElementById("navToggle");
    var sidebar = doc.getElementById("docsSidebar");
    var backdrop = doc.getElementById("docsBackdrop");
    if (!burger || !sidebar) return;

    function open() {
      sidebar.classList.add("is-open");
      if (backdrop) backdrop.classList.add("is-visible");
      burger.setAttribute("aria-expanded", "true");
    }
    function close() {
      sidebar.classList.remove("is-open");
      if (backdrop) backdrop.classList.remove("is-visible");
      burger.setAttribute("aria-expanded", "false");
    }
    function toggle() {
      if (sidebar.classList.contains("is-open")) close(); else open();
    }

    burger.addEventListener("click", toggle);
    if (backdrop) backdrop.addEventListener("click", close);
    doc.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && sidebar.classList.contains("is-open")) close();
    });
    // Close after navigating on mobile.
    sidebar.addEventListener("click", function (e) {
      if (e.target.closest(".docs-nav__link") && window.innerWidth <= 900) {
        close();
      }
    });
  }

  /* --------------------------- theme customizer --------------------- */
  var CUSTOMIZER_KEY = "jui-customizer";
  function loadCustomizer() { try { return JSON.parse(localStorage.getItem(CUSTOMIZER_KEY)) || {}; } catch (e) { return {}; } }
  function saveCustomizer(cfg) { try { localStorage.setItem(CUSTOMIZER_KEY, JSON.stringify(cfg)); } catch (e) {} }
  function applyCustomizer(cfg) {
    if (!cfg) return;
    if (cfg.hue != null) root.style.setProperty("--jui-accent-h", cfg.hue);
    if (cfg.radius != null) root.style.setProperty("--jui-radius-scale", cfg.radius);
    if (cfg.density != null) root.setAttribute("data-density", cfg.density);
  }
  function initCustomizer() {
    var gear = doc.getElementById("themeCustomizerBtn");
    var panel = doc.getElementById("themeCustomizer");
    if (!gear || !panel) return;
    var hue = doc.getElementById("tcHue");
    var hueVal = doc.getElementById("tcHueVal");
    var radiusBtns = panel.querySelectorAll("[data-tc-radius]");
    var densityBtns = panel.querySelectorAll("[data-tc-density]");
    var reset = doc.getElementById("tcReset");
    function syncControls() {
      var cur = loadCustomizer();
      var h = cur.hue != null ? cur.hue : (parseFloat(getComputedStyle(root).getPropertyValue("--jui-accent-h")) || 245);
      if (hue) hue.value = h;
      if (hueVal) hueVal.textContent = Math.round(h) + "°";
      var rad = cur.radius != null ? cur.radius : 1;
      radiusBtns.forEach(function (b) { b.classList.toggle("is-active", parseFloat(b.getAttribute("data-tc-radius")) === rad); });
      var den = cur.density || "comfortable";
      densityBtns.forEach(function (b) { b.classList.toggle("is-active", b.getAttribute("data-tc-density") === den); });
    }
    if (hue) hue.addEventListener("input", function () {
      var h = parseFloat(hue.value), c = loadCustomizer();
      c.hue = h; applyCustomizer(c); saveCustomizer(c);
      if (hueVal) hueVal.textContent = Math.round(h) + "°";
    });
    radiusBtns.forEach(function (b) {
      b.addEventListener("click", function () {
        var c = loadCustomizer(); c.radius = parseFloat(b.getAttribute("data-tc-radius"));
        applyCustomizer(c); saveCustomizer(c); syncControls();
      });
    });
    densityBtns.forEach(function (b) {
      b.addEventListener("click", function () {
        var c = loadCustomizer(); c.density = b.getAttribute("data-tc-density");
        applyCustomizer(c); saveCustomizer(c); syncControls();
      });
    });
    if (reset) reset.addEventListener("click", function () {
      try { localStorage.removeItem(CUSTOMIZER_KEY); } catch (e) {}
      root.style.removeProperty("--jui-accent-h");
      root.style.removeProperty("--jui-radius-scale");
      root.removeAttribute("data-density");
      syncControls();
    });
    syncControls();
  }

  /* ----------------------------- boot ------------------------------ */
  function init() {
    initThemeToggle();
    initSwatches();
    initCustomizer();
    initFilter();
    initCopyButtons();
    initScrollSpy();
    initSidebar();
  }

  if (doc.readyState === "loading") {
    doc.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
