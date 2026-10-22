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
        closeBundle() {
          const srcDir = path.resolve(__dirname, 'dist');
          const destDir = path.resolve(__dirname, 'web/dist');
          if (fs.existsSync(srcDir)) {
            fs.rmSync(destDir, {recursive: true, force: true});
            fs.mkdirSync(path.dirname(destDir), {recursive: true});
            fs.cpSync(srcDir, destDir, {recursive: true});
            fs.rmSync(path.join(destDir, 'assets/aistudio'), {recursive: true, force: true});
          }
        },
      },
    ],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, '.'),
      },
    },
    server: {
      // HMR is disabled in AI Studio via DISABLE_HMR env var.
      // Do not modify—file watching is disabled to prevent flickering during agent edits.
      hmr: process.env.DISABLE_HMR !== 'true',
      // Disable file watching when DISABLE_HMR is true to save CPU during agent edits.
      watch: process.env.DISABLE_HMR === 'true' ? null : {},
    },
  };
});
