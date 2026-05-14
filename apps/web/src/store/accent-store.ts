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
    { name: "meshploy-accent" }
  )
)
