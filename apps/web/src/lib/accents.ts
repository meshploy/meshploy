export interface AccentColor {
  id: string
  label: string
  // oklch value used in dark mode — also the swatch color
  value: string
}

export interface AccentGroup {
  label: string
  colors: AccentColor[]
}

export const ACCENT_GROUPS: AccentGroup[] = [
  {
    label: "Greens",
    colors: [
      { id: "emerald",    label: "Emerald",    value: "oklch(0.72 0.17 160)" },
      { id: "mint",       label: "Mint",       value: "oklch(0.74 0.16 175)" },
      { id: "sage",       label: "Sage",       value: "oklch(0.72 0.10 155)" },
      { id: "lime",       label: "Lime",       value: "oklch(0.78 0.20 130)" },
      { id: "acid",       label: "Acid",       value: "oklch(0.82 0.22 120)" },
    ],
  },
  {
    label: "Blues",
    colors: [
      { id: "cyan",       label: "Cyan",       value: "oklch(0.76 0.15 200)" },
      { id: "teal",       label: "Teal",       value: "oklch(0.72 0.14 185)" },
      { id: "sky",        label: "Sky",        value: "oklch(0.72 0.15 220)" },
      { id: "cobalt",     label: "Cobalt",     value: "oklch(0.68 0.17 245)" },
    ],
  },
  {
    label: "Violets",
    colors: [
      { id: "periwinkle", label: "Periwinkle", value: "oklch(0.70 0.13 270)" },
      { id: "iris",       label: "Iris",       value: "oklch(0.68 0.18 280)" },
      { id: "plum",       label: "Plum",       value: "oklch(0.68 0.16 295)" },
      { id: "magenta",    label: "Magenta",    value: "oklch(0.70 0.20 320)" },
      { id: "rose",       label: "Rose",       value: "oklch(0.72 0.18 345)" },
      { id: "coral",      label: "Coral",      value: "oklch(0.72 0.17 20)"  },
    ],
  },
  {
    label: "Warm",
    colors: [
      { id: "vermilion",  label: "Vermilion",  value: "oklch(0.70 0.20 26)"  },
      { id: "amber",      label: "Amber",      value: "oklch(0.78 0.17 85)"  },
      { id: "mustard",    label: "Mustard",    value: "oklch(0.78 0.15 98)"  },
      { id: "peach",      label: "Peach",      value: "oklch(0.78 0.12 55)"  },
    ],
  },
  {
    label: "Neutral",
    colors: [
      { id: "bone",       label: "Bone",       value: "oklch(0.80 0.03 90)"  },
      { id: "steel",      label: "Steel",      value: "oklch(0.72 0.03 220)" },
    ],
  },
]

export const ACCENTS: AccentColor[] = ACCENT_GROUPS.flatMap((g) => g.colors)

export const DEFAULT_ACCENT_ID = "emerald"

export function getAccent(id: string): AccentColor {
  return ACCENTS.find((a) => a.id === id) ?? ACCENTS[0]
}

export function applyAccent(value: string) {
  const root = document.documentElement
  root.style.setProperty("--primary", value)
  root.style.setProperty("--ring", value)
  root.style.setProperty("--sidebar-primary", value)
  root.style.setProperty("--sidebar-ring", value)
}
