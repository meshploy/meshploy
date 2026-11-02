import { create } from "zustand"
import { persist } from "zustand/middleware"

export type ThemePreference = "system" | "light" | "dark"

interface ThemeStore {
  theme: ThemePreference
  setTheme: (theme: ThemePreference) => void
}

// Stored under the key index.html reads before the first paint, so the page
// opens in the chosen theme rather than switching after it loads.
export const useThemeStore = create<ThemeStore>()(
  persist(
    (set) => ({
      theme: "dark",
      setTheme: (theme) => set({ theme }),
    }),
    { name: "meshploy-theme" }
  )
)
