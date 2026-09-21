export function calendarConsentNotice(result: string | null): string {
  switch (result) {
    case 'denied':
      return 'Google Calendarの接続は許可されませんでした。既存の接続は変更していません。Google画面に403が表示された場合は、接続ヘルプを確認してください。'
    case 'permission_required':
      return '予定の読み取り権限または継続同期の許可を確認できませんでした。既存の接続は変更していません。接続をやり直し、Googleの同意画面で読み取り権限を確認してください。'
    case 'exchange_failed':
      return 'Googleから接続の許可を確認できませんでした。既存の接続は変更していません。接続をやり直してください。繰り返す場合は運営者へお問い合わせください。'
    case 'provider_failed':
      return 'Google側で接続手続きを完了できませんでした。既存の接続は変更していません。時間をおいて接続をやり直してください。'
    default:
      return ''
  }
}
