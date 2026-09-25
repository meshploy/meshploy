import { create } from "zustand"

/**
 * The help drawer: whether it is open, on which topic and section, and
 * whether it is pinned there. Unpinned, it follows the page.
 */
interface HelpState {
  open: boolean
  topic?: string
  section?: string
  pinned: boolean
  show: (topic?: string, section?: string) => void
  close: () => void
  setPinned: (pinned: boolean) => void
}

export const useHelp = create<HelpState>((set) => ({
  open: false,
  pinned: false,
  show: (topic, section) => set({ open: true, topic, section }),
  close: () => set({ open: false, section: undefined }),
  setPinned: (pinned) => set({ pinned }),
}))
