# Domain model

Issue #3 implements the core model defined by Issue 002 in the requirements.

## Trust domains

```text
Private Calendar Domain
  privateevent
      ↓
  projection engine
      ↓
  projection.View

Organization Coordination Domain
  organization
  request
  httpapi
```

`organization`, `request`, and `httpapi` must never import `privateevent`. The architecture test enforces this rule. `projection.View` contains only interaction state and time boundaries.

## Privacy behavior

- `privateevent.PrivateEvent` rejects JSON serialization.
- Private Event details remain encrypted fields in the private calendar domain.
- Projection DTOs do not contain titles, descriptions, locations, attendees, provider IDs, calendar names, or event counts.
- Invalid or unknown values fail validation instead of becoming `available`.
- Persisted timestamps must be UTC. Display conversion uses the user's IANA timezone.

## Core models

- `organization.Organization`
- `organization.User`
- `organization.Membership`
- `privateevent.PrivateEvent`
- `policy.SharingPolicy`
- `policy.Rule`
- `policy.ManualOverride`
- `projection.ScheduleProjection`
- `request.CoordinationRequest`
- `request.Option`

The next implementation step is the Projection Engine, which is the only application component allowed to translate `PrivateEvent + SharingPolicy` into `ScheduleProjection`.
