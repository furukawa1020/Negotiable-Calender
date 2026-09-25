# Client bootstrap teardown isolation (#163, related #94)

Initial session restoration loads identity, Calendar connection, workspaces and
optional invitation preview in sequence. Each asynchronous boundary now checks
the effect's cancellation flag and the captured calendar lifecycle epoch before
applying state or issuing the next request. Obsolete errors cannot overwrite the
current notice; obsolete completion cannot rewrite a newly navigated URL.

Successful logout and account deletion invalidate the epoch synchronously, before
React effect cleanup, and clear Calendar/invitation/workspace remnants and old
callback parameters. Deletion's OWNER conflict and unsuccessful responses do not
advance the epoch: a rejected deletion must not invalidate an active session.
Existing private-read and manual-sync guards therefore also reject responses that
arrive after successful deletion, including before effect cleanup runs.

Deterministic UI tests reproduced three pre-fix failures: bootstrap continued
network requests after logout; unmounted bootstrap continued with another request;
late manual sync overwrote account-deletion success. They also verify rejected
deletion permits current sync, invitation transport errors after logout are ignored,
and StrictMode cleanup does not allow an old identity to replace the current one.
No real account deletion or OAuth consent is used in these tests.

Requests already sent may finish; this change discards obsolete results, it does
not promise to cancel a server-side action. This is scoped client-state isolation,
not a replacement for server authorization/lifecycle fencing or a verification of
every asynchronous UI action. Actual Google consent and account acceptance remain
separate. The user-owned public-launch-draft is not a release artifact.

## Workspace and invitation mutations (#165)

Invitation creation, acceptance and workspace switching now capture a separate
account generation. Only a successfully restored, still-active account may start
these operations. Each response/body boundary, clipboard completion, error and
finally path checks the captured generation. Successful logout/deletion and
component unmount invalidate it synchronously; failed teardown does not.

This prevents an obsolete invite response from copying/showing its bearer link,
an obsolete acceptance response from issuing the follow-on switch, and late
switch/error responses from replacing teardown notices with organization data.
Generated invite links and busy state are cleared at account teardown; successful
workspace switch/acceptance also clears the previous workspace's generated link.
Account generation is intentionally separate from Calendar generation: disconnecting
Calendar must not invalidate an otherwise current invitation operation.

Already-dispatched server operations and clipboard writes cannot be undone by a
client generation check. No clipboard clearing or server invitation revocation is
implied. Tests use synthetic responses and a mocked clipboard; no live invitation
or account deletion is performed. This does not assert that every other UI action
or same-account concurrent workspace mutation has been audited.
