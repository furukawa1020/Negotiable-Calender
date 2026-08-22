# Negotiable Calendar 要件定義書 v0.1

## 0. プロダクト定義

### 一文

**管理職の予定そのものを公開せず、「いつ・どの程度・どう関われるか」だけを組織と共有する管理職向けカレンダー。**

### 30字程度

**予定を見せず可用性だけ共有する管理職向けカレンダー**

### プロダクトの核

一般的なカレンダーは、

```text
予定 → 他人に公開
```

Negotiable Calendar は、

```text
非公開の予定
    ↓
Privacy Projection
    ↓
組織が調整に必要な情報だけ公開
```

とする。

他の社員が知るのは、

```text
14:00〜15:00
役員会議
東京オフィス
参加者 A/B/C
```

ではない。

代わりに、

```text
〜15:20   割り込み非推奨
15:30〜   15分相談可能
今日中    承認依頼対応可能
予定変更余地：低
緊急連絡：可能
```

のみを見る。

---

# 1. 解決する課題

## 1.1 現状

管理職は、

* 会議
* 移動
* 顧客対応
* 採用
* 承認
* レビュー
* 1on1
* 集中作業
* 私的予定

など複数の予定を持つ。

しかし部下や他部署から見ると、

```text
予定あり
```

しか分からないことが多い。

結果として、

* 今声をかけてよいか分からない
* いつ承認してもらえるか分からない
* 会議を変更してよいか分からない
* Slackを送ってよいか分からない
* 管理職の予定変更によって複数人が振り回される
* 「空いているように見える時間」と「実際に対応可能な時間」が一致しない

という問題が起きる。

一方、予定を細かく公開すると、

* 行動監視
* 勤務監視
* 会議相手の漏洩
* 顧客情報漏洩
* 私生活の露出
* 「なぜこの時間空いているのか」という詮索

につながる。

---

# 2. Product Principle

本プロダクトでは以下を絶対条件とする。

## P-01 Privacy by Default

予定詳細はデフォルト非公開。

公開するために設定するのではなく、

**非公開予定から必要な情報だけを生成する。**

---

## P-02 Share Affordance, Not Activity

共有するのは、

**「何をしているか」ではなく「どう関われるか」。**

悪い例：

```text
14:00 顧客A 商談
```

良い例：

```text
15:00まで割り込み非推奨
15:15以降相談可能
```

---

## P-03 Request, Don't Inspect

他者は管理職の予定を「覗く」のではなく、

```text
今日中にレビューしてほしい
30分相談したい
緊急で5分話したい
```

という**要求を送る**。

システムが予定と要求を照合する。

---

## P-04 No Hidden Surveillance

管理者権限でも、

* 非公開イベント名
* 非公開説明文
* 非公開参加者
* 非公開場所

を閲覧できない。

**「管理者なら全部見える」は禁止する。**

---

## P-05 User Agency

予定変更・共有・委譲・承認は最終的に本人が決定する。

システムによる自動予定変更は禁止。

---

## P-06 Future Coordination, Not Past Evaluation

目的は、

**未来の調整**

であり、

**過去の管理職評価**

ではない。

したがって、

* 管理職ランキング
* 稼働率ランキング
* 応答速度ランキング
* 「暇な管理職」
* 「効率の悪い管理職」

などは実装しない。

---

# 3. プロダクト独自概念

## 3.1 Private Event

本人だけが保持する通常の予定。

```text
PrivateEvent
```

例：

```text
title: A社との価格交渉
start: 14:00
end: 15:00
location: 東京
attendees: ...
description: ...
```

原則として他ユーザーには返却しない。

---

## 3.2 Schedule Projection

Private Eventから生成された、

**外部公開可能な予定の射影。**

```text
Private Event
      ↓
Projection Engine
      ↓
Schedule Projection
```

Schedule Projectionには予定内容を含めない。

例：

```json
{
  "start": "14:00",
  "end": "15:30",
  "interruptibility": "urgent_only",
  "requestability": "closed",
  "reschedulability": "low",
  "expected_response": "after_15_30"
}
```

---

# 4. 公開する5つの情報

公開情報は原則として以下に限定する。

## 4.1 Availability

```text
available
limited
unavailable
unknown
```

---

## 4.2 Interruptibility

今声をかけてよいか。

```text
open
normal
urgent_only
do_not_interrupt
```

表示：

```text
話しかけてOK
必要なら連絡OK
緊急のみ
割り込み非推奨
```

---

## 4.3 Requestability

新しい依頼を受けられるか。

```text
open
async_only
later
closed
```

---

## 4.4 Reschedulability

その時間帯を動かせる可能性。

```text
high
medium
low
fixed
```

ただし、

**「予定があるから動かせない」のか「本人が動かしたくない」のかを区別して公開しない。**

