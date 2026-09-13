import { SectionOutline } from "./section-outline"
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react"

type FormSection = { id: string; title: string }
const SectionContext = createContext<
  ((section: FormSection) => () => void) | null
>(null)

export function useFormSection(id: string, title: string) {
  const register = useContext(SectionContext)
  useEffect(() => register?.({ id, title }), [id, title, register])
}

/** An outline of the actual sections in a long form; short forms stay simple. */
export function FormLayout({ children }: { children: ReactNode }) {
  const [sections, setSections] = useState<FormSection[]>([])
  const register = useCallback((section: FormSection) => {
    setSections((current) => [
      ...current.filter((s) => s.id !== section.id),
      section,
    ])
    return () =>
      setSections((current) => current.filter((s) => s.id !== section.id))
  }, [])
  return (
    <SectionContext value={register}>
      <div
        className={`console-form-layout ${sections.length > 3 ? "with-outline" : ""}`}
      >
        <div className="console-form-body">{children}</div>
        {sections.length > 3 && (
          <aside className="console-form-outline"><SectionOutline sections={sections}/></aside>
        )}
      </div>
    </SectionContext>
  )
}
