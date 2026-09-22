import { expect, test } from '@playwright/test'

for (const width of [390, 1280]) {
  test(`public policies have working navigation and fit a ${width}px viewport`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('http://127.0.0.1:5188/')
    await page.getByRole('link', { name: 'プライバシーポリシー', exact: true }).click()
    await expect(page.getByRole('heading', { level: 1, name: 'プライバシーポリシー' })).toBeVisible()
    await expect(page.getByText(/calendar.events.owned.readonly/)).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`privacy-${width}.png`), fullPage: true })
    await page.getByRole('link', { name: '保存と削除', exact: true }).click()
    await expect(page).toHaveURL(/privacy.html#retention$/)
    await page.getByRole('link', { name: '利用規約', exact: true }).first().click()
    await expect(page.getByRole('heading', { level: 1, name: '利用規約' })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`terms-${width}.png`), fullPage: true })
    await page.getByRole('link', { name: 'サービス紹介', exact: true }).click()
    await expect(page).toHaveURL('http://127.0.0.1:5188/')
  })
}

test('login exposes policy links before any Google authorization', async ({ page }) => {
  await page.route('**/api/v1/**', route => route.fulfill({ status: 401, json: { authenticated: false } }))
  await page.goto('http://127.0.0.1:5190/')
  await expect(page.getByRole('link', { name: 'Googleでログイン' })).toBeVisible()
  const links = page.getByRole('navigation', { name: 'サービスの利用条件' })
  await expect(links).toBeVisible()
  for (const [name, path] of [['プライバシーポリシー', 'privacy.html'], ['利用規約', 'terms.html']]) {
    const link = links.getByRole('link', { name: `${name}（別タブ）` })
    await expect(link).toHaveAttribute('href', `https://negotiable-calendar-480760.web.app/${path}`)
    await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  }
})

for (const state of ['disconnected', 'connected', 'reconnect']) {
  test(`account provides one set of policy links when ${state}`, async ({ page }) => {
    await page.route('**/api/v1/**', route => {
      const path = new URL(route.request().url()).pathname
      let json: object = {}
      if (path.endsWith('/auth/session')) json = { authenticated: true, user: { userId: 'owner', organizationId: 'org', displayName: '利用者', email: 'owner@example.com', role: 'OWNER' } }
      if (path.endsWith('/calendar/connection')) json = state === 'disconnected' ? { connected: false } : { connected: true, connection: { grantedScopes: [], connectedAt: '2026-09-23T00:00:00Z', reconnectRequired: state === 'reconnect' } }
      if (path.endsWith('/workspaces')) json = { workspaces: [] }
      if (path.endsWith('/projection')) json = { segments: [] }
      if (path.endsWith('/private-events')) json = { events: [] }
      return route.fulfill({ status: 200, json })
    })
    await page.goto('http://127.0.0.1:5190/')
    await page.getByRole('button', { name: '利用者のアカウントメニュー' }).click()
    await expect(page.getByRole('navigation', { name: 'サービスの利用条件' })).toHaveCount(1)
    await expect(page.getByRole('link', { name: 'プライバシーポリシー（別タブ）' })).toBeVisible()
  })
}
