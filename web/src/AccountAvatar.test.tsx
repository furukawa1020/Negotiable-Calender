import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AccountAvatar } from './AccountAvatar'

describe('AccountAvatar', () => {
  it('uses the actual photo and recovers to the users initial on failure', () => {
    const { rerender } = render(<AccountAvatar name="Person" imageUrl="https://lh3.googleusercontent.com/test-photo" />)
    const photo = screen.getByRole('img', { name: 'Personのプロフィール画像' })
    expect(photo).toHaveAttribute('referrerpolicy', 'no-referrer')
    fireEvent.error(photo)
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByText('P')).toBeInTheDocument()
    rerender(<AccountAvatar name="Another" imageUrl="https://lh3.googleusercontent.com/next-photo" />)
    expect(screen.getByRole('img', { name: 'Anotherのプロフィール画像' })).toHaveAttribute('src', 'https://lh3.googleusercontent.com/next-photo')
  })

  it.each([undefined, '', 'not-a-url', 'http://example.test/avatar', 'javascript:alert(1)', 'data:image/png;base64,abc', 'https://user:password@example.test/avatar'])('falls back for missing or unsafe image %s', (imageUrl) => {
    render(<AccountAvatar name="  高橋 花子  " imageUrl={imageUrl} />)
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByText('高')).toBeInTheDocument()
  })
})
