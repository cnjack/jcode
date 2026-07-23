import { Link } from 'react-router-dom'
import Logo from './Logo'

export default function SiteFooter() {
  return (
    <footer className="site-footer">
      <div className="container">
        <div className="footer-grid">
          <div>
            <Logo />
            <p className="footer-tagline">
              An open-source AI coding agent for your terminal, desktop and browser.
              Think it. Code it.
            </p>
          </div>
          <div>
            <h5>Product</h5>
            <ul>
              <li><Link to="/cli">CLI</Link></li>
              <li><Link to="/desktop">Desktop</Link></li>
              <li><Link to="/cloud">Cloud</Link></li>
              <li><Link to="/docs/web-interface">Web interface</Link></li>
              <li><Link to="/showcase">Showcase</Link></li>
            </ul>
          </div>
          <div>
            <h5>Resources</h5>
            <ul>
              <li><Link to="/docs">Documentation</Link></li>
              <li><Link to="/docs/get-started">Get started</Link></li>
              <li><Link to="/docs/configuration">Configuration</Link></li>
              <li><Link to="/docs/changelog">Changelog</Link></li>
            </ul>
          </div>
          <div>
            <h5>Community</h5>
            <ul>
              <li>
                <a href="https://github.com/cnjack/jcode" target="_blank" rel="noreferrer">GitHub</a>
              </li>
              <li>
                <a href="https://github.com/cnjack/jcode/issues" target="_blank" rel="noreferrer">Issues</a>
              </li>
              <li>
                <a href="https://github.com/cnjack/jcode/releases" target="_blank" rel="noreferrer">Releases</a>
              </li>
            </ul>
          </div>
        </div>
        <div className="footer-bottom">
          <span>© 2026 jcode. Open source, MIT licensed.</span>
          <span className="footer-legal">
            <Link to="/privacy">Privacy</Link>
            <a href="https://beian.miit.gov.cn/" target="_blank" rel="noreferrer">
              津ICP备13004281号-4
            </a>
          </span>
        </div>
      </div>
    </footer>
  )
}
