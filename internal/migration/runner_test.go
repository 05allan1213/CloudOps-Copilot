package migration

import "testing"

func TestLockNameMatchesLegacyRuntimeLock(t *testing.T) {
	const expected = "f8c2380bf099839a7aa64e93efd7b59e23bb57f9237114b1889f66baae543b03"
	if actual := LockName("cloudops_test"); actual != expected {
		t.Fatalf("lock name=%q, want %q", actual, expected)
	}
}
