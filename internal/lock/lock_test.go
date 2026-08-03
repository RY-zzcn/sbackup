package lock

import "testing"

func TestAcquireSlotHonorsLimit(t *testing.T) {
	first, err := AcquireSlot("test-slots", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := AcquireSlot("test-slots", 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	if _, err := AcquireSlot("test-slots", 2); err == nil {
		t.Fatal("third slot acquired beyond limit")
	}
}
