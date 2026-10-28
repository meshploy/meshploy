; Private mesh zone for internal.example.com - generated,
; do not edit. Served only on the WireGuard interface.
;
; No per-application records are written here. The wildcard catches every
; *.internal.example.com and sends it to the gateway; from
; there Caddy hands it to the proxy, which reads the Host header to find the
; target node and port. Routes are entirely dynamic - creating one changes no
; DNS and restarts nothing.

$TTL 300
$ORIGIN internal.example.com.

@   IN SOA ns1.example.com. admin.example.com. (
        2026040201  ; serial
        3600        ; refresh
        900         ; retry
        604800      ; expire
        300         ; minimum TTL
    )

@   IN NS  ns1.example.com.

; Gateway - the single entry point for all mesh traffic
@   IN A   100.64.0.1

; Wildcard - every *.internal.example.com → gateway
*   IN A   100.64.0.1
