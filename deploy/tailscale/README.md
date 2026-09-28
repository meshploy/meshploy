# Meshploy's mesh binaries

Meshploy runs its mesh on a `tailscaled` of its own - `meshploy-tailscaled.service`, interface `meshploy0`, state in `/var/lib/meshploy/tailscale`, socket in `/run/meshploy-tailscale` - so a machine can keep a Tailscale of its own on `tailscale0`, installed before Meshploy or after it. `meshploy mesh <command>` runs the bundled `tailscale` against it: `meshploy mesh status`, `meshploy mesh ip -4`.

Two stock daemons on one host collide: they share policy-routing table 52 and its rules, the `ts-*` firewall chains, and one of them drops every CGNAT packet that did not arrive on `tailscale0`. `meshploy.patch` gives Meshploy's daemon its own:

| | Stock | Meshploy's |
|---|---|---|
| Route table | 52 | 53 |
| Rule priorities | 5210-5270 | 5310-5370 |
| Firewall chains, nftables tables | `ts-*` | `mp-*` |
| Subnet-route mark | `0x40000` | `0x100000` |
| CGNAT anti-spoofing rule | drop | return |

Its `100.100.100.100` route lives in table 53, which is consulted after the stock daemon's table 52, so a machine's own Tailscale keeps its DNS while it runs and Meshploy's answers when it does not.

One thing the patch cannot change is the stock daemon: every time it starts it puts its firewall hook first, and that hook drops the mesh's traffic. The host agent (`meshploy host serve`) moves Meshploy's `mp-input` hook back in front every two seconds, so a restart of the other Tailscale costs the mesh about a second (`apps/cli/internal/mesh`).

## Files

- `VERSION`: the Tailscale release built.
- `meshploy.patch`: the change, against that release.
- `LICENSE.sha256`: the Tailscale licence accepted. `build.sh` refuses to build when it differs.
- `build.sh <out-dir> [goarch]`: clones the release, checks the licence, applies the patch, builds `meshploy-tailscaled-linux-<arch>` and `meshploy-tailscale-linux-<arch>`.

The CLI workflow builds both architectures and publishes them with each CLI release, in its `SHA256SUMS`. `meshploy mesh install` fetches them from the release the CLI came from.

## New Tailscale releases

`.github/workflows/tailscale-bump.yml` checks weekly. When a newer stable release builds with the patch and the licence is unchanged, it opens a pull request moving `VERSION`; when not, an issue says why. Merge only after:

1. Meshploy's pinned Headscale supports that client version.
2. The coexistence test passes: a machine with a stock Tailscale on one tailnet and Meshploy's mesh on another, both reachable in both directions, across restarts of each daemon and a reboot, with the stock tailnet's MagicDNS working.
3. The release notes say nothing that touches routing, netfilter or DNS in a way the patch relies on.
