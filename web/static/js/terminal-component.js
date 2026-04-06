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
    if (typeof FitAddon !== 'undefined' && FitAddon.FitAddon) {
      this.fit = new FitAddon.FitAddon();
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
})();