---

## 4.5 Expected Response Window

依頼に対していつ頃返答できる可能性があるか。

例：

```text
15:30以降
今日中
明日午前
未定
```

---

# 5. 公開してはいけない情報

組織向けAPIでは以下を返さない。

```text
event.title
event.description
event.location
event.attendees
event.organizer
event.conferenceUrl
event.externalId
event.calendarName
event.rawMetadata
```

さらに、

```text
「何件の予定があるか」
```

も原則公開しない。

例えば、

```text
14:00〜15:00に予定3件
```

ではなく、

```text
14:00〜15:00 unavailable
```

としてまとめる。

---

# 6. ユーザー

## 6.1 Manager

主対象。

できること：

* 自分の予定を見る
* Google Calendar等を接続
* Schedule Projectionを見る
* 公開ルールを設定
* 他者からの調整要求を見る
* 要求を承認
* 拒否
* 時間変更
* 委譲
* 公開範囲を変更

---

## 6.2 Member

管理職と仕事をする社員。

できること：

* 管理職のProjectionを見る
* 対応可能性を見る
* 調整要求を送る
* 回答を受け取る

見られないもの：

* 詳細予定
* イベント名
* イベント場所
* イベント参加者

---

## 6.3 Organization Admin

できること：

* メンバー管理
* 管理職設定
* SSO/OAuth設定
* 組織ポリシー設定

できないこと：

* 非公開予定閲覧
* 管理職のPrivate Event閲覧
* 個人の予定詳細エクスポート

---

# 7. 基本画面

## Screen-01 Manager Calendar

本人用カレンダー。

通常の予定詳細を表示する。

各イベントには、

```text
Private
Projection
```

の2レイヤーを表示できる。

例：

```text
本人：

14:00
A社 価格交渉

他者からの見え方：

14:00〜15:30
割り込み非推奨
15:30以降相談可能
```

### 必須操作

* 日
* 週
* 月
* 今日
* 前週
* 次週
* イベントクリック
* Projection確認
* Projection編集

---

# 8. Organization View

組織側から管理職を見る画面。

Google Calendarのような詳細予定表にはしない。

例：

```text
山田さん

09      10      11      12      13      14      15      16

───────
対応可能

        ─────────
        集中時間
        緊急のみ

                        ───────
                        相談可能

                                ───────────
                                15分依頼可能
```

表示する情報：

* 対応可能
* 相談可能
* 非同期対応可能
* 緊急のみ
* 対応困難
* 予定変更余地
* 次に対応できそうな時間

---

# 9. 「予定名」を表示しない

組織ビューでは、

```text
会議
商談
出張
昼食
1on1
採用面接
病院
私用
```

などのカテゴリも原則表示しない。

**カテゴリから予定内容が推測できるため。**

代わりに、

```text
Available
Limited
Unavailable
```

というInteraction Stateのみ表示する。

---

# 10. Coordination Request

本プロダクトの中心機能。

社員は空き時間を探すのではなく、

**目的を送信する。**

例：

```text
依頼内容
----------------
設計レビュー

必要時間
15分

期限
今日17時

同期性
できれば会話
非同期でも可

重要度
通常
```

---

# 11. Request Type

```text
quick_question
meeting
review
approval
decision
async_response
urgent_contact
```

UI表示：

```text
ちょっと質問
相談したい
レビューしてほしい
承認してほしい
判断してほしい
返答がほしい
緊急連絡
```

---

# 12. Scheduling Negotiation

要求：

```text
「今日17時までに15分レビューしてほしい」
```

に対し、システムが候補を生成する。

```text
候補A
15:40〜15:55
直接相談

候補B
16:30までに非同期回答

候補C
別担当者への委譲
```

重要：

**候補生成理由として元の予定を表示してはいけない。**

悪い：

```text
14時に顧客商談があるため無理です
```

良い：

```text
15:40以降で調整可能です
```

---

# 13. Manager Request Inbox

管理職には、

```text
Requests
```

画面を用意する。

例：

```text
今日

[High]
新API設計レビュー
15分
期限 17:00

候補
15:40
16:10
非同期回答

[Accept]
[Change]
[Delegate]
[Decline]
```

---

# 14. Requestへの回答

以下を提供する。

```text
Accept
Suggest another time
Async instead
Delegate
Decline
```

拒否理由は必須にしない。

---

# 15. Privacy Rule

管理職は公開ルールを設定できる。

例：

```text
平日 09:00〜18:00

会議中
→ urgent_only

Focus Event
→ do_not_interrupt

Tentative
→ normal

Free
→ open

18時以降
→ unavailable
```

---

# 16. Rule Priority

ルール優先順位：

```text
Manual Override
>
Event-specific Rule
>
Calendar-specific Rule
>
Organization Rule
>
Default Rule
```

