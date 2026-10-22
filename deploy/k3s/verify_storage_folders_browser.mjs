// Review the real portal using the regular account from verify_storage_folders.py.
// State and screenshots are written only to the protected external evidence dir.
import { readFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'

const { chromium } = await import(
  process.env.MIST_PLAYWRIGHT_MODULE ??
    '/tmp/mist-browser-check/node_modules/playwright/index.mjs'
)
const base = process.env.MIST_SITE_URL ?? 'http://100.73.139.66:8088'
const out = process.env.MIST_STORAGE_EVIDENCE_DIR
if (!out) throw new Error('Set MIST_STORAGE_EVIDENCE_DIR to the live verifier output directory')
const state = JSON.parse(readFileSync(`${out}/member-private.json`, 'utf8'))
const browser = await chromium.launch({ headless: true })
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
const page = await context.newPage()
const report = { checks: [], browser_errors: [] }
page.on('pageerror', (error) => report.browser_errors.push(error.message))
try {
  await page.goto(`${base}/datasets`)
  await page.getByLabel('Email', { exact: true }).fill(state.email)
  await page.getByLabel('Password', { exact: true }).fill(state.password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await page.getByRole('heading', { name: 'Datasets', exact: true }).waitFor()
  await page.getByLabel('Workspace', { exact: true }).click()
  await page.locator(`[role=option][data-value="${state.team_id}"]`).click()
  await page.getByText(/Shared disk:/).waitFor()
  await page.getByText('Advisory allowance — shared disk space is not reserved.').first().waitFor()
  report.checks.push('regular_member_login_and_advisory_storage_display')

  await page.getByRole('button', { name: 'Add dataset', exact: true }).click()
  await page.getByLabel('Dataset name (optional)').fill('Browser Ethernet check')
  const payload = Buffer.from('x,y\n1,2\n')
  await page.getByLabel('File or ZIP archive', { exact: true }).setInputFiles({
    name: 'browser.csv', mimeType: 'text/csv', buffer: payload,
  })
  const response = page.waitForResponse((r) => r.url().includes('/api/datasets?') && r.request().method() === 'POST')
  await page.getByRole('button', { name: 'Upload dataset', exact: true }).click()
  const uploaded = await response
  if (uploaded.status() !== 201) throw new Error(`Upload HTTP ${uploaded.status()}`)
  const dataset = await uploaded.json()
  if (dataset.sha256 !== createHash('sha256').update(payload).digest('hex')) throw new Error('Browser upload checksum mismatch')
  await page.getByText('Browser Ethernet check is ready to use.', { exact: true }).waitFor()
  report.checks.push('real_browser_upload_with_verified_checksum')
  await page.screenshot({ path: `${out}/datasets-browser.png`, fullPage: true })

  await page.goto(`${base}/jobs`)
  await page.getByText('Ethernet folder training nvidia', { exact: true }).waitFor()
  await page.getByText('Ethernet folder training tenstorrent', { exact: true }).waitFor()
  report.checks.push('both_real_training_jobs_visible_in_member_history')
  await page.screenshot({ path: `${out}/jobs-browser.png`, fullPage: true })
  if (report.browser_errors.length) throw new Error('Browser reported JavaScript errors')
  report.checks.push('no_browser_javascript_errors')
  console.log('PASS ' + report.checks.join(', '))
} finally {
  await context.request.post(`${base}/auth/sign-out`, { data: {}, headers: { Origin: base } }).catch(() => {})
  await browser.close()
  writeFileSync(`${out}/browser-results.json`, JSON.stringify(report, null, 2))
}
