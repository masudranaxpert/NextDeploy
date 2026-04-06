/**
 * Reusable file explorer + optional inline Monaco + optional file modal (workspace).
 *
 * Root: data-panel-file-browser, data-tree-base, data-blob-base (required).
 * Modes (pick by markup + click handlers):
 *   - Inline preview (e.g. Git): include preview-title, preview-stage, preview-monaco, preview-body.
 *     File row click -> loadBlob() -> sidebar Monaco.
 *   - Modal only (e.g. workspace uploads): omit preview-monaco; set data-file-preview-modal="1".
 *     File row click -> openFileModal(); Monaco loads from CDN once, editor is created inside the modal.
 * Monaco library is shared globally; sidebar editor is created only when preview-monaco exists.
 */
(function (global) {
  'use strict';

  function q(container, role) {
    return container.querySelector('[data-fb="' + role + '"]');
  }

  function mount(container) {
    if (!container || container.getAttribute('data-fb-mounted') === '1') return;
    container.setAttribute('data-fb-mounted', '1');

    var treeBase = (container.getAttribute('data-tree-base') || '').replace(/\/$/, '');
    var blobBase = (container.getAttribute('data-blob-base') || '').replace(/\/$/, '');
    var deleteUrl = (container.getAttribute('data-delete-url') || '').trim();
    var emptyRootMsg = container.getAttribute('data-empty-root') || 'This folder is empty.';
    var refreshBtnId = (container.getAttribute('data-refresh-button-id') || '').trim();
    var deleteEnabled = !!deleteUrl;
    var filePreviewModal = container.getAttribute('data-file-preview-modal') === '1';
    var bulkSelectActive = false;

    var treeRoot = q(container, 'tree');
    if (!treeRoot) return;

    var appId = (treeRoot.getAttribute('data-app-id') || '').trim();
    if (!appId) return;

    var monacoEditor = null;
    var MONACO_CDN_VER = '0.53.0';
    var monacoBase = 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/' + MONACO_CDN_VER + '/min/vs';
    var monacoWorkerAsset = {
      editor: '/assets/editor.worker.50c051c0.min.js',
      json: '/assets/json.worker.32db31cb.min.js',
      css: '/assets/css.worker.4040859e.min.js',
      html: '/assets/html.worker.1a3caaf3.min.js',
      ts: '/assets/ts.worker.77b7bdc2.min.js'
    };
    var selectedFileRow = null;
    var selectedRelPath = '';

    function formatBytes(n) {
      n = Number(n) || 0;
      if (n < 1024) return n + ' B';
      if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
      return (n / 1048576).toFixed(1) + ' MB';
    }
    function setLoading(on) {
      var el = q(container, 'loading');
      if (!el) return;
      if (on) {
        el.classList.remove('hidden');
        el.classList.add('flex');
      } else {
        el.classList.add('hidden');
        el.classList.remove('flex');
      }
    }
    function showTreeError(msg) {
      var el = q(container, 'error');
      if (!el) return;
      el.textContent = msg || '';
      if (msg) el.classList.remove('hidden');
      else el.classList.add('hidden');
    }
    function setFlash(msg, isError) {
      var el = q(container, 'flash');
      if (!el) return;
      el.textContent = msg || '';
      var base =
        'rounded-xl border px-3 py-2.5 text-xs shadow-sm ' +
        (isError
          ? 'border-rose-500/30 bg-rose-500/10 text-rose-200'
          : 'border-emerald-500/25 bg-emerald-500/10 text-emerald-200');
      el.className = base + (msg ? '' : ' hidden');
    }
    function setPreviewActions(show, url) {
      var act = q(container, 'preview-actions');
      var dl = q(container, 'preview-dl');
      if (!act || !dl) return;
      if (show && url) {
        act.classList.remove('hidden');
        act.classList.add('flex');
        dl.href = url;
      } else {
        act.classList.add('hidden');
        act.classList.remove('flex');
        dl.removeAttribute('href');
      }
    }
    function setPreviewMode(mode) {
      var empty = q(container, 'preview-empty');
      var body = q(container, 'preview-body');
      var monacoHost = q(container, 'preview-monaco');
      if (empty) empty.classList.toggle('hidden', mode !== 'empty');
      if (body) {
        body.classList.toggle('hidden', mode !== 'fallback');
        if (mode !== 'fallback') body.textContent = '';
      }
      if (monacoHost) {
        monacoHost.classList.toggle('hidden', mode !== 'monaco');
        monacoHost.setAttribute('aria-hidden', mode === 'monaco' ? 'false' : 'true');
      }
    }
    function clearPreview() {
      var title = q(container, 'preview-title');
      var meta = q(container, 'preview-meta');
      if (title) title.textContent = 'Select a file';
      if (meta) meta.textContent = '';
      setPreviewActions(false, '');
      setPreviewMode('empty');
      if (selectedFileRow) {
        selectedFileRow.classList.remove('bg-primary/12', 'ring-1', 'ring-primary/35');
        selectedFileRow = null;
      }
      selectedRelPath = '';
    }
    function sortEntries(entries) {
      var list = (entries || []).slice();
      list.sort(function (a, b) {
        if (!!a.is_dir !== !!b.is_dir) return a.is_dir ? -1 : 1;
        return String(a.name).localeCompare(String(b.name), undefined, { sensitivity: 'base' });
      });
      return list;
    }
    function fileExt(name) {
      var s = String(name);
      var lower = s.toLowerCase();
      if (lower === 'dockerfile') return 'dockerfile';
      if (lower.startsWith('.') && lower.length > 1) {
        var rest = lower.slice(1);
        var d = rest.lastIndexOf('.');
        if (d > 0) return rest.slice(d + 1);
        return rest;
      }
      var i = s.lastIndexOf('.');
      if (i <= 0 || i === s.length - 1) return '';
      return s.slice(i + 1).toLowerCase();
    }
    function monacoLanguageForPath(relPath) {
      var base = relPath.split('/').pop() || '';
      var ext = fileExt(base);
      var map = {
        go: 'go', js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript', ts: 'typescript', tsx: 'typescript',
        json: 'json', yaml: 'yaml', yml: 'yaml', md: 'markdown', mdx: 'markdown',
        html: 'html', htm: 'html', css: 'css', scss: 'scss', less: 'less',
        xml: 'xml', svg: 'xml',
        sql: 'sql', sh: 'shell', bash: 'shell', zsh: 'shell', ps1: 'powershell',
        py: 'python', pyw: 'python', pyi: 'python', ipynb: 'python', rb: 'ruby', rs: 'rust', java: 'java', kt: 'kotlin', swift: 'swift',
        c: 'c', h: 'c', cpp: 'cpp', cc: 'cpp', cxx: 'cpp', hpp: 'cpp',
        dockerfile: 'dockerfile', env: 'plaintext', gitignore: 'plaintext', toml: 'ini', ini: 'ini',
        mod: 'go', sum: 'plaintext', vue: 'html', svelte: 'html', 'python-version': 'plaintext',
        txt: 'plaintext', log: 'plaintext', rst: 'markdown'
      };
      if (ext === 'dockerfile' || String(base).toLowerCase() === 'dockerfile') return 'dockerfile';
      return map[ext] || 'plaintext';
    }
    function flushFileIconQueue() {
      var queue = global.__panelFileIconQueue;
      global.__panelFileIconQueue = null;
      var fn = global.__gitGetIconClass;
      if (!fn || !queue) return;
      queue.forEach(function (item) {
        if (!item.el.isConnected) return;
        var isDir = item.key.endsWith('/');
        var n = isDir ? item.key.slice(0, -1) : item.key;
        var cls = fn(n, isDir);
        if (cls) item.el.className = cls + ' git-browser-file-icon';
      });
    }
    function applyFileIcon(iconEl, lookupKey) {
      var fn = global.__gitGetIconClass;
      if (fn) {
        var isDir = lookupKey.endsWith('/');
        var n = isDir ? lookupKey.slice(0, -1) : lookupKey;
        var cls = fn(n, isDir);
        if (cls) iconEl.className = cls + ' git-browser-file-icon';
        return;
      }
      if (!global.__panelFileIconQueue) {
        global.__panelFileIconQueue = [];
        global.addEventListener('gitfileiconsready', flushFileIconQueue, { once: true });
      }
      global.__panelFileIconQueue.push({ el: iconEl, key: lookupKey });
    }
    function fileIconWrap(isDir, displayName) {
      var wrap = document.createElement('span');
      wrap.className = 'git-browser-icon-cell flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ring-1 ring-border/25 bg-muted/15';
      var inner = document.createElement('i');
      inner.setAttribute('aria-hidden', 'true');
      inner.className = 'icon git-browser-file-icon default-icon';
      wrap.appendChild(inner);
      var name = String(displayName || '');
      var key = isDir ? (name.replace(/\/?$/, '') + '/') : name;
      applyFileIcon(inner, key);
      return wrap;
    }
    function layoutMonaco() {
      if (monacoEditor) {
        requestAnimationFrame(function () {
          requestAnimationFrame(function () {
            monacoEditor.layout();
          });
        });
      }
    }
    /** Match file newline style so Save preserves CRLF vs LF (Monaco defaults to LF otherwise). */
    function applyModelEOL(editor, text) {
      var m = editor && editor.getModel && editor.getModel();
      if (!m || !global.monaco || !global.monaco.editor.EndOfLineSequence) return;
      var EOL = global.monaco.editor.EndOfLineSequence;
      var t = text != null ? String(text) : '';
      if (/\r\n/.test(t)) {
        m.setEOL(EOL.CRLF);
      } else {
        m.setEOL(EOL.LF);
      }
    }
    function registerPanelMonacoTheme() {
      if (!global.monaco || !global.monaco.editor || global.__panelMonacoThemeDone) return;
      global.__panelMonacoThemeDone = true;
      global.monaco.editor.defineTheme('panel-code-dark', {
        base: 'vs-dark',
        inherit: true,
        rules: [],
        colors: {
          'editor.background': '#06080d',
          'editorGutter.background': '#06080d',
          'minimap.background': '#06080d'
        }
      });
    }
    function ensureMonaco(cb) {
      var sidebarHost = q(container, 'preview-monaco');
      if (global.monaco && (!sidebarHost || monacoEditor)) {
        return cb(!!global.monaco);
      }
      if (global.__panelMonacoPromise) {
        global.__panelMonacoPromise.then(function () {
          finishEnsureMonaco(cb);
        });
        return;
      }
      global.__panelMonacoPromise = new Promise(function (resolve) {
        function monacoWorkerBlobUrl(rel) {
          var u = monacoBase + rel;
          var body = "importScripts('" + u.replace(/\\/g, '\\\\').replace(/'/g, "\\'") + "');";
          try {
            return URL.createObjectURL(new Blob([body], { type: 'application/javascript' }));
          } catch (e) {
            return u;
          }
        }
        self.MonacoEnvironment = {
          getWorkerUrl: function (_workerId, label) {
            if (label === 'json') return monacoWorkerBlobUrl(monacoWorkerAsset.json);
            if (label === 'css' || label === 'scss' || label === 'less') return monacoWorkerBlobUrl(monacoWorkerAsset.css);
            if (label === 'html' || label === 'handlebars' || label === 'razor') return monacoWorkerBlobUrl(monacoWorkerAsset.html);
            if (label === 'typescript' || label === 'javascript') return monacoWorkerBlobUrl(monacoWorkerAsset.ts);
            return monacoWorkerBlobUrl(monacoWorkerAsset.editor);
          }
        };
        var s = document.createElement('script');
        s.src = monacoBase + '/loader.js';
        s.async = true;
        s.onload = function () {
          try {
            require.config({ paths: { vs: monacoBase } });
            require(['vs/editor/editor.main'], function () {
              registerPanelMonacoTheme();
              resolve();
            });
          } catch (e) {
            resolve();
          }
        };
        s.onerror = function () {
          resolve();
        };
        document.head.appendChild(s);
      });
      global.__panelMonacoPromise.then(function () {
        finishEnsureMonaco(cb);
      });
    }
    function finishEnsureMonaco(cb) {
      var host = q(container, 'preview-monaco');
      if (global.monaco && host && !monacoEditor) {
        registerPanelMonacoTheme();
        monacoEditor = global.monaco.editor.create(host, {
          value: '',
          language: 'plaintext',
          readOnly: true,
          theme: 'panel-code-dark',
          minimap: { enabled: false },
          fontSize: 12,
          lineNumbers: 'on',
          scrollBeyondLastLine: false,
          wordWrap: 'on',
          padding: { top: 10, bottom: 10 },
          automaticLayout: true,
          renderLineHighlight: 'line',
          scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 }
        });
        var stageEl = q(container, 'preview-stage');
        if (stageEl && !stageEl.dataset.panelMonacoResizeObs && typeof ResizeObserver !== 'undefined') {
          stageEl.dataset.panelMonacoResizeObs = '1';
          new ResizeObserver(function () {
            if (monacoEditor) monacoEditor.layout();
          }).observe(stageEl);
        }
      }
      cb(!!global.monaco);
    }
    function showInMonaco(relPath, text) {
      ensureMonaco(function (ok) {
        var title = q(container, 'preview-title');
        if (title) title.textContent = relPath;
        var empty = q(container, 'preview-empty');
        if (!ok || !monacoEditor) {
          setPreviewMode('empty');
          if (empty) {
            empty.classList.remove('hidden');
            empty.textContent = ok
              ? 'Code editor failed to initialize.'
              : 'Could not load the code editor. Check your network and refresh.';
          }
          return;
        }
        setPreviewMode('monaco');
        if (empty) empty.textContent = 'Select a file in the explorer tree';
        monacoEditor.setValue(text != null ? text : '');
        applyModelEOL(monacoEditor, text);
        var lang = monacoLanguageForPath(relPath);
        global.monaco.editor.setModelLanguage(monacoEditor.getModel(), lang);
        layoutMonaco();
      });
    }
    function showFallbackMessage(text, metaLine, dlUrl) {
      var body = q(container, 'preview-body');
      var meta = q(container, 'preview-meta');
      setPreviewMode('fallback');
      if (body) body.textContent = text || '';
      if (meta) meta.textContent = metaLine || '';
      setPreviewActions(!!dlUrl, dlUrl || '');
    }
    function selectFileRow(row, relPath) {
      if (selectedFileRow) {
        selectedFileRow.classList.remove('bg-primary/12', 'ring-1', 'ring-primary/35');
      }
      selectedFileRow = row;
      selectedRelPath = relPath;
      if (row) row.classList.add('bg-primary/12', 'ring-1', 'ring-primary/35');
    }
    function appendTreeItems(parentUl, entries, depth) {
      sortEntries(entries).forEach(function (e) {
        parentUl.appendChild(buildTreeLi(e, depth));
      });
    }
    function setBulkSelect(on) {
      bulkSelectActive = !!on;
      container.querySelectorAll('[data-fb-delete-cb-wrap]').forEach(function (w) {
        w.classList.toggle('hidden', !bulkSelectActive);
      });
      var dt = q(container, 'delete-toolbar');
      if (dt) {
        if (bulkSelectActive) {
          dt.classList.remove('hidden');
          dt.classList.add('flex');
        } else {
          dt.classList.add('hidden');
          dt.classList.remove('flex');
        }
      }
      var saw = document.getElementById('fb-select-all-wrap');
      if (saw) {
        if (bulkSelectActive) {
          saw.classList.remove('hidden');
          saw.classList.add('inline-flex');
        } else {
          saw.classList.add('hidden');
          saw.classList.remove('inline-flex');
        }
      }
      if (!bulkSelectActive) {
        var sacb = document.getElementById('fb-select-all');
        if (sacb) sacb.checked = false;
        container.querySelectorAll('input[data-fb-delete-cb]').forEach(function (cb) {
          cb.checked = false;
        });
      }
      var bulkBtn = document.getElementById('fb-bulk-select-toggle');
      if (bulkBtn) {
        var bulkLabel = bulkBtn.querySelector('[data-fb-bulk-label]');
        if (bulkSelectActive) {
          bulkBtn.classList.add('border-primary/45', 'bg-primary/15', 'text-primary');
          if (bulkLabel) bulkLabel.textContent = 'Cancel';
          bulkBtn.setAttribute('aria-pressed', 'true');
        } else {
          bulkBtn.classList.remove('border-primary/45', 'bg-primary/15', 'text-primary');
          if (bulkLabel) bulkLabel.textContent = 'Bulk select';
          bulkBtn.setAttribute('aria-pressed', 'false');
        }
      }
    }
    function buildTreeLi(e, depth) {
      var li = document.createElement('li');
      li.className = 'select-none';
      li.setAttribute('role', 'treeitem');
      if (e.is_dir) li.setAttribute('aria-expanded', 'false');

      var row = document.createElement('div');
      row.className = 'group flex w-full cursor-pointer items-center gap-1 rounded-lg py-1 pr-1.5 text-left transition-colors hover:bg-muted/30 hover:ring-1 hover:ring-border/30';

      if (deleteEnabled) {
        var cbWrap = document.createElement('span');
        cbWrap.className = 'flex w-7 shrink-0 justify-center';
        cbWrap.setAttribute('data-fb-delete-cb-wrap', '1');
        if (!bulkSelectActive) {
          cbWrap.classList.add('hidden');
        }
        cbWrap.addEventListener('click', function (ev) {
          ev.stopPropagation();
        });
        var cb = document.createElement('input');
        cb.type = 'checkbox';
        cb.className = 'rounded border-border';
        cb.setAttribute('data-fb-delete-cb', '1');
        cb.value = e.rel_path;
        cb.title = 'Select for bulk delete or ZIP';
        cbWrap.appendChild(cb);
        row.appendChild(cbWrap);
      }

      var chevSlot = document.createElement('span');
      chevSlot.className = 'flex w-5 shrink-0 justify-center';
      var chev = document.createElement('span');
      if (e.is_dir) {
        chev.className = 'git-browser-chevron inline-block text-muted-foreground transition-transform duration-150';
        chev.innerHTML =
          '<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7"/></svg>';
        chevSlot.appendChild(chev);
      } else {
        chevSlot.innerHTML = '<span class="inline-block w-3.5"></span>';
      }

      var icon = fileIconWrap(e.is_dir, e.name);
      var name = document.createElement('span');
      name.className = 'min-w-0 flex-1 truncate font-mono text-[11px] text-foreground sm:text-xs';
      name.textContent = e.name + (e.is_dir ? '' : '');

      row.appendChild(chevSlot);
      row.appendChild(icon);
      row.appendChild(name);

      if (!e.is_dir) {
        var sz = document.createElement('span');
        sz.className = 'w-14 shrink-0 text-right font-mono text-[10px] text-muted-foreground/90 sm:text-[11px]';
        sz.textContent = formatBytes(e.size);
        row.appendChild(sz);
      }

      // Action buttons for non-git (modal) mode
      if (filePreviewModal && !e.is_dir) {
        var actions = document.createElement('span');
        actions.className = 'fb-actions flex shrink-0 items-center gap-0.5';
        actions.style.zIndex = '10';
        actions.style.position = 'relative';
        actions.addEventListener('click', function (ev) { ev.stopPropagation(); });

        // View/Edit button
        var viewBtn = document.createElement('button');
        viewBtn.type = 'button';
        viewBtn.className = 'p-1 rounded text-muted-foreground hover:text-primary transition-colors';
        viewBtn.title = 'View / Edit file';
        viewBtn.innerHTML = '<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z"/><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/></svg>';
        viewBtn.addEventListener('click', function (ev) { ev.stopPropagation(); openFileModal(e.rel_path, e.size); });
        actions.appendChild(viewBtn);

        // Download single file button
        var dlBtn = document.createElement('a');
        dlBtn.className = 'p-1 rounded text-muted-foreground hover:text-accent transition-colors';
        dlBtn.title = 'Download file';
        dlBtn.href = '/apps/' + appId + '/file?path=' + encodeURIComponent(e.rel_path) + '&download=1';
        dlBtn.setAttribute('download', e.name || 'file');
        dlBtn.innerHTML = '<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5M16.5 12L12 16.5m0 0L7.5 12m4.5 4.5V3"/></svg>';
        actions.appendChild(dlBtn);

        // Delete button
        if (deleteEnabled) {
          var delBtn = document.createElement('button');
          delBtn.type = 'button';
          delBtn.className = 'btn-delete-icon';
          delBtn.title = 'Delete';
          delBtn.innerHTML = '<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>';
          delBtn.addEventListener('click', function (ev) {
            ev.stopPropagation();
            if (confirm('Delete "' + e.rel_path + '"? This cannot be undone.')) {
              var fd = new FormData();
              fd.append('path', '');
              fd.append('paths', e.rel_path);
              fetch(deleteUrl + (deleteUrl.indexOf('?') >= 0 ? '&' : '?') + 'format=json', { method: 'POST', body: fd })
                .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
                .then(function (x) {
                  if (x.ok && x.j.ok) { loadRoot(); }
                  else { setFlash(x.j.message || 'Delete failed.', true); }
                })
                .catch(function () { setFlash('Network error while deleting.', true); });
            }
          });
          actions.appendChild(delBtn);
        }

        row.appendChild(actions);
      }

      var childUl = document.createElement('ul');
      childUl.className = 'git-browser-children m-0 mt-0.5 hidden list-none space-y-0.5 border-l border-border/35 pl-2';

      if (e.is_dir) {
        li.dataset.dirPath = e.rel_path;
        li.dataset.loaded = '0';
        row.addEventListener('click', function (ev) {
          ev.stopPropagation();
          if (!filePreviewModal) clearPreview();
          toggleFolder(li);
        });
      } else {
        row.addEventListener('click', function (ev) {
          ev.stopPropagation();
          if (filePreviewModal) {
            openFileModal(e.rel_path, e.size);
          } else {
            selectFileRow(row, e.rel_path);
            loadBlob(e.rel_path);
          }
        });
      }

      li.appendChild(row);
      li.appendChild(childUl);
      return li;
    }
    function treeUrl(dirPath) {
      return treeBase + '?path=' + encodeURIComponent(dirPath || '');
    }
    function blobUrl(relPath) {
      return blobBase + '?path=' + encodeURIComponent(relPath);
    }
    function toggleFolder(li) {
      var childUl = li.querySelector(':scope > ul.git-browser-children');
      var chev = li.querySelector('.git-browser-chevron');
      if (!childUl) return;

      if (li.dataset.loaded === '1') {
        if (childUl.classList.contains('hidden')) {
          childUl.classList.remove('hidden');
          if (chev) chev.classList.add('rotate-90');
          li.setAttribute('aria-expanded', 'true');
        } else {
          childUl.classList.add('hidden');
          if (chev) chev.classList.remove('rotate-90');
          li.setAttribute('aria-expanded', 'false');
        }
        return;
      }

      var dirPath = li.dataset.dirPath || '';
      setLoading(true);
      showTreeError('');
      fetch(treeUrl(dirPath))
        .then(function (r) {
          return r.json().then(function (j) {
            return { ok: r.ok, j: j };
          });
        })
        .then(function (x) {
          if (!x.ok) {
            showTreeError(x.j.error || 'Could not load folder');
            return;
          }
          var entries = x.j.entries || [];
          childUl.innerHTML = '';
          if (!entries.length) {
            var emptyLi = document.createElement('li');
            emptyLi.className = 'py-1 pl-7 text-[10px] text-muted-foreground';
            emptyLi.textContent = 'Empty folder';
            childUl.appendChild(emptyLi);
          } else {
            appendTreeItems(childUl, entries, 1);
          }
          li.dataset.loaded = '1';
          childUl.classList.remove('hidden');
          if (chev) chev.classList.add('rotate-90');
          li.setAttribute('aria-expanded', 'true');
        })
        .catch(function () {
          showTreeError('Network error while loading tree');
        })
        .finally(function () {
          setLoading(false);
        });
    }
    function loadRoot() {
      showTreeError('');
      treeRoot.innerHTML = '';
      clearPreview();
      setLoading(true);
      fetch(treeUrl(''))
        .then(function (r) {
          return r.json().then(function (j) {
            return { ok: r.ok, j: j };
          });
        })
        .then(function (x) {
          if (!x.ok) {
            showTreeError(x.j.error || 'Could not load directory');
            return;
          }
          var entries = x.j.entries || [];
          if (!entries.length) {
            var emptyLi = document.createElement('li');
            emptyLi.className = 'list-none px-2 py-6 text-center text-xs text-muted-foreground';
            emptyLi.textContent = emptyRootMsg;
            treeRoot.appendChild(emptyLi);
            return;
          }
          appendTreeItems(treeRoot, entries, 0);
        })
        .catch(function () {
          showTreeError('Network error while loading tree');
        })
        .finally(function () {
          setLoading(false);
        });
    }
    function collapseAll() {
      treeRoot.querySelectorAll('li[data-loaded="1"]').forEach(function (li) {
        var ul = li.querySelector(':scope > ul.git-browser-children');
        var chev = li.querySelector('.git-browser-chevron');
        if (ul) ul.classList.add('hidden');
        if (chev) chev.classList.remove('rotate-90');
        li.setAttribute('aria-expanded', 'false');
      });
      clearPreview();
    }
    function loadBlob(relPath) {
      var title = q(container, 'preview-title');
      var meta = q(container, 'preview-meta');
      if (title) title.textContent = relPath;
      if (meta) meta.textContent = 'Loading…';
      setPreviewActions(false, '');
      setPreviewMode('empty');
      var empty = q(container, 'preview-empty');
      if (empty) {
        empty.classList.remove('hidden');
        empty.textContent = 'Loading file…';
      }
      fetch(blobUrl(relPath))
        .then(function (r) {
          return r.json().then(function (j) {
            return { ok: r.ok, j: j };
          });
        })
        .then(function (x) {
          if (empty) empty.textContent = 'Select a file in the explorer tree';
          if (!x.ok) {
            showFallbackMessage(x.j.error || 'Failed to load file', '', '');
            return;
          }
          var d = x.j;
          if (d.too_large) {
            showFallbackMessage(
              'This file is too large for inline preview. Use Download to fetch it.',
              'Size ' + formatBytes(d.size) + ' · preview limit ' + formatBytes(d.max_bytes || 0),
              d.download_url || d.raw_url
            );
            return;
          }
          if (d.binary) {
            showFallbackMessage('Binary file — not shown inline for safety.', 'Size ' + formatBytes(d.size), d.download_url || d.raw_url);
            return;
          }
          var text = d.text != null ? d.text : '';
          if (meta) meta.textContent = d.size ? formatBytes(d.size) : '';
          setPreviewActions(true, d.download_url || d.raw_url);
          showInMonaco(relPath, text);
        })
        .catch(function () {
          if (empty) empty.textContent = 'Select a file in the explorer tree';
          showFallbackMessage('Network error', '', '');
        });
    }

    // openFileModal: file view + inline edit + save
    function openFileModal(relPath, size) {
      var title = relPath.split('/').pop() || relPath;
      var saveUrl = '/apps/' + appId + '/files/save?path=' + encodeURIComponent(relPath);

      var overlay = document.createElement('div');
      overlay.className = 'modal-overlay';
      overlay.onclick = function (ev) { if (ev.target === overlay) overlay.remove(); };
      overlay.innerHTML =
        '<div class="modal-box modal-file-editor max-w-5xl w-full" onclick="event.stopPropagation()">' +
          '<div class="modal-file-editor-toolbar sticky top-0 z-20 -mx-6 -mt-6 mb-3 border-b border-border/50 bg-card px-6 pt-6 pb-3">' +
          '<div class="flex items-center justify-between gap-3 flex-wrap">' +
            '<div class="min-w-0">' +
              '<h3 class="text-base font-semibold text-foreground">' + escapeHtml(title) + '</h3>' +
              '<p class="mt-0.5 text-xs text-muted-foreground font-mono">' + escapeHtml(relPath) + '</p>' +
            '</div>' +
            '<div class="flex items-center gap-2 shrink-0">' +
              '<span class="modal-status text-xs text-muted-foreground"></span>' +
              '<button type="button" class="btn-ghost px-2 py-1 text-xs modal-edit-btn hidden">Edit</button>' +
              '<button type="button" class="btn-primary px-3 py-1 text-xs modal-save-btn hidden">Save</button>' +
              '<button type="button" class="shrink-0 text-muted-foreground hover:text-foreground transition-colors modal-close-btn">' +
                '<svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/></svg>' +
              '</button>' +
            '</div>' +
          '</div>' +
          '</div>' +
          '<div class="modal-preview-stage relative min-h-[min(60vh,480px)] overflow-hidden rounded-xl border border-border/40 bg-[#04060a] ring-1 ring-border/40">' +
            '<div class="modal-preview-empty absolute inset-0 z-[1] flex items-center justify-center px-4 text-center text-xs text-muted-foreground">Loading…</div>' +
            '<pre class="modal-preview-body absolute inset-0 z-[2] hidden overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-[12px] leading-relaxed text-foreground/90" tabindex="0"></pre>' +
            '<div class="modal-preview-monaco absolute inset-0 z-[2] hidden min-h-0 w-full"></div>' +
          '</div>' +
        '</div>';

      overlay.querySelector('.modal-close-btn').addEventListener('click', function () { overlay.remove(); });
      document.body.appendChild(overlay);

      var emptyEl = overlay.querySelector('.modal-preview-empty');
      var bodyEl = overlay.querySelector('.modal-preview-body');
      var monacoHost = overlay.querySelector('.modal-preview-monaco');
      var statusEl = overlay.querySelector('.modal-status');
      var editBtn = overlay.querySelector('.modal-edit-btn');
      var saveBtn = overlay.querySelector('.modal-save-btn');
      var modalEditor = null;
      var isEditing = false;
      var originalContent = '';

      function setMode(mode) {
        if (emptyEl) emptyEl.style.display = mode === 'empty' ? 'flex' : 'none';
        if (bodyEl) bodyEl.style.display = mode === 'fallback' ? 'block' : 'none';
        if (monacoHost) monacoHost.style.display = mode === 'monaco' ? 'block' : 'none';
      }

      function enableEdit() {
        if (!modalEditor) return;
        isEditing = true;
        modalEditor.updateOptions({ readOnly: false });
        modalEditor.focus();
        editBtn.classList.add('hidden');
        saveBtn.classList.remove('hidden');
        statusEl.textContent = 'Editing…';
      }

      function saveFile() {
        if (!modalEditor) return;
        var content = modalEditor.getValue();
        statusEl.textContent = 'Saving…';
        saveBtn.disabled = true;
        fetch(saveUrl, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content: content })
        })
          .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
          .then(function (x) {
            if (x.j.ok) {
              originalContent = content;
              isEditing = false;
              modalEditor.updateOptions({ readOnly: true });
              editBtn.classList.remove('hidden');
              saveBtn.classList.add('hidden');
              saveBtn.disabled = false;
              statusEl.textContent = 'Saved \u2714';
              setTimeout(function () { statusEl.textContent = ''; }, 3000);
              // Reload tree in background to update sizes
              loadRoot();
            } else {
              statusEl.textContent = x.j.message || 'Save failed';
              statusEl.style.color = 'var(--tw-color-rose-400, #f87171)';
              saveBtn.disabled = false;
            }
          })
          .catch(function () {
            statusEl.textContent = 'Network error';
            saveBtn.disabled = false;
          });
      }

      if (editBtn) editBtn.addEventListener('click', enableEdit);
      if (saveBtn) saveBtn.addEventListener('click', saveFile);

      fetch(blobUrl(relPath))
        .then(function (r) {
          return r.json().then(function (j) {
            return { ok: r.ok, j: j };
          });
        })
        .then(function (x) {
          if (!x.ok) {
            setMode('fallback');
            bodyEl.textContent = x.j.error || 'Failed to load file';
            bodyEl.className = 'modal-preview-body absolute inset-0 z-[2] flex items-center justify-center overflow-auto p-4 text-sm text-rose-300';
            return;
          }
          var d = x.j;
          if (d.too_large) {
            setMode('fallback');
            bodyEl.textContent = 'This file is too large to open in the editor.';
            bodyEl.className = 'modal-preview-body absolute inset-0 z-[2] flex items-center justify-center overflow-auto p-4 text-sm text-amber-300';
            return;
          }
          if (d.binary) {
            setMode('fallback');
            bodyEl.textContent = 'Binary file \u2014 cannot be edited in the editor.';
            bodyEl.className = 'modal-preview-body absolute inset-0 z-[2] flex items-center justify-center overflow-auto p-4 text-sm text-muted-foreground';
            return;
          }
          var text = d.text != null ? d.text : '';
          originalContent = text;

          function createEditor(ready) {
            if (ready && global.monaco && global.monaco.editor) {
              registerPanelMonacoTheme();
              modalEditor = global.monaco.editor.create(monacoHost, {
                value: text,
                language: monacoLanguageForPath(relPath),
                readOnly: true,
                theme: 'panel-code-dark',
                minimap: { enabled: false },
                fontSize: 12,
                lineNumbers: 'on',
                scrollBeyondLastLine: false,
                wordWrap: 'on',
                padding: { top: 10, bottom: 10 },
                automaticLayout: true,
                renderLineHighlight: 'line',
                scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 }
              });
              applyModelEOL(modalEditor, text);
              if (typeof ResizeObserver !== 'undefined') {
                new ResizeObserver(function () { if (modalEditor) modalEditor.layout(); }).observe(monacoHost.parentElement);
              }
              setMode('monaco');
              // Show Edit button only for text files
              editBtn.classList.remove('hidden');
            } else {
              setMode('fallback');
              bodyEl.textContent = 'Could not load the code editor. Check your network and refresh the page.';
              bodyEl.className = 'modal-preview-body absolute inset-0 z-[2] flex items-center justify-center overflow-auto p-4 text-sm text-rose-300';
            }
          }

          if (global.monaco && global.monaco.editor) {
            createEditor(true);
          } else {
            ensureMonaco(createEditor);
          }
        })
        .catch(function () {
          setMode('fallback');
          bodyEl.textContent = 'Network error while loading file';
          bodyEl.className = 'modal-preview-body absolute inset-0 z-[2] flex items-center justify-center overflow-auto p-4 text-sm text-rose-300';
        });
    }

    // Keep legacy loadBlobModal as alias
    var loadBlobModal = openFileModal;

    function escapeHtml(s) {
      var div = document.createElement('div');
      div.textContent = s || '';
      return div.innerHTML;
    }

    var collapseBtn = q(container, 'collapse');
    if (collapseBtn) collapseBtn.addEventListener('click', collapseAll);

    var reloadBtn = q(container, 'reload');
    if (reloadBtn) reloadBtn.addEventListener('click', function () {
      loadRoot();
    });

    // Bulk select: checkboxes + delete bar hidden until user enables (workspace)
    var bulkToggleEl = document.getElementById('fb-bulk-select-toggle');
    if (bulkToggleEl && deleteEnabled && filePreviewModal) {
      bulkToggleEl.setAttribute('aria-pressed', 'false');
      bulkToggleEl.addEventListener('click', function () {
        setBulkSelect(!bulkSelectActive);
      });
    }

    // Select all checkbox + Download ZIP button
    var selectAllCb = document.getElementById('fb-select-all');
    if (selectAllCb) {
      selectAllCb.addEventListener('change', function () {
        var checked = selectAllCb.checked;
        container.querySelectorAll('input[data-fb-delete-cb]').forEach(function (cb) {
          cb.checked = checked;
        });
      });
    }

    // Download ZIP button (downloads selected or all files)
    var downloaZipBtn = document.getElementById('fb-download-zip');
    if (downloaZipBtn) {
      downloaZipBtn.addEventListener('click', function () {
        var boxes = container.querySelectorAll('input[data-fb-delete-cb]:checked');
        var zipUrl = '/apps/' + appId + '/files/download-zip';
        if (boxes.length > 0) {
          var paths = [];
          boxes.forEach(function (b) { paths.push(b.value); });
          zipUrl += '?paths=' + paths.map(encodeURIComponent).join(',');
        }
        var a = document.createElement('a');
        a.href = zipUrl;
        a.download = '';
        document.body.appendChild(a);
        a.click();
        a.remove();
      });
    }

    if (refreshBtnId) {
      var rb = document.getElementById(refreshBtnId);
      if (rb) rb.addEventListener('click', function () {
        loadRoot();
      });
    }

    var delBtn = q(container, 'delete-selected');
    if (delBtn && deleteUrl) {
      delBtn.addEventListener('click', function () {
        var boxes = container.querySelectorAll('input[data-fb-delete-cb]:checked');
        if (!boxes.length) {
          setFlash('Select at least one file or folder to delete.', true);
          return;
        }
        if (!global.confirm('Delete selected files and/or folders? This cannot be undone.')) return;
        var fd = new FormData();
        fd.append('path', '');
        boxes.forEach(function (b) {
          fd.append('paths', b.value);
        });
        fetch(deleteUrl + (deleteUrl.indexOf('?') >= 0 ? '&' : '?') + 'format=json', {
          method: 'POST',
          body: fd
        })
          .then(function (r) {
            return r.json().then(function (j) {
              return { ok: r.ok, j: j };
            });
          })
          .then(function (x) {
            var j = x.j || {};
            if (!x.ok) {
              setFlash(j.message || 'Delete failed.', true);
              return;
            }
            if (!j.ok) {
              setFlash(j.message || 'Some deletions failed.', true);
            } else {
              setFlash(j.message || 'Selected items removed.', false);
            }
            loadRoot();
          })
          .catch(function () {
            setFlash('Network error while deleting.', true);
          });
      });
    }

    loadRoot();
  }

  function boot() {
    document.querySelectorAll('[data-panel-file-browser]').forEach(mount);
  }

  global.PanelFileBrowser = { mount: mount, boot: boot };
  boot();
})(window);
