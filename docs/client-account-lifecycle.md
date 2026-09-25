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
