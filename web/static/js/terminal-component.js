/**
 * SharedTerminal — reusable xterm.js + WebSocket terminal component.
 *
 * Usage:
 *   const t = new SharedTerminal('host-id', {
 *     wsUrl:       () => 'ws://…',
 *     reconnect:   true,
 *     theme:       { … },
 *     fontFamily:  '"Cascadia Code", monospace',
 *     fontSize:    13,
 *     scrollback:  5000,
 *     height:      'min(36rem,70vh)',
 *     onStatus:    (text, state) => { … },
 *   });
 *   t.connect();
 *   t.disconnect();
 *   t.clear();
 */
(function () {
  'use strict';

  var instances = {};

  function SharedTerminal(hostId, opts) {
    this.hostId   = hostId;
    this.opts     = opts || {};
    this.term     = null;
    this.fit      = null;
    this.ws       = null;
    this.enc      = new TextEncoder();
    this.dec      = new TextDecoder();
    this.reconnectTimer = 0;
    this.autoReconnect = !!this.opts.reconnect;

    instances[hostId] = this;
  }

  SharedTerminal.prototype._status = function (text, state) {
    if (this.opts.onStatus) this.opts.onStatus(text, state);
  };

  SharedTerminal.prototype._defaultTheme = function () {
    return {
      background: '#0a0a0a',
      foreground: '#e2e8f0',
      cursor: '#7c3aed',
      cursorAccent: '#0a0a0a',
      selectionBackground: 'rgba(124,58,237,0.25)',
      black: '#1e293b', red: '#f87171', green: '#4ade80', yellow: '#fbbf24',
      blue: '#60a5fa', magenta: '#c084fc', cyan: '#22d3ee', white: '#e2e8f0',
      brightBlack: '#475569', brightRed: '#fca5a5', brightGreen: '#86efac',
      brightYellow: '#fde68a', brightBlue: '#93c5fd', brightMagenta: '#d8b4fe',
      brightCyan: '#67e8f9', brightWhite: '#f8fafc',
    };
  };

  SharedTerminal.prototype._ensureTerm = function () {
    if (this.term) return;
    var host = document.getElementById(this.hostId);
    if (!host) return;
    if (typeof Terminal === 'undefined') {
      this._status('xterm.js failed to load', 'error');
      return;
    }
    var o = this.opts;
    this.term = new Terminal({
      fontFamily: o.fontFamily || '"Cascadia Code", "Fira Code", "JetBrains Mono", monospace',
      fontSize:   o.fontSize || 13,
      lineHeight: o.lineHeight || 1.4,
      theme:      o.theme || this._defaultTheme(),
      cursorBlink: true,
      allowTransparency: true,
      scrollback: o.scrollback != null ? o.scrollback : 5000,
    });
    var FitCtor = null;
    if (typeof FitAddon !== 'undefined') {
      FitCtor = FitAddon.FitAddon || FitAddon.default || FitAddon;
    }
    if (typeof FitCtor === 'function') {
      this.fit = new FitCtor();
      this.term.loadAddon(this.fit);
    }
    this.term.open(host);
    if (this.fit) this.fit.fit();
    var self = this;
    this.term.onData(function (data) {
      if (self.ws && self.ws.readyState === WebSocket.OPEN) {
        self.ws.send(self.enc.encode(data));
      }
    });
  };

  SharedTerminal.prototype._sendResize = function () {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN || !this.term) return;
    try {
      this.ws.send(JSON.stringify({ op: 'resize', cols: this.term.cols, rows: this.term.rows }));
    } catch (e) {}
  };

  SharedTerminal.prototype._getWsUrl = function () {
    if (typeof this.opts.wsUrl === 'function') return this.opts.wsUrl();
    return this.opts.wsUrl || '';
  };

  SharedTerminal.prototype.connect = function () {
    var self = this;
    this.autoReconnect = !!this.opts.reconnect;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = 0;
    }
    if (this.ws) {
      this._stripHandlers(this.ws);
      try { this.ws.close(); } catch (e) {}
      this.ws = null;
    }
    this._ensureTerm();
    if (!this.term) return;

    var url = this._getWsUrl();
    if (!url) { this._status('No WebSocket URL', 'error'); return; }

    this._status(this.opts.connectingText || 'Connecting…', 'connecting');

    var socket = new WebSocket(url);
    socket.binaryType = 'arraybuffer';
    this.ws = socket;

    socket.onopen = function () {
      if (self.ws !== socket) return;
      self._status(self.opts.connectedText || 'Connected', 'connected');
      if (self.fit) self.fit.fit();
      self._sendResize();
      self.term.focus();
    };

    socket.onmessage = function (ev) {
      if (self.ws !== socket || !self.term) return;
      if (ev.data instanceof ArrayBuffer) {
        self.term.write(new Uint8Array(ev.data));
      } else {
        self.term.write(ev.data);
      }
    };

    socket.onerror = function () {
      if (self.ws !== socket) return;
      self._status(self.opts.errorText || 'Connection error', 'error');
    };

    socket.onclose = function () {
      if (self.ws !== socket) return;
      self.ws = null;
      if (!self.autoReconnect) {
        self._status(self.opts.disconnectedText || 'Disconnected', 'disconnected');
        return;
      }
      self._status(self.opts.reconnectingText || 'Reconnecting…', 'connecting');
      self.reconnectTimer = setTimeout(function () {
        if (self.autoReconnect) self.connect();
      }, self.opts.reconnectDelay || 1200);
    };
  };

  SharedTerminal.prototype.disconnect = function () {
    this.autoReconnect = false;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = 0;
    }
    if (this.ws) {
      this._stripHandlers(this.ws);
      try { this.ws.close(); } catch (e) {}
      this.ws = null;
    }
    this._status(this.opts.disconnectedText || 'Disconnected', 'disconnected');
  };

  SharedTerminal.prototype.clear = function () {
    if (this.term) this.term.clear();
  };

  SharedTerminal.prototype.destroy = function () {
    this.disconnect();
    if (this.term) { this.term.dispose(); this.term = null; }
    delete instances[this.hostId];
  };

  SharedTerminal.prototype._stripHandlers = function (sock) {
    if (!sock) return;
    sock.onopen = null; sock.onmessage = null; sock.onerror = null; sock.onclose = null;
  };

  SharedTerminal.setupResize = function (instance) {
    if (!instance) return;
    var host = document.getElementById(instance.hostId);
    if (!host) return;
    var ro = new ResizeObserver(function () {
      if (instance.term && instance.fit) {
        instance.fit.fit();
        instance._sendResize();
      }
    });
    ro.observe(host);
    window.addEventListener('resize', function () {
      if (instance.term && instance.fit) {
        instance.fit.fit();
        instance._sendResize();
      }
    });
  };

  SharedTerminal.getInstance = function (hostId) {
    return instances[hostId] || null;
  };

  window.SharedTerminal = SharedTerminal;

  var appTermIO = null;
  var appTermInstance = null;
  var appTermVisBound = false;

  function appTermScope(root) {
    return root || document.getElementById('app-tab-panel') || document;
  }

  function appTermSetStatus(text, state) {
    var statusEl = document.getElementById('term-status');
    var statusDot = document.getElementById('term-status-dot');
    if (!statusEl || !statusDot) return;
    statusEl.textContent = text || '';
    if (state === 'connected') {
      statusDot.className = 'inline-block h-2 w-2 rounded-full bg-emerald-400 shadow-[0_0_4px_1px_rgba(52,211,153,0.5)]';
    } else if (state === 'error') {
      statusDot.className = 'inline-block h-2 w-2 rounded-full bg-red-400';
    } else if (state === 'connecting') {
      statusDot.className = 'inline-block h-2 w-2 rounded-full bg-amber-400 animate-pulse';
    } else {
      statusDot.className = 'inline-block h-2 w-2 rounded-full bg-muted-foreground/40';
    }
  }

  function appTermWsURL(pick, host) {
    if (!host || !pick) return '';
    var appId = (host.getAttribute('data-app-id') || '').trim();
    var container = (pick.value || '').trim();
    if (!appId || !container) return '';
    var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    var q = 'app=' + encodeURIComponent(appId) + '&container=' + encodeURIComponent(container) + '&cols=80&rows=24';
    return proto + '//' + location.host + '/apps/' + appId + '/ws/terminal?' + q;
  }

  function appTermWaitLibs(cb, left) {
    if (typeof Terminal !== 'undefined' && typeof SharedTerminal !== 'undefined') {
      cb();
      return;
    }
    if (left <= 0) {
      appTermSetStatus('xterm.js failed to load', 'error');
      return;
    }
    setTimeout(function () { appTermWaitLibs(cb, left - 1); }, 50);
  }

  function appTermEnsure(pick, host) {
    if (appTermInstance) return appTermInstance;
    appTermInstance = new SharedTerminal('xterm-host', {
      wsUrl: function () { return appTermWsURL(pick, host); },
      reconnect: false,
      scrollback: 1000,
      fontSize: 14,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      theme: {
        background: '#0a0a0a',
        foreground: '#e2e8f0',
        cursor: '#a78bfa',
        selectionBackground: '#4c1d95',
      },
      onStatus: appTermSetStatus,
    });
    SharedTerminal.setupResize(appTermInstance);
    window.containerTerm = appTermInstance;
    return appTermInstance;
  }

  function appTermTryConnect(pick, host) {
    if (!host || !pick || !(pick.value || '').trim()) return;
    appTermWaitLibs(function () {
      requestAnimationFrame(function () {
        appTermEnsure(pick, host).connect();
      });
    }, 120);
  }

  function appTermDestroyIO() {
    if (appTermIO) {
      appTermIO.disconnect();
      appTermIO = null;
    }
  }

  function appTermDestroy() {
    appTermDestroyIO();
    if (appTermInstance) {
      appTermInstance.destroy();
      appTermInstance = null;
    }
    window.containerTerm = null;
  }

  function appTermInit(root) {
    var scope = appTermScope(root);
    var host = scope.querySelector('#xterm-host');
    if (!host) {
      appTermDestroy();
      return;
    }
    var pick = scope.querySelector('#term-pick');
    if (!pick) return;

    if (!pick.dataset.termBound) {
      pick.dataset.termBound = '1';
      pick.addEventListener('change', function () {
        if (appTermInstance) appTermInstance.connect();
        else appTermTryConnect(pick, host);
      });
    }
    if (!appTermVisBound) {
      appTermVisBound = true;
      document.addEventListener('visibilitychange', function () {
        if (!appTermInstance) return;
        var activePick = document.querySelector('#app-tab-panel #term-pick') || document.getElementById('term-pick');
        var activeHost = document.querySelector('#app-tab-panel #xterm-host') || document.getElementById('xterm-host');
        if (!activePick || !activeHost) return;
        if (document.hidden) appTermInstance.disconnect();
        else appTermTryConnect(activePick, activeHost);
      });
    }

    appTermDestroyIO();
    if (typeof IntersectionObserver !== 'undefined') {
      appTermIO = new IntersectionObserver(function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) appTermTryConnect(pick, host);
          else if (appTermInstance) appTermInstance.disconnect();
        });
      }, { threshold: 0.1 });
      appTermIO.observe(host);
    } else {
      appTermTryConnect(pick, host);
    }
  }

  window.AppContainerTerminal = {
    init: appTermInit,
    destroy: appTermDestroy,
  };
})();
