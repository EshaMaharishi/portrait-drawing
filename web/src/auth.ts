// Credential discovery for a single-machine Viam Application.
//
// Ported from the RDK's `viam module generate` app template (auth.js). In
// order of preference:
//  1. host / api-key-id / api-key cookies (set by `viam module local-app-testing`)
//  2. the per-machine cookie viamapplications.com sets, named by the machine
//     ID in the URL (/machine/<id>/...)
//  3. a cookie saved from the manual connect form (dev convenience)
import { getCookie, setCookie } from 'typescript-cookie'
import type { DialConf } from '@viamrobotics/sdk'

const SAVED_HOST_COOKIE = 'portrait-drawing-host'
export const SIGNALING_ADDRESS = 'https://app.viam.com:443'

export interface Connection {
  machineId: string
  dialConf: DialConf
}

function conf(host: string, id: string, key: string): DialConf {
  return {
    host,
    credentials: { type: 'api-key', authEntity: id, payload: key },
    signalingAddress: SIGNALING_ADDRESS,
  }
}

export function getConnection(): Connection | undefined {
  const host = getCookie('host')
  const id = getCookie('api-key-id')
  const key = getCookie('api-key')
  if (host && id && key) {
    return { machineId: host, dialConf: conf(host, id, key) }
  }

  const parts = window.location.pathname.split('/')
  if (parts.length >= 3 && parts[1] === 'machine') {
    const raw = getCookie(parts[2])
    if (raw) {
      try {
        const parsed = JSON.parse(raw)
        const h = parsed?.hostname
        const kid = parsed?.apiKey?.id
        const k = parsed?.apiKey?.key
        if (h && kid && k) {
          return { machineId: parsed?.machineId ?? parts[2], dialConf: conf(h, kid, k) }
        }
      } catch {
        // malformed cookie; fall through
      }
    }
  }

  const saved = getCookie(SAVED_HOST_COOKIE)
  if (saved) {
    try {
      const { host: h, id: kid, key: k } = JSON.parse(saved)
      if (h && kid && k) {
        return { machineId: h, dialConf: conf(h, kid, k) }
      }
    } catch {
      // malformed cookie; fall through
    }
  }
  return undefined
}

export function saveConnection(host: string, id: string, key: string) {
  setCookie(SAVED_HOST_COOKIE, JSON.stringify({ host, id, key }), { expires: 30 })
}
