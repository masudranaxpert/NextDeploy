import {createRoot} from 'react-dom/client';
import App from './App.tsx';
import './index.css';
import { reloadForNewVersion } from './lib/lazyRetry';

// Vite fires this when a preloaded piece of the app cannot be fetched (the panel was updated while
// this tab was open). One reload brings the matching files.
window.addEventListener('vite:preloadError', (event) => {
  event.preventDefault();
  reloadForNewVersion();
});

createRoot(document.getElementById('root')!).render(<App />);
