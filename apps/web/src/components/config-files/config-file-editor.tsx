import CodeMirror from "@uiw/react-codemirror"
import { configFileLanguage } from "@/lib/config-file-lang"

/**
 * The editor for a config file's contents.
 *
 * Shared by the new-resource page and the replace-contents dialog so the two
 * cannot drift: the same highlighting, the same gutter, the same font size. A
 * file typed in one place and replaced in the other should look identical.
 *
 * Highlighting follows `path`, so it changes as the user types an extension.
 */
export function ConfigFileEditor({
  value,
  onChange,
  path,
  height = "320px",
  placeholder = '{\n  "http": { "address": "0.0.0.0", "port": "5000" }\n}',
}: {
  value: string
  onChange: (v: string) => void
  path: string
  height?: string
  placeholder?: string
}) {
  return (
    <div className="rounded-md overflow-hidden border border-border/60">
      <CodeMirror
        value={value}
        height={height}
        theme="dark"
        extensions={configFileLanguage(path)}
        onChange={onChange}
        placeholder={placeholder}
        style={{ fontSize: 13 }}
        basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
      />
    </div>
  )
}