---

# 17. Manual Override

本人は任意の時間帯について、

```text
Open
Normal
Urgent only
Do not disturb
```

を上書きできる。

例：

```text
13:00〜15:00
Do not disturb
```

理由入力不要。

---

# 18. 非勤務時間

非勤務時間については、

```text
Unavailable
```

のみ表示する。

「予定なし」は表示しない。

悪い：

```text
18:00〜23:00 空き
```

良い：

```text
Outside availability
```

---

# 19. Projection Engine

入力：

```text
PrivateEvent[]
SharingPolicy
ManualOverride[]
WorkingHours
PendingRequests[]
```

出力：

```text
ScheduleProjection[]
```

---

# 20. Projection生成単位

最低時間粒度：

```text
15分
```

推奨表示粒度：

```text
30分
```

数分単位の状態変化を公開しない。

目的：

* 行動追跡防止
* 予定開始終了時刻の推測防止

---

# 21. Projection Algorithm

概念実装：

```text
for each time bucket:

    state = defaultPolicy(bucket)

    events = privateEvents overlapping bucket

    for event in events:
        state = combine(
            state,
            applyEventRule(event)
        )

    state = applyOrganizationPolicy(state)

    state = applyManualOverride(state)

    projection.append(state)

mergeAdjacentEqualStates()
```

---

# 22. State Merge

連続した同一状態をまとめる。

入力：

```text
14:00 unavailable
14:15 unavailable
14:30 unavailable
14:45 unavailable
```

出力：

```text
14:00〜15:00 unavailable
```

イベント数は推測できない。

---

# 23. Privacy Leakage対策

## Timing Leakage

ユーザーが何度も問い合わせることで、

```text
14:02 状態A
14:05 状態B
```

のような変化を検知できないようにする。

対策：

* 状態更新をbucket単位に丸める
* 外部表示キャッシュ
* 秒単位更新禁止

---

## Probe Attack

攻撃者が大量のMeeting Requestを送り、

```text
14:00ならOK？
14:15ならOK？
14:30ならOK？
```

と問い合わせて予定を復元する攻撃を防ぐ。

対策：

* Rate Limit
* 候補生成結果を3候補程度に限定
* 任意時刻に対するavailability APIを公開しない

---

# 24. 最重要API原則

以下のAPIは禁止する。

```http
GET /users/{id}/events
GET /users/{id}/calendar
GET /users/{id}/freebusy?start=...
```

代わりに、

```http
GET /users/{id}/projection
POST /requests
```

を提供する。

---

# 25. 認証

OAuth/OIDC。

MVP：

```text
Google Login
```

を必須とする。

組織識別：

```text
Organization
Membership
```

---

# 26. Role

```text
OWNER
ADMIN
MANAGER
MEMBER
```

---

# 27. 権限

### OWNER

```text
organization:update
member:manage
policy:manage
integration:manage
```

### ADMIN

```text
member:manage
policy:manage
```

### MANAGER

```text
private_calendar:read:self
private_calendar:update:self
projection:read:self
projection:update:self
request:manage:self
```

### MEMBER

```text
projection:read:organization
request:create
request:read:self
```

---

# 28. 絶対的アクセス制約

以下は全Roleで禁止。

```text
private_calendar:read:other
private_event:read:other
```

OWNERでも不可。

---

# 29. データモデル

## organizations

```text
id
name
created_at
updated_at
```

---

## users

```text
id
email
display_name
avatar_url
timezone
created_at
updated_at
```

---

## memberships

```text
id
organization_id
user_id
role
created_at
```

---

## calendar_integrations

```text
id
user_id
provider
provider_account_id
encrypted_access_token
encrypted_refresh_token
token_expires_at
sync_cursor
created_at
updated_at
```

---

## private_events

```text
id
user_id
provider_event_id

start_at
end_at

title_encrypted
description_encrypted
location_encrypted
attendees_encrypted

calendar_id

busy_status
visibility

created_at
updated_at
deleted_at
```

---

# 30. private_eventsの原則

Raw Eventは、

```text
Projection Engine
Manager本人
Calendar Sync Worker
```

以外からアクセスしない。

Organization APIから直接Joinしない。

---

# 31. sharing_policies

```text
id
user_id

default_availability
default_interruptibility
default_requestability
default_reschedulability

working_hours_json

created_at
updated_at
```

---

# 32. policy_rules

```text
id
policy_id

condition_type
condition_json

availability
interruptibility
requestability
reschedulability

priority
enabled
```

---

# 33. manual_overrides

```text
id
user_id
start_at
end_at

availability
interruptibility
requestability
reschedulability

expires_at
created_at
```

---

# 34. schedule_projections

Private Eventを含まない独立テーブルにする。

