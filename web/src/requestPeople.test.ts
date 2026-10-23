import { expect, it } from 'vitest'
import { readRequestPeople } from './requestPeople'

it.each([null, {}, { people: null }, { people: [null] }, { people: [{ id: '', displayName: 'Alice' }] }, { people: [{ id: 'a', displayName: {} }] }, { people: [{ id: 'a', displayName: 'Alice' }, { id: 'a', displayName: 'Bob' }] }])('rejects invalid directories atomically', async value => {
  await expect(readRequestPeople(Response.json(value))).rejects.toThrow('invalid request directory')
})
it('accepts empty directories and opaque IDs without prototype collisions', async () => {
  expect(await readRequestPeople(Response.json({ people: [] }))).toEqual({})
  const names = await readRequestPeople(Response.json({ people: [{ id: '__proto__', displayName: '利用者' }] }))
  expect(names.__proto__).toBe('利用者')
  expect(Object.getPrototypeOf(names)).toBeNull()
})
