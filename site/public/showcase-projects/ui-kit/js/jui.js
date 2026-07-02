/* ==========================================================================
   Jui — a zero-dependency UI component library
   js/jui.js  ·  component behaviors (vanilla JS)
   --------------------------------------------------------------------------
   Behaviors:
     • Theme handling        — Jui.getTheme / setTheme / toggleTheme
     • Dismissable alerts &  — [data-jui-dismiss] removes closest
       tags / chips            [data-jui-dismissible] with a fade transition
     • Textarea auto-grow    — <textarea data-jui="autogrow">
     • Button loading toggle — [data-jui="loading-toggle"] spins for 2s
     • Avatar initials       — .jui-avatar[data-name]
     • Select                — data-jui="select" (combobox + listbox)
     • Checkbox group        — data-jui="checkbox-group" (select-all/indeterminate)
     • Switch                — .jui-switch (aria-checked sync)
     • Slider                — data-jui="slider" (value bubble + filled track)
     • Modal / Drawer        — data-jui="modal-trigger"/"drawer-trigger" (trap, ESC, scroll-lock, focus return)
     • Dropdown menu         — data-jui="dropdown" (menu, arrow nav, flip)
     • Tooltip               — data-jui="tooltip" / data-jui-tooltip="…" (delayed/focus, clamped)
     • Popover               — data-jui="popover" (click-toggled, focus in/out)
     • Tabs                  — data-jui="tabs" (roving, sliding indicator)
     • Accordion             — data-jui="accordion" (single/multiple, height anim)
     • Toast                 — Jui.toast({ title, message, variant, action })
   Everything auto-initialises from data-attributes on DOMContentLoaded.
   A single `window.Jui` namespace is exported; Jui.init(root) for dynamic DOM.
   ========================================================================== */
