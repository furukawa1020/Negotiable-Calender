import { expect, test } from '@playwright/test'

test('failed consent restores session, shows disclosure and permits explicit retry on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  let connects = 0
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    let json: object = {}
    if (path.endsWith('/auth/session')) json = { authenticated: true, user: { userId: 'owner', organizationId: 'org', displayName: '利用者', email: 'owner@example.com', role: 'OWNER' } }
    if (path.endsWith('/calendar/connection')) json = { connected: false }
    if (path.endsWith('/workspaces')) json = { workspaces: [] }
    if (path.endsWith('/projection')) json = { segments: [] }
    if (path.endsWith('/google/connect')) {
      connects++
      return route.fulfill({ status: 200, contentType: 'text/html', body: '<p>synthetic consent destination</p>' })
    }
    await route.fulfill({ status: 200, json })
  })
  await page.goto('http://127.0.0.1:5190/?calendar=denied')
  await expect(page.getByRole('button', { name: '利用者のアカウントメニュー' })).toBeVisible()
  await expect(page.getByRole('status')).toContainText('既存の接続は変更していません')
  await expect(page).toHaveURL('http://127.0.0.1:5190/')
  expect(connects).toBe(0)
  await page.getByRole('button', { name: '利用者のアカウントメニュー' }).click()
  const disclosure = page.getByRole('region', { name: 'カレンダー接続前の確認' })
  await expect(disclosure.getByText(/Google上の予定は作成・変更しません/)).toBeVisible()
  await disclosure.getByText('カレンダー接続で403などが出る場合', { exact: true }).click()
  await expect(disclosure.getByText(/認証コード、Cookie、秘密鍵は送らない/)).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
  await disclosure.getByRole('link', { name: 'Google Calendarを接続', exact: true }).click()
  await expect(page.getByText('synthetic consent destination')).toBeVisible()
  expect(connects).toBe(1)
})

test('provider-hosted error help is available without login', async ({ page }) => {
  await page.route('**/api/v1/**', route => route.fulfill({ status: 401, json: { authenticated: false } }))
  await page.goto('http://127.0.0.1:5190/')
  await expect(page.getByRole('link', { name: 'Googleでログイン' })).toBeVisible()
  await page.getByText('カレンダー接続で403などが出る場合', { exact: true }).click()
  await expect(page.getByRole('link', { name: '運営者に問い合わせる' })).toHaveAttribute('href', 'mailto:f.kotaro.0530@gmail.com')
})
