import { SectionOutline } from "./section-outline"
import type { ReactNode } from "react"

export function SettingsWorkspace({ sections, children }: { sections: [string, string][]; children: ReactNode }) {
  return <div className="settings-workspace"><div className="settings-workspace-body space-y-6">{children}</div><aside className="settings-workspace-nav"><SectionOutline sections={sections.map(([id,title])=>({id,title}))}/></aside></div>
}
