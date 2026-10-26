import { expect, test } from '@playwright/test'

for (const width of [1440, 1024, 390, 320]) {
  test(`loaded request and activity workflows at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 })
    const writes: string[] = []
    await page.route('**/api/v1/**', route => {
      const req = route.request(), path = new URL(req.url()).pathname
      if (req.method() !== 'GET') writes.push(path)
      if (path.endsWith('/notifications')) return route.fulfill({ json: { notifications: Array.from({ length: 23 }, (_, i) => ({
        id: `n${String(i).padStart(2, '0')}`, requestId: `r${i}`, type: i === 22 ? 'request_accepted' : 'request_changed', message: `通知 ${i}`, createdAt: '2030-01-01T00:00:00Z',
      })) } })
      if (path.endsWith('/audit-logs')) return route.fulfill({ json: { auditLogs: Array.from({ length: 23 }, (_, i) => ({
        id: `a${String(i).padStart(2, '0')}`, organizationId: 'demo-org', actorUserId: 'alice', resourceType: 'request', resourceId: `r${i}`, action: 'request_created', createdAt: i === 22 ? '2030-01-03T00:00:00Z' : '2030-01-01T00:00:00Z',
      })) } })
      if (path.endsWith('/requests')) return route.fulfill({ json: { requests: [
        { id: 'later', start: '2030-01-04T00:00:00Z', end: '2030-01-04T00:30:00Z', status: 'accepted' },
        { id: 'earlier', start: '2030-01-03T00:00:00Z', end: '2030-01-03T00:30:00Z', status: 'accepted' },
        { id: 'cancelled', start: '2030-01-02T00:00:00Z', end: '2030-01-02T00:30:00Z', status: 'cancelled' },
      ].map(row => ({ id: row.id, requesterUserId: 'peer', targetUserId: 'demo-manager', title: row.id, type: 'meeting', priority: 'normal', status: row.status, acceptedOptionId: 'o', durationMinutes: 30, createdAt: '2030-01-01T00:00:00Z', deadlineAt: '2030-02-01T00:00:00Z', options: [{ id: 'o', type: 'meeting', startAt: row.start, endAt: row.end }] })) } })
      return route.fulfill({ status: 404, json: {} })
    })
    await page.goto('http://127.0.0.1:5187')
    await page.getByRole('button', { name: '通知', exact: true }).click()
    await expect(page.locator('.notification-entry')).toHaveCount(20)
    await page.getByRole('navigation', { name: '通知のページ送り' }).getByRole('button', { name: '次のページ' }).click()
    await expect(page.locator('.notification-entry')).toHaveCount(3)
    await page.getByRole('combobox', { name: '通知種別' }).selectOption('request_accepted')
    await expect(page.locator('.notification-entry')).toHaveCount(1)
    await expect(page.locator('.notification-entry')).toContainText('通知 22')
    await page.getByRole('button', { name: '通知の絞り込みを解除' }).click()
    await expect(page.getByText('1 / 2 ページ')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('notification-pagination.png'), fullPage: true })
    await page.getByRole('button', { name: '通知', exact: true }).click()
    await page.getByRole('button', { name: '依頼', exact: true }).click()
    await page.getByRole('combobox', { name: '状態', exact: true }).selectOption('active')
    await page.getByRole('combobox', { name: '並び順' }).selectOption('meeting')
    await expect(page.locator('.request-summary h2')).toHaveText(['earlier', 'later'])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('request-meeting-order.png'), fullPage: true })
    await page.getByRole('button', { name: '監査', exact: true }).click()
    await expect(page.locator('.audit-row')).toHaveCount(20)
    await page.getByRole('navigation', { name: '操作履歴のページ送り' }).getByRole('button', { name: '次のページ' }).click()
    await expect(page.locator('.audit-row')).toHaveCount(3)
    await page.getByLabel('履歴の開始日').fill('2030-01-03')
    await page.getByLabel('履歴の終了日').fill('2030-01-03')
    await expect(page.locator('.audit-row')).toHaveCount(1)
    await expect(page.locator('.audit-row')).toContainText('r22')
    await page.getByLabel('履歴の終了日').fill('2030-01-02')
    await expect(page.getByRole('alert')).toContainText('終了日は開始日以降')
    await page.getByRole('button', { name: '履歴の絞り込みを解除' }).click()
    await expect(page.getByText('1 / 2 ページ')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('audit-date-pagination.png'), fullPage: true })
    expect(writes).toEqual([])
  })
}
