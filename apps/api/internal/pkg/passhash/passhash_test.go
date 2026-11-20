package passhash

import "testing"

func TestHashVerify(t *testing.T) {
	hash, err := Hash("s3cret-password")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(hash, "s3cret-password") {
		t.Fatal("correct password rejected")
	}
	if Verify(hash, "wrong-password") {
		t.Fatal("wrong password accepted")
	}
	// bcrypt hashes never repeat for the same input.
	hash2, _ := Hash("s3cret-password")
	if hash == hash2 {
		t.Fatal("hash must be salted (never identical)")
	}
}
