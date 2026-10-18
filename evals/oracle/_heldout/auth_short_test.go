package auth

import "testing"

func TestRejectsShortToken(t *testing.T) {
	if Allowed("short") {
		t.Fatal("short token must be rejected")
	}
}
