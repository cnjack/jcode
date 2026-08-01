const { chromium } = require('playwright')
const fs = require('node:fs')
const path = require('node:path')

const baseURL = process.env.PROTOTYPE_URL || 'http://127.0.0.1:4179/internal-doc/artifacts-ui/index.html'
const outputDir = path.join(__dirname, 'screenshots')
fs.mkdirSync(outputDir, { recursive: true })

function check(condition, message) {
  if (!condition) throw new Error(message)
}

;(async () => {
  const browser = await chromium.launch({ headless: true })
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 })
  const pageErrors = []
  page.on('pageerror', (error) => pageErrors.push(error.message))
  await page.goto(baseURL, { waitUntil: 'networkidle' })

  check(await page.locator('#viewer-title').textContent() === 'Release readiness report', 'default Artifact was not selected')
  check(await page.locator('#share-button').isVisible(), 'Share should be visible while logged in')
  await page.screenshot({ path: path.join(outputDir, '01-docked-workbench.png'), fullPage: true })

  await page.getByRole('button', { name: 'B · Focus canvas' }).click()
  check(await page.locator('.focus-shell').isVisible(), 'Focus canvas did not open')
  await page.getByRole('button', { name: 'Summary', exact: true }).click()
  check(await page.locator('.focus-preview h1').textContent() === 'Artifact release summary', 'Focus canvas did not switch Artifact')
  await page.screenshot({ path: path.join(outputDir, '02-focus-canvas.png'), fullPage: true })

  await page.getByRole('button', { name: 'C · Quick Look' }).click()
  check(await page.locator('.quick-preview').isVisible(), 'Quick Look did not open')
  await page.locator('.artifact-row[data-artifact="html"]').click()
  await page.screenshot({ path: path.join(outputDir, '03-inline-quick-look.png'), fullPage: true })

  await page.getByRole('button', { name: 'A · Docked' }).click()
  await page.locator('#login-toggle').uncheck()
  check(await page.locator('#share-button').isHidden(), 'Share must be hidden while logged out')
  await page.screenshot({ path: path.join(outputDir, '04-logged-out.png'), fullPage: true })

  await page.locator('#login-toggle').check()
  await page.locator('#share-state').selectOption('stale')
  check(await page.getByText('Newer local revision').isVisible(), 'Stale share state was not shown')
  await page.screenshot({ path: path.join(outputDir, '06-stale-share.png'), fullPage: true })
  await page.getByRole('button', { name: 'Share latest' }).click()
  check(await page.getByText('Encrypting and uploading').isVisible(), 'Share latest did not enter uploading state')

  await page.locator('#share-state').selectOption('shared')
  await page.getByRole('button', { name: 'Copy link', exact: true }).click()
  check(await page.getByRole('status').textContent() === 'Encrypted share link copied', 'Copy link feedback missing')

  await page.locator('.fullscreen-open').last().click()
  check(await page.locator('#fullscreen').isVisible(), 'Fullscreen Viewer did not open')
  await page.locator('#fullscreen-close').click()
  check(await page.locator('#fullscreen').isHidden(), 'Fullscreen Viewer did not close')

  await page.setViewportSize({ width: 1024, height: 768 })
  await page.getByRole('button', { name: 'A · Docked' }).click()
  const appBox = await page.locator('#app').boundingBox()
  check(appBox && appBox.width <= 1008, 'Prototype overflowed the narrow desktop viewport')
  await page.screenshot({ path: path.join(outputDir, '05-narrow-web.png'), fullPage: true })

  check(pageErrors.length === 0, `Page errors: ${pageErrors.join('; ')}`)
  await browser.close()
  process.stdout.write('prototype checks passed\n')
})().catch((error) => {
  process.stderr.write(`${error.stack || error}\n`)
  process.exit(1)
})