```text
id
user_id

start_at
end_at

availability
interruptibility
requestability
reschedulability

expected_response_bucket

generated_at
expires_at
```

---

# 35. coordination_requests

```text
id
organization_id

requester_user_id
target_user_id

type
title

duration_minutes
deadline_at

sync_preference
priority

status

created_at
updated_at
```

---

# 36. Request Status

```text
pending
suggested
accepted
declined
delegated
cancelled
expired
completed
```

---

# 37. request_options

```text
id
request_id

option_type

start_at
end_at

delegate_user_id

score

created_at
```

option_type:

```text
meeting
async
delegate
decline
```

---

# 38. audit_logs

保存する：

```text
request created
request accepted
request declined
policy changed
integration connected
integration removed
projection changed manually
```

保存しない：

```text
private event title
private event description
private event attendee
private event location
```

---

# 39. Calendar Integration

Google Calendarを初期対象とする。

同期対象：

```text
start
end
busy/free
visibility
event id
```

Projection生成に必要な場合のみ、

```text
title
calendar
event metadata
```

を利用する。

ただしRaw情報を外部レスポンスに含めない。

---

# 40. Source of Truth

Google Calendar接続時：

```text
Google Calendar = Private EventのSource of Truth
Negotiable Calendar = Projection / NegotiationのSource of Truth
```

---

# 41. Calendar Sync

必要：

```text
Initial Sync
Incremental Sync
Webhook通知
Token Refresh
Deleted Event処理
Timezone処理
Recurring Event処理
```

---

# 42. Recurring Event

繰り返し予定は、

```text
series
instance
exception
```

を区別する。

Projection Engineはinstance単位で処理する。

---

# 43. Timezone

全DB保存：

```text
UTC
```

表示：

```text
user.timezone
```

組織内でtimezoneが異なる場合も正常動作させる。

---

# 44. Request Candidate Engine

入力：

```text
request
target_projection
target_policy
working_hours
existing_requests
```

出力：

最大3候補。

---

# 45. 候補探索

例：

```text
duration = 15min
deadline = 17:00

candidate windows:
15:30
15:45
16:15
16:30
```

候補スコア：

```text
score =
  availability_score
+ interruptibility_score
+ reschedulability_score
+ deadline_score
+ fragmentation_penalty
```

---

# 46. Candidate Score

例：

```text
Available          +100
Limited             +40
Unavailable        -1000

Reschedulability
High                 +30
Medium               +10
Low                  -20
Fixed               -100

Deadline proximity
earlier               +20

Schedule fragmentation
new small gap         -20
```

ただし数値はUIには表示しない。

---

# 47. 自動変更禁止

Candidate Engineは、

```text
提案
```

しかし、

```text
予定確定
```

はしない。

ManagerがAcceptした場合のみ確定する。

---

# 48. Negotiation UX

Member：

```text
レビューしてほしい
↓
15分
↓
今日中
↓
Request
```

Manager：

```text
Requestを見る
↓
候補を見る
↓
Accept
```

Member：

```text
15:40〜15:55で確定
```

---

# 49. 「空いてる時間選択」UIを主導線にしない

従来：

```text
カレンダーを見る
↓
空きを探す
↓
時間選択
```

Negotiable Calendar：

```text
目的を書く
↓
期限を書く
↓
必要時間を書く
↓
システムが調整
```

これをUX上の重要差別化とする。

---

# 50. Home

Manager Home：

```text
Today

次に相談可能
15:30〜

Requests
3

Today status
Available until 10:30
Focused 10:30〜12:00
Available after 13:00
```

---

# 51. Member Home

```text
People

山田
相談可能 15:30〜
[Request]

佐藤
今日は非同期対応のみ
[Request]

鈴木
16時以降対応可能
[Request]
```

---

# 52. People Search

検索：

```text
name
team
role
```

表示：

```text
avatar
name
role
interaction status
next requestable window
```

予定内容は表示しない。

---

# 53. Privacy Preview

管理職が必ず、

```text
「他の人からどう見えているか」
```

を確認できる。

ボタン：

```text
View as Member
```

表示内容はOrganization Viewと完全に同一APIを使用する。

**管理職用に模擬表示を別実装しない。**

---

# 54. Privacy Dashboard

表示：

```text
Shared with organization

Availability
ON

Interruptibility
ON

Expected response
ON

Reschedulability
OFF

Event titles
NEVER SHARED

Locations
NEVER SHARED

Attendees
NEVER SHARED
```

---

# 55. 「監視されている感」を防ぐUI原則

禁止：

```text
08:55 出社
09:03 オンライン
09:17 会議開始
10:02 会議終了
```

禁止：

```text
Today 76% occupied
```

禁止：

```text
Average response 18min
```

禁止：

```text
Manager productivity score
```

