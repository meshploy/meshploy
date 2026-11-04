import { useQuery } from "@tanstack/react-query"
import { nodes as nodesApi, system as systemApi } from "@/lib/api"

/**
 * The gateway's public address, for the DNS records a domain needs.
 *
 * Read from the gateway's node, which install.sh backfills with PUBLIC_IP:
 * there is no other place the console can learn it. Empty until known, and on a
 * machine that is not a gateway.
 */
export function useGatewayPublicIp(orgId: string, token: string) {
  const { data } = useQuery({
    queryKey: ["gateway-public-ip", orgId],
    queryFn: async () => {
      const list = await nodesApi.list(orgId, token)
      return list.find((n) => n.k3s_role === "server")?.public_ip ?? ""
    },
    enabled: !!orgId && !!token,
    staleTime: 5 * 60_000,
  })
  return data ?? ""
}

/**
 * The gateway's edge name, for a custom hostname's CNAME, and whether it
 * resolves to the gateway yet. Undefined while loading or on an API that
 * predates it, which the caller treats as "no edge name": the A record alone.
 */
export function useGatewayEdge(token: string) {
  const { data } = useQuery({
    queryKey: ["gateway-edge"],
    queryFn: () => systemApi.edge(token),
    enabled: !!token,
    staleTime: 60_000,
    retry: false,
  })
  return data
}
