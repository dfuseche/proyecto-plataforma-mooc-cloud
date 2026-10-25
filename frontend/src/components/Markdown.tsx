import type { ReactNode } from 'react'

// Render mínimo y seguro de Markdown (sin HTML crudo): títulos, listas,
// negrita, cursiva, código en línea y enlaces http(s).
function inline(text: string, key: string): ReactNode[] {
  const out: ReactNode[] = []
  // Cuantificadores acotados: evitan retroceso super-lineal con texto sin cerrar.
  const re = /(\*\*[^*]{1,1000}\*\*|\*[^*]{1,1000}\*|`[^`]{1,1000}`|\[[^\]]{1,500}\]\(https?:\/\/[^)\s]{1,2000}\))/g
  let last = 0
  let m: RegExpExecArray | null
  let i = 0
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index))
    const t = m[0]
    const k = `${key}-${i++}`
    if (t.startsWith('**')) out.push(<strong key={k}>{t.slice(2, -2)}</strong>)
    else if (t.startsWith('`')) out.push(<code key={k}>{t.slice(1, -1)}</code>)
    else if (t.startsWith('[')) {
      const sep = t.indexOf('](')
      const label = t.slice(1, sep)
      const href = t.slice(sep + 2, -1)
      out.push(<a key={k} href={href} target="_blank" rel="noreferrer noopener">{label}</a>)
    } else out.push(<em key={k}>{t.slice(1, -1)}</em>)
    last = m.index + t.length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

export default function Markdown({ source }: { source: string }) {
  const blocks = source.replace(/\r\n/g, '\n').split(/\n{2,}/)
  return (
    <div className="prose">
      {blocks.map((b, i) => {
        const lines = b.split('\n')
        const h = /^(#{1,3})\s+(\S.*)$/.exec(lines[0])
        if (h && lines.length === 1) {
          const level = h[1].length + 1
          const Tag = (`h${level}` as 'h2' | 'h3' | 'h4')
          return <Tag key={i}>{inline(h[2], `h${i}`)}</Tag>
        }
        if (lines.every((l) => /^\s*[-*]\s+/.test(l))) {
          return <ul key={i}>{lines.map((l, j) => <li key={j}>{inline(l.replace(/^\s*[-*]\s+/, ''), `l${i}-${j}`)}</li>)}</ul>
        }
        if (lines.every((l) => /^\s*\d+[.)]\s+/.test(l))) {
          return <ol key={i}>{lines.map((l, j) => <li key={j}>{inline(l.replace(/^\s*\d+[.)]\s+/, ''), `o${i}-${j}`)}</li>)}</ol>
        }
        return <p key={i}>{inline(lines.join(' '), `p${i}`)}</p>
      })}
    </div>
  )
}
