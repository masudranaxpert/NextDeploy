import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import fs from 'fs';
import path from 'path';
import {defineConfig} from 'vite';

export default defineConfig(() => {
  return {
    plugins: [
      react(),
      tailwindcss(),
      {
        name: 'sync-web-dist-for-go-embed',
        // keep hashed files from older builds for three days (open tabs may still ask for them), then drop them
        writeBundle(_opts: unknown, bundle: Record<string, unknown>) {
          const fresh = new Set(Object.keys(bundle).map((f) => path.basename(f)));
          const cutoff = Date.now() - 3 * 24 * 3600 * 1000;
          for (const dir of ['dist/assets', 'web/dist/assets']) {
            const abs = path.resolve(import.meta.dirname, dir);
            if (!fs.existsSync(abs)) continue;
            for (const f of fs.readdirSync(abs)) {
              const full = path.join(abs, f);
              if (!fresh.has(f) && fs.statSync(full).isFile() && fs.statSync(full).mtimeMs < cutoff) fs.rmSync(full);
            }
          }
        },
        closeBundle() {
          const srcDir = path.resolve(import.meta.dirname, 'dist');
          const destDir = path.resolve(import.meta.dirname, 'web/dist');
          if (fs.existsSync(srcDir)) {
            fs.mkdirSync(destDir, {recursive: true});
            fs.cpSync(srcDir, destDir, {recursive: true});
            fs.rmSync(path.join(destDir, 'assets/aistudio'), {recursive: true, force: true});
          }
        },
      },
    ],
    build: {
      emptyOutDir: false,
      rolldownOptions: {
        output: {
          // libraries change rarely: their own files stay cached in the browser across app updates
          codeSplitting: {
            groups: [
              { name: 'vendor-react', test: /node_modules[\\/](react|react-dom|scheduler|react-is)[\\/]/ },
              { name: 'vendor-ui', test: /node_modules[\\/](sonner|lucide-react|motion|framer-motion|motion-dom|motion-utils)[\\/]/ },
            ],
          },
        },
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(import.meta.dirname, '.'),
      },
    },
    server: {
      // vite's own internals AND the bare root stay local: the root path is where
      // vite's HMR websocket upgrades ("GET /?token=…") land — req.url includes the
      // query, so without the $/? exclusion the upgrade gets proxied to the panel,
      // the client reports "server connection lost", pings the bare root
      // successfully and reloads the page in an endless loop.
      // Everything after the root (API calls, /apps/:id/ws/*) goes to the live panel.
      proxy: process.env.ND_DEV_PROXY
        ? {
            '^\\/(?!(?:$|[?]|(?:@vite|@fs|@id|@react-refresh|__open-in-editor|src|node_modules)\\b))': {
              target: process.env.ND_DEV_PROXY,
              changeOrigin: true,
              ws: true,
              // the panel rejects state-changing requests whose Origin is not the panel host
              headers: { origin: process.env.ND_DEV_PROXY },
              // page navigations stay on vite (HMR); API calls go to the panel
              bypass(req) {
                const h = req.headers;
                if (!h['x-requested-with'] && String(h.accept || '').includes('text/html')) return '/index.html';
              },
            },
          }
        : undefined,
      // HMR is disabled in AI Studio via DISABLE_HMR env var.
      // Do not modify—file watching is disabled to prevent flickering during agent edits.
      hmr: process.env.DISABLE_HMR !== 'true',
      // Disable file watching when DISABLE_HMR is true to save CPU during agent edits.
      watch: process.env.DISABLE_HMR === 'true' ? null : {},
    },
  };
});
