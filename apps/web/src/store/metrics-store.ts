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

/** A reading a page took itself, in the order of the charts it ends. */
export interface TailPoint {
  t: number
  values: (number | null)[]
}

interface MetricsStore {
  history: Record<string, RawSample[]>
  addSample: (nodeId: string, sample: RawSample) => void
  /** Readings taken while a page was open, by what they are of: the end of its charts. */
  tails: Record<string, TailPoint[]>
  addTailPoint: (key: string, point: TailPoint) => void
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
  tails: {},
  addTailPoint: (key, point) =>
    set((state) => {
      // The same reading seen twice, as when a second query settles, replaces
      // itself rather than adding a point.
      const tail = (state.tails[key] ?? []).filter((p) => p.t !== point.t)
      return { tails: { ...state.tails, [key]: [...tail.slice(-199), point] } }
    }),
}))
