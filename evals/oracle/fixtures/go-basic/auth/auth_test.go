package auth

import "testing"

func TestAllowedWithLongToken(t *testing.T) {
	if !Allowed("a-long-token") {
		t.Fatal("long token must be allowed")
	}
}
