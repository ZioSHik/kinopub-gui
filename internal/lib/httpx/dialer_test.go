package httpx

import (
	"testing"
	"time"
)

func TestNewDialer_Config(t *testing.T) {
	d := NewDialer()
	if d == nil {
		t.Fatal("nil dialer")
	}
	if d.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", d.Timeout)
	}
	if d.KeepAlive != 30*time.Second {
		t.Errorf("KeepAlive = %v, want 30s", d.KeepAlive)
	}
}

func TestNewDialer_ReturnsDistinctInstances(t *testing.T) {
	a := NewDialer()
	b := NewDialer()
	if a == b {
		t.Error("expected distinct dialer instances")
	}
}

func TestNewDialerAlias_MatchesUnexported(t *testing.T) {
	pub := NewDialer()
	priv := newDialer()
	if pub.Timeout != priv.Timeout || pub.KeepAlive != priv.KeepAlive {
		t.Errorf("NewDialer and newDialer differ: %+v vs %+v", pub, priv)
	}
}
