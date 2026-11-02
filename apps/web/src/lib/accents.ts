export interface AccentColor {
  id: string
  label: string
  // oklch value used in dark mode - also the swatch color
  value: string
  // Its form in the light theme, and the text on it, when darkening the
  // dark-mode value would not do
  light?: { value: string; foreground: string }
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
    label: "Meshploy",
    colors: [
      { id: "bone",       label: "Bone",       value: "oklch(0.86 0.03 88)",
        light: { value: "oklch(0.32 0.045 165)", foreground: "oklch(0.93 0.025 88)" } },
    ],
  },
  {
    label: "Neutral",
    colors: [
      { id: "steel",      label: "Steel",      value: "oklch(0.72 0.03 220)" },
    ],
  },
]

export const ACCENTS: AccentColor[] = ACCENT_GROUPS.flatMap((g) => g.colors)

export const DEFAULT_ACCENT_ID = "bone"

export function getAccent(id: string): AccentColor {
  return ACCENTS.find((a) => a.id === id) ?? ACCENTS[0]
}

/**
 * Sets the accent for both themes: as given in the dark one (with the text
 * drawn on it, when one is given), and in the light one its own light form,
 * or else the colour darkened to a fixed lightness, so white text on it and
 * links in it stay legible on paper.
 */
export function applyAccent(value: string, foreground?: string, light?: AccentColor["light"]) {
  const root = document.documentElement
  const set = (name: string, v?: string) => (v ? root.style.setProperty(name, v) : root.style.removeProperty(name))
  set("--pick", value)
  set("--pick-fg", foreground)
  set("--pick-light", light?.value ?? `oklch(from ${value} min(l, 0.5) c h)`)
  set("--pick-light-fg", light?.foreground ?? "oklch(0.985 0.004 85)")
}
