package credstore

import "testing"

// All tests redirect storage to a temp dir via XDG_CONFIG_HOME so they never
// touch the real ~/.config/kinopub credentials. They exercise the real,
// platform-specific machineSeed() on whatever OS runs them — which is how the
// Windows CI lane verifies the registry-based seed (and guards against another
// "wmic"-class regression).

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	want := Credentials{
		Cookie:    "cf_clearance=abc123; sessionid=xyz",
		UserAgent: "Mozilla/5.0 (round-trip test)",
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoadEmptyWhenAbsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, err := Load()
	if err != nil {
		t.Fatalf("Load with no file: %v", err)
	}
	if !got.IsEmpty() {
		t.Fatalf("expected empty credentials, got %+v", got)
	}
}

func TestClearRemovesCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save(Credentials{Cookie: "c", UserAgent: "u"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load after Clear: %v", err)
	}
	if !got.IsEmpty() {
		t.Fatalf("expected empty credentials after Clear, got %+v", got)
	}
	// Clearing again (no file) must not error.
	if err := Clear(); err != nil {
		t.Fatalf("Clear when already absent: %v", err)
	}
}

func TestMachineSeedNonEmptyAndStable(t *testing.T) {
	a, err := machineSeed()
	if err != nil {
		t.Fatalf("machineSeed: %v", err)
	}
	if len(a) == 0 {
		t.Fatal("machineSeed returned an empty seed")
	}
	b, err := machineSeed()
	if err != nil {
		t.Fatalf("machineSeed (second call): %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("machineSeed is not stable across calls: %q vs %q", a, b)
	}
}
