/**
 * Monaco-based .env editor (CDN: same stack as file-browser.js).
 * Syncs to #env-raw-textarea for form submit; exposes PanelEnvEditor.syncToTextarea / layout.
 */
(function (global) {
  'use strict';

  var MONACO_VER = '0.52.2';
  var monacoBase = 'https://cdn.jsdelivr.net/npm/monaco-editor@0.52.2/min/vs';
  var monacoBaseFallback = 'https://unpkg.com/monaco-editor@0.52.2/min/vs';
  var monacoWorkerAsset = {
    editor: '/editor/editor.worker.js',
    json: '/language/json/json.worker.js',
    css: '/language/css/css.worker.js',
    html: '/language/html/html.worker.js',
    ts: '/language/typescript/ts.worker.js'
  };

  function loadMonaco(cb) {
    if (global.monaco && global.monaco.editor) {
      cb(true);
      return;
    }
    if (global.__panelMonacoPromise) {
      global.__panelMonacoPromise.then(function () {
        cb(!!(global.monaco && global.monaco.editor));
      });
      return;
    }
    
    global.__panelMonacoPromise = new Promise(function (resolve) {
      function tryLoad(base, fallback) {
        global.MonacoEnvironment = {
          getWorkerUrl: function (_workerId, label) {
            var rel = monacoWorkerAsset[label] || monacoWorkerAsset.editor;
            var u = base + rel;
            var body = "importScripts('" + u.replace(/\\/g, '\\\\').replace(/'/g, "\\'") + "');";
            try {
              return URL.createObjectURL(new Blob([body], { type: 'application/javascript' }));
            } catch (e) {
              return u;
            }
          }
        };

        var s = document.createElement('script');
        s.src = base + '/loader.js';
        s.async = true;
        s.onload = function () {
          try {
            require.config({ paths: { vs: base } });
            require(['vs/editor/editor.main'], function () {
              resolve();
            });
          } catch (e) {
            resolve();
          }
        };
        s.onerror = function () {
          document.head.removeChild(s);
          if (fallback) {
            tryLoad(fallback, null);
          } else {
            resolve();
          }
        };
        document.head.appendChild(s);
      }

      tryLoad(monacoBase, monacoBaseFallback);
    });

    global.__panelMonacoPromise.then(function () {
      cb(!!(global.monaco && global.monaco.editor));
    });
  }

  function registerEnvTheme() {
    if (!global.monaco || global.__panelEnvThemeDone) return;
    global.__panelEnvThemeDone = true;
    global.monaco.editor.defineTheme('panel-env-dark', {
      base: 'vs-dark',
      inherit: true,
      rules: [
        { token: 'comment', foreground: '6b7280', fontStyle: 'italic' },
        { token: 'keyword', foreground: 'c084fc' },
        { token: 'key', foreground: '38bdf8' },
        { token: 'delimiter', foreground: '94a3b8' },
        { token: 'string', foreground: 'a5b4fc' }
      ],
      colors: {
        'editor.background': '#0a0d12',
        'editorGutter.background': '#0a0d12',
        'editorLineNumber.foreground': '#64748b',
        'minimap.background': '#0a0d12'
      }
    });
  }

  function registerDotenvLanguage() {
    if (!global.monaco || global.__panelDotenvLangDone) return;
    global.__panelDotenvLangDone = true;
    global.monaco.languages.register({ id: 'dotenv' });
    global.monaco.languages.setMonarchTokensProvider('dotenv', {
      tokenizer: {
        root: [
          [/^\s*#.*$/, 'comment'],
          [/^\s*export\s+/, 'keyword'],
          [/^(\s*)([A-Za-z_][\w]*)(\s*)(=)(.*)$/, ['white', 'key', 'white', 'delimiter', 'string']]
        ]
      }
    });
  }

  function boot() {
    var host = document.getElementById('env-monaco-host');
    var ta = document.getElementById('env-raw-textarea');
    var form = document.getElementById('env-save-form');
    if (!host || !ta) return;

    loadMonaco(function (ok) {
      if (!ok || !global.monaco || !global.monaco.editor) {
        ta.classList.remove('hidden');
        return;
      }

      registerEnvTheme();
      registerDotenvLanguage();

      var editor = global.monaco.editor.create(host, {
        value: ta.value || '',
        language: 'dotenv',
        theme: 'panel-env-dark',
        fontSize: 15,
        lineHeight: 24,
        minimap: { enabled: false },
        scrollBeyondLastLine: false,
        wordWrap: 'on',
        wrappingIndent: 'same',
        padding: { top: 14, bottom: 14 },
        automaticLayout: true,
        tabSize: 4,
        insertSpaces: true,
        renderLineHighlight: 'line',
        scrollbar: { verticalScrollbarSize: 11, horizontalScrollbarSize: 11 },
        overviewRulerLanes: 0,
        hideCursorInOverviewRuler: true,
        folding: true,
        glyphMargin: false
      });

      host.classList.remove('hidden');
      ta.classList.add('hidden');

      function syncToTextarea() {
        ta.value = editor.getValue();
      }

      editor.onDidChangeModelContent(function () {
        syncToTextarea();
      });

      if (form) {
        form.addEventListener('submit', syncToTextarea);
      }

      global.PanelEnvEditor = {
        syncToTextarea: syncToTextarea,
        layout: function () {
          requestAnimationFrame(function () {
            requestAnimationFrame(function () {
              editor.layout();
            });
          });
        },
        getValue: function () {
          return editor.getValue();
        }
      };

      if (typeof ResizeObserver !== 'undefined') {
        new ResizeObserver(function () {
          editor.layout();
        }).observe(host);
      }
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }
})(window);
