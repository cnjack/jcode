import { useEffect, useRef, useState } from 'react'

export type TermLine =
  | { t: 'cmd'; text: string }
  | { t: 'agent'; text: string }
  | { t: 'tool'; text: string }
  | { t: 'diff'; lines: string[] }
  | { t: 'ok'; text: string }
  | { t: 'pause'; ms: number }

/** A looping, scripted terminal session with typed commands. */
export default function TypingTerminal({
  script,
  title = 'jcode',
  loop = true,
}: {
  script: TermLine[]
  title?: string
  loop?: boolean
}) {
  const [rendered, setRendered] = useState<TermLine[]>([])
  const [typing, setTyping] = useState('') // partially-typed cmd
  const bodyRef = useRef<HTMLDivElement>(null)
  const timers = useRef<number[]>([])

  useEffect(() => {
    let alive = true
    const t = (fn: () => void, ms: number) => {
      const id = window.setTimeout(fn, ms)
      timers.current.push(id)
    }

    function playFrom(i: number) {
      if (!alive) return
      if (i >= script.length) {
        if (loop) t(() => { setRendered([]); playFrom(0) }, 4200)
        return
      }
      const line = script[i]
      if (line.t === 'pause') {
        t(() => playFrom(i + 1), line.ms)
        return
      }
      if (line.t === 'cmd') {
        // type it character by character
        let pos = 0
        const step = () => {
          if (!alive) return
          pos++
          setTyping(line.text.slice(0, pos))
          if (pos < line.text.length) {
            t(step, 26 + Math.random() * 40)
          } else {
            t(() => {
              setTyping('')
              setRendered((r) => [...r, line])
              playFrom(i + 1)
            }, 350)
          }
        }
        t(step, 120)
        return
      }
      setRendered((r) => [...r, line])
      t(() => playFrom(i + 1), line.t === 'diff' ? 700 : 420)
    }

    playFrom(0)
    return () => {
      alive = false
      timers.current.forEach(clearTimeout)
      timers.current = []
      setRendered([])
      setTyping('')
    }
  }, [script, loop])

  useEffect(() => {
    const el = bodyRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [rendered, typing])

  return (
    <div className="tt-frame">
      <div className="tt-bar">
        <span className="tt-dot r" />
        <span className="tt-dot y" />
        <span className="tt-dot g" />
        <span className="tt-title">{title}</span>
      </div>
      <div className="tt-body" ref={bodyRef}>
        {rendered.map((l, i) => {
          switch (l.t) {
            case 'cmd':
              return (
                <div key={i} className="tt-line tt-cmd">
                  <span className="tt-prompt">$</span> {l.text}
                </div>
              )
            case 'agent':
              return (
                <div key={i} className="tt-line tt-agent">
                  <span className="tt-diamond">◆</span> {l.text}
                </div>
              )
            case 'tool':
              return (
                <div key={i} className="tt-line tt-tool">
                  ⚙ {l.text}
                </div>
              )
            case 'diff':
              return (
                <div key={i} className="tt-diff">
                  {l.lines.map((d, j) => (
                    <div
                      key={j}
                      className={d.startsWith('+') ? 'add' : d.startsWith('-') ? 'del' : ''}
                    >
                      {d}
                    </div>
                  ))}
                </div>
              )
            case 'ok':
              return (
                <div key={i} className="tt-line tt-ok">
                  ✓ {l.text}
                </div>
              )
            default:
              return null
          }
        })}
        {typing !== '' && (
          <div className="tt-line tt-cmd">
            <span className="tt-prompt">$</span> {typing}
            <span className="tt-caret" />
          </div>
        )}
        {typing === '' && rendered.length === 0 && (
          <div className="tt-line tt-cmd">
            <span className="tt-prompt">$</span>
            <span className="tt-caret" />
          </div>
        )}
      </div>
    </div>
  )
}