(function () {
  "use strict";

  var STORAGE_KEY = "jui-theme";
  var LEAVE_TIMEOUT = 450; // ms safety net for dismiss transitions
  var uid = 0;

  function reducedMotion() {
    return !!(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
  }

  var FOCUSABLE =
    'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

  function getFocusable(root) {
    return Array.prototype.slice
      .call(root.querySelectorAll(FOCUSABLE))
      .filter(function (el) {
        return el.offsetParent !== null || el === document.activeElement;
      });
  }

  /* ---- body scroll lock (ref-counted) ---- */
  function lockScroll() {
    var b = document.body;
    b.__juiLock = (b.__juiLock || 0) + 1;
    if (b.__juiLock === 1) b.style.overflow = "hidden";
  }
  function unlockScroll() {
    var b = document.body;
    if (b.__juiLock) {
      b.__juiLock -= 1;
      if (b.__juiLock <= 0) {
        b.style.overflow = "";
        b.__juiLock = 0;
      }
    }
  }

  /* ----------------------------- theme ----------------------------- */
  function getTheme() {
    return document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
  }
  function setTheme(theme) {
    var t = theme === "dark" ? "dark" : "light";
    document.documentElement.setAttribute("data-theme", t);
    try { localStorage.setItem(STORAGE_KEY, t); } catch (e) { /* ignore */ }
    document.dispatchEvent(new CustomEvent("jui:theme", { detail: { theme: t } }));
    return t;
  }
  function toggleTheme() { return setTheme(getTheme() === "dark" ? "light" : "dark"); }

  /* --------------------------- dismiss ----------------------------- */
  function dismissTarget(target) {
    if (!target || target.dataset.juiDismissed === "1") return;
    target.dataset.juiDismissed = "1";
    target.classList.add("is-leaving");
    var done = false;
    function finish() {
      if (done) return;
      done = true;
      if (target.parentNode) target.parentNode.removeChild(target);
      target.dispatchEvent(new CustomEvent("jui:dismiss", { bubbles: true }));
    }
    target.addEventListener("transitionend", function handler(ev) {
      if (ev.propertyName === "opacity" || ev.propertyName === "transform") {
        target.removeEventListener("transitionend", handler);
        finish();
      }
    });
    setTimeout(finish, LEAVE_TIMEOUT);
  }
  function findDismissable(trigger) {
    return trigger.closest("[data-jui-dismissible], .jui-alert, .jui-tag");
  }

  /* -------------------------- autogrow ----------------------------- */
  function autogrow(el) {
    if (!el || el.__juiAutogrow) return;
    el.__juiAutogrow = true;
    function grow() {
      el.style.height = "auto";
      el.style.height = Math.max(el.scrollHeight, el.dataset.minH || 0) + "px";
    }
    el.addEventListener("input", grow);
    window.addEventListener("resize", grow);
    requestAnimationFrame(grow);
    setTimeout(grow, 60);
  }

  /* ----------------------- loading toggle -------------------------- */
  function bindLoadingToggle(btn) {
    if (btn.__juiLoading) return;
    btn.__juiLoading = true;
    var ms = parseInt(btn.dataset.loadingMs, 10) || 2000;
    btn.addEventListener("click", function () {
      if (btn.hasAttribute("data-loading")) return;
      btn.setAttribute("data-loading", "");
      btn.setAttribute("aria-busy", "true");
      setTimeout(function () {
        btn.removeAttribute("data-loading");
        btn.removeAttribute("aria-busy");
      }, ms);
    });
  }

  /* ------------------------ avatar initials ------------------------ */
  function initialsFromName(name) {
    var parts = (name || "").trim().split(/\s+/).filter(Boolean);
    if (!parts.length) return "?";
    if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
  }
  function hueFromName(name) {
    var h = 0, str = name || "?";
    for (var i = 0; i < str.length; i++) h = (h * 31 + str.charCodeAt(i)) >>> 0;
    return h % 360;
  }
  function initAvatar(el) {
    if (el.__juiAvatar) return;
    el.__juiAvatar = true;
    if (el.querySelector("img, svg")) return;
    var name = el.dataset.name || "";
    if (!name) return;
    var h = hueFromName(name), h2 = (h + 38) % 360;
    el.style.setProperty("--av-grad", "linear-gradient(135deg, hsl(" + h + " 62% 52%), hsl(" + h2 + " 68% 44%))");
    if (!el.textContent.trim()) {
      var span = document.createElement("span");
      span.className = "jui-avatar__initials";
      span.textContent = initialsFromName(name);
      el.appendChild(span);
    }
  }
  function initAvatarGroup(group) {
    if (group.__juiAvGroup) return;
    group.__juiAvGroup = true;
    Array.prototype.slice.call(group.querySelectorAll(":scope > .jui-avatar")).forEach(initAvatar);
  }

  /* ==========================================================================
     SELECT  ·  data-jui="select"
     ========================================================================== */
  function initSelect(wrap) {
    if (wrap.__juiSelect) return;
    wrap.__juiSelect = true;
    var trigger = wrap.querySelector(".jui-select__trigger");
    var list = wrap.querySelector(".jui-select__list");
    if (!trigger || !list) return;
    var options = Array.prototype.slice.call(list.querySelectorAll('[role="option"]'));
    var enabled = options.filter(function (o) { return o.getAttribute("aria-disabled") !== "true"; });
    var valueEl = wrap.querySelector(".jui-select__value");
    var hidden = wrap.querySelector('input[type="hidden"]');
    var active = -1, typing = "", typingTimer;
    options.forEach(function (o) { if (!o.id) o.id = "jui-opt-" + (++uid); });
    trigger.setAttribute("aria-haspopup", "listbox");
    trigger.setAttribute("aria-controls", list.id || "");

    function syncValue() {
      var sel = list.querySelector('[role="option"][aria-selected="true"]');
      if (valueEl) valueEl.textContent = sel ? sel.textContent : (wrap.dataset.placeholder || "Select…");
      if (hidden) hidden.value = sel ? (sel.dataset.value || sel.textContent) : "";
    }
    function isOpen() { return trigger.getAttribute("aria-expanded") === "true"; }
    function setActive(idx, scroll) {
      enabled.forEach(function (o) { o.classList.remove("is-active"); });
      if (idx >= 0 && enabled[idx]) {
        enabled[idx].classList.add("is-active");
        trigger.setAttribute("aria-activedescendant", enabled[idx].id);
        if (scroll) enabled[idx].scrollIntoView({ block: "nearest" });
      } else { trigger.removeAttribute("aria-activedescendant"); }
      active = idx;
    }
    function open() {
      if (isOpen()) return;
      list.hidden = false;
      trigger.setAttribute("aria-expanded", "true");
      wrap.classList.add("is-open");
      var selIdx = -1;
      enabled.forEach(function (o, i) { if (o.getAttribute("aria-selected") === "true") selIdx = i; });
      requestAnimationFrame(function () { setActive(selIdx >= 0 ? selIdx : 0, true); });
    }
    function close() {
      list.hidden = true;
      trigger.setAttribute("aria-expanded", "false");
      wrap.classList.remove("is-open");
      trigger.removeAttribute("aria-activedescendant");
      active = -1;
    }
    function choose(opt) {
      options.forEach(function (o) { o.setAttribute("aria-selected", "false"); });
      opt.setAttribute("aria-selected", "true");
      syncValue();
      close();
      trigger.focus();
      wrap.dispatchEvent(new CustomEvent("jui:select", { bubbles: true, detail: { value: opt.dataset.value || opt.textContent } }));
    }

    trigger.addEventListener("click", function () { if (isOpen()) close(); else open(); });
    trigger.addEventListener("keydown", function (e) {
      var k = e.key;
      if (!isOpen()) {
        if (k === "Enter" || k === " " || k === "Spacebar" || k === "ArrowDown" || k === "Down") { e.preventDefault(); open(); }
        return;
      }
      if (k === "ArrowDown" || k === "Down") { e.preventDefault(); setActive(Math.min(active + 1, enabled.length - 1), true); }
      else if (k === "ArrowUp" || k === "Up") { e.preventDefault(); setActive(Math.max(active - 1, 0), true); }
      else if (k === "Home") { e.preventDefault(); setActive(0, true); }
      else if (k === "End") { e.preventDefault(); setActive(enabled.length - 1, true); }
      else if (k === "Escape") { e.preventDefault(); close(); }
      else if (k === "Enter" || k === " " || k === "Spacebar") { e.preventDefault(); if (active >= 0) choose(enabled[active]); }
      else if (k === "Tab") { close(); }
      else if (k.length === 1) {
        typing += k.toLowerCase();
        clearTimeout(typingTimer);
        typingTimer = setTimeout(function () { typing = ""; }, 600);
        for (var i = 0; i < enabled.length; i++) {
          if (enabled[i].textContent.trim().toLowerCase().indexOf(typing) === 0) { setActive(i, true); break; }
        }
      }
    });
    list.addEventListener("click", function (e) {
      var o = e.target.closest('[role="option"]');
      if (o && o.getAttribute("aria-disabled") !== "true") choose(o);
    });
    document.addEventListener("click", function (e) { if (isOpen() && !wrap.contains(e.target)) close(); });
    syncValue();
  }

  /* ==========================================================================
     CHECKBOX GROUP (select-all / indeterminate)  ·  data-jui="checkbox-group"
     ========================================================================== */
  function initCheckboxGroup(g) {
    if (g.__juiCB) return;
    g.__juiCB = true;
    var master = g.querySelector("[data-checkbox-master]");
    var items = Array.prototype.slice.call(g.querySelectorAll("[data-checkbox-item]"));
    if (!master || !items.length) return;
    function sync() {
      var n = items.filter(function (i) { return i.checked; }).length;
      master.checked = n === items.length;
      master.indeterminate = n > 0 && n < items.length;
    }
    master.addEventListener("change", function () { items.forEach(function (i) { i.checked = master.checked; }); });
    items.forEach(function (i) { i.addEventListener("change", sync); });
    sync();
  }

  /* ==========================================================================
     SWITCH  ·  .jui-switch  (aria-checked sync)
     ========================================================================== */
  function initSwitch(el) {
    if (el.__juiSwitch) return;
    el.__juiSwitch = true;
    function sync() { el.setAttribute("aria-checked", el.checked ? "true" : "false"); }
    sync();
    el.addEventListener("change", sync);
  }

  /* ==========================================================================
     SLIDER  ·  data-jui="slider"  (value bubble + filled track)
     ========================================================================== */
  function initSlider(input) {
    if (input.__juiSlider) return;
    input.__juiSlider = true;
    var host = input.parentNode;
    if (getComputedStyle(host).position === "static") host.style.position = "relative";
    var bubble = document.createElement("span");
    bubble.className = "jui-slider__bubble";
    bubble.setAttribute("aria-hidden", "true");
    host.appendChild(bubble);
    function pct() {
      var min = parseFloat(input.min) || 0, max = parseFloat(input.max) || 100, val = parseFloat(input.value);
      return max === min ? 0 : ((val - min) / (max - min)) * 100;
    }
    function render() {
      var p = pct();
      bubble.textContent = input.value;
      bubble.style.left = p + "%";
      input.style.setProperty("--jui-slider-pct", p + "%");
    }
    render();
    input.addEventListener("input", render);
    input.addEventListener("focus", function () { bubble.classList.add("is-visible"); });
    input.addEventListener("blur", function () { bubble.classList.remove("is-visible"); });
    input.addEventListener("pointerdown", function () { bubble.classList.add("is-visible"); });
    input.addEventListener("pointerup", function () { if (document.activeElement !== input) bubble.classList.remove("is-visible"); });
  }

  /* ==========================================================================
     OVERLAY controller (shared by Modal + Drawer)
     ========================================================================== */
  function openOverlay(overlay, trigger) {
    if (overlay.classList.contains("is-open")) return;
    overlay.__juiLastFocused = trigger || document.activeElement;
    overlay.classList.add("is-open");
    overlay.setAttribute("aria-hidden", "false");
    lockScroll();
    openStack.push(overlay);
    var panel = overlay.querySelector(".jui-modal__panel, .jui-drawer__panel") || overlay;
    // Tab-trap stays keyed to the overlay element.
    overlay.__juiKey = function (e) {
      if (e.key === "Tab") {
        var f = getFocusable(panel);
        if (!f.length) { e.preventDefault(); return; }
        var first = f[0], last = f[f.length - 1];
        if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
        else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
      }
    };
    overlay.addEventListener("keydown", overlay.__juiKey);
    overlay.addEventListener("click", overlay.__juiClick = function (e) { if (e.target === overlay) closeOverlay(overlay); });
    // Double rAF: frame 1 lets .is-open paint, frame 2 means computed
    // visibility is now 'visible' (CSS delays visibility only on close), so
    // focus() lands reliably instead of silently failing on a hidden element.
    requestAnimationFrame(function () {
      requestAnimationFrame(function () {
        if (!overlay.classList.contains("is-open")) return;
        var f = getFocusable(panel);
        var target = f[0] || panel;
        if (target.tabIndex < 0) target.setAttribute("tabindex", "-1");
        try { target.focus({ preventScroll: true }); } catch (e) { /* ignore */ }
      });
    });
  }
  function closeOverlay(overlay) {
    if (!overlay.classList.contains("is-open")) return;
    overlay.classList.remove("is-open");
    overlay.setAttribute("aria-hidden", "true");
    unlockScroll();
    var idx = openStack.indexOf(overlay);
    if (idx > -1) openStack.splice(idx, 1);
    if (overlay.__juiKey) { overlay.removeEventListener("keydown", overlay.__juiKey); overlay.__juiKey = null; }
    if (overlay.__juiClick) { overlay.removeEventListener("click", overlay.__juiClick); overlay.__juiClick = null; }
    var t = overlay.__juiLastFocused;
    if (t && t.focus) { try { t.focus(); } catch (e) { /* ignore */ } }
    overlay.__juiLastFocused = null;
  }
  // Document-level Escape: closes the TOPMOST open overlay regardless of where
  // focus sits, so ESC works even if focus never entered the panel.
  var openStack = [];
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && openStack.length) {
      e.preventDefault();
      e.stopPropagation();
      closeOverlay(openStack[openStack.length - 1]);
    }
  }, true);

  /* ==========================================================================
     DROPDOWN MENU  ·  data-jui="dropdown"
     ========================================================================== */
  function initDropdown(wrap) {
    if (wrap.__juiDD) return;
    wrap.__juiDD = true;
    var trigger = wrap.querySelector("[data-dropdown-trigger]") || wrap.querySelector("button");
    var menu = wrap.querySelector('[role="menu"]');
    if (!trigger || !menu) return;
    var items = Array.prototype.slice.call(menu.querySelectorAll('[role="menuitem"]'));
    var enabled = items.filter(function (i) { return i.getAttribute("aria-disabled") !== "true"; });
    var highlight = -1;
    trigger.setAttribute("aria-haspopup", "menu");
    function isOpen() { return trigger.getAttribute("aria-expanded") === "true"; }
    function flip() {
      menu.classList.remove("is-up");
      var r = menu.getBoundingClientRect();
      if (r.bottom > window.innerHeight && r.top - r.height > 0) menu.classList.add("is-up");
    }
    function open() {
      menu.hidden = false;
      trigger.setAttribute("aria-expanded", "true");
      wrap.classList.add("is-open");
      flip();
      highlight = 0;
      requestAnimationFrame(function () { setH(0); });
    }
    function close() {
      menu.hidden = true;
      trigger.setAttribute("aria-expanded", "false");
      wrap.classList.remove("is-open");
    }
    function setH(idx) {
      highlight = idx;
      enabled.forEach(function (i) { i.classList.remove("is-highlight"); });
      if (idx >= 0 && enabled[idx]) { enabled[idx].classList.add("is-highlight"); enabled[idx].focus({ preventScroll: true }); }
    }
    function activate(it) {
      close();
      trigger.focus();
      var out = wrap.dataset.dropdownOutput;
      if (out) { var el = document.getElementById(out); if (el) el.textContent = "Last action: " + (it.dataset.value || it.textContent.trim()); }
      wrap.dispatchEvent(new CustomEvent("jui:dropdown", { bubbles: true, detail: { value: it.dataset.value || it.textContent.trim() } }));
    }
    trigger.addEventListener("click", function (e) { e.stopPropagation(); if (isOpen()) close(); else open(); });
    trigger.addEventListener("keydown", function (e) {
      if (!isOpen() && (e.key === "ArrowDown" || e.key === "Enter" || e.key === " " || e.key === "Spacebar")) { e.preventDefault(); open(); }
    });
    menu.addEventListener("keydown", function (e) {
      var k = e.key;
      if (k === "ArrowDown") { e.preventDefault(); setH(highlight < enabled.length - 1 ? highlight + 1 : 0); }
      else if (k === "ArrowUp") { e.preventDefault(); setH(highlight > 0 ? highlight - 1 : enabled.length - 1); }
      else if (k === "Home") { e.preventDefault(); setH(0); }
      else if (k === "End") { e.preventDefault(); setH(enabled.length - 1); }
      else if (k === "Escape") { e.preventDefault(); close(); trigger.focus(); }
      else if (k === "Tab") { close(); }
    });
    enabled.forEach(function (it, i) {
      it.addEventListener("mouseenter", function () { setH(i); });
      it.addEventListener("keydown", function (e) { if (e.key === "Enter" || e.key === " " || e.key === "Spacebar") { e.preventDefault(); activate(it); } });
    });
    menu.addEventListener("click", function (e) { var it = e.target.closest('[role="menuitem"]'); if (it && it.getAttribute("aria-disabled") !== "true") activate(it); });
    document.addEventListener("click", function (e) { if (isOpen() && !wrap.contains(e.target)) close(); });
  }

  /* ==========================================================================
     TOOLTIP  ·  data-jui="tooltip" | data-jui-tooltip="…"
     ========================================================================== */
  function initTooltip(el) {
    if (el.__juiTip) return;
    el.__juiTip = true;
    var isAttr = el.hasAttribute("data-jui-tooltip");
    var childTip = el.querySelector(":scope > .jui-tooltip");
    var tip = childTip;
    if (!tip) {
      tip = document.createElement("div");
      tip.className = "jui-tooltip";
      tip.setAttribute("role", "tooltip");
      tip.textContent = isAttr ? el.getAttribute("data-jui-tooltip") : (el.getAttribute("data-tooltip") || "");
      document.body.appendChild(tip);
    } else if (tip.parentNode !== document.body) {
      // Relocate a child tooltip to <body> so it positions against the viewport.
      document.body.appendChild(tip);
    }
    if (!tip.id) tip.id = "jui-tip-" + (++uid);
    el.setAttribute("aria-describedby", tip.id);
    var placement = el.getAttribute("data-placement") || "top";
    var showTimer, hideTimer;
    function position() {
      tip.className = "jui-tooltip is-visible jui-tooltip--" + placement;
      var r = el.getBoundingClientRect(), tr = tip.getBoundingClientRect();
      var sx = window.scrollX, sy = window.scrollY;
      var x, y;
      if (placement === "top") { x = r.left + r.width / 2 - tr.width / 2; y = r.top - tr.height - 8; }
      else if (placement === "bottom") { x = r.left + r.width / 2 - tr.width / 2; y = r.bottom + 8; }
      else if (placement === "left") { x = r.left - tr.width - 8; y = r.top + r.height / 2 - tr.height / 2; }
      else { x = r.right + 8; y = r.top + r.height / 2 - tr.height / 2; }
      var pad = 8;
      x = Math.max(pad, Math.min(x, document.documentElement.clientWidth - tr.width - pad));
      y = Math.max(pad, Math.min(y, document.documentElement.clientHeight - tr.height - pad));
      tip.style.left = x + sx + "px";
      tip.style.top = y + sy + "px";
    }
    function reveal(immediate) {
      clearTimeout(hideTimer); clearTimeout(showTimer);
      // Keyboard focus shows instantly; mouse hover waits ~400ms.
      if (immediate) { position(); } else { showTimer = setTimeout(position, 400); }
    }
    function hide() { clearTimeout(showTimer); hideTimer = setTimeout(function () { tip.classList.remove("is-visible"); }, 60); }
    el.addEventListener("mouseenter", function () { reveal(false); });
    el.addEventListener("mouseleave", hide);
    el.addEventListener("focus", function () { reveal(true); });
    el.addEventListener("blur", hide);
    el.addEventListener("keydown", function (e) { if (e.key === "Escape") hide(); });
  }

  /* ==========================================================================
     POPOVER  ·  data-jui="popover"
     ========================================================================== */
  function initPopover(trigger) {
    if (trigger.__juiPop) return;
    trigger.__juiPop = true;
    var sel = trigger.getAttribute("data-target") || trigger.getAttribute("aria-controls");
    var pop = sel ? document.getElementById(sel.replace("#", "")) : null;
    if (!pop) return;
    trigger.setAttribute("aria-controls", pop.id);
    var lastFocused;
    function isOpen() { return trigger.getAttribute("aria-expanded") === "true"; }
    function open() {
      pop.hidden = false;
      trigger.setAttribute("aria-expanded", "true");
      pop.classList.add("is-open");
      lastFocused = document.activeElement;
      // Double rAF so computed visibility is 'visible' before focusing.
      requestAnimationFrame(function () { requestAnimationFrame(function () { var f = getFocusable(pop); var t = f[0] || pop; if (t.tabIndex < 0) t.setAttribute("tabindex", "-1"); try { t.focus(); } catch (e) {} }); });
    }
    function close() {
      pop.hidden = true;
      trigger.setAttribute("aria-expanded", "false");
      pop.classList.remove("is-open");
      if (lastFocused && lastFocused.focus) lastFocused.focus();
    }
    trigger.addEventListener("click", function (e) { e.stopPropagation(); if (isOpen()) close(); else open(); });
    pop.addEventListener("keydown", function (e) { if (e.key === "Escape") { e.preventDefault(); close(); trigger.focus(); } });
    document.addEventListener("click", function (e) { if (isOpen() && !pop.contains(e.target) && e.target !== trigger && !trigger.contains(e.target)) close(); });
  }

  /* ==========================================================================
     TABS  ·  data-jui="tabs"
     ========================================================================== */
  function initTabs(wrap) {
    if (wrap.__juiTabs) return;
    wrap.__juiTabs = true;
    var tabs = Array.prototype.slice.call(wrap.querySelectorAll('[role="tab"]'));
    var indicator = wrap.querySelector(".jui-tabs__indicator");
    function moveIndicator(tab) {
      if (!indicator) return;
      var list = tab.parentNode;
      var lr = list.getBoundingClientRect(), tr = tab.getBoundingClientRect();
      indicator.style.width = tr.width + "px";
      indicator.style.transform = "translateX(" + (tr.left - lr.left + list.scrollLeft) + "px)";
    }
    function activate(tab, focus) {
      tabs.forEach(function (t) {
        var sel = t === tab;
        t.setAttribute("aria-selected", sel ? "true" : "false");
        t.tabIndex = sel ? 0 : -1;
        t.classList.toggle("is-active", sel);
        var p = document.getElementById(t.getAttribute("aria-controls"));
        if (p) p.hidden = !sel;
      });
      moveIndicator(tab);
      if (focus) tab.focus();
    }
    tabs.forEach(function (tab, i) {
      tab.addEventListener("click", function () { activate(tab, true); });
      tab.addEventListener("keydown", function (e) {
        var k = e.key;
        if (k === "ArrowRight") { e.preventDefault(); activate(tabs[(i + 1) % tabs.length], true); }
        else if (k === "ArrowLeft") { e.preventDefault(); activate(tabs[(i - 1 + tabs.length) % tabs.length], true); }
        else if (k === "Home") { e.preventDefault(); activate(tabs[0], true); }
        else if (k === "End") { e.preventDefault(); activate(tabs[tabs.length - 1], true); }
      });
    });
    var initial = tabs.filter(function (t) { return t.getAttribute("aria-selected") === "true"; })[0] || tabs[0];
    if (initial) activate(initial, false);
    window.addEventListener("resize", function () {
      var sel = tabs.filter(function (t) { return t.getAttribute("aria-selected") === "true"; })[0];
      if (sel) moveIndicator(sel);
    });
  }

  /* ==========================================================================
     ACCORDION  ·  data-jui="accordion"
     ========================================================================== */
  function initAccordion(wrap) {
    if (wrap.__juiAcc) return;
    wrap.__juiAcc = true;
    var headers = Array.prototype.slice.call(wrap.querySelectorAll(".jui-accordion__header"));
    var multiple = wrap.hasAttribute("data-multiple");
    function setHeader(h, open) {
      h.setAttribute("aria-expanded", open ? "true" : "false");
      h.classList.toggle("is-open", open);
      var panel = document.getElementById(h.getAttribute("aria-controls"));
      if (panel) panel.classList.toggle("is-open", open);
    }
    headers.forEach(function (h, i) {
      h.addEventListener("click", function () {
        var open = h.getAttribute("aria-expanded") === "true";
        if (!open && !multiple) { headers.forEach(function (o) { if (o !== h) setHeader(o, false); }); }
        setHeader(h, !open);
      });
      h.addEventListener("keydown", function (e) {
        var k = e.key;
        if (k === "ArrowDown") { e.preventDefault(); headers[(i + 1) % headers.length].focus(); }
        else if (k === "ArrowUp") { e.preventDefault(); headers[(i - 1 + headers.length) % headers.length].focus(); }
        else if (k === "Home") { e.preventDefault(); headers[0].focus(); }
        else if (k === "End") { e.preventDefault(); headers[headers.length - 1].focus(); }
      });
    });
  }

  /* ==========================================================================
     TOAST  ·  Jui.toast({ title, message, variant, action, duration })
     ========================================================================== */
  var TOAST_ICONS = {
    success: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 5 5L20 7"/></svg>',
    warning: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 2 20h20L12 3z"/><path d="M12 10v4M12 17h.01"/></svg>',
    danger: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M15 9l-6 6M9 9l6 6"/></svg>',
    info: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 8v5M12 16h.01"/></svg>'
  };
  function ensureToastRegion() {
    var r = document.querySelector(".jui-toast-region");
    if (!r) {
      r = document.createElement("div");
      r.className = "jui-toast-region";
      r.setAttribute("role", "status");
      r.setAttribute("aria-live", "polite");
      r.setAttribute("aria-atomic", "false");
      document.body.appendChild(r);
    }
    return r;
  }
  function removeToast(t) {
    if (!t || t.__leaving) return;
    t.__leaving = true;
    t.classList.add("is-leaving");
    t.classList.remove("is-visible");
    if (t.__timer) clearTimeout(t.__timer);
    setTimeout(function () { if (t.parentNode) t.parentNode.removeChild(t); }, 260);
  }
  function toast(opts) {
    opts = opts || {};
    var region = ensureToastRegion();
    var live = Array.prototype.slice.call(region.querySelectorAll(".jui-toast:not(.is-leaving)"));
    while (live.length >= 5) { removeToast(live.shift()); }
    var variant = opts.variant && TOAST_ICONS[opts.variant] ? opts.variant : "info";
    var t = document.createElement("div");
    t.className = "jui-toast jui-toast--" + variant;
    t.setAttribute("role", "status");
    var icon = '<div class="jui-toast__icon">' + (TOAST_ICONS[variant] || TOAST_ICONS.info) + "</div>";
    var body = '<div class="jui-toast__body"><div class="jui-toast__title"></div>' + (opts.message ? '<div class="jui-toast__text"></div>' : "") + "</div>";
    t.innerHTML = icon + body;
    t.querySelector(".jui-toast__title").textContent = opts.title || "";
    if (opts.message) t.querySelector(".jui-toast__text").textContent = opts.message;
    if (opts.action) {
      var b = document.createElement("button");
      b.type = "button";
      b.className = "jui-toast__action";
      b.textContent = opts.action.label || "Action";
      b.addEventListener("click", function () { try { opts.action.onClick && opts.action.onClick(); } catch (e) { /* ignore */ } removeToast(t); });
      t.querySelector(".jui-toast__body").appendChild(b);
    }
    var close = document.createElement("button");
    close.type = "button";
    close.className = "jui-toast__close";
    close.setAttribute("aria-label", "Dismiss notification");
    close.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 6l12 12M18 6 6 18"/></svg>';
    close.addEventListener("click", function () { removeToast(t); });
    t.appendChild(close);
    var bar = document.createElement("div");
    bar.className = "jui-toast__progress";
    var fill = document.createElement("div");
    fill.className = "jui-toast__progress-fill";
    bar.appendChild(fill);
    t.appendChild(bar);
    region.appendChild(t);
    requestAnimationFrame(function () { t.classList.add("is-visible"); });

    var duration = opts.duration || 4500, remaining = duration, start = Date.now(), started = false;
    function runProgress(ms) { fill.style.transition = "none"; fill.style.width = "100%"; requestAnimationFrame(function () { fill.style.transition = "width " + ms + "ms linear"; fill.style.width = "0%"; }); }
    function startTimer() { start = Date.now(); started = true; t.__timer = setTimeout(function () { removeToast(t); }, remaining); runProgress(remaining); }
    function pauseTimer() { if (!started) return; clearTimeout(t.__timer); var elapsed = Date.now() - start; remaining = Math.max(0, remaining - elapsed); var frac = remaining / duration; fill.style.transition = "none"; fill.style.width = (frac * 100) + "%"; }
    t.addEventListener("mouseenter", pauseTimer);
    t.addEventListener("mouseleave", startTimer);
    startTimer();
    return { dismiss: function () { removeToast(t); } };
  }

  /* ==========================================================================
      TABLE SORT  ·  data-jui="table-sort"  (th[data-sort-key])
      ========================================================================== */
  function tableCellVal(cell, type) {
    if (!cell) return type === "number" ? 0 : "";
    var v = (cell.getAttribute("data-sort-value") || cell.textContent || "").trim();
    if (type === "number") { var n = parseFloat(v.replace(/[^0-9.\-]/g, "")); return isNaN(n) ? 0 : n; }
    return v.toLowerCase();
  }
  function initTableSort(table) {
    if (table.__juiSort) return;
    table.__juiSort = true;
    var headers = Array.prototype.slice.call(table.querySelectorAll("th[data-sort-key]"));
    headers.forEach(function (th) {
      if (!th.querySelector(".jui-table__sort")) {
        var s = document.createElement("span");
        s.className = "jui-table__sort";
        s.setAttribute("aria-hidden", "true");
        s.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>';
        th.appendChild(s);
      }
      th.setAttribute("tabindex", "0");
      if (!th.getAttribute("aria-sort")) th.setAttribute("aria-sort", "none");
    });
    function applySort(th, dir) {
      var idx = th.cellIndex;
      var type = th.getAttribute("data-sort-type");
      var tbody = table.tBodies[0];
      if (!tbody) return;
      var rows = Array.prototype.slice.call(tbody.rows);
      if (!type) {
        var numeric = rows.every(function (r) {
          var t = (r.cells[idx] ? r.cells[idx].textContent : "").trim();
          return t !== "" && !isNaN(parseFloat(t.replace(/[^0-9.\-]/g, "")));
        });
        type = numeric ? "number" : "string";
      }
      rows.sort(function (a, b) {
        var av = tableCellVal(a.cells[idx], type), bv = tableCellVal(b.cells[idx], type);
        if (av < bv) return dir === "asc" ? -1 : 1;
        if (av > bv) return dir === "asc" ? 1 : -1;
        return 0;
      });
      rows.forEach(function (r) { tbody.appendChild(r); });
      headers.forEach(function (h) {
        h.setAttribute("aria-sort", h === th ? (dir === "asc" ? "ascending" : "descending") : "none");
      });
    }
    headers.forEach(function (th) {
      var fire = function () { applySort(th, th.getAttribute("aria-sort") === "ascending" ? "desc" : "asc"); };
      th.addEventListener("click", fire);
      th.addEventListener("keydown", function (e) { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); fire(); } });
    });
  }

  /* ==========================================================================
      PAGINATION  ·  data-jui="pagination"  data-pages="N"  data-pagination-output="#id"
      ========================================================================== */
  function pageRange(cur, total) {
    var res = [];
    if (total <= 7) { for (var i = 1; i <= total; i++) res.push(i); return res; }
    res.push(1);
    if (cur > 3) res.push("...");
    var start = Math.max(2, cur - 1), end = Math.min(total - 1, cur + 1);
    for (var j = start; j <= end; j++) res.push(j);
    if (cur < total - 2) res.push("...");
    res.push(total);
    return res;
  }
  function initPagination(nav) {
    if (nav.__juiPg) return;
    nav.__juiPg = true;
    var pages = parseInt(nav.dataset.pages, 10) || 1;
    var outSel = nav.getAttribute("data-pagination-output");
    var out = outSel ? document.querySelector(outSel) : null;
    var current = 1;
    function emit() {
      if (out) out.textContent = "Page " + current + " of " + pages;
      nav.dispatchEvent(new CustomEvent("jui:pagination", { bubbles: true, detail: { page: current, pages: pages } }));
    }
    function arrow(label, path, side) {
      var b = document.createElement("button");
      b.type = "button"; b.className = "jui-pagination__item"; b.setAttribute("aria-label", label);
      b.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="' + path + '"/></svg>';
      b.disabled = side === "prev" ? current === 1 : current === pages;
      b.addEventListener("click", function () {
        if (side === "prev" && current > 1) current--;
        else if (side === "next" && current < pages) current++;
        render(); emit();
      });
      return b;
    }
    function render() {
      nav.innerHTML = "";
      nav.appendChild(arrow("Previous page", "m15 18-6-6 6-6", "prev"));
      pageRange(current, pages).forEach(function (p) {
        if (p === "...") {
          var e = document.createElement("span");
          e.className = "jui-pagination__item jui-pagination__ellipsis"; e.textContent = "…";
          nav.appendChild(e);
        } else {
          var b = document.createElement("button");
          b.type = "button"; b.className = "jui-pagination__item"; b.textContent = String(p);
          if (p === current) { b.classList.add("is-current"); b.setAttribute("aria-current", "page"); b.tabIndex = -1; }
          b.addEventListener("click", function () { current = p; render(); emit(); });
          nav.appendChild(b);
        }
      });
      nav.appendChild(arrow("Next page", "m9 18 6-6-6-6", "next"));
    }
    render(); emit();
  }

  /* ==========================================================================
      STEPPER  ·  data-jui="stepper"  (Next/Back via data-stepper-next/back)
      ========================================================================== */
  function initStepper(host) {
    if (host.__juiStep) return;
    host.__juiStep = true;
    var steps = Array.prototype.slice.call(host.querySelectorAll(".jui-stepper__step"));
    if (!steps.length) return;
    function circle(s, done, idx) {
      var c = s.querySelector(".jui-stepper__circle");
      if (!c) return;
      if (done) c.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 5 5L20 7"/></svg>';
      else c.textContent = String(idx + 1);
    }
    function setCurrent(i) {
      steps.forEach(function (s, idx) {
        s.classList.remove("is-done", "is-current", "is-upcoming");
        s.removeAttribute("aria-current");
        if (idx < i) { s.classList.add("is-done"); circle(s, true, idx); }
        else if (idx === i) { s.classList.add("is-current"); s.setAttribute("aria-current", "step"); circle(s, false, idx); }
        else { s.classList.add("is-upcoming"); circle(s, false, idx); }
      });
      host.dispatchEvent(new CustomEvent("jui:stepper", { bubbles: true, detail: { current: i, total: steps.length } }));
    }
    var initial = 0;
    steps.forEach(function (s, i) { if (s.classList.contains("is-current")) initial = i; });
    setCurrent(initial);
    host.addEventListener("click", function (e) {
      var cur = 0;
      steps.forEach(function (s, i) { if (s.classList.contains("is-current")) cur = i; });
      if (e.target.closest("[data-stepper-next]")) setCurrent(Math.min(cur + 1, steps.length - 1));
      else if (e.target.closest("[data-stepper-back]")) setCurrent(Math.max(cur - 1, 0));
    });
    host.__juiStepperSet = setCurrent;
  }

  /* ==========================================================================
      PROGRESS RING  ·  .jui-progress-ring  (data-value | --value)
      ========================================================================== */
  function initProgressRing(ring) {
    if (ring.__juiRing) return;
    ring.__juiRing = true;
    var fill = ring.querySelector(".jui-progress-ring__fill");
    var label = ring.querySelector(".jui-progress-ring__label");
    if (!fill) return;
    var r = parseFloat(fill.getAttribute("r")) || 28;
    var circ = 2 * Math.PI * r;
    fill.style.strokeDasharray = circ;
    function set(v) {
      v = Math.max(0, Math.min(100, v || 0));
      fill.style.strokeDashoffset = circ * (1 - v / 100);
      if (label) label.textContent = Math.round(v) + "%";
    }
    var v = parseFloat(ring.getAttribute("data-value"));
    if (isNaN(v)) v = parseFloat(getComputedStyle(ring).getPropertyValue("--value")) || 0;
    set(v);
    ring.__juiRingSet = set;
  }

  /* ==========================================================================
      COMMAND PALETTE  ·  data-jui="command-palette"  /  Jui.commandPalette()
      ========================================================================== */
  function cpEscape(s) {
    return String(s).replace(/[&<>"]/g, function (c) {
      return c === "&" ? "&amp;" : c === "<" ? "&lt;" : c === ">" ? "&gt;" : "&quot;";
    });
  }
  function cpFuzzy(q, text) {
    q = q.toLowerCase(); text = text.toLowerCase();
    if (text.indexOf(q) !== -1) {
      var at = text.indexOf(q), idx = []; for (var k = 0; k < q.length; k++) idx.push(at + k);
      return { score: 2 + (q.length / text.length) - at * 0.01, indices: idx };
    }
    var ci = 0, indices = [];
    for (var i = 0; i < text.length && ci < q.length; i++) { if (text[i] === q[ci]) { indices.push(i); ci++; } }
    if (ci === q.length) return { score: 1 - indices[indices.length - 1] * 0.001, indices: indices };
    return null;
  }
  function cpHighlight(text, res) {
    if (!res || !res.indices || !res.indices.length) return cpEscape(text);
    var set = {}; res.indices.forEach(function (n) { set[n] = true; });
    var out = "";
    for (var i = 0; i < text.length; i++) out += set[i] ? '<mark class="jui-cmdk__mark">' + cpEscape(text[i]) + '</mark>' : cpEscape(text[i]);
    return out;
  }
  function buildPaletteIndex() {
    var items = [];
    var links = document.querySelectorAll("#docsNav .docs-nav__link");
    Array.prototype.forEach.call(links, function (l) {
      var id = (l.getAttribute("href") || "").replace("#", "");
      if (!id) return;
      var group = "Components";
      var grp = l.closest(".docs-nav__group");
      if (grp) { var t = grp.querySelector(".docs-nav__title"); if (t) group = t.textContent.trim(); }
      items.push({ id: id, label: l.textContent.trim(), group: group, href: l.getAttribute("href") });
    });
    return items;
  }
  function commandPalette(opts) {
    opts = opts || {};
    var items = opts.items && opts.items.length ? opts.items : buildPaletteIndex();
    var recentKey = opts.recentKey || "jui-cmdk-recent";
    var overlay = document.createElement("div");
    overlay.className = "jui-modal jui-cmdk";
    overlay.setAttribute("role", "dialog");
    overlay.setAttribute("aria-modal", "true");
    overlay.setAttribute("aria-label", "Command palette");
    overlay.setAttribute("aria-hidden", "true");
    overlay.innerHTML =
      '<div class="jui-modal__panel">' +
        '<div class="jui-cmdk__form">' +
          '<span class="jui-cmdk__icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/></svg></span>' +
          '<input class="jui-cmdk__input" type="text" role="combobox" aria-expanded="true" aria-autocomplete="list" aria-controls="cpListbox" aria-activedescendant="" placeholder="Search components…" autocomplete="off" spellcheck="false" />' +
          '<span class="jui-cmdk__hint">Esc</span>' +
        '</div>' +
        '<ul class="jui-cmdk__list" id="cpListbox" role="listbox"></ul>' +
        '<div class="jui-cmdk__empty" hidden>No matching components</div>' +
      '</div>';
    document.body.appendChild(overlay);
    var input = overlay.querySelector(".jui-cmdk__input");
    var list = overlay.querySelector(".jui-cmdk__list");
    var emptyEl = overlay.querySelector(".jui-cmdk__empty");
    var selected = 0, matches = [];
    function recents() { try { return JSON.parse(localStorage.getItem(recentKey)) || []; } catch (e) { return []; } }
    function addRecent(id) { var r = recents().filter(function (x) { return x !== id; }); r.unshift(id); r = r.slice(0, 3); try { localStorage.setItem(recentKey, JSON.stringify(r)); } catch (e) {} }
    function optionNode(it, res) {
      var b = document.createElement("button");
      b.type = "button"; b.className = "jui-cmdk__option"; b.setAttribute("role", "option");
      b.id = "cp-opt-" + (++uid);
      b.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M13 5l7 7-7 7"/></svg>' +
        '<span class="jui-cmdk__option-text">' + (res ? cpHighlight(it.label, res) : cpEscape(it.label)) + '</span>' +
        '<span class="jui-cmdk__meta">' + cpEscape(it.group) + '</span>';
      b.addEventListener("click", function () { choose(it); });
      return b;
    }
    function groupLabel(txt) { var li = document.createElement("li"); li.className = "jui-cmdk__group-label"; li.textContent = txt; return li; }
    function render(q) {
      q = (q || "").trim();
      list.innerHTML = ""; matches = [];
      var append = function (it, res) { list.appendChild(optionNode(it, res)); matches.push(it); };
      if (!q) {
        var rec = recents();
        var recItems = rec.map(function (id) { for (var i = 0; i < items.length; i++) if (items[i].id === id) return items[i]; return null; }).filter(Boolean);
        if (recItems.length) { list.appendChild(groupLabel("Recent")); recItems.forEach(function (it) { append(it, null); }); }
        var groups = {};
        items.forEach(function (it) { (groups[it.group] = groups[it.group] || []).push(it); });
        Object.keys(groups).forEach(function (g) { list.appendChild(groupLabel(g)); groups[g].forEach(function (it) { append(it, null); }); });
      } else {
        var scored = [];
        items.forEach(function (it) { var res = cpFuzzy(q, it.label); if (res) scored.push({ it: it, res: res }); });
        scored.sort(function (a, b) { return b.res.score - a.res.score; });
        var groups2 = {};
        scored.forEach(function (s) { (groups2[s.it.group] = groups2[s.it.group] || []).push(s); });
        Object.keys(groups2).forEach(function (g) { list.appendChild(groupLabel(g)); groups2[g].forEach(function (s) { append(s.it, s.res); }); });
        if (!scored.length) { emptyEl.hidden = false; list.hidden = true; selected = 0; return; }
      }
      emptyEl.hidden = true; list.hidden = false;
      selected = 0; setActive(0);
    }
    function setActive(i) {
      var opts = list.querySelectorAll(".jui-cmdk__option");
      opts.forEach(function (o, idx) { o.classList.toggle("is-active", idx === i); });
      selected = i;
      if (opts[i]) { input.setAttribute("aria-activedescendant", opts[i].id); opts[i].scrollIntoView({ block: "nearest" }); }
      else input.removeAttribute("aria-activedescendant");
    }
    function choose(it) {
      addRecent(it.id);
      closeOverlay(overlay);
      var target = document.getElementById(it.id);
      if (target) {
        target.scrollIntoView({ behavior: "smooth", block: "start" });
        var h = target.querySelector("h2, h1, h3");
        if (h) { h.setAttribute("tabindex", "-1"); try { h.focus({ preventScroll: true }); } catch (e) {} }
      }
      if (opts.onSelect) opts.onSelect(it);
    }
    overlay.addEventListener("keydown", function (e) {
      var opts = list.querySelectorAll(".jui-cmdk__option");
      if (e.key === "ArrowDown") { e.preventDefault(); if (opts.length) setActive((selected + 1) % opts.length); }
      else if (e.key === "ArrowUp") { e.preventDefault(); if (opts.length) setActive((selected - 1 + opts.length) % opts.length); }
      else if (e.key === "Enter") { e.preventDefault(); if (matches[selected]) choose(matches[selected]); }
    });
    input.addEventListener("input", function () { render(input.value); });
    function open() {
      openOverlay(overlay);
      requestAnimationFrame(function () { requestAnimationFrame(function () { input.value = ""; render(""); try { input.focus(); } catch (e) {} }); });
    }
    return { overlay: overlay, open: open };
  }
  function initCommandPaletteTrigger(btn) {
    if (btn.__juiCp) return;
    btn.__juiCp = true;
    var palette;
    btn.addEventListener("click", function () { if (!palette) palette = commandPalette({ trigger: btn }); palette.open(); });
  }
  // Cmd/Ctrl+K opens the palette (bound once).
  document.addEventListener("keydown", function (e) {
    if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
      var btn = document.querySelector('[data-jui="command-palette"]');
      if (btn) { e.preventDefault(); btn.click(); }
    }
  });

  /* --------------------------- init -------------------------------- */
  function init(root) {
    var r = root || document;
    (r.querySelectorAll('[data-jui="autogrow"]') || []).forEach(autogrow);
    (r.querySelectorAll('[data-jui="loading-toggle"]') || []).forEach(bindLoadingToggle);
    (r.querySelectorAll(".jui-avatar[data-name]") || []).forEach(initAvatar);
    (r.querySelectorAll(".jui-avatar-group") || []).forEach(initAvatarGroup);
    (r.querySelectorAll('[data-jui="select"]') || []).forEach(initSelect);
    (r.querySelectorAll('[data-jui="checkbox-group"]') || []).forEach(initCheckboxGroup);
    (r.querySelectorAll(".jui-switch") || []).forEach(initSwitch);
    (r.querySelectorAll('[data-jui="slider"]') || []).forEach(initSlider);
    (r.querySelectorAll('[data-jui="dropdown"]') || []).forEach(initDropdown);
    (r.querySelectorAll('[data-jui="tooltip"],[data-jui-tooltip]') || []).forEach(initTooltip);
    (r.querySelectorAll('[data-jui="popover"]') || []).forEach(initPopover);
    (r.querySelectorAll('[data-jui="tabs"]') || []).forEach(initTabs);
    (r.querySelectorAll('[data-jui="accordion"]') || []).forEach(initAccordion);
    (r.querySelectorAll('[data-jui="table-sort"]') || []).forEach(initTableSort);
    (r.querySelectorAll('[data-jui="pagination"]') || []).forEach(initPagination);
    (r.querySelectorAll('[data-jui="stepper"]') || []).forEach(initStepper);
    (r.querySelectorAll(".jui-progress-ring") || []).forEach(initProgressRing);
    (r.querySelectorAll('[data-jui="command-palette"]') || []).forEach(initCommandPaletteTrigger);
  }

  function onReady() {
    init(document);

    // Delegated: dismiss (alerts/tags)
    document.addEventListener("click", function (e) {
      if (e.target.closest("[data-jui-dismiss]")) {
        var trigger = e.target.closest("[data-jui-dismiss]");
        e.preventDefault();
        var target = findDismissable(trigger);
        if (target) dismissTarget(target);
        return;
      }
      // Delegated: modal/drawer triggers
      var mt = e.target.closest('[data-jui="modal-trigger"],[data-jui="drawer-trigger"]');
      if (mt) {
        var sel = mt.getAttribute("data-target") || (mt.getAttribute("href") || "").replace("#", "#");
        var ov = sel ? document.querySelector(sel) : null;
        if (ov) { e.preventDefault(); openOverlay(ov, mt); }
        return;
      }
      // Delegated: overlay close buttons
      var cl = e.target.closest("[data-jui-close]");
      if (cl) {
        var o = cl.closest(".jui-modal, .jui-drawer");
        if (o) closeOverlay(o);
      }
    });
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", onReady);
  else onReady();

  /* ------------------------- public API ---------------------------- */
  var Jui = {
    version: "3.0.0",
    getTheme: getTheme,
    setTheme: setTheme,
    toggleTheme: toggleTheme,
    dismiss: dismissTarget,
    autogrow: autogrow,
    init: init,
    toast: toast,
    openOverlay: openOverlay,
    closeOverlay: closeOverlay,
    commandPalette: commandPalette,
    util: { initialsFromName: initialsFromName, hueFromName: hueFromName }
  };
  window.Jui = Jui;
})();
