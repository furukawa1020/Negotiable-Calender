export type RequestPeople = Readonly<Record<string, string>>

export async function readRequestPeople(response: Response): Promise<RequestPeople> {
  const value: unknown = await response.json()
  if (!value || typeof value !== 'object' || !('people' in value) || !Array.isArray(value.people)) throw new Error('invalid request directory')
  const names: Record<string, string> = Object.create(null)
  for (const row of value.people) {
    if (!row || typeof row !== 'object' || typeof row.id !== 'string' || !row.id.trim()
      || typeof row.displayName !== 'string' || !row.displayName.trim() || Object.hasOwn(names, row.id)) throw new Error('invalid request directory')
    names[row.id] = row.displayName
  }
  return names
}