代わりに未来志向で、

```text
次に相談可能：15:30以降
```

のみ表示する。

---

# 56. Presence機能禁止

以下は取得しない。

```text
mouse activity
keyboard activity
PC login
camera
GPS
Slack online status
Teams presence
browser activity
```

本製品はEmployee Monitoring Toolにしない。

---

# 57. Analytics

組織Analyticsでは個人を評価しない。

許可：

```text
今週処理されたCoordination Request数
平均調整時間
会議化せず非同期で解決した割合
期限内に処理された依頼割合
```

原則、組織単位集計。

---

# 58. Individual Analytics

本人だけは自分について、

```text
Meeting requests
Async requests
Delegations
```

を見ることができる。

組織管理者には個人ランキングとして提供しない。

---

# 59. Security

必須：

```text
TLS
CSRF protection
XSS protection
SQL injection protection
OAuth state validation
PKCE
Secure cookies
HttpOnly cookies
SameSite
Rate limiting
Input validation
```

---

# 60. Token Storage

OAuth Tokenは、

```text
plaintext保存禁止
ログ出力禁止
Frontend送信禁止
```

サーバー側で暗号化保存する。

---

# 61. Logging

禁止：

```text
logger.info(event)
logger.info(calendarResponse)
```

Raw Calendar Objectをそのままログに出さない。

許可：

```text
event_id_hash
sync_status
error_code
duration
```

---

# 62. API Namespace

```text
/api/v1/
```

---

# 63. Auth API

```http
GET  /api/v1/auth/me
POST /api/v1/auth/logout
```

OAuth：

```http
GET /api/v1/auth/google
GET /api/v1/auth/google/callback
```

---

# 64. Projection API

本人：

```http
GET /api/v1/me/projections
GET /api/v1/me/private-events
PUT /api/v1/me/projections/{id}
```

組織：

```http
GET /api/v1/people/{userId}/projection
```

このAPIレスポンスにはPrivate Event情報を絶対含めない。

---

# 65. People API

```http
GET /api/v1/people
GET /api/v1/people/{id}
GET /api/v1/people/{id}/projection
```

---

# 66. Request API

```http
POST /api/v1/requests
GET  /api/v1/requests
GET  /api/v1/requests/{id}

POST /api/v1/requests/{id}/accept
POST /api/v1/requests/{id}/suggest
POST /api/v1/requests/{id}/delegate
POST /api/v1/requests/{id}/decline
POST /api/v1/requests/{id}/cancel
```

---

# 67. Request Create

```json
{
  "targetUserId": "uuid",
  "type": "review",
  "title": "API design review",
  "durationMinutes": 15,
  "deadlineAt": "2026-08-23T08:00:00Z",
  "syncPreference": "either",
  "priority": "normal"
}
```

---

# 68. Request Response

```json
{
  "id": "uuid",
  "status": "suggested",
  "options": [
    {
      "type": "meeting",
      "startAt": "...",
      "endAt": "..."
    },
    {
      "type": "async",
      "responseBy": "..."
    }
  ]
}
```

Private Eventを含めない。

---

# 69. Projection Response

```json
{
  "userId": "uuid",
  "timezone": "Asia/Tokyo",
  "segments": [
    {
      "startAt": "...",
      "endAt": "...",
      "availability": "limited",
      "interruptibility": "urgent_only",
      "requestability": "later",
      "reschedulability": "low"
    }
  ]
}
```

---

# 70. Error Format

全API共通。

```json
{
  "error": {
    "code": "REQUEST_NOT_FOUND",
    "message": "Request not found"
  }
}
```

内部情報・SQL・OAuth Tokenを返さない。

---

# 71. Architecture

```text
┌───────────────────────────┐
│         Web Client        │
└─────────────┬─────────────┘
              │
              ▼
┌───────────────────────────┐
│         API Server        │
├───────────────────────────┤
│ Auth                      │
│ Organization              │
│ Projection API            │
│ Coordination Request API  │
└───┬───────────────┬───────┘
    │               │
    ▼               ▼
┌─────────────┐  ┌─────────────────┐
│ Projection  │  │ Negotiation     │
│ Engine      │  │ Engine          │
└──────┬──────┘  └────────┬────────┘
       │                  │
       └────────┬─────────┘
                ▼
          ┌──────────┐
          │PostgreSQL│
          └──────────┘
                ▲
                │
┌───────────────┴───────────────┐
│ Calendar Sync Worker          │
└───────────────┬───────────────┘
                │
                ▼
          Google Calendar
```

---

# 72. Trust Boundary

特に、

```text
Private Calendar Domain
```

と

```text
Organization Coordination Domain
```

をコードレベルで分離する。

```text
private_event
      ↓
Projection Engine
      ↓
projection DTO
      ↓
organization API
```

