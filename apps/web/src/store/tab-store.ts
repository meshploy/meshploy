"use client"

import { create } from "zustand"

// ─── Types ────────────────────────────────────────────────────────────────────

export type TabType = "explorer" | "terminal" | "metrics"

export interface ExplorerPayload {
  serviceId: string
  projectId: string
  dbName: string
}

export interface TerminalPayload {
  nodeId: string
  nodeLabel: string
  nodeMeshIP: string
}

export interface MetricsPayload {
  nodeId: string
  nodeLabel: string
}

export type TabPayload = ExplorerPayload | TerminalPayload | MetricsPayload

export interface SessionTab {
  id: string          // unique — serviceId or nodeId works
  type: TabType
  label: string       // display name in the tab bar
  payload: TabPayload
}

interface TabStore {
  tabs: SessionTab[]
  activeTabId: string | null  // null = main tab is active

  openTab: (tab: SessionTab) => void
  closeTab: (id: string) => void
  setActiveTab: (id: string | null) => void
}

// ─── Store ────────────────────────────────────────────────────────────────────

export const useTabStore = create<TabStore>((set, get) => ({
  tabs: [],
  activeTabId: null,

  openTab: (tab) => {
    const existing = get().tabs.find((t) => t.id === tab.id)
    if (existing) {
      // Already open — just focus it.
      set({ activeTabId: tab.id })
      return
    }
    set((s) => ({ tabs: [...s.tabs, tab], activeTabId: tab.id }))
  },

  closeTab: (id) => {
    const { tabs, activeTabId } = get()
    const idx = tabs.findIndex((t) => t.id === id)
    const next = tabs.filter((t) => t.id !== id)
    // If we closed the active tab, activate the one to its left (or main).
    let nextActive = activeTabId
    if (activeTabId === id) {
      nextActive = next[idx - 1]?.id ?? null
    }
    set({ tabs: next, activeTabId: nextActive })
  },

  setActiveTab: (id) => set({ activeTabId: id }),
}))
