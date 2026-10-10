// SPDX-License-Identifier: Apache-2.0
import React, { useEffect, useState } from 'react';
import { FormSelect, FormSelectOption } from '@patternfly/react-core';
import { THEME_KEY, readTheme, saveTheme, applyTheme, themePreference } from './theme.mjs';
const storage = () => window.localStorage;
export function ThemePicker() {
  const [preference, setPreference] = useState(() => readTheme(storage));
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches);
  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const changed = event => setSystemDark(event.matches);
    const persisted = event => { if (event.key === THEME_KEY || event.key === null) setPreference(readTheme(storage)); };
    media.addEventListener('change', changed); window.addEventListener('storage', persisted);
    // Read again after subscribing to cover changes since the initial render.
    setSystemDark(media.matches); setPreference(readTheme(storage));
    return () => { media.removeEventListener('change', changed); window.removeEventListener('storage', persisted); };
  }, []);
  useEffect(() => { applyTheme(document, preference, systemDark); }, [preference, systemDark]);
  return <div className="gr-theme-picker"><label htmlFor="theme-preference">Theme</label><FormSelect id="theme-preference" aria-label="Color theme" value={preference} onChange={(_, value) => { const next = themePreference(value); saveTheme(storage, next); setPreference(next); }}><FormSelectOption value="system" label="System" /><FormSelectOption value="light" label="Light" /><FormSelectOption value="dark" label="Dark" /></FormSelect></div>;
}
