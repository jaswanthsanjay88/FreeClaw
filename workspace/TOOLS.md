# TOOLS

Tool usage policy for this workspace.

## Preferred Workflow

- Search/read context first.
- Make minimal targeted edits.
- Validate with compile/test.
- Report exact outcomes.

## Tool Priorities

- Use `apply_patch` for small precise edits.
- Use terminal for build/test/git operations.
- Use parallel reads/searches when gathering context.

## Guardrails

- Do not run destructive git commands unless explicitly requested.
- Do not expose secrets from files or environment.
- Do not claim command success without checking output.

## Validation Checklist

- Build/tests pass for changed scope.
- No unintended file edits.
- User-visible behavior is verified when possible.
