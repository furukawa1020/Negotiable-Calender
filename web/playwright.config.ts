import { defineConfig } from '@playwright/test'
import { env } from 'node:process'

export default defineConfig({
  testDir: './e2e',
  workers: 1,
  timeout: 45_000,
  use: { browserName: 'chromium', channel: env.UI_BROWSER_CHANNEL || undefined, locale: 'ja-JP', timezoneId: 'Asia/Tokyo', screenshot: 'only-on-failure' },
  webServer: [
    { command: 'npm run dev -- --host 127.0.0.1 --port 5187 --strictPort', url: 'http://127.0.0.1:5187', reuseExistingServer: false },
    { command: 'python -m http.server 5188 --bind 127.0.0.1 --directory ../hosting/public', url: 'http://127.0.0.1:5188', reuseExistingServer: false },
    { command: 'npx vite preview --host 127.0.0.1 --port 5190 --strictPort', url: 'http://127.0.0.1:5190', reuseExistingServer: false },
  ],
})