Organization APIからPrivateEvent Repositoryを直接呼び出すことを禁止する。

---

# 73. 推奨バックエンド構造

```text
apps/
  api/
  worker/

internal/
  auth/
  organization/
  calendar/
  privateevent/
  projection/
  negotiation/
  request/
  policy/
  audit/

  repository/
  crypto/
```

---

# 74. フロントエンド構造

```text
web/
  src/
    pages/
    components/
    features/
      calendar/
      people/
      projection/
      requests/
      privacy/
      settings/
    api/
    hooks/
    domain/
```

---

# 75. Repository Rule

`projection`から`privateevent`への依存：

```text
OK
```

`organization`から`privateevent`への依存：

```text
NG
```

`request`から取得するのは：

```text
projection
```

PrivateEventではない。

---

# 76. 推奨技術構成

Backend：

```text
Go
PostgreSQL
```

Frontend：

```text
React
Vite
```

Calendar UI：

```text
週表示・日表示を自前コンポーネント化、
またはCalendarライブラリを表示層としてのみ利用
```

認証：

```text
Google OAuth / OIDC
```

Infrastructure：

```text
Docker
Cloud Run等のコンテナ環境
Managed PostgreSQL
Secret Manager
```

---

# 77. AI利用

MVPではLLMを必須にしない。

特に、

```text
Calendar内容を外部LLMへ送信
```

する設計は禁止。

将来的に自然言語Request解析を行う場合でも、

```text
レビューしてほしい。今日17時まで、15分程度。
```

から、

```json
{
  "type": "review",
  "deadline": "...",
  "duration": 15
}
```

を生成する用途に限定する。

---

# 78. Accessibility

必須：

* キーボード操作
* focus state
* aria-label
* 色だけに依存しない状態表現
* mobile responsive

---

# 79. Performance

目標：

```text
通常API p95 < 500ms
Projection取得 p95 < 500ms
Page interactive < 3s
```

Candidate生成：

```text
通常 < 1s
```

---

# 80. Projection Cache

ProjectionはRaw Eventとは別に生成しキャッシュする。

Private Event変更時：

```text
Calendar Event Update
↓
Projection regeneration
↓
Projection Cache Update
```

Organization Viewは原則Projection Cacheのみを見る。

---

# 81. Projection Freshness

UIには内部的に、

```text
generated_at
```

を保持する。

古い場合、

```text
Status temporarily unavailable
```

とする。

Raw Calendarへフォールバックして情報を返してはいけない。

---

# 82. Failure Mode

Google Calendar障害時：

悪い：

```text
予定取得できないのでFreeとして扱う
```

禁止。

正：

```text
unknown
```

として扱う。

Privacy Failureは常に**閉じる方向**に倒す。

---

# 83. Privacy Fail Closed

判断できない場合：

```text
available
```

ではなく、

```text
unknown / unavailable
```

を返す。

---

# 84. Notification

最低限：

```text
Request received
Request accepted
Request changed
Request declined
Request delegated
```

通知チャネル：

MVPではアプリ内。

メール通知は追加可能。

---

# 85. Notification Privacy

通知文に管理職のPrivate Eventを含めない。

---

# 86. Audit

本人が、

```text
誰に何を共有したか
```

を確認できる。

例：

```text
22 Aug 14:00
Organization members received:
- unavailable
- urgent_only

No event details shared
```

---

# 87. Data Export

本人は、

```text
自分のRequest
自分のPolicy
自分のProjection
```

をエクスポート可能。

他人のPrivate Eventsは当然含めない。

---

# 88. Account Deletion

削除時：

```text
OAuth Token削除
Private Event削除
Projection削除
Policy削除
```

Auditは必要最小限の匿名化情報のみ残す。

---

# 89. MVP必須機能

* Google Login
* Organization
* Member / Manager Role
* Google Calendar接続
* Private Event同期
* Projection Engine
* Manager Calendar
* Member Organization View
* View as Member
* Sharing Policy
* Manual Override
* Coordination Request
* Candidate生成
* Accept
* Decline
* Suggest
* Projection専用API
* Privacy-preserving Audit Log

---

# 90. MVPに入れない機能

* Slack監視
* Teams監視
* PC Activity
* GPS
* Employee Productivity Score
* 自動人事評価
* 管理職ランキング
* 完全自動予定変更
* AIによる予定内容解析
* 組織構造自動推論
* 会話録音
* メール本文解析
* 詳細な勤務時間分析

---

# 91. 最重要E2E Scenario A

### 条件

Manager A：

```text
14:00〜15:00
Private Event:
「A社との契約交渉」
```

### Member B

Organization Viewを見る。

### 期待結果

表示：

```text
14:00〜15:00
割り込み非推奨
```

### 絶対に表示されない

