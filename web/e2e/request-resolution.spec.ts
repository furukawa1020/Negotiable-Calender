import { expect, test } from '@playwright/test'

// Synthetic users and requests only; server-side concurrency is tested against
// the isolated Firestore emulator and Postgres database in their integration suites.
for (const action of ['async', 'decline', 'cancel'] as const) {
  test(`recover ${action} after a committed response is lost`, async ({ page }) => {
    const actor = action === 'cancel' ? 'alice' : 'bob'
    const value = {
      id: 'resolution-fixture', requesterUserId: 'alice', targetUserId: 'bob', title: '資料の確認',
      type: 'review', durationMinutes: 15, deadlineAt: '2099-10-02T08:00:00Z', priority: 'normal',
      status: 'suggested', asyncMessage: '', options: [],
    }
    const submissions: (string | null)[] = []
    await page.route('**/api/v1/**', async route => {
      const request = route.request()
      const path = new URL(request.url()).pathname
      let json: object = {}
      if (path.endsWith('/auth/session')) json = { authenticated: true, user: { userId: actor, organizationId: 'org', displayName: '操作テスト', email: `${actor}@example.com`, role: 'MANAGER' } }
      if (path.endsWith('/calendar/connection')) json = { connected: false }
      if (path.endsWith('/workspaces')) json = { activeWorkspaceId: 'org', workspaces: [{ id: 'org', name: '試験組織', role: 'MANAGER' }] }
      if (path.endsWith('/requests')) json = { requests: [value] }
      if (path.endsWith('/projection')) json = { segments: [] }
      if (path.endsWith(`/${action}`) && request.method() === 'POST') {
        submissions.push(request.postData())
        expect(request.headers()['x-organization-id']).toBe('org')
        value.status = { async: 'async', decline: 'declined', cancel: 'cancelled' }[action]
        if (action === 'async') value.asyncMessage = request.postDataJSON().message
        if (submissions.length === 1) return route.abort('failed')
        return route.fulfill({ status: 200, json: value, headers: { 'Idempotency-Replayed': 'true' } })
      }
      return route.fulfill({ status: 200, json })
    })
    await page.goto('http://127.0.0.1:5190/?auth=success')
    await expect(page.getByRole('button', { name: '操作テストのアカウントメニュー' })).toBeVisible()
    const view = action === 'cancel' ? '送信済み' : '依頼'
    await page.getByRole('navigation').getByRole('button', { name: view, exact: true }).click()
    await expect(page.getByRole('heading', { name: '資料の確認' })).toBeVisible()
    if (action === 'async') await page.getByLabel('非同期メッセージ').fill('文書の内容で進めてください <b>安全な本文</b>')
    const label = { async: '非同期で回答', decline: '今回は辞退', cancel: '依頼をキャンセル' }[action]
    await page.getByRole('button', { name: label, exact: true }).click()
    await expect(page.getByRole('status')).toContainText('同じ操作・同じ回答文')
    if (action === 'async') await expect(page.getByLabel('非同期メッセージ')).toHaveValue('文書の内容で進めてください <b>安全な本文</b>')
    await page.getByRole('button', { name: label, exact: true }).click()
    await expect(page.getByRole('button', { name: label, exact: true })).toBeHidden()
    expect(submissions).toHaveLength(2)
    expect(submissions[1]).toEqual(submissions[0])
    await page.reload()
    await expect(page.getByRole('button', { name: '操作テストのアカウントメニュー' })).toBeVisible()
    await page.getByRole('navigation').getByRole('button', { name: view, exact: true }).click()
    await expect(page.getByRole('heading', { name: '資料の確認' })).toBeVisible()
    await expect(page.getByRole('button', { name: label, exact: true })).toBeHidden()
    if (action === 'async') {
      await expect(page.locator('.async-message')).toHaveText(value.asyncMessage)
      await expect(page.locator('.async-message b')).toHaveCount(0)
    }
  })
}
