/**
 * PanelFileBrowser v2.0 — Full-featured cPanel-style file manager
 *
 * Features:
 *   - Tree + Table dual view (Grid/List toggle)
 *   - Drag & drop upload with progress bar
 *   - URL upload (fetch remote file by URL)
 *   - Copy / Move / Rename / Delete (single + bulk)
 *   - Create folder / Create file
 *   - ZIP compress + Extract
 *   - Download single file / Download as ZIP (bulk)
 *   - File preview: Monaco editor (code), image lightbox, video/audio player, PDF iframe
 *   - Copy direct URL to clipboard
 *   - File properties modal (name, path, size, modified, permissions)
 *   - Search + filter (live)
 *   - Sort by name / size / date (asc/desc)
 *   - Breadcrumb navigation
 *   - Clipboard (cut/copy → paste)
 *   - Context menu (right-click)
 *   - Keyboard shortcuts (Delete, F2 rename, Ctrl+C/X/V)
 *   - Permission / chmod editor
 *   - Mobile touch friendly
 *
 * Root element attributes:
 *   data-panel-file-browser   — mount marker
 *   data-tree-base            — API base for listing  e.g. /apps/123/files/tree
 *   data-blob-base            — API base for content  e.g. /apps/123/files/blob
 *   data-delete-url           — POST endpoint for delete
 *   data-upload-url           — POST endpoint for upload (multipart/form-data, field "file", param "path")
 *   data-save-url-base        — base for /apps/{id}/files/save
 *   data-zip-url              — POST endpoint to compress
 *   data-unzip-url            — POST endpoint to extract
 *   data-rename-url           — POST endpoint for rename
 *   data-move-url             — POST endpoint for move
 *   data-copy-url             — POST endpoint for copy
 *   data-mkdir-url            — POST endpoint for mkdir
 *   data-chmod-url            — POST endpoint for chmod
 *   data-url-upload-url       — POST endpoint for URL upload
 *   data-empty-root           — message when empty
 *   data-app-id               — app id (used for session key + API paths)
 *   data-file-preview-modal   — "1" = modal mode (workspace), "0" = sidebar mode (git)
 */