```text
A社
契約
交渉
場所
参加者
event id
calendar name
```

---

# 92. E2E Scenario B

Member：

```text
今日17時までに15分レビューしてほしい
```

Manager予定：

```text
15:00〜15:30 Private
16:00〜17:00 Private
```

Projection：

```text
15:30〜16:00 requestable
```

期待：

```text
15:30〜15:45
```

が候補になる。

候補レスポンスに、

```text
15時の予定
16時の予定
```

の存在を示す情報を含めない。

---

# 93. E2E Scenario C

Calendar Sync失敗。

期待：

```text
availability = unknown
```

禁止：

```text
availability = available
```

---

# 94. E2E Scenario D

Organization AdminがManagerの予定APIにアクセス。

期待：

```http
403 Forbidden
```

またはAPI自体が存在しない。

---

# 95. E2E Scenario E

Managerが、

```text
View as Member
```

を押す。

Memberが取得するのと**完全に同じProjection API**から表示する。

---

# 96. E2E Scenario F

Memberが短時間に、

```text
14:00?
14:05?
14:10?
14:15?
```

相当の大量Requestを作成。

期待：

```text
429 Too Many Requests
```

または候補情報を制限する。

---

# 97. Privacy Test

テストコードで、

Organization APIのJSONに以下キーが含まれていないことを検査する。

```text
title
description
location
attendees
organizer
conferenceUrl
providerEventId
```

---

# 98. Repository Boundary Test

CIでOrganization DTOに、

```text
PrivateEvent
```

型を利用していないことを確認する。

可能であればパッケージ依存を静的解析する。

---

# 99. Security Test

最低限：

```text
IDOR
Broken Access Control
OAuth CSRF
XSS
SQL Injection
Rate Limit
Token exposure
Log exposure
```

についてテストする。

---

# 100. UX Acceptance Criteria

新規Memberが説明なしで、

**30秒以内に**

```text
「この人にいつ相談できそうか」
```

を理解できる。

新規Managerが、

**1分以内に**

```text
「他人から自分の予定がどう見えているか」
```

を確認できる。

---

# 101. Privacy Acceptance Criteria

ユーザーに、

```text
このサービスを使うと、部下から監視されていると感じますか？
```

と質問する。

主要UX評価では、

```text
「予定内容を見られている」
```

と誤解されないことを重視する。

---

# 102. Product Success Metric

North Star候補：

```text
Coordination Requests resolved without exposing calendar details
```

日本語：

**予定詳細を公開せず成立した調整数**

---

# 103. Secondary Metrics

```text
Request resolution rate
Median time to resolution
Async resolution rate
Delegation rate
Declined request rate
Manual override usage
Privacy preview usage
```

---

# 104. Guardrail Metrics

プロダクト成長より優先する。

```text
Privacy concern reports
Unexpected disclosure reports
Admin access violations
Projection mismatch reports
```

---

# 105. デモ用データ

Manager：

```text
山田 太郎
事業責任者
```

Private Calendar：

```text
09:00 Product Review
10:00 Customer Meeting
11:30 Focus
13:00 Recruiting Interview
14:30 Executive Meeting
16:00 Free
```

Memberには：

```text
09:00〜10:00 相談可能
10:00〜11:30 緊急のみ
11:30〜13:00 割り込み非推奨
13:00〜15:30 対応困難
15:30〜        相談可能
```

のみ表示する。

---

# 106. デモの決定的瞬間

画面を左右に分割する。

左：

```text
MY CALENDAR
```

詳細予定が大量に並ぶ。

右：

```text
WHAT OTHERS SEE
```

表示されるのは、

```text
Available
Urgent only
Available after 15:30
```

だけ。

その後Memberが、

```text
「今日中に15分レビュー」
```

を送る。

Manager側に、

```text
15:40〜15:55
```

が提案される。

**予定を一つも公開せず調整が成立する。**

これをプロダクトの中心デモとする。

---

# 107. プロダクトコピー

第一候補：

**予定を見せずに、予定を共有する。**

説明：

**管理職の予定内容を公開せず、「いつ・どう関われるか」だけを組織と共有するカレンダー。**

---

# 108. UIコピー原則

避ける：

```text
暇
忙しい
稼働率
監視
追跡
効率スコア
生産性
```

使用：

```text
相談可能
対応可能
緊急のみ
後で対応可能
依頼する
調整する
共有範囲
```

---

# 109. GitHub Issue構成

## Issue 001 — Project bootstrap

* Web
* API
* PostgreSQL
* Docker
* lint
* test
* CI

### Done

```text
Web / API / DBがローカル起動する。
CIでbuild/testが通る。
```

---

## Issue 002 — Domain model

実装：

```text
Organization
User
Membership
PrivateEvent
ScheduleProjection
SharingPolicy
CoordinationRequest
```

