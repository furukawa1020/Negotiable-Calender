const labels: Record<string, string> = {
  request_created: '依頼の作成', request_accepted: '日時の確定', request_changed: '依頼の変更',
  reschedule_proposed: '日時変更の提案', reschedule_accepted: '日時変更の承認',
  reschedule_declined: '日時変更の辞退', reschedule_withdrawn: '日時変更の撤回',
  request_declined: '依頼の辞退', request_async: '非同期の回答', request_delegated: '担当の変更',
  request_cancelled: '依頼のキャンセル', request_completed: '依頼の完了',
  invitation_created: '招待の作成', invitation_accepted: '招待の承認', workspace_switched: '組織の切り替え',
}
export const auditActionLabel = (action: string): string => Object.hasOwn(labels, action) ? labels[action] : action
