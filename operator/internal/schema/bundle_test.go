package schema

import "testing"

func TestVerify(t *testing.T) {
	if err := Verify(); err != nil {
		t.Fatal(err)
	}
}
