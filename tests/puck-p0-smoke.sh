#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

# This smoke is deterministic and never opens an existing Codex thread. The
# real Desktop follower observation is recorded in docs/testing/0130-probe-results.md.
go test ./internal/relay -run 'TestPuckBridgeFailsClosedWhenExecutionPolicyIsUnverified' -count=1
go test ./internal/puck -run 'TestModuleExecutesTrustedBrainToolCallAndReturnsResult' -count=1
go test ./internal/desktopagent -run 'TestBrainHostRestartsAndResumesAfterProcessExit' -count=1
(cd web && npm test -- --run src/puck/PuckWorkspace.test.tsx)

printf 'Puck P0 safety smoke passed; automatic execution remains fail-closed under Q23.\n'
