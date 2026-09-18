import { useState } from 'react'

export function AccountAvatar({ name, imageUrl }: { name: string; imageUrl?: string }) {
  const [failedSource, setFailedSource] = useState('')
  let source = ''
  try {
    const url = new URL(imageUrl ?? '')
    if (url.protocol === 'https:' && !url.username && !url.password) source = url.href
  } catch { /* A missing or invalid image uses the account initial. */ }

  if (source && source !== failedSource) {
    return <img className="account-avatar-image" src={source} alt={`${name}のプロフィール画像`}
      referrerPolicy="no-referrer" onError={() => setFailedSource(source)} />
  }
  return <span aria-hidden="true">{Array.from(name.trim())[0] ?? '?'}</span>
}
