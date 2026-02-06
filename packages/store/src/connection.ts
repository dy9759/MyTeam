export interface ConnectionInfo {
  type: "multica-connect"
  gateway: string
  hubId: string
  agentId: string
  token: string
  expires: number
}

function isConnectionInfo(obj: unknown): obj is ConnectionInfo {
  if (typeof obj !== "object" || obj === null) return false
  const o = obj as Record<string, unknown>
  return (
    o.type === "multica-connect" &&
    typeof o.gateway === "string" &&
    typeof o.hubId === "string" &&
    typeof o.agentId === "string" &&
    typeof o.token === "string" &&
    typeof o.expires === "number"
  )
}

// Parse multica://connect?gateway=...&hub=...&agent=...&token=...&exp=... URL format
// Uses string prefix + URLSearchParams to avoid cross-engine URL hostname differences
function parseConnectionUrl(input: string): ConnectionInfo | null {
  const prefix = "multica://connect?"
  if (!input.startsWith(prefix)) return null
  try {
    const params = new URLSearchParams(input.slice(prefix.length))
    const gateway = params.get("gateway")
    const hubId = params.get("hub")
    const agentId = params.get("agent")
    const token = params.get("token")
    const exp = params.get("exp")
    if (!gateway || !hubId || !agentId || !token || !exp) return null
    return {
      type: "multica-connect",
      gateway,
      hubId,
      agentId,
      token,
      expires: Number(exp),
    }
  } catch {
    return null
  }
}

function isExpired(expires: number): boolean {
  // Desktop generates expires as millisecond timestamp (Date.now() + seconds * 1000)
  return Date.now() > expires
}

export function parseConnectionCode(input: string): ConnectionInfo {
  const trimmed = input.trim()

  // Try multica:// URL format first (desktop "Copy Link" output)
  const fromUrl = parseConnectionUrl(trimmed)
  if (fromUrl) {
    if (isExpired(fromUrl.expires)) {
      throw new Error("Connection code has expired")
    }
    return fromUrl
  }

  // Try JSON (QR code scan output)
  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    // Try base64 decode then JSON
    try {
      parsed = JSON.parse(atob(trimmed))
    } catch {
      throw new Error("Invalid connection code")
    }
  }

  if (!isConnectionInfo(parsed)) {
    throw new Error("Invalid connection code format")
  }

  if (isExpired(parsed.expires)) {
    throw new Error("Connection code has expired")
  }

  return parsed
}
