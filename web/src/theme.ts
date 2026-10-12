// Light by default; dark is an explicit choice stored in localStorage. The
// inline script in index.html applies a stored choice before first paint.
import { useState } from 'react'

export type Theme = 'light' | 'dark'

export const themeKey = 'tracelet.theme'

export function getTheme(): Theme {
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

export function applyTheme(theme: Theme): void {
  document.documentElement.classList.toggle('dark', theme === 'dark')
}

export function setTheme(theme: Theme): void {
  applyTheme(theme)
  try {
    localStorage.setItem(themeKey, theme)
  } catch {
    // Storage may be unavailable (private mode); the choice then lasts the page.
  }
}

export function useTheme(): [Theme, (theme: Theme) => void] {
  const [theme, set] = useState<Theme>(getTheme)
  return [
    theme,
    (next: Theme) => {
      setTheme(next)
      set(next)
    },
  ]
}
