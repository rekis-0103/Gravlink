(function () {
  'use strict';

  var hubPort = window.__GRAVLINK_HUB_PORT__ || '';
  var originBase = window.location.origin + '/hub';

  function rewriteUrl(input) {
    if (typeof input !== 'string') return input;

    // Rewrite hardcoded localhost/127.0.0.1 hub URLs to current browser origin + /hub
    if (hubPort) {
      var httpPrefix = 'http://127.0.0.1:' + hubPort;
      var httpsPrefix = 'https://127.0.0.1:' + hubPort;
      var lhHttp = 'http://localhost:' + hubPort;
      var lhHttps = 'https://localhost:' + hubPort;
      var wsPrefix = 'ws://127.0.0.1:' + hubPort;
      var wssPrefix = 'wss://127.0.0.1:' + hubPort;
      var wsProto = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
      var wsOriginBase = wsProto + window.location.host + '/hub';

      if (input.indexOf(httpPrefix) === 0) return originBase + input.slice(httpPrefix.length);
      if (input.indexOf(httpsPrefix) === 0) return originBase + input.slice(httpsPrefix.length);
      if (input.indexOf(lhHttp) === 0) return originBase + input.slice(lhHttp.length);
      if (input.indexOf(lhHttps) === 0) return originBase + input.slice(lhHttps.length);
      if (input.indexOf(wsPrefix) === 0) return wsOriginBase + input.slice(wsPrefix.length);
      if (input.indexOf(wssPrefix) === 0) return wsOriginBase + input.slice(wssPrefix.length);
    }

    // Rewrite root-relative paths (/exa.*, /main.js, etc.) under /hub
    if (
      input.charAt(0) === '/' &&
      input.indexOf('/hub/') !== 0 &&
      input !== '/hub' &&
      input.indexOf('/patches/') !== 0 &&
      input.indexOf('/api/') !== 0
    ) {
      return '/hub' + input;
    }

    // Rewrite ws(s)://current-host/exa.* to ws(s)://current-host/hub/exa.*
    try {
      var parsed = new URL(input, window.location.href);
      if (
        parsed.host === window.location.host &&
        parsed.pathname.charAt(0) === '/' &&
        parsed.pathname.indexOf('/hub/') !== 0 &&
        parsed.pathname.indexOf('/patches/') !== 0 &&
        parsed.pathname.indexOf('/api/') !== 0
      ) {
        parsed.pathname = '/hub' + parsed.pathname;
        return parsed.toString();
      }
    } catch (_) {
      // Leave unparseable strings untouched
    }
    return input;
  }

  // Patch window.fetch
  var origFetch = window.fetch;
  if (typeof origFetch === 'function') {
    window.fetch = function (resource, init) {
      if (typeof resource === 'string') {
        resource = rewriteUrl(resource);
      } else if (resource instanceof Request) {
        var nextUrl = rewriteUrl(resource.url);
        if (nextUrl !== resource.url) {
          resource = new Request(nextUrl, resource);
        }
      }
      init = init || {};
      var headers = new Headers(init.headers || (resource instanceof Request ? resource.headers : undefined));
      if (window.__APP_CONFIG__ && window.__APP_CONFIG__.csrfToken && !headers.has('x-codeium-csrf-token')) {
        headers.set('x-codeium-csrf-token', window.__APP_CONFIG__.csrfToken);
      }
      init.headers = headers;
      init.credentials = 'same-origin';
      return origFetch.call(this, resource, init);
    };
  }

  // Patch window.WebSocket
  var OrigWS = window.WebSocket;
  if (typeof OrigWS === 'function') {
    window.WebSocket = function (url, protocols) {
      var rewritten = rewriteUrl(String(url));
      return protocols !== undefined ? new OrigWS(rewritten, protocols) : new OrigWS(rewritten);
    };
    window.WebSocket.prototype = OrigWS.prototype;
    window.WebSocket.CONNECTING = OrigWS.CONNECTING;
    window.WebSocket.OPEN = OrigWS.OPEN;
    window.WebSocket.CLOSING = OrigWS.CLOSING;
    window.WebSocket.CLOSED = OrigWS.CLOSED;
  }

  // Minimal VS Code webview API shim if extensionView params are ever passed
  if (typeof window.acquireVsCodeApi !== 'function') {
    window.acquireVsCodeApi = function () {
      return {
        postMessage: function (msg) {
          if (window.parent && window.parent !== window) {
            window.parent.postMessage({ source: 'gravlink-hub-shim', payload: msg }, window.location.origin);
          }
        },
        getState: function () { return {}; },
        setState: function () {}
      };
    };
  }

  // On touch devices, Enter inserts a newline instead of submitting prematurely
  var isCoarse = window.matchMedia && window.matchMedia('(pointer: coarse)').matches;
  if (isCoarse) {
    document.addEventListener(
      'keydown',
      function (ev) {
        if (ev.key !== 'Enter' || ev.ctrlKey || ev.metaKey || ev.altKey || ev.shiftKey) return;
        var target = ev.target;
        if (!target) return;
        var tag = target.tagName;
        var isEditable = tag === 'TEXTAREA' || target.isContentEditable;
        if (isEditable) {
          ev.stopPropagation();
        }
      },
      true
    );
  }
})();
