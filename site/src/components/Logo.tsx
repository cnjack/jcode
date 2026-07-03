import { Link } from 'react-router-dom'

export default function Logo({ size = 18 }: { size?: number }) {
  return (
    <Link to="/" className="jc-logo" style={{ fontSize: size }} aria-label="jcode home">
      <span className="br">[</span>
      <span className="j">J</span>
      <span>CODE</span>
      <span className="br">]</span>
    </Link>
  )
}
