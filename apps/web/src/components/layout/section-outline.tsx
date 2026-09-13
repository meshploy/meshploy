export function SectionOutline({ sections }: { sections: { id: string; title: string }[] }) {
  return <nav className="section-outline" aria-label="On this page"><p>On this page</p>{sections.map(section => <a key={section.id} href={`#${section.id}`} onClick={event => { event.preventDefault(); document.getElementById(section.id)?.scrollIntoView({ behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth", block: "start" }) }}>{section.title}</a>)}</nav>
}
