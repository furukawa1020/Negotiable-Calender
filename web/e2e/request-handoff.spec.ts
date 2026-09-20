import { expect, test } from '@playwright/test'

test('original owner hands off, new owner answers, requester sees the result', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 1000 })
  let actor = 'bob'
  let handoffs = 0
  const value = { id: 'r', requesterUserId: 'alice', targetUserId: 'bob', delegatedFromUserId: '', title: '引継ぎ動作の確認', durationMinutes: 15, deadlineAt: '2099-01-01T00:00:00Z', priority: 'normal', status: 'suggested', asyncMessage: '', options: [] }
  await page.route('**/api/v1/**', async route => {
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname
    let json: object = {}
    if (path.endsWith('/auth/session')) json = { authenticated: true, user: { userId: actor, organizationId: 'org', displayName: actor, email: `${actor}@example.com`, role: 'MANAGER' } }
    if (path.endsWith('/calendar/connection')) json = { connected: false }
    if (path.endsWith('/workspaces')) json = { activeWorkspaceId: 'org', workspaces: [{ id: 'org', name: '試験組織', role: 'MANAGER' }] }
    if (path.endsWith('/people')) json = { people: [{ id: 'alice', displayName: '依頼者' }, { id: 'bob', displayName: '元担当' }, { id: 'carol', displayName: '引継ぎ担当' }] }
    if (path.endsWith('/requests')) json = { requests: (url.searchParams.get('scope') === 'sent' ? actor === value.requesterUserId : actor === value.targetUserId) ? [value] : [] }
    if (path.endsWith('/projection')) json = { segments: [] }
    if (path.endsWith('/delegate')) {
      expect(request.postDataJSON()).toEqual({ delegateUserId: 'carol' })
      handoffs++; value.targetUserId = 'carol'; value.delegatedFromUserId = 'bob'
      if (handoffs === 1) return route.abort('failed')
      json = { id: 'r', handedOff: true, delegatedUserId: 'carol' }
    }
    if (path.endsWith('/async')) { expect(actor).toBe('carol'); value.status = 'async'; value.asyncMessage = request.postDataJSON().message; json = value }
    await route.fulfill({ status: 200, json })
  })
  const login = async (user: string, view: string) => {
    actor = user; await page.goto('http://127.0.0.1:5190/?auth=success')
    await expect(page.getByRole('button', { name: `${user}のアカウントメニュー` })).toBeVisible()
    await page.getByRole('navigation').getByRole('button', { name: view, exact: true }).click()
  }
  await login('bob', '依頼')
  await page.getByRole('button', { name: '担当を引き継ぐ' }).click()
  await page.getByLabel('引継ぎ先', { exact: true }).selectOption('carol')
  await expect(page.getByText(/閲覧・回答できなくなります/)).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
  await page.getByRole('button', { name: 'この相手へ引き継ぐ' }).click()
  await expect(page.getByRole('alert')).toContainText('同じ相手への再試行')
  await page.getByRole('button', { name: 'この相手へ引き継ぐ' }).click()
  await expect(page.getByRole('heading', { name: value.title })).toBeHidden()
  await login('bob', '依頼'); await expect(page.getByRole('heading', { name: value.title })).toBeHidden()
  await login('carol', '依頼')
  await expect(page.getByText('引き継いだ依頼です。再委譲はできません。')).toBeVisible()
  await expect(page.getByRole('button', { name: '担当を引き継ぐ' })).toBeHidden()
  await page.getByLabel('非同期メッセージ').fill('確認できました')
  await page.getByRole('button', { name: '非同期で回答', exact: true }).click()
  await expect(page.locator('.async-message')).toHaveText('確認できました')
  await login('alice', '送信済み')
  await expect(page.locator('.async-message')).toHaveText('確認できました')
  await expect(page.getByText('担当変更済み · 現在の担当: carol')).toBeVisible()
  expect(handoffs).toBe(2)
})