---

## Issue 003 — Authentication

* Google OAuth
* session
* logout
* auth middleware

---

## Issue 004 — Organization RBAC

* OWNER
* ADMIN
* MANAGER
* MEMBER

PrivateEventへの他者アクセス禁止テスト必須。

---

## Issue 005 — Google Calendar integration

* connect
* disconnect
* token encryption
* initial sync
* incremental sync

---

## Issue 006 — Private Event repository

Private Event専用repository。

Organization packageからimport禁止。

---

## Issue 007 — Projection Engine

入力：

```text
PrivateEvent + Policy
```

出力：

```text
ScheduleProjection
```

---

## Issue 008 — Projection merge

隣接bucket統合。

イベント数を露出しない。

---

## Issue 009 — Sharing Policy

* working hours
* interruptibility
* requestability
* reschedulability

---

## Issue 010 — Manual Override

任意時間帯にInteraction Stateを設定。

---

## Issue 011 — Manager Calendar

本人専用カレンダー。

---

## Issue 012 — Privacy Preview

```text
View as Member
```

---

## Issue 013 — Organization People View

管理職一覧＋Projection。

---

## Issue 014 — Coordination Request create

MemberからManagerへRequest送信。

---

## Issue 015 — Candidate Engine

最大3候補生成。

---

## Issue 016 — Request Inbox

Manager側Request一覧。

---

## Issue 017 — Accept / Decline / Suggest

状態遷移を実装。

---

## Issue 018 — Delegate

別ユーザーへの委譲。

---

## Issue 019 — Notifications

アプリ内Notification。

---

## Issue 020 — Audit Log

Privacy-safe audit。

---

## Issue 021 — Privacy attack tests

* probing
* IDOR
* raw field leakage
* admin override
* timing leakage

---

## Issue 022 — Demo mode

Seed Dataでプロダクト価値を即確認可能にする。

---

# 110. Pull Request Rule

1 PR = 1 responsibility。

禁止：

```text
feat: implement everything
```

例：

```text
feat(projection): add schedule projection domain model

feat(projection): generate availability buckets

feat(projection): merge adjacent segments

feat(request): add coordination request creation

feat(request): generate meeting candidates
```

---

# 111. Definition of Done

各Issueは以下を満たす。

```text
Implementation complete
Unit tests
Integration tests where required
Authorization checked
Privacy leakage checked
Error state handled
Loading state handled
Empty state handled
README/API docs updated
CI passes
```

---

# 112. Codex実装ルール

Codexには以下を遵守させる。

```text
1. 未実装をTODOで誤魔化さない。
2. IssueのAcceptance Criteriaを満たしてから終了する。
3. PrivateEventをOrganization向けDTOに使用しない。
4. API変更時はテストを追加する。
5. 認可チェックをhandlerだけに依存しない。
6. OAuth tokenをログに出さない。
7. private event raw dataをログに出さない。
8. mockだけで正常扱いしない。
9. エラーを握り潰さない。
10. privacy failureはfail closedにする。
11. UIに予定タイトルを誤って露出しない。
12. 1 PRの変更範囲を限定する。
```

---

# 113. Codexに最初に渡す指示

```text
このリポジトリでは docs/requirements.md を仕様のSource of Truthとする。

最重要設計原則は、
「Private Calendarの内容をOrganization側へ公開せず、
調整に必要なInteraction StateのみをSchedule Projectionとして共有する」
ことである。

PrivateEventとScheduleProjectionは異なるTrust Domainとして扱うこと。

Organization向けAPIからPrivateEvent repositoryを直接参照してはいけない。

管理者であっても他人のPrivateEventを取得できるAPIを作ってはいけない。

各Issueについて、
1. 実装
2. Unit Test
3. 必要なIntegration Test
4. Authorization Test
5. Privacy Leakage Test
を行うこと。

不明点がある場合も、
利便性よりPrivacy Preserving側に倒すこと。
```

---

# 114. このプロダクトで絶対に壊してはいけないもの

このサービスの価値は、

**「管理職のカレンダーを詳しく共有できる」**

ことではない。

価値は、

**「詳しく共有しなくても、詳しく共有した場合と同程度に組織が調整できる」**

ことである。

したがって、

```text
共有情報を増やして便利にする
```

という方向に進めてはいけない。

常に、

```text
共有情報を減らしたまま
調整能力を上げる
```

方向に進化させる。

---

# 115. 最終Product Thesis

```text
Calendars currently expose schedules
so that people can coordinate.

Negotiable Calendar exposes
only the ability to coordinate.
```

日本語：

**従来のカレンダーは調整のために予定を共有する。**

**Negotiable Calendarは、予定ではなく「調整可能性」そのものを共有する。**
