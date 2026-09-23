; ACME challenge zone for _acme-challenge.internal.example.com - generated, do not edit.
;
; Served on the public interface so Let's Encrypt can reach it during DNS-01
; verification. The Caddy DNS module writes TXT records here before issuance and
; removes them afterwards; nothing should be added by hand.

$TTL 60
$ORIGIN _acme-challenge.internal.example.com.

@   IN SOA ns1.example.com. admin.example.com. (
        2026040201  ; serial - managed during issuance
        3600        ; refresh
        900         ; retry
        604800      ; expire
        60          ; minimum TTL - challenges are short-lived
    )

@   IN NS  ns1.example.com.

; TXT records are written and deleted here automatically during issuance:
;   @   IN TXT  "<challenge token>"
