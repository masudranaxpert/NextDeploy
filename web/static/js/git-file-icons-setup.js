/**
 * file-icons/atom port for the panel file browser (Git + Files tabs).
 * https://github.com/csandman/atom-file-icons (from https://github.com/file-icons/atom)
 */
import { getIconClass } from '/static/vendor/atom-file-icons/index.js';

window.__gitGetIconClass = function (name, isDir) {
  var cls = getIconClass(name, { colorMode: 'dark', isDir: !!isDir });
  return cls ? 'icon ' + cls : 'icon default-icon medium-grey';
};
window.dispatchEvent(new Event('gitfileiconsready'));
