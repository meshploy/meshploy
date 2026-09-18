import { SectionOutline } from "./section-outline"
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
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
  // Sections register from an effect, so they arrive in mount order - and a
  // section that appears later, or re-registers when its title changes, lands
  // at the end. An outline is a map of the page, so it is ordered by where the
  // sections actually are on it.
  const ordered = useMemo(() => {
    return [...sections].sort((a, b) => {
      const first = document.getElementById(a.id)
      const second = document.getElementById(b.id)
      if (!first || !second) return 0
      return first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1
    })
  }, [sections])

  return (
    <SectionContext value={register}>
      <div
        className={`console-form-layout ${sections.length > 3 ? "with-outline" : ""}`}
      >
        <div className="console-form-body">{children}</div>
        {sections.length > 3 && (
          <aside className="console-form-outline"><SectionOutline sections={ordered}/></aside>
        )}
      </div>
    </SectionContext>
  )
}
