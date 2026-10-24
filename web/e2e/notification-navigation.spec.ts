import { expect, test } from '@playwright/test'

for (const width of [1440, 1024, 390, 320]) {
  test(`notification navigation and audit search at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 })
    const writes: string[] = []
    await page.route('**/api/v1/**', async route => {
      const request = route.request(), path = new URL(request.url()).pathname
      if (request.method() !== 'GET') writes.push(path)
      if (path.endsWith('/notifications')) return route.fulfill({ json: { notifications: [
        { id: 'n1', requestId: 'r1', type: 'request_changed', message: 'ＡＢＣ 日時変更', createdAt: '2030-01-01T00:00:00Z' },
        { id: 'n2', requestId: 'r2', type: 'request_changed', message: '既読の通知', createdAt: '2030-01-01T00:00:00Z', readAt: '2030-01-02T00:00:00Z' },
      ] } })
      if (path.endsWith('/requests/r1')) return route.fulfill({ json: {
        id: 'r1', organizationId: 'demo-org', requesterUserId: 'peer', targetUserId: 'demo-manager', title: '通知からの設計確認',
        type: 'review', priority: 'normal', status: 'cancelled', durationMinutes: 30, deadlineAt: '2030-02-01T00:00:00Z', createdAt: '2030-01-01T00:00:00Z', options: [],
      } })
      if (path.endsWith('/audit-logs')) return route.fulfill({ json: { auditLogs: [
        { id: 'a1', organizationId: 'demo-org', actorUserId: 'alice', resourceType: 'request', resourceId: 'r1', action: 'request_created', createdAt: '2030-01-01T00:00:00Z' },
        { id: 'a2', organizationId: 'demo-org', actorUserId: 'bob', resourceType: 'request', resourceId: 'r2', action: 'request_cancelled', createdAt: '2030-01-02T00:00:00Z' },
      ] } })
      return route.fulfill({ status: 404, json: {} })
    })
    await page.goto('http://127.0.0.1:5187')
    await page.getByRole('button', { name: '通知', exact: true }).click()
    await expect(page.locator('.notification-entry')).toHaveCount(2)
    await page.getByRole('checkbox', { name: '未読のみ' }).check()
    await page.getByRole('searchbox', { name: '通知を検索' }).fill('abc 日時')
    await expect(page.locator('.notification-entry')).toHaveCount(1)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('notification-search.png'), fullPage: true })
    await page.getByRole('button', { name: '依頼を開く: ＡＢＣ 日時変更' }).click()
    await expect(page.getByRole('heading', { name: '通知からの設計確認' })).toBeVisible()
    await expect(page.getByRole('searchbox', { name: '依頼を検索' })).toHaveValue('r1')
    await expect(page.getByRole('region', { name: '通知一覧' })).toBeHidden()
    await page.getByRole('button', { name: '監査', exact: true }).click()
    await page.getByRole('searchbox', { name: '操作履歴を検索' }).fill('ＡＬＩＣＥ r1')
    await page.getByRole('combobox', { name: '操作種別' }).selectOption('request_created')
    await expect(page.locator('.audit-row')).toHaveCount(1)
    await expect(page.locator('.audit-row')).toContainText('alice')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('audit-search.png'), fullPage: true })
    expect(writes).toEqual([])
  })
}
