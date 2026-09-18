import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// Synthetic responses only: these tests never log in or mutate a real calendar.
async function fixtureAPI(page: Page) {
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const option = { id: 'option-1', type: 'meeting', startAt: '2026-10-01T01:00:00Z', endAt: '2026-10-01T01:15:00Z' }
    let json: object = {}
    if (path.endsWith('/auth/session')) return route.fulfill({ status: 401, json: { error: 'unauthenticated' } })
    if (path.endsWith('/requests')) json = { requests: [{
      id: 'request-1', requesterUserId: 'sample-member', targetUserId: 'demo-manager',
      title: '設計レビューの相談', durationMinutes: 15, deadlineAt: '2026-10-02T08:00:00Z',
      priority: 'high', status: 'suggested', options: [option],
    }] }
    if (path.endsWith('/people')) json = { people: [{ id: 'sample-manager', displayName: 'サンプル管理者', role: 'MANAGER', timezone: 'Asia/Tokyo' }] }
    if (path.endsWith('/projection')) json = { segments: [{ startAt: option.startAt, endAt: option.endAt, availability: 'available', interruptibility: 'normal' }] }
    if (path.endsWith('/sharing-policy')) json = {
      default: { availability: 'available', interruptibility: 'normal', requestability: 'open', reschedulability: 'medium' },
      workingHours: [1, 2, 3, 4, 5].map((weekday) => ({ weekday, startMinute: 540, endMinute: 1080 })), rules: [],
    }
    if (path.endsWith('/audit-logs')) json = { auditLogs: [{ id: 'audit-1', action: 'request_created', actorUserId: 'sample-member', resourceType: 'request', resourceId: 'request-1', createdAt: option.startAt }] }
    if (path.endsWith('/notifications')) json = { notifications: [] }
    return route.fulfill({ status: 200, json })
  })
}

async function noPageOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
}

for (const width of [1440, 1024, 390, 320]) {
  test.describe(`viewport ${width}`, () => {
    test.use({ viewport: { width, height: 1000 } })

    test('calendar, controls and dialogs remain usable', async ({ page }, testInfo) => {
      await fixtureAPI(page)
      await page.goto('http://127.0.0.1:5187')
      await expect(page.getByRole('heading', { name: 'カレンダー', exact: true })).toBeVisible()
      await expect(page.getByRole('button', { name: 'マイカレンダー' })).toHaveAttribute('aria-current', 'page')
      await expect(page.getByRole('button', { name: '共有ルールを確認' })).toBeVisible()
      await noPageOverflow(page)
      await page.screenshot({ path: testInfo.outputPath('calendar.png'), fullPage: true })
      const date = await page.locator('.calendar-date').textContent()
      await page.getByRole('button', { name: '次の日', exact: true }).click()
      await expect(page.locator('.calendar-date')).not.toHaveText(date!)
      await page.getByRole('button', { name: '週', exact: true }).click()
      await expect(page.getByRole('button', { name: '週', exact: true })).toHaveAttribute('aria-pressed', 'true')
      await page.getByRole('button', { name: '自分の予定', exact: true }).click()
      await expect(page.getByRole('heading', { name: '組織に見える状態' })).toBeHidden()
      await page.getByRole('button', { name: '公開状態', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'あなたの予定' })).toBeHidden()
      await page.getByRole('button', { name: '両方', exact: true }).click()
      await page.getByRole('button', { name: '依頼を作成', exact: true }).click()
      await expect(page.getByRole('dialog', { name: '依頼を作成' })).toBeVisible()
      await noPageOverflow(page)
      await page.screenshot({ path: testInfo.outputPath('request-dialog.png'), fullPage: true })
      await page.getByRole('button', { name: '閉じる', exact: true }).click()
      await page.getByRole('button', { name: '共有ルールを確認' }).click()
      await expect(page.getByLabel('基本の公開状態')).toBeEnabled()
      await noPageOverflow(page)
      expect(await page.getByRole('dialog').evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath('sharing-rules.png'), fullPage: true })
      await page.getByRole('button', { name: '閉じる', exact: true }).click()
      await page.getByRole('button', { name: '状態を上書き' }).click()
      await expect(page.getByRole('dialog', { name: '公開状態を上書き' })).toBeVisible()
      await page.getByRole('button', { name: 'キャンセル', exact: true }).click()
      await page.getByRole('button', { name: '山田 太郎のアカウントメニュー' }).click()
      await expect(page.getByRole('button', { name: '本人データをエクスポート' })).toBeVisible()
      await noPageOverflow(page)
    })

    test('organization, inbox, sent and audit use continuous lists', async ({ page }, testInfo) => {
      await fixtureAPI(page)
      await page.goto('http://127.0.0.1:5187')
      for (const [button, heading, loaded] of [
        ['組織', '組織の公開状態', 'サンプル管理者'],
        ['依頼', '受信した依頼', '設計レビューの相談'],
        ['送信済み', '送信した依頼', '設計レビューの相談'],
        ['監査', '操作履歴', 'request created'],
      ]) {
        await page.getByRole('navigation').getByRole('button', { name: button, exact: true }).click()
        await expect(page.getByRole('heading', { name: heading })).toBeVisible()
        await expect(page.getByText(loaded, { exact: true })).toBeVisible()
        await expect(page.getByRole('navigation').getByRole('button', { name: button, exact: true })).toHaveAttribute('aria-current', 'page')
        await noPageOverflow(page)
        await page.screenshot({ path: testInfo.outputPath(`${button}.png`), fullPage: true })
      }
    })

    test('public entry and production sign-in share the visual system', async ({ page }, testInfo) => {
      await page.goto('http://127.0.0.1:5188')
      await expect(page.getByRole('heading', { name: /カレンダー共有と\s*相談の調整/ })).toBeVisible()
      await expect(page.getByRole('link', { name: 'アプリを開く' })).toHaveAttribute('href', 'https://negotiable-calendar-480760664246.asia-northeast1.run.app')
      await expect(page.getByText(/公開設定・審査が未完了/)).toBeVisible()
      await noPageOverflow(page)
      await page.screenshot({ path: testInfo.outputPath('hosting.png'), fullPage: true })
      await fixtureAPI(page)
      await page.goto('http://127.0.0.1:5190')
      await expect(page.getByRole('heading', { name: 'カレンダーにログイン' })).toBeVisible()
      await expect(page.getByText(/デモ表示/)).toBeHidden()
      await expect(page.getByRole('link', { name: 'Googleでログイン' })).toHaveAttribute('href', 'http://127.0.0.1:5190/api/v1/auth/google/login')
      await noPageOverflow(page)
      await page.screenshot({ path: testInfo.outputPath('signin.png'), fullPage: true })
    })
  })
}
