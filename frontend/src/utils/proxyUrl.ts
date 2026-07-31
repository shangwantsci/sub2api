import type { ProxyProtocol } from '@/types'

/**
 * 代理连接串解析。
 *
 * 与后端 service.ParseProviderProxyURL 一一对应，两边必须同时改：前端只负责即时
 * 预览，落库以后端解析为准，规则漂移会让供号商看到的预览和实际存的代理不是一回事。
 *
 * 刻意手写而不用 `new URL()`：socks5 之类的非特殊 scheme 在 WHATWG URL 里解析行为
 * 依赖实现，且会对凭据做 percent-decode，把密码里的 % 悄悄改掉。
 */

export interface ParsedProxy {
  protocol: ProxyProtocol
  host: string
  port: number
  username: string
  password: string
}

export interface ParseProxyUrlOptions {
  /**
   * 允许省略协议头的写法：`host:port`、`host:port:user:pass`、`user:pass@host:port`。
   *
   * 默认关闭。管理端批量创建代理保持只认标准 URL —— 那里的代理可能是 http，
   * 默认补一个 socks5h 会把协议猜错。供号商上号页才打开它。
   */
  allowSchemeless?: boolean
  /** 省略协议头时补的协议，默认 socks5h。 */
  defaultProtocol?: ProxyProtocol
  /**
   * 把 socks5 归一成 socks5h。
   *
   * 网关侧 proxyurl.Parse 无论如何都会做这个升级（socks5 会在本机解析 DNS，
   * 目标域名从服务器自己的出口漏出去）。需要预览与落库结果一致时打开。
   */
  upgradeSocks5?: boolean
}

const ALLOWED_PROTOCOLS: readonly string[] = ['http', 'https', 'socks5', 'socks5h']

/** 省略协议头时补的默认协议，与后端 defaultProviderProxyProtocol 一致。 */
export const DEFAULT_PROXY_PROTOCOL: ProxyProtocol = 'socks5h'

/**
 * 把一行代理连接串解析成结构化字段，无法解析时返回 null。
 *
 * IPv6 必须带方括号（`[::1]:1080`）：冒号分隔的格式与 IPv6 语法天然冲突。
 */
export function parseProxyUrl(raw: string, options: ParseProxyUrlOptions = {}): ParsedProxy | null {
  const trimmed = raw.trim()
  if (!trimmed) return null

  if (trimmed.includes('://')) return parseStandardUrl(trimmed, options)
  if (!options.allowSchemeless) return null
  if (trimmed.includes('@')) return parseCredentialForm(trimmed, options)
  return parseColonForm(trimmed, options)
}

/** scheme://[user:pass@]host:port */
function parseStandardUrl(trimmed: string, options: ParseProxyUrlOptions): ParsedProxy | null {
  const schemeEnd = trimmed.indexOf('://')
  const protocol = trimmed.slice(0, schemeEnd).toLowerCase()
  if (!ALLOWED_PROTOCOLS.includes(protocol)) return null

  const rest = trimmed.slice(schemeEnd + 3)
  // 从最后一个 @ 切：代理密码里出现 @ 并不罕见，用第一个会把密码截断成一个
  // 能连上但错误的凭据，表现是上号莫名其妙失败。
  const at = rest.lastIndexOf('@')
  const credentials = at >= 0 ? rest.slice(0, at) : ''
  const hostPort = at >= 0 ? rest.slice(at + 1) : rest
  if (at >= 0 && !credentials) return null

  const parsed = splitHostPort(hostPort)
  if (!parsed) return null

  return {
    protocol: normalizeProtocol(protocol, options),
    host: parsed.host,
    port: parsed.port,
    ...splitCredentials(credentials)
  }
}

/** user:pass@host:port（无协议头） */
function parseCredentialForm(trimmed: string, options: ParseProxyUrlOptions): ParsedProxy | null {
  const at = trimmed.lastIndexOf('@')
  const credentials = trimmed.slice(0, at)
  if (!credentials) return null

  const parsed = splitHostPort(trimmed.slice(at + 1))
  if (!parsed) return null

  return {
    protocol: schemelessProtocol(options),
    host: parsed.host,
    port: parsed.port,
    ...splitCredentials(credentials)
  }
}

