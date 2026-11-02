import { StreamLanguage } from "@codemirror/language"
import { EditorView } from "@codemirror/view"

export const envLanguage = StreamLanguage.define<{ inValue: boolean }>({
  startState: () => ({ inValue: false }),
  token(stream, state) {
    // Reset state at the start of every new line
    if (stream.sol()) {
      state.inValue = false
    }
    // Comment line
    if (stream.sol() && stream.match(/\s*#/)) {
      stream.skipToEnd()
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
  blankLine(state) { state.inValue = false },
  copyState: (s) => ({ ...s }),
  // "#" starts a comment, which is what Ctrl/Cmd+/ toggles on the selected
  // lines; without it the editor's toggle has nothing to insert.
  languageData: { commentTokens: { line: "#" } },
})

export const envTheme = EditorView.theme({
  ".cm-content": { fontFamily: "var(--font-mono, ui-monospace, monospace)", fontSize: "12px" },
  ".tok-variableName": { color: "var(--code-key)" },
  ".tok-operator":     { color: "var(--code-op)" },
  ".tok-string":       { color: "var(--code-string)" },
  ".tok-atom":         { color: "var(--code-value)" },
  ".tok-comment":      { color: "var(--code-comment)", fontStyle: "italic" },
})
