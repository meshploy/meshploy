/**
 * DNS providers ask for a record's name relative to the zone it is added in:
 * `_meshploy-verify` for `_meshploy-verify.example.com`, `@` or blank for
 * `example.com` itself. Pasting the full name into most of them doubles it
 * (`_meshploy-verify.example.com.example.com`), so the console offers the
 * relative form and shows the full one beside it.
 *
 * The zone is taken to be the registrable domain: the last two labels, or
 * three under a second-level suffix such as `co.uk`. That is where almost
 * every name is added; a reader whose zone is a delegated subdomain still has
 * the full name to go by.
 */

// Second-level suffixes under which a registrable domain has three labels.
// The common ones, not the whole public suffix list.
const SECOND_LEVEL = new Set([
  "ac", "co", "com", "edu", "gen", "gov", "ltd", "me", "net", "nic", "org", "plc", "res", "sch",
])

export function zoneOf(host: string): string {
  const labels = host.replace(/\.$/, "").toLowerCase().split(".")
  if (labels.length <= 2) return labels.join(".")
  const [sld, tld] = labels.slice(-2)
  const take = tld.length === 2 && SECOND_LEVEL.has(sld) ? 3 : 2
  return labels.slice(-take).join(".")
}

/** The name as it goes into the zone's own record list: "@" at its root. */
export function relativeName(name: string, zone: string): string {
  const n = name.replace(/\.$/, "").toLowerCase()
  const z = zone.toLowerCase()
  if (n === z) return "@"
  return n.endsWith("." + z) ? n.slice(0, -(z.length + 1)) : n
}
