; Public zone for example.com - generated, do not edit.
; Rendered by `meshploy domain apply`; edits are lost when it next runs.
;
; CoreDNS reloads this file every 5s, so no restart is needed after a change.

$TTL 300
$ORIGIN example.com.

@   IN SOA ns1.example.com. admin.example.com. (
        2026040201  ; serial
        3600        ; refresh
        900         ; retry
        604800      ; expire
        300         ; minimum TTL
    )

; Nameserver - this server is authoritative
@       IN NS   ns1.example.com.

; Root of example.com → gateway public IP
@       IN A    203.0.113.10

; Named subdomains
headscale   IN A    203.0.113.10
api         IN A    203.0.113.10
console     IN A    203.0.113.10

; Wildcard - every other *.example.com → gateway, routed by the proxy
*           IN A    203.0.113.10

; ── ACME challenge delegation ────────────────────────────────────────────────
; Delegates _acme-challenge.example.com here so CoreDNS can serve the TXT
; records for wildcard DNS-01 verification.
_acme-challenge IN NS ns1.example.com.

; Named subdomain challenges - CNAME to the central challenge zone. Recursive
; resolvers follow the CNAME and Let's Encrypt verifies the TXT at the target.
_acme-challenge.headscale   IN CNAME  _acme-challenge.example.com.
_acme-challenge.api         IN CNAME  _acme-challenge.example.com.
_acme-challenge.console     IN CNAME  _acme-challenge.example.com.

; Internal wildcard challenge - NS delegation rather than CNAME, so CertMagic
; can query the authoritative zone directly.
_acme-challenge.internal    IN NS     ns1.example.com.

; ── Internal blackhole, on the public internet only ──────────────────────────
; Stops the public wildcard from resolving internal names. Mesh clients never
; see this: they query the private zone on the mesh address.
internal    IN A    127.0.0.1
*.internal  IN A    127.0.0.1
