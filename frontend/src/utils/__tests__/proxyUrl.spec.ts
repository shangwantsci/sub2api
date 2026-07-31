import { describe, it, expect } from 'vitest'
import { parseProxyUrl, describeParsedProxy, DEFAULT_PROXY_PROTOCOL } from '@/utils/proxyUrl'

const schemeless = { allowSchemeless: true }

describe('parseProxyUrl - 标准 URL', () => {
  it('解析带认证的连接串', () => {
    expect(parseProxyUrl('socks5h://alice:s3cret@proxy.example.com:1080')).toEqual({
      protocol: 'socks5h',
      host: 'proxy.example.com',
      port: 1080,
      username: 'alice',
      password: 's3cret'
    })
  })

  it('解析不带认证的连接串', () => {
    expect(parseProxyUrl('http://1.2.3.4:8080')).toEqual({
      protocol: 'http',
      host: '1.2.3.4',
      port: 8080,
      username: '',
      password: ''
    })
  })

  it('协议大小写不敏感', () => {
    expect(parseProxyUrl('HTTPS://1.2.3.4:8080')?.protocol).toBe('https')
  })

  // 管理端批量创建保持原样存 socks5：网关侧 proxyurl.Parse 拨号时总会升级，
  // 存哪个值运行时行为一样，但把管理员填的协议悄悄改掉会让人以为存错了。
  it('默认不升级 socks5', () => {
    expect(parseProxyUrl('socks5://1.2.3.4:1080')?.protocol).toBe('socks5')
  })

  it('显式要求时才升级 socks5 到 socks5h', () => {
    expect(parseProxyUrl('socks5://1.2.3.4:1080', { upgradeSocks5: true })?.protocol).toBe('socks5h')
  })
})

describe('parseProxyUrl - 省略协议头的写法', () => {
  it('默认不接受，避免把协议猜错', () => {
    expect(parseProxyUrl('1.2.3.4:1080')).toBeNull()
    expect(parseProxyUrl('alice:s3cret@1.2.3.4:1080')).toBeNull()
    expect(parseProxyUrl('1.2.3.4:1080:alice:s3cret')).toBeNull()
  })

  it('host:port 补默认协议', () => {
    expect(parseProxyUrl('1.2.3.4:1080', schemeless)).toEqual({
      protocol: DEFAULT_PROXY_PROTOCOL,
      host: '1.2.3.4',
      port: 1080,
      username: '',
      password: ''
    })
  })

  it('host:port:user:pass 补默认协议', () => {
    expect(parseProxyUrl('1.2.3.4:1080:alice:s3cret', schemeless)).toEqual({
      protocol: DEFAULT_PROXY_PROTOCOL,
      host: '1.2.3.4',
      port: 1080,
      username: 'alice',
      password: 's3cret'
    })
  })

  it('user:pass@host:port 补默认协议', () => {
    expect(parseProxyUrl('alice:s3cret@1.2.3.4:1080', schemeless)).toEqual({
      protocol: DEFAULT_PROXY_PROTOCOL,
      host: '1.2.3.4',
      port: 1080,
      username: 'alice',
      password: 's3cret'
    })
  })

  it('可以覆盖默认协议', () => {
    expect(parseProxyUrl('1.2.3.4:8080', { ...schemeless, defaultProtocol: 'http' })?.protocol).toBe(
      'http'
    )
  })
})

describe('parseProxyUrl - 凭据里的特殊字符', () => {
  // 用第一个 @ 切会把密码截断成一个能连上但错误的凭据，表现是上号莫名其妙失败。
  it('密码含 @ 时不被截断', () => {
    expect(parseProxyUrl('socks5h://alice:p@ss@1.2.3.4:1080')).toMatchObject({
      username: 'alice',
      password: 'p@ss',
      host: '1.2.3.4'
    })
    expect(parseProxyUrl('alice:p@ss@1.2.3.4:1080', schemeless)).toMatchObject({
      username: 'alice',
      password: 'p@ss'
    })
  })

  it('密码含冒号时不被截断', () => {
    expect(parseProxyUrl('socks5h://alice:p:ss@1.2.3.4:1080')).toMatchObject({
      username: 'alice',
      password: 'p:ss'
    })
    expect(parseProxyUrl('1.2.3.4:1080:alice:p:ss', schemeless)).toMatchObject({
      username: 'alice',
      password: 'p:ss'
    })
  })

  it('不对凭据做 percent-decode', () => {
    expect(parseProxyUrl('socks5h://alice:100%25@1.2.3.4:1080')?.password).toBe('100%25')
  })

  it('只有用户名没有密码也可以', () => {
    expect(parseProxyUrl('socks5h://alice@1.2.3.4:1080')).toMatchObject({
      username: 'alice',
      password: ''
    })
  })
})

describe('parseProxyUrl - IPv6', () => {
  it('带方括号时可解析', () => {
    expect(parseProxyUrl('socks5h://[2001:db8::1]:1080')).toMatchObject({
      host: '2001:db8::1',
      port: 1080
    })
    expect(parseProxyUrl('[2001:db8::1]:1080', schemeless)).toMatchObject({
      host: '2001:db8::1',
      port: 1080
    })
  })

  it('带认证的 IPv6 可解析', () => {
    expect(parseProxyUrl('socks5h://alice:s3cret@[2001:db8::1]:1080')).toMatchObject({
      host: '2001:db8::1',
      port: 1080,
      username: 'alice',
      password: 's3cret'
    })
  })

  // 裸 IPv6 与 host:port:user:pass 无法区分，必须要求方括号。
  it('不带方括号时拒绝', () => {
    expect(parseProxyUrl('socks5h://2001:db8::1:1080')).toBeNull()
    expect(parseProxyUrl('2001:db8::1:1080', schemeless)).toBeNull()
  })
})

describe('parseProxyUrl - 非法输入', () => {
  it.each([
    ['', '空串'],
    ['   ', '只有空白'],
    ['1.2.3.4', '缺端口'],
    ['ftp://1.2.3.4:1080', '协议不在白名单'],
    ['socks5://1.2.3.4', 'URL 缺端口'],
    ['socks5://1.2.3.4:0', '端口为 0'],
    ['socks5://1.2.3.4:70000', '端口越界'],
    ['socks5://1.2.3.4:notaport', '端口非数字'],
    ['socks5://:1080', '缺 host'],
    ['socks5://@1.2.3.4:1080', '空凭据'],
    ['1.2.3.4:1080:alice', '三段歧义']
  ])('拒绝 %s（%s）', (raw) => {
    expect(parseProxyUrl(raw, schemeless)).toBeNull()
  })

  it('两端空白会被裁掉', () => {
    expect(parseProxyUrl('  socks5h://1.2.3.4:1080  ')?.host).toBe('1.2.3.4')
  })
})

describe('describeParsedProxy', () => {
  // 预览会渲染在页面上，也可能被截图发出去，绝不能带密码。
  it('不回显凭据', () => {
    const parsed = parseProxyUrl('socks5h://alice:s3cret@1.2.3.4:1080')!
    const described = describeParsedProxy(parsed)
    expect(described).toBe('socks5h://1.2.3.4:1080')
    expect(described).not.toContain('s3cret')
    expect(described).not.toContain('alice')
  })

  it('IPv6 加回方括号', () => {
    const parsed = parseProxyUrl('socks5h://[2001:db8::1]:1080')!
    expect(describeParsedProxy(parsed)).toBe('socks5h://[2001:db8::1]:1080')
  })
})
