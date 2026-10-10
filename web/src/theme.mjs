// SPDX-License-Identifier: Apache-2.0
export const THEME_KEY = 'grillo.theme';
export const themePreference = value => ['system', 'light', 'dark'].includes(value) ? value : 'system';
export const effectiveTheme = (preference, systemDark) => preference === 'dark' || (themePreference(preference) === 'system' && systemDark) ? 'dark' : 'light';
export function readTheme(storage) {
  try { return themePreference((typeof storage === 'function' ? storage() : storage).getItem(THEME_KEY)); } catch { return 'system'; }
}
export function saveTheme(storage, preference) {
  try { (typeof storage === 'function' ? storage() : storage).setItem(THEME_KEY, themePreference(preference)); return true; } catch { return false; }
}
export function applyTheme(document, preference, systemDark) {
  const theme = effectiveTheme(preference, systemDark);
  document.documentElement.classList.toggle('pf-v6-theme-dark', theme === 'dark');
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.themePreference = themePreference(preference);
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#171f25' : '#f0f4f4');
  return theme;
}
