// flow prelude — injected and evaluated before every workflow body.
//
// It installs: determinism guards (Date.now / Math.random / argless new Date throw,
// so a run's agent() journal stays valid on resume), a console shim routed to
// log(), and the parallel()/pipeline() orchestration helpers built on Promise.
//
// This file is embedded into the binary (see parse.go) — keep it ES5.1-safe: no
// external deps, no modules, and only features goja implements reliably.
(function () {
  'use strict';

  function notAllowed(name) {
    return function () {
      throw new Error(name + ' is not allowed inside a workflow: runs journal every agent() call for resume, so non-determinism would corrupt the cache. Pass timestamps via args, and vary prompts/labels by index for variety.');
    };
  }

  if (typeof Date !== 'undefined') {
    Date.now = notAllowed('Date.now');
    // Guard argless `new Date()` (non-deterministic) while allowing new Date(ms)
    // and new Date(string). Uses Reflect.construct to preserve Date semantics.
    if (typeof Reflect !== 'undefined' && Reflect.construct) {
      var OrigDate = Date;
      function GuardedDate() {
        if (arguments.length === 0) {
          throw new Error('new Date() with no arguments is not allowed inside a workflow (non-deterministic). Pass an explicit timestamp via args.');
        }
        return Reflect.construct(OrigDate, arguments, this && this.constructor === GuardedDate ? GuardedDate : OrigDate);
      }
      GuardedDate.prototype = OrigDate.prototype;
      GuardedDate.parse = OrigDate.parse;
      GuardedDate.UTC = OrigDate.UTC;
      GuardedDate.now = notAllowed('Date.now');
      globalThis.Date = GuardedDate;
    }
  }
  if (typeof Math !== 'undefined') {
    Math.random = notAllowed('Math.random');
  }

  // console → log() so scripts that console.log() surface in the run's progress
  // instead of the process stdout.
  if (typeof log === 'function') {
    var joinArgs = function (a) { return Array.prototype.join.call(a, ' '); };
    globalThis.console = {
      log: function () { log(joinArgs(arguments)); },
      info: function () { log(joinArgs(arguments)); },
      warn: function () { log(joinArgs(arguments), 'warn'); },
      error: function () { log(joinArgs(arguments), 'error'); },
      debug: function () {},
    };
  }

  // parallel(thunks) — barrier concurrency. Runs an array of () => Promise thunks
  // together and resolves once ALL complete, results in thunk order. A thunk that
  // throws (or whose promise rejects) resolves to null rather than rejecting the
  // whole call, so callers .filter(Boolean).
  globalThis.parallel = function (thunks) {
    if (!Array.isArray(thunks)) {
      throw new Error('parallel(thunks): thunks must be an array of functions');
    }
    return Promise.all(thunks.map(function (t) {
      try {
        return Promise.resolve(t()).then(function (v) { return v; }, function () { return null; });
      } catch (e) {
        return Promise.resolve(null);
      }
    }));
  };

  // pipeline(items, ...stages) — run each item through the stages independently,
  // with NO barrier between stages (item A can be in stage 3 while item B is still
  // in stage 1). Each stage callback receives (prevResult, originalItem, index). A
  // stage that throws drops that item to null and skips its remaining stages.
  globalThis.pipeline = function (items) {
    var stages = Array.prototype.slice.call(arguments, 1);
    if (!Array.isArray(items)) {
      throw new Error('pipeline(items, ...stages): items must be an array');
    }
    return Promise.all(items.map(function (item, index) {
      var p = Promise.resolve(item);
      stages.forEach(function (stage) {
        p = p.then(function (prev) {
          if (prev === null || prev === undefined) return null;
          return stage(prev, item, index);
        }).then(function (v) { return v; }, function () { return null; });
      });
      return p;
    }));
  };
})();
