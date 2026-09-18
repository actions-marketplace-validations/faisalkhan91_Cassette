package wirefix

import "testing"

func TestCoffeeTurns(t *testing.T) {
	turns := CoffeeTurns()
	if len(turns) == 0 {
		t.Fatal("CoffeeTurns returned no turns (fixtures missing or unreadable?)")
	}
	for i, b := range turns {
		if len(b) == 0 {
			t.Errorf("turn %d is empty", i)
		}
	}
}
