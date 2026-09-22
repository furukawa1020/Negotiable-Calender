import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { PolicyLinks } from './PolicyLinks'

it('links to the same published policy origin without losing the current task or sending a referrer', () => {
  render(<PolicyLinks />)
  expect(screen.getByRole('navigation', { name: 'サービスの利用条件' })).toBeInTheDocument()
  for (const [name, path] of [['プライバシーポリシー', 'privacy.html'], ['利用規約', 'terms.html']]) {
    const link = screen.getByRole('link', { name: `${name}（別タブ）` })
    expect(link).toHaveAttribute('href', `https://negotiable-calendar-480760.web.app/${path}`)
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  }
})
