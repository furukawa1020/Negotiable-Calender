export class WorkspaceRejected extends Error {}
export async function workspaceError(response: Response): Promise<never> {
  throw [400, 401, 403, 404, 409, 422].includes(response.status) ? new WorkspaceRejected() : new Error('workspace outcome unknown')
}
export async function readWorkspace(response: Response, expectedID: string): Promise<{ id: string; name: string; role: string }> {
  const value = (await response.json())?.activeWorkspace
  if (!value || value.id !== expectedID || typeof value.name !== 'string' || !value.name.trim()
    || !['OWNER', 'ADMIN', 'MANAGER', 'MEMBER'].includes(value.role)) throw new Error('workspace acknowledgement invalid')
  return { id: value.id, name: value.name, role: value.role }
}
export async function readInvitationURL(response: Response): Promise<string> {
  const value = await response.json()
  if (!value || typeof value.inviteUrl !== 'string') throw new Error('invitation acknowledgement invalid')
  const url = new URL(value.inviteUrl)
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password || !url.searchParams.get('invite')) throw new Error('invitation URL invalid')
  return value.inviteUrl
}
