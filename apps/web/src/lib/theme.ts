import type { ThemePreference } from "@/store/theme-store"

const darkQuery = () => window.matchMedia("(prefers-color-scheme: dark)")

/** Puts the page in the theme a preference names; "system" follows the device. */
export function applyTheme(pref: ThemePreference) {
  const dark = pref === "dark" || (pref === "system" && darkQuery().matches)
  document.documentElement.classList.toggle("dark", dark)
}

/** Re-applies "system" when the device switches; returns the unsubscribe. */
export function followSystemTheme(pref: ThemePreference): () => void {
  if (pref !== "system") return () => {}
  const q = darkQuery()
  const onChange = () => applyTheme("system")
  q.addEventListener("change", onChange)
  return () => q.removeEventListener("change", onChange)
}

/**
 * The theme an embedded widget (CodeMirror, React Flow) should draw in. Read when an editor renders: the
 * theme is chosen in Settings, so an editor on screen is always drawn after it.
 */
export function editorTheme(): "light" | "dark" {
  return document.documentElement.classList.contains("dark") ? "dark" : "light"
}
