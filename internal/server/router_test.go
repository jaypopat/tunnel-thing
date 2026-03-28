package server

import "testing"

func TestRouter_RegisterAndLookup(t *testing.T) {
	r := NewRouter()
	s := &session{clientID: "jay"}

	if err := r.Register("myapp", s); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := r.Lookup("myapp")
	if !ok || got != s {
		t.Fatalf("Lookup('myapp') = (%v, %v), want (%v, true)", got, ok, s)
	}

	_, ok = r.Lookup("other")
	if ok {
		t.Fatal("Lookup('other') should not exist")
	}
}

func TestRouter_DuplicateRegister(t *testing.T) {
	r := NewRouter()
	s1 := &session{clientID: "jay"}
	s2 := &session{clientID: "other"}

	r.Register("myapp", s1)
	err := r.Register("myapp", s2)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRouter_Unregister(t *testing.T) {
	r := NewRouter()
	s := &session{clientID: "jay"}

	r.Register("myapp", s)
	r.Unregister("myapp")

	_, ok := r.Lookup("myapp")
	if ok {
		t.Fatal("Lookup after Unregister should not exist")
	}

	// Should be able to re-register after unregister.
	if err := r.Register("myapp", s); err != nil {
		t.Fatalf("Re-register after Unregister: %v", err)
	}
}
