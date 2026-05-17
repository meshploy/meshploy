import { create } from "zustand"

export interface RawSample {
  ts: number
  cpuTotal: number
  cpuIdle: number
  cpuCores: number
  memTotal: number
  memAvail: number
  diskTotal: number
  diskAvail: number
  netRx: number
  netTx: number
}

interface MetricsStore {
  history: Record<string, RawSample[]>
  addSample: (nodeId: string, sample: RawSample) => void
}

export const useMetricsStore = create<MetricsStore>((set) => ({
  history: {},
  addSample: (nodeId, sample) =>
    set((state) => ({
      history: {
        ...state.history,
        [nodeId]: [...(state.history[nodeId] ?? []).slice(-199), sample],
      },
    })),
}))
