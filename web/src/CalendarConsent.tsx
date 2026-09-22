import { PolicyLinks } from './PolicyLinks'

export function CalendarConnectionHelp() {
  return (
    <details className="calendar-consent-help">
      <summary>カレンダー接続で403などが出る場合</summary>
      <p>Googleログインとカレンダーの接続は別の許可です。ログインできても、Google側の公開設定や権限審査によりカレンダー接続が止まる場合があります。</p>
      <p>Google画面に「403: access_denied」「テスト中」「確認が完了していない」と表示される場合は、運営者がOAuthの公開・審査状態を確認する必要があります。利用者によるテストユーザー登録や警告の回避は解決手順ではありません。</p>
      <p>問い合わせには、発生日時、Google画面かアプリ画面か、エラー本文だけをお知らせください。アドレスバーのURL全体、認証コード、Cookie、秘密鍵は送らないでください。</p>
      <a href="mailto:f.kotaro.0530@gmail.com">運営者に問い合わせる</a>
    </details>
  )
}

export function CalendarConsent({ connectURL, reconnect = false }: { connectURL: string; reconnect?: boolean }) {
  return (
    <section className="calendar-consent" aria-label="カレンダー接続前の確認">
      <p>Googleカレンダーを読み取り、自分の予定表示と相談可能な時間帯の計算に使います。現在の同期・表示対象はメインカレンダーです。Google上の予定は作成・変更しません。</p>
      <p>今回要求する権限は「自分が所有するカレンダーの予定の読み取り」です。他人が所有する共有カレンダーやカレンダー設定の読み取り権限は要求しません。以前に許可したGoogle側の権限が、この更新だけで取り消されるわけではありません。</p>
      <p>予定名・説明・場所・参加者などの詳細は本人向けに取得・表示し、同期用データベースには保存しません。予定の識別子・開始終了時刻・busy状態・同期情報を保存し、継続同期のための認証情報は暗号化して保管します。組織には予定詳細ではなく共有ルールから作った公開状態を表示します。</p>
      <p>接続後はメニューの「Calendar接続を解除」から連携情報と同期用予定データを削除し、公開状態の共有を停止できます。カレンダー接続は任意です。</p>
      <PolicyLinks />
      <a href={connectURL}>{reconnect ? 'Google Calendarを再接続' : 'Google Calendarを接続'}</a>
      <CalendarConnectionHelp />
    </section>
  )
}
