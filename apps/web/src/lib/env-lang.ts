import { StreamLanguage } from "@codemirror/language"
import { EditorView } from "@codemirror/view"

export const envLanguage = StreamLanguage.define<{ inValue: boolean }>({
  startState: () => ({ inValue: false }),
  token(stream, state) {
    // Comment line
    if (stream.sol() && stream.match(/\s*#/)) {
      stream.skipToEnd()
      state.inValue = false
      return "comment"
    }
    // After = sign: consume rest of line as string/value
    if (state.inValue) {
      // Quoted value
      if (stream.match(/^"(?:[^"\\]|\\.)*"/)) return "string"
      if (stream.match(/^'(?:[^'\\]|\\.)*'/)) return "string"
      stream.skipToEnd()
      return "atom"
    }
    // Key (anything before =)
    if (stream.match(/^[A-Za-z_][A-Za-z0-9_]*(?=\s*=)/)) {
      return "variableName"
    }
    // = sign
    if (stream.eat("=")) {
      state.inValue = true
      return "operator"
    }
    stream.next()
    return null
  },
  blankLine(_state) {},
  copyState: (s) => ({ ...s }),
})

export const envTheme = EditorView.theme({
  ".cm-content": { fontFamily: "var(--font-mono, ui-monospace, monospace)", fontSize: "12px" },
  ".tok-variableName": { color: "oklch(0.78 0.12 200)" },   // cyan key
  ".tok-operator":     { color: "oklch(0.75 0 0)" },         // neutral =
  ".tok-string":       { color: "oklch(0.75 0.12 55)" },     // amber quoted value
  ".tok-atom":         { color: "oklch(0.85 0 0)" },         // light unquoted value
  ".tok-comment":      { color: "oklch(0.55 0 0)", fontStyle: "italic" },
})
