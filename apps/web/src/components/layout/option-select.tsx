import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select"
export function OptionSelect({ label, value, onChange, options, className }: { label: string; value: string; onChange: (value: string) => void; options: { value: string; label: string }[]; className?: string }) {
  return <Select value={value} onValueChange={v => { if (v !== null) onChange(v) }}><SelectTrigger aria-label={label} className={`console-option-select h-10 ${className ?? ""}`}><SelectValue>{options.find(option => option.value === value)?.label ?? label}</SelectValue></SelectTrigger><SelectContent>{options.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent></Select>
}
