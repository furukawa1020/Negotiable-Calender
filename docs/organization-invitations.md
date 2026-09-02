# Organization invitations and workspace switching

Authenticated OWNER and ADMIN members can create invitation links from the
account menu. Links expire after 72 hours and are accepted once.

Security properties:

- Only a SHA-256 token hash is stored. The raw token is returned once to the
  creator and is submitted in JSON request bodies for preview and acceptance.
- Authorization is checked against the current database membership, not
  client-provided role headers.
- OWNER may invite ADMIN, MANAGER, or MEMBER. ADMIN may invite only MANAGER or
  MEMBER.
- Acceptance consumes the invitation and creates membership in one database
  transaction. Existing membership is never overwritten or downgraded.
- Workspace switching verifies both membership and the current hashed server
  session before updating its active organization.
- Creation, acceptance, and switching append organization audit records.

Invitation email delivery is not included. Copy the generated link using the
account menu and send it through an approved internal channel.
