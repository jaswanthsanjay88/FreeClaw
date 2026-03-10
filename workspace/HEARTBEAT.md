# HEARTBEAT

Periodic checklist to keep FreeClaw healthy and efficient.

## Daily

- Check gateway startup logs for channel initialization errors.
- Confirm WAReplyMate bridge/API connectivity.
- Confirm message flow for owner chat is working.

## Per Change

- Run focused compile check for touched packages.
- Validate key user flow after behavior changes.
- Update docs if setup or runtime behavior changed.

## Weekly

- Review unresolved runtime warnings.
- Review memory files for stale or incorrect facts.
- Verify GitHub main branch reflects local expected state.

## Incident Response

- Capture failing command and exact error output.
- Identify whether issue is config, runtime dependency, or code regression.
- Apply minimal fix, verify, then document in history.
