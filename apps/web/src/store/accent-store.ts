import { create } from "zustand"
import { persist } from "zustand/middleware"
import { DEFAULT_ACCENT_ID } from "@/lib/accents"

interface AccentStore {
  accentId: string
  setAccent: (id: string) => void
}

export const useAccentStore = create<AccentStore>()(
  persist(
    (set) => ({
      accentId: DEFAULT_ACCENT_ID,
      setAccent: (id) => set({ accentId: id }),
    }),
    {
      name: "meshploy-accent",
      // Emerald was the default before bone; a browser still on it is moved
      // to the new default.
      version: 1,
      migrate: (state, version) => {
        const s = state as AccentStore
        return version < 1 && s.accentId === "emerald" ? { ...s, accentId: DEFAULT_ACCENT_ID } : s
      },
    }
  )
)
