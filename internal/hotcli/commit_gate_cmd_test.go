package hotcli

import (
	"testing"

	"github.com/amargautam/pakka/internal/policy"
)

func TestBoundFreshnessClampsLocalToCeiling(t *testing.T) {
	if got := boundFreshness(7200); got != policy.MaxMarkerFreshnessSeconds {
		t.Fatalf("boundFreshness(7200) = %d, want %d", got, policy.MaxMarkerFreshnessSeconds)
	}
	if got := boundFreshness(policy.MaxMarkerFreshnessSeconds); got != policy.MaxMarkerFreshnessSeconds {
		t.Fatalf("boundFreshness at ceiling = %d, want %d", got, policy.MaxMarkerFreshnessSeconds)
	}
	if got := boundFreshness(300); got != 300 {
		t.Fatalf("boundFreshness(300) = %d, want 300", got)
	}
}

func TestCommitGateCmdName(t *testing.T) {
	cmd := &CommitGateCmd{}
	if cmd.Name() != "commit-gate" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "commit-gate")
	}
}

func TestCommitGateCmdImplementsCommand(t *testing.T) {
	var _ Command = &CommitGateCmd{}
}
