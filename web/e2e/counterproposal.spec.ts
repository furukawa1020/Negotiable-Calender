import { expect, test } from '@playwright/test'

test('target proposes, requester agrees, and both can export the confirmed meeting', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 1000 })
  let actor = 'bob', proposals = 0, approvals = 0
  const original = { id: 'generated', type: 'meeting', startAt: '2099-01-01T01:00:00.000Z', endAt: '2099-01-01T01:30:00.000Z', proposedByUserId: '' }
  const offer = { ...original, id: 'offer', requestId: 'r', startAt: '2099-01-02T01:00:00.000Z', endAt: '2099-01-02T01:30:00.000Z', proposedByUserId: 'bob' }
  const value = { id: 'r', requesterUserId: 'alice', targetUserId: 'bob', title: '別時間の合意確認', durationMinutes: 30, deadlineAt: '2099-01-03T00:00:00Z', priority: 'normal', status: 'suggested', acceptedOptionId: '', options: [original] }
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    let json: object = {}
    if (path.endsWith('/auth/session')) json = { authenticated: true, user: { userId: actor, organizationId: 'org', displayName: actor, email: `${actor}@example.com`, role: 'MANAGER' } }
    if (path.endsWith('/calendar/connection')) json = { connected: false }
    if (path.endsWith('/workspaces')) json = { activeWorkspaceId: 'org', workspaces: [{ id: 'org', name: '試験組織', role: 'MANAGER' }] }
    if (path.endsWith('/requests')) json = { requests: (url.searchParams.get('scope') === 'sent' ? actor === 'alice' : actor === 'bob') ? [value] : [] }
    if (path.endsWith('/projection')) json = { segments: [] }
    if (path.endsWith('/suggest')) {
      expect(actor).toBe('bob'); expect(request.postDataJSON()).toEqual({ startAt: offer.startAt, endAt: offer.endAt })
      proposals++; value.options = [original, offer]
      if (proposals === 1) return route.abort('failed')
      json = offer
    }
    if (path.endsWith('/accept')) {
      expect(actor).toBe('alice'); expect(request.postDataJSON()).toEqual({ optionId: 'offer' })
      approvals++; value.status = 'accepted'; value.acceptedOptionId = 'offer'
      if (approvals === 1) return route.abort('failed')
      json = { id: 'r', status: 'accepted', acceptedOptionId: 'offer' }
    }
    if (path.endsWith('/calendar.ics')) return route.fulfill({ status: 200, contentType: 'text/calendar', body: 'BEGIN:VCALENDAR\r\nVERSION:2.0\r\nEND:VCALENDAR\r\n' })
    await route.fulfill({ status: 200, json })
  })
  const login = async (user: string, view: string) => {
    actor = user; await page.goto('http://127.0.0.1:5190/?auth=success')
    await expect(page.getByRole('button', { name: `${user}のアカウントメニュー` })).toBeVisible()
    await page.getByRole('navigation').getByRole('button', { name: view, exact: true }).click()
  }
  await login('bob', '依頼')
  await page.getByLabel('別の開始時間').fill('2099-01-02T10:00')
  await page.getByLabel('終了時間', { exact: true }).fill('2099-01-02T10:30')
  await page.getByRole('button', { name: '別時間を提案' }).click()
  await expect(page.getByRole('alert')).toContainText('同じ日時で再試行')
  await expect(page.getByLabel('別の開始時間')).toBeDisabled()
  await page.getByRole('button', { name: '別時間を提案' }).click()
  await expect(page.getByText('依頼者の承認待ち', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'この候補を承認' })).toHaveCount(1)
  await login('alice', '送信済み')
  await expect(page.getByRole('button', { name: 'この候補を承認' })).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
  await page.getByRole('button', { name: 'この提案を承認' }).click()
  await expect(page.getByRole('alert')).toContainText('同じ提案への再試行')
  await page.getByRole('button', { name: 'この提案を承認' }).click()
  await expect(page.getByRole('region', { name: '確定した会議' }).locator('time').first()).toHaveAttribute('datetime', offer.startAt)
  for (const user of ['alice', 'bob']) {
    await login(user, user === 'alice' ? '送信済み' : '依頼')
    const meeting = page.getByRole('region', { name: '確定した会議' })
    await expect(meeting.locator('time').first()).toHaveAttribute('datetime', offer.startAt)
    const download = page.waitForEvent('download')
    await meeting.getByRole('button', { name: 'カレンダーに登録（ICS）' }).click()
    expect((await download).suggestedFilename()).toBe('negotiable-meeting.ics')
  }
  expect(proposals).toBe(2); expect(approvals).toBe(2)
})
