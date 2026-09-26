# Request list ordering after mutations

Issue #177, part of #102.

List sequence numbers alone protect against out-of-order refreshes, but a list
snapshot can also predate an acknowledged mutation. Applying it afterwards can
make a cancelled meeting appear active or hide a completed acceptance/change.

Every successful local request mutation now supersedes both inbox and sent
read generations before applying its result. This covers acceptance, decline,
asynchronous response, pending/confirmed cancellation, rescheduling, a new
counterproposal, counterproposal acceptance, and handoff. Existing list checks
discard superseded responses, decoded bodies, failures, and finally handlers.
Loading flags are cleared immediately; rows and loaded-state markers remain.
The user can explicitly refresh again to fetch the latest persisted snapshot.

Only success invalidates reads. Failed/ambiguous operations leave pending reads
available to reconcile the current state. Request/account/workspace mutation
lifetimes are not changed, and one mutation does not cancel another. These are
browser display-order guarantees, not server versioning or cross-client locks.

BookingLifecycle.test.tsx adds 24 response/body/failure ordering cases spanning
both lists, reschedule, confirmed cancellation, acceptance, decline, async, and
unconfirmed cancellation. Each verifies a subsequent fresh read is accepted.
Two further cases verify that failed mutations preserve pending reads. Existing
counterproposal, handoff, and account/workspace lifecycle tests remain required.
