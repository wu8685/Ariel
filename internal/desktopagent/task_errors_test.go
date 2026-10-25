package desktopagent

import (
	chillingclient "github.com/wu8685/Ariel/internal/chilling"
	"testing"
)

func TestTaskBusinessErrorPreservesConflictCode(t *testing.T) {
	if got := requestErrorCode("task.action", &chillingclient.Error{Code: "TASK_VERSION_CONFLICT", Message: "private backend payload"}); got != "TASK_VERSION_CONFLICT" {
		t.Fatalf("business error=%s", got)
	}
}