/** host:port 与 host:port:user:pass */
function parseColonForm(trimmed: string, options: ParseProxyUrlOptions): ParsedProxy | null {
  const hostPort = splitHostPort(trimmed)
  if (hostPort) {
    return {
      protocol: schemelessProtocol(options),
      host: hostPort.host,
      port: hostPort.port,
      username: '',
      password: ''
    }
  }

  // 只接受严格四段。三段（host:port:user）歧义太大，宁可让用户补全也不猜。
  // 留最后一段不切，密码就可以含冒号。
  const parts = splitN(trimmed, ':', 4)
  if (parts.length !== 4) return null
  const port = parsePort(parts[1])
  if (port === null || !parts[0]) return null

  return {
    protocol: schemelessProtocol(options),
    host: parts[0],
    port,
    username: parts[2],
    password: parts[3]
  }
}

function splitHostPort(value: string): { host: string; port: number } | null {
  const trimmed = value.trim()
  if (!trimmed) return null

  let host: string
  let rawPort: string

  if (trimmed.startsWith('[')) {
    // IPv6 的方括号写法。
    const close = trimmed.indexOf(']')
    if (close < 0) return null
    host = trimmed.slice(1, close)
    const rest = trimmed.slice(close + 1)
    if (!rest.startsWith(':')) return null
    rawPort = rest.slice(1)
  } else {
    const colon = trimmed.lastIndexOf(':')
    if (colon < 0) return null
    host = trimmed.slice(0, colon)
    rawPort = trimmed.slice(colon + 1)
    // 裸 IPv6 无法与 host:port:user:pass 区分，要求加方括号。
    if (host.includes(':')) return null
  }

  if (!host) return null
  const port = parsePort(rawPort)
  if (port === null) return null
  return { host, port }
}

function splitCredentials(raw: string): { username: string; password: string } {
  if (!raw) return { username: '', password: '' }
  // 反过来，用户名不允许含冒号，所以用第一个冒号切，密码可以含冒号。
  const colon = raw.indexOf(':')
  if (colon < 0) return { username: raw, password: '' }
  return { username: raw.slice(0, colon), password: raw.slice(colon + 1) }
}

function parsePort(raw: string): number | null {
  const value = raw.trim()
  if (!/^\d+$/.test(value)) return null
  const port = Number(value)
  if (port < 1 || port > 65535) return null
  return port
}

/** JS 的 split 带 limit 会丢弃剩余部分，这里要的是 Go SplitN 那种语义。 */
function splitN(value: string, separator: string, limit: number): string[] {
  const out: string[] = []
  let rest = value
  while (out.length < limit - 1) {
    const index = rest.indexOf(separator)
    if (index < 0) break
    out.push(rest.slice(0, index))
    rest = rest.slice(index + 1)
  }
  out.push(rest)
  return out
}

function schemelessProtocol(options: ParseProxyUrlOptions): ProxyProtocol {
  return normalizeProtocol(options.defaultProtocol ?? DEFAULT_PROXY_PROTOCOL, options)
}

function normalizeProtocol(protocol: string, options: ParseProxyUrlOptions): ProxyProtocol {
  if (options.upgradeSocks5 && protocol === 'socks5') return 'socks5h'
  return protocol as ProxyProtocol
}

/**
 * 供预览用的脱敏描述，例如 `socks5h://1.2.3.4:1080（已带认证）`。
 *
 * 绝不回显密码：预览会渲染在页面上，也可能被截图发出去。
 */
export function describeParsedProxy(parsed: ParsedProxy): string {
  return `${parsed.protocol}://${formatHostPort(parsed.host, parsed.port)}`
}

function formatHostPort(host: string, port: number): string {
  return host.includes(':') ? `[${host}]:${port}` : `${host}:${port}`
}
