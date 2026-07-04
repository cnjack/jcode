import { Suspense, lazy, useEffect } from 'react'
import { Outlet, Route, Routes, useLocation } from 'react-router-dom'
import SiteNav from './components/SiteNav'
import SiteFooter from './components/SiteFooter'
import HomePage from './pages/HomePage'

const DesktopPage = lazy(() => import('./pages/DesktopPage'))
const CliPage = lazy(() => import('./pages/CliPage'))
const ShowcasePage = lazy(() => import('./pages/ShowcasePage'))
const ShowcaseProjectPage = lazy(() => import('./pages/ShowcaseProjectPage'))
const DocsLayout = lazy(() => import('./pages/docs/DocsLayout'))
const DocPage = lazy(() => import('./pages/docs/DocPage'))
const PrivacyPage = lazy(() => import('./pages/PrivacyPage'))

function ScrollToTop() {
  const { pathname } = useLocation()
  useEffect(() => {
    window.scrollTo(0, 0)
  }, [pathname])
  return null
}

function Shell() {
  return (
    <>
      <SiteNav />
      <Outlet />
      <SiteFooter />
    </>
  )
}

/** Showcase project pages manage their own chrome (nav + toolbar, no footer). */
function BareShell() {
  return (
    <>
      <SiteNav />
      <Outlet />
    </>
  )
}

const spinner = (
  <div
    style={{
      minHeight: '60vh',
      display: 'grid',
      placeItems: 'center',
      fontFamily: 'var(--font-mono)',
      color: 'var(--ink-faint)',
    }}
  >
    [ loading… ]
  </div>
)

export default function App() {
  return (
    <>
      <ScrollToTop />
      <Suspense fallback={spinner}>
        <Routes>
          <Route element={<Shell />}>
            <Route path="/" element={<HomePage />} />
            <Route path="/desktop" element={<DesktopPage />} />
            <Route path="/cli" element={<CliPage />} />
            <Route path="/showcase" element={<ShowcasePage />} />
            <Route path="/privacy" element={<PrivacyPage />} />
            <Route path="/docs" element={<DocsLayout />}>
              <Route index element={<DocPage />} />
              <Route path="*" element={<DocPage />} />
            </Route>
            <Route
              path="*"
              element={
                <div style={{ minHeight: '50vh', display: 'grid', placeItems: 'center' }}>
                  <div style={{ textAlign: 'center' }}>
                    <div className="mono-label">404</div>
                    <h2 style={{ marginTop: 12 }}>This page walked off the grid.</h2>
                    <a href="/">← Back home</a>
                  </div>
                </div>
              }
            />
          </Route>
          <Route element={<BareShell />}>
            <Route path="/showcase/:id" element={<ShowcaseProjectPage />} />
          </Route>
        </Routes>
      </Suspense>
    </>
  )
}