(function (global) {
  'use strict';

  /* ─── tiny helpers ───────────────────────────────────────────────── */
  function esc(s) {
    var d = document.createElement('div');
    d.textContent = s || '';
    return d.innerHTML;
  }
  function q(el, role) { return el.querySelector('[data-fb="' + role + '"]'); }
  function fmtBytes(n) {
    n = Number(n) || 0;
    if (n < 1024) return n + ' B';
    if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
    if (n < 1073741824) return (n / 1048576).toFixed(1) + ' MB';
    return (n / 1073741824).toFixed(2) + ' GB';
  }
  function fmtDate(ts) {
    if (!ts) return '--';
    var d = new Date(ts * 1000);
    return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }
  function ce(tag, cls, html) {
    var el = document.createElement(tag);
    if (cls) el.className = cls;
    if (html) el.innerHTML = html;
    return el;
  }
  function svgIcon(d, size) {
    size = size || '16';
    return '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="' + d + '"/></svg>';
  }

  /* SVG icon paths */
  var ICONS = {
    folder: 'M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z',
    file: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z M14 2v6h6',
    image: 'M21 15a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z M8.5 10a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3z M21 15l-5-5L5 21',
    video: 'M15 10l4.553-2.277A1 1 0 0 1 21 8.723v6.554a1 1 0 0 1-1.447.894L15 14v-4z M3 8a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8z',
    audio: 'M9 18V5l12-2v13 M6 15H3a3 3 0 0 0 0 6h1a2 2 0 0 0 2-2v-4z M18 13h-3a3 3 0 0 0 0 6h1a2 2 0 0 0 2-2v-4z',
    archive: 'M21 8v13H3V8 M1 3h22v5H1z M10 12h4',
    pdf: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z M14 2v6h6 M9 13h1a1 1 0 0 1 0 2H9v-2z M14 13h2 M14 17h2 M9 17v-2',
    upload: 'M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4 M17 8l-5-5-5 5 M12 3v12',
    download: 'M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4 M7 10l5 5 5-5 M12 15V3',
    trash: 'M3 6h18 M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2',
    rename: 'M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7 M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z',
    copy: 'M8 17.929H6c-1.105 0-2-.912-2-2.036V5.036C4 3.91 4.895 3 6 3h8c1.105 0 2 .911 2 2.036v1.866m-6 .17h8c1.105 0 2 .91 2 2.035v10.857C20 21.09 19.105 22 18 22h-8c-1.105 0-2-.911-2-2.036V9.107c0-1.124.895-2.036 2-2.036z',
    cut: 'M6 9a3 3 0 1 0 0-6 3 3 0 0 0 0 6z M6 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6z M20 4L8.12 15.88 M14.47 14.48L20 20 M8.12 8.12L12 12',
    paste: 'M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2 M15 2H9a1 1 0 0 0-1 1v2a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1V3a1 1 0 0 0-1-1z',
    link: 'M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71 M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71',
    info: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z',
    zip: 'M21 8v13H3V8 M1 3h22v5H1z M10 12h4 M12 12v4',
    chmod: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z',
    search: 'M21 21l-4.35-4.35 M17 11A6 6 0 1 1 5 11a6 6 0 0 1 12 0z',
    home: 'M3 9.5L12 3l9 6.5V20a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z M9 21V12h6v9',
    grid: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
    list: 'M8 6h13 M8 12h13 M8 18h13 M3 6h.01 M3 12h.01 M3 18h.01',
    close: 'M18 6L6 18 M6 6l12 12',
    chevronR: 'M9 18l6-6-6-6',
    chevronD: 'M6 9l6 6 6-6',
    plus: 'M12 5v14 M5 12h14',
    check: 'M20 6L9 17l-5-5',
    urlUp: 'M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71 M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71',
    move: 'M5 9V5h4 M19 15v4h-4 M5 5l14 14',
    refresh: 'M23 4v6h-6 M1 20v-6h6 M3.51 9a9 9 0 0 1 14.85-3.36L23 10 M1 14l4.64 4.36A9 9 0 0 0 20.49 15',
  };

  function icon(name, size) {
    return svgIcon(ICONS[name] || ICONS.file, size || '14');
  }

  /* ─── file type detection ─────────────────────────────────────────── */
  function fileType(name) {
    var ext = (name.split('.').pop() || '').toLowerCase();
    if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'ico', 'avif'].includes(ext)) return 'image';
    if (['mp4', 'webm', 'mkv', 'avi', 'mov', 'flv', 'm4v'].includes(ext)) return 'video';
    if (['mp3', 'ogg', 'wav', 'flac', 'aac', 'm4a'].includes(ext)) return 'audio';
    if (['zip', 'tar', 'gz', 'bz2', '7z', 'rar', 'tgz', 'xz'].includes(ext)) return 'archive';
    if (ext === 'pdf') return 'pdf';
    return 'code';
  }

  function fileIconHtml(name, isDir) {
    if (isDir) return '<span style="color:#f59e0b">' + icon('folder', '18') + '</span>';
    if (global.MaterialFileIcons && typeof global.MaterialFileIcons.getIcon === 'function') {
      var mIcon = global.MaterialFileIcons.getIcon(name);
      if (mIcon && mIcon.svg) {
        return '<span style="display:inline-block; width:20px; height:20px;">' + mIcon.svg + '</span>';
      }
    }
    var t = fileType(name);
    var colors = { image: '#a78bfa', video: '#34d399', audio: '#fb923c', archive: '#60a5fa', pdf: '#f87171', code: '#94a3b8' };
    return '<span style="color:' + (colors[t] || '#94a3b8') + '">' + icon(t === 'code' ? 'file' : t, '18') + '</span>';
  }

  /* ─── Monaco helpers (shared across instances) ────────────────────── */
  var monacoReady = false;
  var monacoQueue = [];
  var monacoBase = 'https://cdn.jsdelivr.net/npm/monaco-editor@0.45.0/min/vs';
  var monacoBaseFallback = 'https://unpkg.com/monaco-editor@0.45.0/min/vs';
  function loadMonaco(cb) {
    if (global.monaco && global.monaco.editor) { 
      if (cb) cb(); 
      return; 
    }
    
    if (!global._fbMonacoQueue) {
      global._fbMonacoQueue = [];
    }
    if (cb) {
      global._fbMonacoQueue.push(cb);
    }

    if (document.querySelector('script[data-monaco-loader]')) {
      return;
    }

    function tryLoad(base, fallback) {
      global.MonacoEnvironment = {
        getWorkerUrl: function (workerId, label) {
          var workerUrl = base + '/base/worker/workerMain.js';
          var baseUrl = base.substring(0, base.lastIndexOf('/vs'));
          var code = [
            "self.MonacoEnvironment = {",
            "  baseUrl: '" + baseUrl + "/'",
            "};",
            "importScripts('" + workerUrl + "');"
          ].join('\n');
          try {
            return URL.createObjectURL(new Blob([code], { type: 'application/javascript' }));
          } catch (e) {
            return 'data:text/javascript;charset=utf-8,' + encodeURIComponent(code);
          }
        }
      };

      var loaderScript = document.createElement('script');
      loaderScript.setAttribute('data-monaco-loader', 'true');
      loaderScript.src = base + '/loader.js';
      loaderScript.onload = function () {
        require.config({ paths: { vs: base } });
        require(['vs/editor/editor.main'], function () {
          try {
            global.monaco.editor.defineTheme('fb-dark', {
              base: 'vs-dark', inherit: true, rules: [],
              colors: { 'editor.background': '#050810', 'editorGutter.background': '#050810', 'minimap.background': '#050810' }
            });
          } catch(e) {}
          
          var q = global._fbMonacoQueue || [];
          global._fbMonacoQueue = [];
          q.forEach(function (fn) { fn(); });
        });
      };
      loaderScript.onerror = function () {
        document.head.removeChild(loaderScript);
        if (fallback) {
          tryLoad(fallback, null);
        } else {
          var q = global._fbMonacoQueue || [];
          global._fbMonacoQueue = [];
          q.forEach(function (fn) { fn(); });
        }
      };
      document.head.appendChild(loaderScript);
    }

    tryLoad(monacoBase, monacoBaseFallback);
  }

  // Preload Monaco Editor in the background when the browser is idle
  if (typeof window.requestIdleCallback === 'function') {
    window.requestIdleCallback(function() {
      setTimeout(function() { loadMonaco(); }, 1000);
    }, { timeout: 10000 });
  } else {
    setTimeout(function() { loadMonaco(); }, 3000);
  }

  function monacoLang(path) {
    var ext = (path.split('.').pop() || '').toLowerCase();
    var base = path.split('/').pop().toLowerCase();
    if (base === 'dockerfile') return 'dockerfile';
    var map = {
      js: 'javascript', jsx: 'javascript', mjs: 'javascript', ts: 'typescript', tsx: 'typescript',
      json: 'json', yaml: 'yaml', yml: 'yaml', md: 'markdown', mdx: 'markdown',
      html: 'html', htm: 'html', css: 'css', scss: 'scss', less: 'less',
      xml: 'xml', svg: 'xml', sql: 'sql', sh: 'shell', bash: 'shell', zsh: 'shell',
      py: 'python', pyw: 'python', rb: 'ruby', rs: 'rust', java: 'java', kt: 'kotlin',
      go: 'go', c: 'c', h: 'c', cpp: 'cpp', cc: 'cpp', hpp: 'cpp', cs: 'csharp',
      php: 'php', vue: 'html', svelte: 'html', toml: 'ini', ini: 'ini', env: 'plaintext',
      txt: 'plaintext', log: 'plaintext', gitignore: 'plaintext', mod: 'go',
    };
    return map[ext] || 'plaintext';
  }

  /* ════════════════════════════════════════════════════════════════════
     MAIN MOUNT
  ════════════════════════════════════════════════════════════════════ */
  function mount(container) {
    if (!container || container.getAttribute('data-fb-mounted') === '1') return;
    container.setAttribute('data-fb-mounted', '1');

    /* ── config ── */
    var cfg = {
      treeBase: (container.getAttribute('data-tree-base') || '').replace(/\/$/, ''),
      blobBase: (container.getAttribute('data-blob-base') || '').replace(/\/$/, ''),
      deleteUrl: (container.getAttribute('data-delete-url') || '').trim(),
      uploadUrl: (container.getAttribute('data-upload-url') || '').trim(),
      saveBase: (container.getAttribute('data-save-url-base') || '').trim(),
      zipUrl: (container.getAttribute('data-zip-url') || '').trim(),
      unzipUrl: (container.getAttribute('data-unzip-url') || '').trim(),
      renameUrl: (container.getAttribute('data-rename-url') || '').trim(),
      moveUrl: (container.getAttribute('data-move-url') || '').trim(),
      copyUrl: (container.getAttribute('data-copy-url') || '').trim(),
      mkdirUrl: (container.getAttribute('data-mkdir-url') || '').trim(),
      chmodUrl: (container.getAttribute('data-chmod-url') || '').trim(),
      urlUpUrl: (container.getAttribute('data-url-upload-url') || '').trim(),
      emptyMsg: container.getAttribute('data-empty-root') || 'This folder is empty.',
      appId: (container.getAttribute('data-app-id') || '').trim(),
      modalMode: container.getAttribute('data-file-preview-modal') === '1',
      readOnly: container.getAttribute('data-read-only') === '1',
    };

    /* ── state ── */
    var state = {
      currentPath: sessionStorage.getItem('fb2_path_' + cfg.appId) || '',
      viewMode: localStorage.getItem('fb2_view_' + cfg.appId) || 'list',  // 'list' | 'grid'
      sortKey: 'name',    // 'name' | 'size' | 'date'
      sortAsc: true,
      searchQuery: '',
      entries: [],
      selectedPaths: new Set(),
      clipboard: null,      // { mode: 'copy'|'cut', paths: [] }
      bulkActive: false,
    };

    /* ── DOM refs ── */
    var treeRoot = q(container, 'tree');
    var breadcrumbs = container.querySelector('#fb-breadcrumbs');
    var searchInput = container.querySelector('#fb-search');
    var flashEl = q(container, 'flash');
    var loadingEl = q(container, 'loading');
    var sortBtns = container.querySelectorAll('[data-fb-sort]');

    if (!treeRoot || !cfg.appId) return;

    /* ── context menu (shared singleton) ── */
    var ctxMenu = null;
    function removeCtxMenu() { if (ctxMenu) { ctxMenu.remove(); ctxMenu = null; } }
    document.addEventListener('click', removeCtxMenu);
    document.addEventListener('keydown', function (e) { if (e.key === 'Escape') removeCtxMenu(); });

    /* ════════════════════════════════
       UTILITY: flash, loading
    ════════════════════════════════ */
    function flash(msg, isErr) {
      if (!msg) return;
      var toaster = document.getElementById('fb-toaster');
      if (!toaster) {
        toaster = ce('div', 'fixed bottom-6 right-6 z-[9999] flex flex-col gap-3 pointer-events-none');
        toaster.id = 'fb-toaster';
        document.body.appendChild(toaster);
      }
      var toast = ce('div', 'transform transition-all duration-300 translate-y-4 opacity-0 flex items-center gap-3 rounded-xl border px-4 py-3 shadow-2xl backdrop-blur-md min-w-[250px] pointer-events-auto ' +
        (isErr ? 'border-rose-500/40 bg-[#1e0f13]/90 text-rose-200'
          : 'border-emerald-500/30 bg-[#0c1a14]/90 text-emerald-200')
      );
      toast.innerHTML = (isErr ? icon('close', '18') : icon('check', '18')) + '<span class="text-sm font-medium">' + esc(msg) + '</span>';
      toaster.appendChild(toast);

      requestAnimationFrame(function () {
        toast.classList.remove('translate-y-4', 'opacity-0');
      });
      setTimeout(function () {
        toast.classList.add('opacity-0', 'scale-95');
        setTimeout(function () { toast.remove(); }, 300);
      }, 4000);
    }
    function setLoading(on) {
      if (!loadingEl) return;
      loadingEl.classList.toggle('hidden', !on);
      loadingEl.classList.toggle('flex', !!on);
    }
    function apiPost(url, body, isJson) {
      var opts = { method: 'POST' };
      if (isJson) {
        opts.headers = { 'Content-Type': 'application/json' };
        opts.body = JSON.stringify(body);
      } else {
        var fd = new URLSearchParams();
        Object.keys(body).forEach(function (k) {
          if (Array.isArray(body[k])) body[k].forEach(function (v) { fd.append(k, v); });
          else fd.append(k, body[k]);
        });
        opts.body = fd;
      }
      return fetch(url + (url.includes('?') ? '&' : '?') + 'format=json', opts)
        .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); });
    }

    /* ════════════════════════════════
       BREADCRUMBS
    ════════════════════════════════ */
    function renderBreadcrumbs() {
      if (!breadcrumbs) return;
      breadcrumbs.innerHTML = '';
      var home = ce('span', 'cursor-pointer hover:text-primary transition-colors flex items-center gap-1 text-muted-foreground');
      home.innerHTML = icon('home', '13') + ' Root';
      home.onclick = function () { navigate(''); };
      breadcrumbs.appendChild(home);

      if (state.currentPath) {
        var parts = state.currentPath.split('/').filter(Boolean);
        var acc = '';
        parts.forEach(function (p, i) {
          var sep = ce('span', 'text-muted-foreground/40 mx-0.5', '/');
          breadcrumbs.appendChild(sep);
          acc += (acc ? '/' : '') + p;
          var seg = ce('span', i === parts.length - 1
            ? 'text-foreground font-semibold'
            : 'cursor-pointer hover:text-primary transition-colors text-muted-foreground');
          seg.textContent = p;
          if (i < parts.length - 1) {
            (function (path) { seg.onclick = function () { navigate(path); }; })(acc);
          }
          breadcrumbs.appendChild(seg);
        });
      }
    }

    function navigate(path) {
      state.currentPath = path;
      sessionStorage.setItem('fb2_path_' + cfg.appId, path);
      state.selectedPaths.clear();
      loadRoot();
    }

    /* ════════════════════════════════
       SORT + FILTER
    ════════════════════════════════ */
    function sortAndFilter(entries) {
      var list = entries.filter(function (e) {
        if (!state.searchQuery) return true;
        return e.name.toLowerCase().includes(state.searchQuery);
      });
      list.sort(function (a, b) {
        if (!!a.is_dir !== !!b.is_dir) return a.is_dir ? -1 : 1;
        var av, bv;
        if (state.sortKey === 'size') { av = a.size || 0; bv = b.size || 0; }
        else if (state.sortKey === 'date') { av = a.mod_ts || 0; bv = b.mod_ts || 0; }
        else { av = a.name.toLowerCase(); bv = b.name.toLowerCase(); }
        if (av < bv) return state.sortAsc ? -1 : 1;
        if (av > bv) return state.sortAsc ? 1 : -1;
        return 0;
      });
      return list;
    }

    /* ════════════════════════════════
       RENDER TABLE (list view)
    ════════════════════════════════ */
    function renderList(entries) {
      treeRoot.innerHTML = '';

      /* parent row (..) */
      if (state.currentPath) {
        var parentPath = state.currentPath.split('/').slice(0, -1).join('/');
        var ptr = ce('tr', 'group hover:bg-muted/15 transition-colors cursor-pointer select-none');
        ptr.innerHTML =
          '<td class="py-2 px-3 w-8"><input type="checkbox" class="opacity-0 pointer-events-none h-3 w-3"></td>' +
          '<td class="py-2 px-2" colspan="3"><div class="flex items-center gap-2 text-muted-foreground/70 hover:text-primary">' +
          '<span class="flex h-7 w-7 items-center justify-center rounded-md bg-muted/20">' + icon('chevronD', '13') + '</span>' +
          '<span class="font-semibold text-sm">..</span></div></td>' +
          '<td class="py-2 px-3 text-right w-28 text-muted-foreground/40 text-xs">--</td>' +
          '<td class="py-2 px-3 w-36"></td>';
        ptr.onclick = function () { navigate(parentPath); };
        treeRoot.appendChild(ptr);
      }

      if (!entries.length && !state.currentPath) {
        var emp = ce('tr'); var etd = ce('td', 'py-10 text-center text-xs text-muted-foreground/60', esc(cfg.emptyMsg));
        etd.colSpan = 6; emp.appendChild(etd); treeRoot.appendChild(emp); return;
      }

      sortAndFilter(entries).forEach(function (e) {
        var tr = buildRow(e);
        treeRoot.appendChild(tr);
      });
    }

    /* ════════════════════════════════
       RENDER GRID (card view)
    ════════════════════════════════ */
    function renderGrid(entries) {
      treeRoot.innerHTML = '';
      var wrap = ce('div', 'grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3 p-3');

      if (state.currentPath) {
        var parentPath = state.currentPath.split('/').slice(0, -1).join('/');
        var card = ce('div', 'group flex flex-col items-center gap-1.5 rounded-xl border border-border/30 bg-muted/10 p-3 cursor-pointer hover:bg-muted/20 hover:border-primary/30 transition-all select-none');
        card.innerHTML =
          '<div class="flex h-10 w-10 items-center justify-center rounded-lg bg-muted/20">' + icon('chevronD', '22') + '</div>' +
          '<span class="text-xs text-muted-foreground font-medium">..</span>';
        card.onclick = function () { navigate(parentPath); };
        wrap.appendChild(card);
      }

      sortAndFilter(entries).forEach(function (e) {
        var card = ce('div', 'group relative flex flex-col items-center gap-1.5 rounded-xl border border-border/30 bg-muted/10 p-3 cursor-pointer hover:bg-muted/20 hover:border-primary/30 transition-all select-none');
        card.setAttribute('data-path', e.rel_path);

        /* checkbox overlay */
        var cbWrap = ce('div', 'absolute top-1.5 left-1.5');
        var cb = ce('input'); cb.type = 'checkbox'; cb.className = 'h-3.5 w-3.5 rounded border-border/50';
        cb.checked = state.selectedPaths.has(e.rel_path);
        cb.addEventListener('change', function (ev) { ev.stopPropagation(); toggleSelect(e.rel_path, cb.checked); });
        cbWrap.appendChild(cb); card.appendChild(cbWrap);

        /* icon */
        var iconWrap = ce('div', 'flex h-10 w-10 items-center justify-center rounded-lg bg-muted/20 text-2xl');
        iconWrap.innerHTML = fileIconHtml(e.name, e.is_dir);
        card.appendChild(iconWrap);

        /* name */
        var nm = ce('span', 'text-xs text-foreground text-center w-full truncate');
        nm.textContent = e.name; card.appendChild(nm);

        /* meta */
        var mt = ce('span', 'text-[10px] text-muted-foreground/60');
        mt.textContent = e.is_dir ? 'Folder' : fmtBytes(e.size); card.appendChild(mt);

        card.addEventListener('click', function () {
          if (e.is_dir) { navigate(e.rel_path); return; }
          openPreview(e.rel_path, e.size, e.name);
        });
        card.addEventListener('contextmenu', function (ev) { ev.preventDefault(); showCtxMenu(ev, e); });

        if (state.selectedPaths.has(e.rel_path)) card.classList.add('ring-2', 'ring-primary/40');
        wrap.appendChild(card);
      });

      /* grid view uses a single outer wrapper cell */
      treeRoot.innerHTML = '';
      var tr = ce('tr'); var td = ce('td'); td.colSpan = 6; td.appendChild(wrap);
      tr.appendChild(td); treeRoot.appendChild(tr);
    }

    /* ════════════════════════════════
       BUILD TABLE ROW
    ════════════════════════════════ */
    function buildRow(e) {
      var tr = ce('tr', 'group hover:bg-muted/15 transition-colors');
      tr.setAttribute('data-path', e.rel_path);
      if (state.selectedPaths.has(e.rel_path)) tr.classList.add('bg-primary/8');

      /* checkbox */
      var cbTd = ce('td', 'py-2 px-3 w-8');
      var cb = ce('input'); cb.type = 'checkbox'; cb.className = 'h-3 w-3 rounded border-border/50';
      cb.checked = state.selectedPaths.has(e.rel_path);
      cb.addEventListener('change', function (ev) { ev.stopPropagation(); toggleSelect(e.rel_path, cb.checked); });
      cbTd.appendChild(cb); tr.appendChild(cbTd);

      /* name */
      var nameTd = ce('td', 'py-2 px-2 min-w-0');
      var nameDiv = ce('div', 'flex items-center gap-2 cursor-pointer select-none min-w-0');
      var iconEl = ce('span', 'shrink-0 flex h-7 w-7 items-center justify-center rounded-md bg-muted/15 ring-1 ring-border/20');
      iconEl.innerHTML = fileIconHtml(e.name, e.is_dir);
      var nameSpan = ce('span', 'truncate text-foreground hover:text-primary transition-colors text-sm');
      nameSpan.textContent = e.name;
      nameDiv.appendChild(iconEl); nameDiv.appendChild(nameSpan); nameTd.appendChild(nameDiv);
      tr.appendChild(nameTd);

      /* size */
      var sizeTd = ce('td', 'py-2 px-3 text-right text-xs text-muted-foreground/70 w-24');
      sizeTd.textContent = e.is_dir ? '--' : fmtBytes(e.size); tr.appendChild(sizeTd);

      /* modified */
      var modTd = ce('td', 'py-2 px-3 text-right text-xs text-muted-foreground/50 w-40 hidden sm:table-cell');
      modTd.textContent = fmtDate(e.mod_ts); tr.appendChild(modTd);

      /* perms */
      var permTd = ce('td', 'py-2 px-3 text-right text-xs font-mono text-muted-foreground/40 w-20 hidden md:table-cell');
      permTd.textContent = e.perms || '--'; tr.appendChild(permTd);

      /* actions */
      var actTd = ce('td', 'py-2 px-3 text-right w-40');
      actTd.innerHTML = buildActionBtns(e);
      tr.appendChild(actTd);

      /* bind action buttons */
      bindActionBtns(actTd, e);

      /* click nav/open */
      nameDiv.addEventListener('click', function () {
        if (e.is_dir) { navigate(e.rel_path); return; }
        openPreview(e.rel_path, e.size, e.name);
      });

      /* right click */
      tr.addEventListener('contextmenu', function (ev) { ev.preventDefault(); showCtxMenu(ev, e); });

      /* keyboard (row needs tabindex) */
      tr.setAttribute('tabindex', '0');
      tr.addEventListener('keydown', function (ev) {
        if (ev.key === 'Delete') { ev.preventDefault(); doDelete([e.rel_path]); }
        if (ev.key === 'F2') { ev.preventDefault(); doRename(e); }
        if ((ev.ctrlKey || ev.metaKey) && ev.key === 'c') { setClipboard('copy', [e.rel_path]); }
        if ((ev.ctrlKey || ev.metaKey) && ev.key === 'x') { setClipboard('cut', [e.rel_path]); }
      });

      return tr;
    }

    function buildActionBtns(e) {
      return '<div class="flex justify-end items-center opacity-60 hover:opacity-100 focus-within:opacity-100 transition-opacity">' +
        '<button data-act="menu" class="p-1.5 rounded-md hover:bg-muted/40 text-muted-foreground hover:text-foreground transition-colors" title="Actions">' +
        '<svg class="h-4 w-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M12 6.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 12.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 18.75a.75.75 0 110-1.5.75.75 0 010 1.5z" /></svg>' +
        '</button></div>';
    }

    function bindActionBtns(td, e) {
      td.querySelector('[data-act="menu"]').onclick = function (ev) {
        ev.stopPropagation();
        var rect = ev.currentTarget.getBoundingClientRect();
        // Mock a mouse event to position context menu near the button
        showCtxMenu({ clientX: rect.right - 180, clientY: rect.bottom, preventDefault: function () { } }, e);
      };
    }

    /* ════════════════════════════════
       CONTEXT MENU
    ════════════════════════════════ */
    function showCtxMenu(ev, e) {
      removeCtxMenu();
      var items = [
        { label: 'Open', act: function () { e.is_dir ? navigate(e.rel_path) : openPreview(e.rel_path, e.size, e.name); } },
        { sep: true },
        ...(cfg.readOnly ? [] : [
          { label: 'Copy', act: function () { setClipboard('copy', [e.rel_path]); } },
          { label: 'Cut', act: function () { setClipboard('cut', [e.rel_path]); } },
          ...(state.clipboard ? [{ label: 'Paste here', act: function () { doPaste(state.currentPath); } }] : []),
          { sep: true },
          { label: 'Rename', act: function () { doRename(e); } },
          { label: 'Delete', act: function () { doDelete([e.rel_path]); } },
          { sep: true },
        ]),
        ...(!e.is_dir ? [
          { label: 'Download', act: function () { dlFile(e.rel_path, e.name); } },
          (fileType(e.name) === 'archive' && cfg.unzipUrl && !cfg.readOnly
            ? { label: 'Extract', act: function () { doUnzip(e.rel_path); } }
            : { label: 'Compress', act: function () { doZip([e.rel_path]); } }),
        ] : [
          { label: 'Compress folder', act: function () { doZip([e.rel_path]); } },
        ]),
        { label: 'Copy URL', act: function () { copyToClipboard('/apps/' + cfg.appId + '/file?path=' + encodeURIComponent(e.rel_path)); } },
        { label: 'Properties', act: function () { openProps(e); } },
        ...(cfg.chmodUrl && !cfg.readOnly ? [{ label: 'Permissions', act: function () { openChmod(e); } }] : []),
      ];

      ctxMenu = ce('div',
        'fixed z-[9999] min-w-[144px] rounded-xl border border-border/40 bg-[#0d121c]/95 backdrop-blur-md shadow-2xl py-1.5 text-[11px] font-medium text-foreground overflow-hidden');
      ctxMenu.style.left = Math.min(ev.clientX, innerWidth - 160) + 'px';
      ctxMenu.style.top = Math.min(ev.clientY, innerHeight - 300) + 'px';

      items.forEach(function (it) {
        if (it.sep) { ctxMenu.appendChild(ce('div', 'my-1 border-t border-border/30')); return; }
        var row = ce('button', 'w-full text-left px-3 py-1.5 hover:bg-primary/20 transition-colors text-foreground/85 hover:text-primary');
        row.textContent = it.label;
        row.addEventListener('click', function (ev) { ev.stopPropagation(); removeCtxMenu(); it.act(); });
        ctxMenu.appendChild(row);
      });

      document.body.appendChild(ctxMenu);
    }

    function showEmptyCtxMenu(ev) {
      if (ctxMenu) removeCtxMenu();
      var items = cfg.readOnly ? [
        { label: 'Reload', act: loadRoot }
      ] : [
        { label: 'New File', act: doNewFile },
        { label: 'New Folder', act: doMkdir },
        { label: 'Upload File', act: function () { document.getElementById('fb-file-picker').click(); } },
        { sep: true },
        { label: 'Paste', act: function () { doPaste(state.currentPath); } },
        { sep: true },
        { label: 'Reload', act: loadRoot }
      ];

      ctxMenu = ce('div',
        'fixed z-[9999] min-w-[144px] rounded-xl border border-border/40 bg-[#0d121c]/95 backdrop-blur-md shadow-2xl py-1.5 text-[11px] font-medium text-foreground overflow-hidden');
      ctxMenu.style.left = Math.min(ev.clientX, innerWidth - 160) + 'px';
      ctxMenu.style.top = Math.min(ev.clientY, innerHeight - 200) + 'px';

      items.forEach(function (it) {
        if (it.sep) { ctxMenu.appendChild(ce('div', 'my-1 border-t border-border/30')); return; }
        var row = ce('button', 'w-full text-left px-3 py-1.5 hover:bg-primary/20 transition-colors text-foreground/85 hover:text-primary');
        row.textContent = it.label;
        if (it.label === 'Paste' && (!state.clipboard || !state.clipboard.paths.length)) {
          row.classList.add('opacity-50', 'cursor-not-allowed');
          row.disabled = true;
        } else {
          row.addEventListener('click', function (e) { e.stopPropagation(); removeCtxMenu(); it.act(); });
        }
        ctxMenu.appendChild(row);
      });
      document.body.appendChild(ctxMenu);
    }

    container.addEventListener('contextmenu', function (ev) {
      if (ev.target.closest('[data-path]')) return;
      ev.preventDefault();
      showEmptyCtxMenu(ev);
    });

    /* ════════════════════════════════
       SELECTION
    ════════════════════════════════ */
    function toggleSelect(path, on) {
      if (on) state.selectedPaths.add(path);
      else state.selectedPaths.delete(path);
      updateBulkBar();
    }
    function updateBulkBar() {
      var bar = q(container, 'bulk-bar');
      var cnt = q(container, 'bulk-count');
      if (!bar) return;
      var n = state.selectedPaths.size;
      if (n > 0) { bar.classList.remove('hidden'); bar.classList.add('flex'); }
      else { bar.classList.add('hidden'); bar.classList.remove('flex'); }
      if (cnt) cnt.textContent = n + ' selected';
    }
    function clearSelection() {
      state.selectedPaths.clear();
      container.querySelectorAll('input[type="checkbox"]').forEach(function (cb) { cb.checked = false; });
      container.querySelectorAll('tr[data-path]').forEach(function (tr) { tr.classList.remove('bg-primary/8'); });
      updateBulkBar();
    }

    /* ════════════════════════════════
       CLIPBOARD (copy/cut/paste)
    ════════════════════════════════ */
    function setClipboard(mode, paths) {
      state.clipboard = { mode: mode, paths: paths };
      flash((mode === 'copy' ? 'Copied' : 'Cut') + ' ' + paths.length + ' item(s). Navigate to destination and paste.', false);
    }
    function openDestSelectorModal(title, defaultPath, cb) {
      var overlay = ce('div', 'fixed inset-0 z-[9999] flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm modal-overlay');
      var inner = ce('div', 'relative w-full max-w-md overflow-hidden rounded-2xl border border-border/50 bg-[#0d121c] shadow-2xl');
      var header = ce('div', 'border-b border-border/40 bg-muted/10 px-5 py-3.5 flex justify-between items-center');
      header.innerHTML = '<h3 class="font-semibold tracking-tight text-foreground/90">' + esc(title) + '</h3><button class="text-muted-foreground hover:text-foreground modal-close-x transition-colors">' + icon('close', '18') + '</button>';
      var body = ce('div', 'p-5 space-y-3');
      var inputWrap = ce('div', 'relative');
      var input = ce('input', 'flex h-9 w-full rounded-lg border border-border/60 bg-background/50 px-3 py-1.5 text-sm text-foreground placeholder:text-muted-foreground/50 focus:border-primary/45 focus:outline-none focus:ring-1 focus:ring-primary/30');
      input.value = defaultPath; input.placeholder = "Destination path (e.g. /src/public)";
      inputWrap.appendChild(input);
      var navArea = ce('div', 'h-[220px] overflow-auto border border-border/40 rounded-xl bg-background/20 flex flex-col p-1.5 space-y-0.5 custom-scrollbar');
      body.appendChild(inputWrap); body.appendChild(navArea);
      var footer = ce('div', 'flex items-center justify-end gap-2 border-t border-border/40 bg-muted/5 px-5 py-3.5');
      var cancelBtn = ce('button', 'h-8 px-4 rounded-lg hover:bg-muted/50 text-xs font-medium transition-colors text-muted-foreground');
      cancelBtn.textContent = 'Cancel';
      var okBtn = ce('button', 'h-8 px-4 rounded-lg bg-primary text-primary-foreground shadow hover:bg-primary/90 text-xs font-medium transition-colors');
      okBtn.textContent = 'Confirm';
      footer.appendChild(cancelBtn); footer.appendChild(okBtn);
      inner.appendChild(header); inner.appendChild(body); inner.appendChild(footer); overlay.appendChild(inner);
      document.body.appendChild(overlay);

      var close = function () { overlay.remove(); };
      overlay.onclick = function (e) { if (e.target === overlay) close(); };
      header.querySelector('.modal-close-x').onclick = close; cancelBtn.onclick = close;
      okBtn.onclick = function () { var v = input.value.trim(); close(); cb(v); };

      function loadDirs(path) {
        navArea.innerHTML = '<div class="text-xs text-muted-foreground p-4 text-center">Loading directories...</div>';
        fetch(cfg.treeBase + '?path=' + encodeURIComponent(path)).then(function (r) { return r.json() }).then(function (j) {
          navArea.innerHTML = '';
          if (path !== '') {
            var upBtn = ce('button', 'w-full text-left px-2.5 py-1.5 hover:bg-muted/40 rounded-lg flex items-center gap-2 text-sm font-medium text-foreground transition-colors');
            upBtn.innerHTML = '<span class="text-amber-500">' + icon('folder', '16') + '</span><span class="opacity-75">.. (Up)</span>';
            upBtn.onclick = function () { var p = path.split('/'); p.pop(); var newP = p.join('/'); input.value = newP; loadDirs(newP); };
            navArea.appendChild(upBtn);
          }
          var hasDirs = false;
          (j.entries || []).forEach(function (e) {
            if (e.is_dir) {
              hasDirs = true;
              var b = ce('button', 'w-full text-left px-2.5 py-1.5 hover:bg-muted/40 rounded-lg flex items-center gap-2 text-sm text-foreground transition-colors');
              b.innerHTML = '<span class="text-amber-500">' + icon('folder', '16') + '</span>' + esc(e.name);
              b.onclick = function () { input.value = e.rel_path; loadDirs(e.rel_path); };
              navArea.appendChild(b);
            }
          });
          if (!hasDirs && path === '') navArea.innerHTML = '<div class="text-xs text-muted-foreground p-4 text-center">No subfolders</div>';
        }).catch(function () { navArea.innerHTML = '<div class="text-xs text-rose-400 p-4 text-center">Failed to load</div>'; });
      }
      loadDirs(defaultPath);
    }

    function doPaste(destDir) {
      if (!state.clipboard) { flash('Nothing in clipboard.', true); return; }
      openDestSelectorModal(state.clipboard.mode === 'copy' ? 'Copy Here' : 'Move Here', destDir, function (dest) {
        if (dest === null || dest === undefined) return;
        var url = state.clipboard.mode === 'copy' ? cfg.copyUrl : cfg.moveUrl;
        if (!url) { flash('Endpoint not configured.', true); return; }
        setLoading(true);
        apiPost(url, { paths: state.clipboard.paths, dest: dest })
          .then(function (x) {
            if (x.j.ok) { flash(state.clipboard.mode === 'copy' ? 'Copied successfully.' : 'Moved successfully.', false); if (state.clipboard.mode === 'cut') state.clipboard = null; loadRoot(); }
            else { flash(x.j.message || 'Operation failed.', true); }
          }).catch(function () { flash('Network error.', true); }).finally(function () { setLoading(false); });
      });
    }

    /* ════════════════════════════════
       ACTIONS: rename / delete / zip / mkdir / chmod
    ════════════════════════════════ */
    function doRename(e) {
      openPromptModal('Rename', 'New name:', e.name, function (newName) {
        if (!newName || newName === e.name) return;
        var parentDir = e.rel_path.substring(0, e.rel_path.length - e.name.length);
        setLoading(true);
        apiPost(cfg.renameUrl || ('/apps/' + cfg.appId + '/files/rename'), { old_path: e.rel_path, new_path: parentDir + newName })
          .then(function (x) { if (x.j.ok) { flash('Renamed.', false); loadRoot(); } else flash(x.j.message || 'Rename failed.', true); })
          .catch(function () { flash('Network error.', true); }).finally(function () { setLoading(false); });
      });
    }

    function doDelete(paths) {
      openConfirmModal('Delete ' + paths.length + ' item(s)?', 'This cannot be undone.', function () {
        if (!cfg.deleteUrl) { flash('Delete endpoint not configured.', true); return; }
        setLoading(true);
        apiPost(cfg.deleteUrl, { path: '', paths: paths })
          .then(function (x) { if (x.j.ok) { flash('Deleted.', false); clearSelection(); loadRoot(); } else flash(x.j.message || 'Delete failed.', true); })
          .catch(function () { flash('Network error.', true); }).finally(function () { setLoading(false); });
      });
    }

    function doZip(paths) {
      if (!cfg.zipUrl) { flash('Compress endpoint not configured.', true); return; }
      openPromptModal('Compress', 'Archive name (.zip):', (paths[0].split('/').pop() || 'archive') + '.zip', function (name) {
        if (!name) return;
        setLoading(true);
        apiPost(cfg.zipUrl, { paths: paths, name: name, dest: state.currentPath })
          .then(function (x) { if (x.j.ok) { flash('Compressed: ' + name, false); loadRoot(); } else flash(x.j.message || 'Compress failed.', true); })
          .catch(function () { flash('Network error.', true); }).finally(function () { setLoading(false); });
      });
    }

    function doUnzip(relPath) {
      if (!cfg.unzipUrl) { flash('Extract endpoint not configured.', true); return; }
      openConfirmModal('Extract archive?', relPath + ' → current folder', function () {
        setLoading(true);
        apiPost(cfg.unzipUrl, { path: relPath, dest: state.currentPath })
          .then(function (x) { if (x.j.ok) { flash('Extracted.', false); loadRoot(); } else flash(x.j.message || 'Extract failed.', true); })
          .catch(function () { flash('Network error.', true); }).finally(function () { setLoading(false); });
      });
    }

    function doMkdir() {
      openPromptModal('New Folder', 'Folder name:', '', function (name) {
        if (!name) return;
        var fullPath = state.currentPath ? state.currentPath + '/' + name : name;
        setLoading(true);
        apiPost(cfg.mkdirUrl || ('/apps/' + cfg.appId + '/files/mkdir'), { path: fullPath })
          .then(function (x) { if (x.j.ok) { flash('Folder created.', false); loadRoot(); } else flash(x.j.message || 'Create failed.', true); })
          .catch(function () { flash('Network error.', true); }).finally(function () { setLoading(false); });
      });
    }

    function doNewFile() {
      openPromptModal('New File', 'File name:', '', function (name) {
        if (!name) return;
        var fullPath = state.currentPath ? state.currentPath + '/' + name : name;
        setLoading(true);
        apiPost('/apps/' + cfg.appId + '/files/create', { filename: fullPath })
          .then(function (x) { if (x.j.ok) { flash('File created.', false); loadRoot(); } else flash(x.j.message || 'Create failed.', true); })
          .catch(function () { flash('Network error.', true); }).finally(function () { setLoading(false); });
      });
    }

    function doMoveSelected() {
      var paths = Array.from(state.selectedPaths);
      if (!paths.length) { flash('Select items to move.', true); return; }
      openDestSelectorModal('Move Items', state.currentPath, function (dest) {
        if (dest === null || dest === undefined) return;
        setLoading(true);
        apiPost(cfg.moveUrl || ('/apps/' + cfg.appId + '/files/move'), { paths: paths, dest: dest })
          .then(function (x) { if (x.j.ok) { flash('Moved.', false); clearSelection(); loadRoot(); } else flash(x.j.message || 'Move failed.', true); })
          .catch(function () { flash('Network error.', true); }).finally(function () { setLoading(false); });
      });
    }

    function openChmod(e) {
      var modal = openModal('Permissions — ' + e.name,
        '<div class="space-y-4">' +
        '<p class="text-xs text-muted-foreground font-mono">Path: ' + esc(e.rel_path) + '</p>' +
        '<div class="grid grid-cols-4 gap-2 text-xs font-semibold text-muted-foreground">' +
        '<div></div><div class="text-center">Owner</div><div class="text-center">Group</div><div class="text-center">Other</div></div>' +
        ['Read', 'Write', 'Execute'].map(function (label, ri) {
          return '<div class="grid grid-cols-4 gap-2 items-center">' +
            '<span class="text-xs text-muted-foreground">' + label + '</span>' +
            [0, 1, 2].map(function (ci) {
              var val = (ri === 0 ? [4, 4, 4] : ri === 1 ? [2, 0, 0] : [1, 0, 0]);
              return '<div class="flex justify-center"><input type="checkbox" class="h-4 w-4" ' + (val[ci] ? 'checked' : '') + ' data-perm-r="' + ri + '" data-perm-c="' + ci + '"></div>';
            }).join('') + '</div>';
        }).join('') +
        '<div class="mt-2 rounded-lg bg-muted/20 px-3 py-2 font-mono text-sm text-center" id="chmod-preview">644</div>' +
        '</div>',
        [{
          label: 'Apply', primary: true, action: function (modalEl) {
            var octal = computeChmod(modalEl);
            if (!cfg.chmodUrl) { flash('chmod endpoint not configured.', true); modal.remove(); return; }
            apiPost(cfg.chmodUrl, { path: e.rel_path, mode: octal })
              .then(function (x) {
                if (x.j.ok) { flash('Permissions set to ' + octal + '.', false); loadRoot(); }
                else flash(x.j.message || 'chmod failed.', true);
                modal.remove();
              }).catch(function () { flash('Network error.', true); modal.remove(); });
          }
        }, { label: 'Cancel', action: function (m) { m.remove(); } }]
      );
      /* live preview */
      modal.querySelectorAll('input[data-perm-r]').forEach(function (cb) {
        cb.addEventListener('change', function () {
          document.getElementById('chmod-preview').textContent = computeChmod(modal);
        });
      });
    }
    function computeChmod(modalEl) {
      var vals = [0, 0, 0];
      modalEl.querySelectorAll('input[data-perm-r]').forEach(function (cb) {
        if (cb.checked) {
          var r = parseInt(cb.getAttribute('data-perm-r'));
          var c = parseInt(cb.getAttribute('data-perm-c'));
          vals[c] += [4, 2, 1][r];
        }
      });
      return vals.join('');
    }

    /* ════════════════════════════════
       PROPERTIES MODAL
    ════════════════════════════════ */
    function openProps(e) {
      openModal('Properties',
        '<dl class="space-y-2.5 text-sm">' +
        prop('Name', e.name) + prop('Type', e.is_dir ? 'Folder' : fileType(e.name).toUpperCase()) +
        prop('Path', e.rel_path) + prop('Size', e.is_dir ? '--' : fmtBytes(e.size)) +
        prop('Modified', fmtDate(e.mod_ts)) + prop('Permissions', e.perms || '--') +
        '<dt class="text-muted-foreground text-xs">Direct URL</dt>' +
        '<dd class="flex items-center gap-2 mt-0.5">' +
        '<code class="flex-1 truncate rounded bg-muted/30 px-2 py-1 text-xs font-mono text-foreground/80">' +
        esc('/apps/' + cfg.appId + '/file?path=' + encodeURIComponent(e.rel_path)) + '</code>' +
        '<button id="copy-url-btn" class="shrink-0 rounded px-2 py-1 bg-primary/20 text-xs text-primary hover:bg-primary/30 transition-colors">Copy</button>' +
        '</dd>' + '</dl>',
        [{ label: 'Close', action: function (m) { m.remove(); } }]
      );
      var btn = document.getElementById('copy-url-btn');
      if (btn) btn.onclick = function () { copyToClipboard('/apps/' + cfg.appId + '/file?path=' + encodeURIComponent(e.rel_path)); };
    }
    function prop(label, val) {
      return '<div><dt class="text-muted-foreground text-xs">' + esc(label) + '</dt>' +
        '<dd class="mt-0.5 font-mono text-xs text-foreground/90 break-all">' + esc(val) + '</dd></div>';
    }

    /* ════════════════════════════════
       FILE PREVIEW MODAL
    ════════════════════════════════ */
    function openPreview(relPath, size, name) {
      var type = fileType(name);

      var inner = '<div class="modal-box max-w-5xl w-full" onclick="event.stopPropagation()">' +
        '<div class="sticky top-0 z-20 -mx-6 -mt-6 mb-3 border-b border-border/50 bg-card px-6 pt-6 pb-3 flex items-center justify-between gap-3">' +
        '<div class="min-w-0"><h3 class="text-base font-semibold truncate">' + esc(name) + '</h3>' +
        '<p class="mt-0.5 text-xs text-muted-foreground font-mono">' + esc(relPath) + '</p></div>' +
        '<div class="flex items-center gap-2">' +
        '<span class="text-xs text-muted-foreground">' + fmtBytes(size) + '</span>' +
        '<a href="/apps/' + cfg.appId + '/file?path=' + encodeURIComponent(relPath) + '&download=1" download="' + esc(name) + '" class="btn-ghost px-2 py-1 text-xs">' + icon('download', '12') + ' Download</a>' +
        '<button class="shrink-0 text-muted-foreground hover:text-foreground modal-close-x">' + icon('close', '18') + '</button>' +
        '</div></div>' +
        '<div id="preview-content" class="relative min-h-[min(60vh,480px)] overflow-hidden rounded-xl border border-border/40 bg-[#04060a] ring-1 ring-border/40">' +
        '<div id="preview-loading" class="absolute inset-0 flex items-center justify-center text-xs text-muted-foreground">Loading…</div>' +
        '<div id="preview-body"></div>' +
        '</div></div>';

      var overlay = ce('div', 'modal-overlay');
      overlay.innerHTML = inner;
      overlay.onclick = function (ev) { if (ev.target === overlay) overlay.remove(); };
      overlay.querySelector('.modal-close-x').onclick = function () { overlay.remove(); };
      document.body.appendChild(overlay);

      var pbody = overlay.querySelector('#preview-body');
      var ploading = overlay.querySelector('#preview-loading');

      /* render by type */
      if (type === 'image') {
        var src = '/apps/' + cfg.appId + '/file?path=' + encodeURIComponent(relPath);
        pbody.className = 'absolute inset-0 flex items-center justify-center p-4';
        pbody.innerHTML = '<img src="' + esc(src) + '" class="max-h-full max-w-full object-contain rounded-lg" onload="this.previousSibling&&this.previousSibling.remove()">';
        ploading.remove();
        return;
      }
      if (type === 'video') {
        var src = '/apps/' + cfg.appId + '/file?path=' + encodeURIComponent(relPath);
        pbody.className = 'absolute inset-0 flex items-center justify-center bg-black p-2';
        pbody.innerHTML = '<video controls class="max-h-full max-w-full rounded-lg" src="' + esc(src) + '"></video>';
        ploading.remove();
        return;
      }
      if (type === 'audio') {
        var src = '/apps/' + cfg.appId + '/file?path=' + encodeURIComponent(relPath);
        pbody.className = 'absolute inset-0 flex items-center justify-center p-8';
        pbody.innerHTML = '<div class="w-full space-y-4 text-center">' +
          '<div class="text-4xl">' + icon('audio', '48') + '</div>' +
          '<audio controls class="w-full" src="' + esc(src) + '"></audio></div>';
        ploading.remove();
        return;
      }
      if (type === 'pdf') {
        var src = '/apps/' + cfg.appId + '/file?path=' + encodeURIComponent(relPath);
        pbody.className = 'absolute inset-0';
        pbody.innerHTML = '<iframe src="' + esc(src) + '" class="w-full h-full rounded-xl" title="PDF preview"></iframe>';
        ploading.remove();
        return;
      }
      if (type === 'archive') {
        pbody.className = 'absolute inset-0 flex flex-col items-center justify-center gap-4 p-8';
        pbody.innerHTML = '<div class="text-5xl opacity-50">' + icon('archive', '48') + '</div>' +
          '<p class="text-sm text-muted-foreground">' + esc(name) + ' · ' + fmtBytes(size) + '</p>' +
          (cfg.unzipUrl ? '<button id="extract-btn" class="btn-primary px-4 py-2 text-sm">' + icon('zip', '13') + ' Extract here</button>' : '');
        ploading.remove();
        if (cfg.unzipUrl) {
          overlay.querySelector('#extract-btn').onclick = function () { overlay.remove(); doUnzip(relPath); };
        }
        return;
      }

      /* code / text via Monaco */
      fetch(cfg.blobBase + '?path=' + encodeURIComponent(relPath))
        .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
        .then(function (x) {
          ploading.remove();
          if (!x.ok || x.j.binary) {
            pbody.className = 'absolute inset-0 flex items-center justify-center text-sm text-muted-foreground';
            pbody.textContent = x.j.binary ? 'Binary file — cannot display.' : (x.j.error || 'Failed to load.');
            return;
          }
          if (x.j.too_large) {
            pbody.className = 'absolute inset-0 flex items-center justify-center text-sm text-amber-400';
            pbody.textContent = 'File too large for inline preview.';
            return;
          }
          var text = x.j.text || '';
          pbody.className = 'absolute inset-0';
          var monacoHost = ce('div', 'absolute inset-0');
          pbody.appendChild(monacoHost);

          /* Add edit/save toolbar */
          var toolbar = overlay.querySelector('.sticky');
          var statusSp = ce('span', 'text-xs text-muted-foreground modal-edit-status hidden');
          var editBtn = ce('button', 'btn-ghost px-2 py-1 text-xs', icon('rename', '12') + ' Edit');
          var saveBtn = ce('button', 'btn-primary px-2 py-1 text-xs hidden', icon('check', '12') + ' Save');
          if (cfg.readOnly) editBtn.classList.add('hidden');
          toolbar.querySelector('.flex.items-center.gap-2').prepend(statusSp, editBtn, saveBtn);

          // Show fallback basic textarea immediately so it never appears blank
          var fallbackTextArea = ce('textarea', 'absolute inset-0 w-full h-full p-4 bg-[#050810] text-foreground/90 font-mono text-xs resize-none focus:outline-none custom-scrollbar');
          fallbackTextArea.value = text;
          fallbackTextArea.readOnly = true;
          monacoHost.appendChild(fallbackTextArea);

          var activeEditor = { type: 'basic', getValue: function() { return fallbackTextArea.value; }, setReadOnly: function(ro) { fallbackTextArea.readOnly = ro; if(!ro) fallbackTextArea.focus(); } };

          editBtn.onclick = function () {
            activeEditor.setReadOnly(false);
            editBtn.classList.add('hidden'); saveBtn.classList.remove('hidden');
            statusSp.textContent = activeEditor.type === 'basic' ? 'Editing (Basic)…' : 'Editing…';
            statusSp.classList.remove('hidden');
          };
          saveBtn.onclick = function () {
            var content = activeEditor.getValue();
            statusSp.textContent = 'Saving…'; saveBtn.disabled = true;
            var saveUrl = (cfg.saveBase || ('/apps/' + cfg.appId + '/files/save')) + '?path=' + encodeURIComponent(relPath);
            fetch(saveUrl, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content: content }) })
              .then(function (r) { return r.json(); })
              .then(function (j) {
                if (j.ok) {
                  statusSp.textContent = 'Saved ✔'; activeEditor.setReadOnly(true);
                  editBtn.classList.remove('hidden'); saveBtn.classList.add('hidden'); saveBtn.disabled = false; loadRoot();
                  setTimeout(function () { statusSp.textContent = ''; }, 3000);
                } else { statusSp.textContent = j.message || 'Save failed'; saveBtn.disabled = false; }
              }).catch(function () { statusSp.textContent = 'Network error'; saveBtn.disabled = false; });
          };

          loadMonaco(function () {
            if (!global.monaco) return;
            
            // Monaco loaded! Clear fallback and init monaco
            monacoHost.innerHTML = '';
            var ed = global.monaco.editor.create(monacoHost, {
              value: text, language: monacoLang(relPath), readOnly: true,
              theme: 'fb-dark', minimap: { enabled: false }, fontSize: 12,
              lineNumbers: 'on', scrollBeyondLastLine: false, wordWrap: 'on',
              padding: { top: 10, bottom: 10 }, automaticLayout: true,
            });
            activeEditor = { 
              type: 'monaco', 
              getValue: function() { return ed.getValue(); }, 
              setReadOnly: function(ro) { ed.updateOptions({ readOnly: ro }); if(!ro) ed.focus(); } 
            };
          });
        });
    }

    /* ════════════════════════════════
       UPLOAD — drag/drop + file picker
    ════════════════════════════════ */
    function initUpload() {
      var zone = container.querySelector('#fb-drop-zone');
      if (!zone || !cfg.uploadUrl) return;

      zone.addEventListener('dragover', function (ev) { ev.preventDefault(); zone.classList.add('ring-2', 'ring-primary/50', 'bg-primary/5'); });
      zone.addEventListener('dragleave', function () { zone.classList.remove('ring-2', 'ring-primary/50', 'bg-primary/5'); });
      zone.addEventListener('drop', function (ev) {
        ev.preventDefault();
        zone.classList.remove('ring-2', 'ring-primary/50', 'bg-primary/5');
        var files = Array.from(ev.dataTransfer.files);
        if (files.length) uploadFiles(files);
      });

      var picker = container.querySelector('#fb-file-picker');
      if (picker) picker.addEventListener('change', function () {
        var files = Array.from(picker.files);
        if (files.length) { uploadFiles(files); picker.value = ''; }
      });
    }

    function uploadFiles(files) {
      var bar = container.querySelector('#fb-upload-progress');
      var pct = container.querySelector('#fb-upload-pct');
      if (bar) bar.classList.remove('hidden');

      var total = files.length, done = 0;
      function next(i) {
        if (i >= files.length) {
          if (bar) bar.classList.add('hidden');
          flash('Uploaded ' + total + ' file(s).', false);
          loadRoot(); return;
        }
        var fd = new FormData();
        fd.append('file', files[i]);
        fd.append('path', state.currentPath);
        var xhr = new XMLHttpRequest();
        xhr.open('POST', cfg.uploadUrl);
        xhr.upload.onprogress = function (ev) {
          if (ev.lengthComputable && pct) {
            var overall = Math.round(((done + ev.loaded / ev.total) / total) * 100);
            pct.style.width = overall + '%';
          }
        };
        xhr.onload = function () { done++; next(i + 1); };
        xhr.onerror = function () { flash('Upload failed: ' + files[i].name, true); done++; next(i + 1); };
        xhr.send(fd);
      }
      next(0);
    }

    function doUrlUpload() {
      openPromptModal('Upload from URL', 'Remote URL:', 'https://', function (url) {
        if (!url || !cfg.urlUpUrl) return;
        setLoading(true);
        apiPost(cfg.urlUpUrl, { url: url, dest: state.currentPath })
          .then(function (x) {
            if (x.j.ok) { flash('Downloaded to server.', false); loadRoot(); }
            else flash(x.j.message || 'URL upload failed.', true);
          })
          .catch(function () { flash('Network error.', true); })
          .finally(function () { setLoading(false); });
      });
    }

    /* ════════════════════════════════
       REUSABLE MODALS
    ════════════════════════════════ */
    function openModal(title, bodyHtml, actions) {
      var overlay = ce('div', 'modal-overlay');
      var btnsHtml = (actions || []).map(function (a, i) {
        return '<button data-modal-act="' + i + '" class="' + (a.primary ? 'btn-primary' : 'btn-ghost') + ' px-4 py-1.5 text-sm">' + esc(a.label) + '</button>';
      }).join('');
      overlay.innerHTML =
        '<div class="modal-box max-w-lg w-full" onclick="event.stopPropagation()">' +
        '<div class="flex items-center justify-between mb-4">' +
        '<h3 class="font-semibold text-base">' + esc(title) + '</h3>' +
        '<button class="modal-close-x text-muted-foreground hover:text-foreground transition-colors">' + icon('close', '18') + '</button>' +
        '</div>' + bodyHtml +
        '<div class="flex justify-end gap-2 mt-5">' + btnsHtml + '</div>' +
        '</div>';
      overlay.onclick = function (ev) { if (ev.target === overlay) overlay.remove(); };
      overlay.querySelector('.modal-close-x').onclick = function () { overlay.remove(); };
      (actions || []).forEach(function (a, i) {
        overlay.querySelector('[data-modal-act="' + i + '"]').onclick = function () { a.action(overlay); };
      });
      document.body.appendChild(overlay);
      return overlay;
    }

    function openPromptModal(title, label, defaultVal, cb) {
      var overlay = openModal(title,
        '<label class="block text-xs text-muted-foreground mb-1.5">' + esc(label) + '</label>' +
        '<input id="prompt-input" type="text" value="' + esc(defaultVal) + '" class="w-full rounded-lg border border-border/40 bg-muted/20 px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-primary/50">',
        [{
          label: 'OK', primary: true,
          action: function (m) { var v = m.querySelector('#prompt-input').value.trim(); m.remove(); cb(v || null); }
        }, { label: 'Cancel', action: function (m) { m.remove(); } }]
      );
      var inp = overlay.querySelector('#prompt-input');
      inp.focus(); inp.select();
      inp.addEventListener('keydown', function (ev) {
        if (ev.key === 'Enter') { var v = inp.value.trim(); overlay.remove(); cb(v || null); }
        if (ev.key === 'Escape') { overlay.remove(); cb(null); }
      });
    }

    function openConfirmModal(title, body, cb) {
      openModal(title,
        '<p class="text-sm text-muted-foreground">' + esc(body) + '</p>',
        [{
          label: 'Confirm', primary: true,
          action: function (m) { m.remove(); cb(); }
        }, { label: 'Cancel', action: function (m) { m.remove(); } }]
      );
    }

    /* ════════════════════════════════
       MISC UTILS
    ════════════════════════════════ */
    function copyToClipboard(text) {
      navigator.clipboard.writeText(text)
        .then(function () { flash('URL copied to clipboard.', false); })
        .catch(function () { flash('Copy failed.', true); });
    }

    function dlFile(relPath, name) {
      var a = ce('a');
      a.href = '/apps/' + cfg.appId + '/file?path=' + encodeURIComponent(relPath) + '&download=1';
      a.download = name || 'file';
      document.body.appendChild(a); a.click(); a.remove();
    }

    function dlZip(paths) {
      var zipUrl = '/apps/' + cfg.appId + '/files/download-zip';
      if (paths && paths.length) zipUrl += '?paths=' + paths.map(encodeURIComponent).join(',');
      var a = ce('a'); a.href = zipUrl; a.download = '';
      document.body.appendChild(a); a.click(); a.remove();
    }

    /* ════════════════════════════════
       LOAD ROOT
    ════════════════════════════════ */
    function loadRoot() {
      if (searchInput) searchInput.value = '';
      state.searchQuery = '';
      setLoading(true);
      renderBreadcrumbs();

      /* update hidden upload path field */
      var pathField = container.querySelector('input[name="path"]');
      if (pathField) pathField.value = state.currentPath;

      fetch(cfg.treeBase + '?path=' + encodeURIComponent(state.currentPath))
        .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
        .then(function (x) {
          if (!x.ok) { flash(x.j.error || 'Load failed.', true); return; }
          state.entries = x.j.entries || [];
          render();
        })
        .catch(function () { flash('Network error loading directory.', true); })
        .finally(function () { setLoading(false); });
    }

    function render() {
      if (state.viewMode === 'grid') renderGrid(state.entries);
      else renderList(state.entries);
      updateBulkBar();
    }

    /* ════════════════════════════════
       UPLOAD WIRING
    ════════════════════════════════ */
    function initUpload() {
      var picker = container.querySelector('#fb-file-picker');
      if (!picker) return;

      function handleFiles(files) {
        if (!files || !files.length) return;
        if (!cfg.uploadUrl) { flash('Upload endpoint not configured.', true); return; }

        var fd = new FormData();
        for (var i = 0; i < files.length; i++) fd.append('file', files[i]);
        fd.append('path', state.currentPath);

        setLoading(true);
        fetch(cfg.uploadUrl + '?format=json', { method: 'POST', body: fd })
          .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
          .then(function (x) {
            if (x.ok) { flash('Uploaded ' + files.length + ' file(s).', false); loadRoot(); }
            else flash(x.j.message || 'Upload failed.', true);
          })
          .catch(function () { flash('Network error during upload.', true); })
          .finally(function () { setLoading(false); picker.value = ''; });
      }

      picker.addEventListener('change', function () { handleFiles(picker.files); });

      // Drag and drop overlay
      var dropZone = container;
      var dropOverlay = ce('div', 'absolute inset-0 z-[100] hidden items-center justify-center bg-primary/10 backdrop-blur-[2px] border-2 border-dashed border-primary rounded-2xl');
      dropOverlay.innerHTML = '<div class="text-xl font-semibold text-primary pointer-events-none flex flex-col items-center gap-4">' + icon('upload', '48') + 'Drop files to upload</div>';
      container.appendChild(dropOverlay);

      container.addEventListener('dragover', function (e) {
        e.preventDefault(); e.stopPropagation();
        if (e.dataTransfer.types.includes('Files')) dropOverlay.classList.remove('hidden');
      });
      container.addEventListener('dragleave', function (e) {
        e.preventDefault(); e.stopPropagation();
        if (e.target === dropOverlay || e.target === container) dropOverlay.classList.add('hidden');
      });
      container.addEventListener('drop', function (e) {
        e.preventDefault(); e.stopPropagation();
        dropOverlay.classList.add('hidden');
        if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
          handleFiles(e.dataTransfer.files);
        }
      });
    }

    /* ════════════════════════════════
       TOOLBAR WIRING
    ════════════════════════════════ */
    function wireToolbar() {
      /* view toggle */
      var listBtn = container.querySelector('[data-view="list"]');
      var gridBtn = container.querySelector('[data-view="grid"]');
      function updateViewBtns() {
        if (listBtn) listBtn.classList.toggle('bg-muted/40', state.viewMode === 'list');
        if (gridBtn) gridBtn.classList.toggle('bg-muted/40', state.viewMode === 'grid');
      }
      if (listBtn) listBtn.onclick = function () { state.viewMode = 'list'; localStorage.setItem('fb2_view_' + cfg.appId, 'list'); render(); updateViewBtns(); };
      if (gridBtn) gridBtn.onclick = function () { state.viewMode = 'grid'; localStorage.setItem('fb2_view_' + cfg.appId, 'grid'); render(); updateViewBtns(); };
      updateViewBtns();

      /* sort buttons */
      sortBtns.forEach(function (btn) {
        btn.onclick = function () {
          var key = btn.getAttribute('data-fb-sort');
          if (state.sortKey === key) state.sortAsc = !state.sortAsc;
          else { state.sortKey = key; state.sortAsc = true; }
          render();
        };
      });

      /* search */
      if (searchInput) {
        searchInput.addEventListener('input', function () {
          state.searchQuery = (searchInput.value || '').toLowerCase().trim();
          render();
        });
      }

      /* new folder / new file */
      var mkdirBtn = container.querySelector('[data-fb-action="mkdir"]');
      if (mkdirBtn) mkdirBtn.onclick = doMkdir;
      var newFileBtn = container.querySelector('[data-fb-action="newfile"]');
      if (newFileBtn) newFileBtn.onclick = doNewFile;

      /* url upload */
      var urlUpBtn = container.querySelector('[data-fb-action="url-upload"]');
      if (urlUpBtn) urlUpBtn.onclick = doUrlUpload;

      /* refresh */
      var refreshBtn = container.querySelector('[data-fb="reload"]') || container.querySelector('[data-fb-action="reload"]');
      if (refreshBtn) refreshBtn.onclick = loadRoot;

      /* bulk bar actions */
      var bulkDelBtn = container.querySelector('[data-fb="delete-selected"]');
      if (bulkDelBtn) bulkDelBtn.onclick = function () {
        var paths = Array.from(state.selectedPaths);
        if (!paths.length) { flash('Nothing selected.', true); return; }
        doDelete(paths);
      };
      var bulkDlBtn = container.querySelector('[data-fb-action="dl-selected"]');
      if (bulkDlBtn) bulkDlBtn.onclick = function () { dlZip(Array.from(state.selectedPaths)); };
      var bulkZipBtn = container.querySelector('[data-fb-action="zip-selected"]');
      if (bulkZipBtn) bulkZipBtn.onclick = function () { doZip(Array.from(state.selectedPaths)); };
      var bulkMoveBtn = container.querySelector('[data-fb-action="move-selected"]');
      if (bulkMoveBtn) bulkMoveBtn.onclick = doMoveSelected;
      var bulkCopyBtn = container.querySelector('[data-fb-action="copy-selected"]');
      if (bulkCopyBtn) bulkCopyBtn.onclick = function () { setClipboard('copy', Array.from(state.selectedPaths)); };
      var bulkClearBtn = container.querySelector('[data-fb-action="clear-sel"]');
      if (bulkClearBtn) bulkClearBtn.onclick = clearSelection;

      /* select all */
      var selAllCb = container.querySelector('#fb-select-all');
      if (selAllCb) selAllCb.addEventListener('change', function () {
        if (selAllCb.checked) state.entries.forEach(function (e) { state.selectedPaths.add(e.rel_path); });
        else state.selectedPaths.clear();
        render();
      });

      /* paste */
      var pasteBtn = container.querySelector('[data-fb-action="paste"]');
      if (pasteBtn) pasteBtn.onclick = function () { doPaste(state.currentPath); };

      /* global keyboard: Ctrl+V = paste, Escape = clear */
      document.addEventListener('keydown', function (ev) {
        if ((ev.ctrlKey || ev.metaKey) && ev.key === 'v' && state.clipboard) {
          if (document.activeElement.tagName === 'INPUT' || document.activeElement.tagName === 'TEXTAREA') return;
          doPaste(state.currentPath);
        }
        if (ev.key === 'Escape') clearSelection();
      });
    }

    /* ── INIT ── */
    wireToolbar();
    initUpload();
    loadRoot();
  }

  /* ── boot all instances ── */
  function boot() {
    document.querySelectorAll('[data-panel-file-browser]').forEach(mount);
  }

  global.PanelFileBrowser = { mount: mount, boot: boot };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
  else boot();

})(window);