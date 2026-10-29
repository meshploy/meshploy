; Private mesh zone for internal.acme.eu.example.com - generated,
; do not edit. Served only on the WireGuard interface.
;
; No per-application records are written here. The wildcard catches every
; *.internal.acme.eu.example.com and sends it to the gateway; from
; there Caddy hands it to the proxy, which reads the Host header to find the
; target node and port. Routes are entirely dynamic - creating one changes no
; DNS and restarts nothing.

$TTL 300
$ORIGIN internal.acme.eu.example.com.

@   IN SOA ns1.acme.eu.example.com. admin.acme.eu.example.com. (
        2026040201  ; serial
        3600        ; refresh
        900         ; retry
        604800      ; expire
        300         ; minimum TTL
    )

@   IN NS  ns1.acme.eu.example.com.

; Gateway - the single entry point for all mesh traffic
@   IN A   100.64.0.1

; Wildcard - every *.internal.acme.eu.example.com → gateway
*   IN A   100.64.0.1
