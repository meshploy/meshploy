import { StreamLanguage } from "@codemirror/language"
import { yaml } from "@codemirror/legacy-modes/mode/yaml"
import { json } from "@codemirror/legacy-modes/mode/javascript"
import { properties } from "@codemirror/legacy-modes/mode/properties"
import { toml } from "@codemirror/legacy-modes/mode/toml"
import { nginx } from "@codemirror/legacy-modes/mode/nginx"
import { shell } from "@codemirror/legacy-modes/mode/shell"
import type { Extension } from "@codemirror/state"

// Highlighting for a config file, chosen from the path the user typed.
//
// The extension is all we have to go on: a config file has no declared type,
// and asking for one would be a field that exists only to feed the editor.
// An unrecognised extension gets no highlighting rather than a wrong guess --
// htpasswd and hosts files are not any language, and colouring them as one
// makes them harder to read, not easier.
export function configFileLanguage(path: string): Extension[] {
  const name = path.split("/").pop()?.toLowerCase() ?? ""
  const ext = name.includes(".") ? name.slice(name.lastIndexOf(".") + 1) : ""

  switch (ext) {
    case "json":
      return [StreamLanguage.define(json)]
    case "yaml":
    case "yml":
      return [StreamLanguage.define(yaml)]
    case "toml":
      return [StreamLanguage.define(toml)]
    case "ini":
    case "cfg":
    case "properties":
      return [StreamLanguage.define(properties)]
    case "sh":
    case "bash":
      return [StreamLanguage.define(shell)]
    default:
      // nginx.conf, redis.conf and friends are matched by name, not extension.
      if (name.endsWith(".conf")) {
        return name.startsWith("nginx") ? [StreamLanguage.define(nginx)] : [StreamLanguage.define(properties)]
      }
      return []
  }
}
