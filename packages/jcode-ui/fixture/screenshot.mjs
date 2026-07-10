/**
 * Playwright acceptance screenshots for tool-output UX fixture.
 * Usage: node screenshot.mjs <baseUrl> <outDir>
 */
import { chromium } from 'playwright'
import path from 'node:path'
import fs from 'node:fs'

const baseUrl = process.argv[2] || 'http://127.0.0.1:5199'
const outDir = process.argv[3] || '.'

fs.mkdirSync(outDir, { recursive: true })

const browser = await chromium.launch({ headless: true })
const page = await browser.newPage({ viewport: { width: 900, height: 1200 } })
await page.goto(baseUrl, { waitUntil: 'networkidle' })
await page.waitForSelector('[data-fixture-ready="true"]', { timeout: 15000 })

// Full page
await page.screenshot({ path: path.join(outDir, 'ui-full.png'), fullPage: true })

// Exploring group
const exploring = page.locator('.jcode-exploring').first()
await exploring.scrollIntoViewIfNeeded()
await exploring.screenshot({ path: path.join(outDir, 'ui-exploring.png') })

// Terminal cards
const terms = page.locator('[data-tool-name="execute"]')
const termCount = await terms.count()
if (termCount >= 1) {
  await terms.nth(0).scrollIntoViewIfNeeded()
  await terms.nth(0).screenshot({ path: path.join(outDir, 'ui-terminal-ok.png') })
}
if (termCount >= 2) {
  await terms.nth(1).scrollIntoViewIfNeeded()
  await terms.nth(1).screenshot({ path: path.join(outDir, 'ui-terminal-err.png') })
}

// Subagent
const sub = page.locator('[data-tool-name="subagent"]').first()
await sub.scrollIntoViewIfNeeded()
await sub.screenshot({ path: path.join(outDir, 'ui-subagent.png') })

console.log('screenshots written to', outDir)
await browser.close()
