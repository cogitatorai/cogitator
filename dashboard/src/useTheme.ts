import { useState, useEffect, useCallback } from 'react';

export type ThemePreference = 'dark' | 'light' | 'system';
export type ColorScheme = 'dark' | 'light';

const mq = window.matchMedia('(prefers-color-scheme: dark)');

function getSystemScheme(): ColorScheme {
  return mq.matches ? 'dark' : 'light';
}

function getInitialPreference(): ThemePreference {
  const stored = localStorage.getItem('theme');
  if (stored === 'light' || stored === 'dark' || stored === 'system') return stored;
  return 'system';
}

export function useTheme(): [ColorScheme, ThemePreference, (t: ThemePreference) => void] {
  const [preference, setPreference] = useState<ThemePreference>(getInitialPreference);
  const [systemScheme, setSystemScheme] = useState<ColorScheme>(getSystemScheme);

  useEffect(() => {
    const handler = (e: MediaQueryListEvent) => setSystemScheme(e.matches ? 'dark' : 'light');
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);

  const scheme: ColorScheme = preference === 'system' ? systemScheme : preference;

  useEffect(() => {
    const root = document.documentElement;
    root.classList.remove('dark', 'light');
    root.classList.add(scheme);
    delete root.dataset.theme;
    localStorage.setItem('theme', preference);
  }, [scheme, preference]);

  const setTheme = useCallback((t: ThemePreference) => setPreference(t), []);

  return [scheme, preference, setTheme];
}
