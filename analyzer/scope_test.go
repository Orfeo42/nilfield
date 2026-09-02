package analyzer

import "testing"

func TestScopeInvalidate(t *testing.T) {
	t.Run("the path itself is dropped", func(t *testing.T) {
		sc := scope{proven: map[string]bool{"o.p": true}, alias: map[string]string{}}

		sc.invalidate("o.p")

		if sc.proven["o.p"] {
			t.Fatal("invalidate() kept the proof for the written path")
		}
	})

	t.Run("proofs reached through the path are dropped", func(t *testing.T) {
		sc := scope{proven: map[string]bool{"o.p": true, "o.p.q": true}, alias: map[string]string{}}

		sc.invalidate("o.p")

		if sc.proven["o.p.q"] {
			t.Fatal("invalidate() kept a proof reachable through the written path")
		}
	})

	t.Run("a sibling proof survives", func(t *testing.T) {
		sc := scope{proven: map[string]bool{"o.p": true, "o.q": true}, alias: map[string]string{}}

		sc.invalidate("o.p")

		if !sc.proven["o.q"] {
			t.Fatal("invalidate() dropped an unrelated sibling proof")
		}
	})

	t.Run("aliases of the written path are dropped", func(t *testing.T) {
		sc := scope{
			proven: map[string]bool{},
			alias:  map[string]string{"p": "o.p", "deep": "o.p.q", "other": "o.q"},
		}

		sc.invalidate("o.p")

		if _, ok := sc.alias["p"]; ok {
			t.Fatal("invalidate() kept an alias of the written path")
		}

		if _, ok := sc.alias["deep"]; ok {
			t.Fatal("invalidate() kept an alias reached through the written path")
		}

		if _, ok := sc.alias["other"]; !ok {
			t.Fatal("invalidate() dropped an unrelated alias")
		}
	})

	t.Run("a known-nil state is dropped", func(t *testing.T) {
		sc := scope{
			proven:  map[string]bool{},
			alias:   map[string]string{},
			nilable: map[string]nilState{"p": isNil},
		}

		sc.invalidate("p")

		if _, ok := sc.nilable["p"]; ok {
			t.Fatal("invalidate() kept the nilable state for the written name")
		}
	})
}

func TestNilStateMessage(t *testing.T) {
	t.Run("isNil", func(t *testing.T) {
		if got := isNil.message(); got != "is nil here" {
			t.Fatalf("isNil.message() = %q, want %q", got, "is nil here")
		}
	})

	t.Run("typedNil", func(t *testing.T) {
		if got := typedNil.message(); got != "holds a nil pointer here" {
			t.Fatalf("typedNil.message() = %q, want %q", got, "holds a nil pointer here")
		}
	})

	t.Run("maybeNil", func(t *testing.T) {
		if got := maybeNil.message(); got != "may be nil here" {
			t.Fatalf("maybeNil.message() = %q, want %q", got, "may be nil here")
		}
	})
}

func TestGoroutineScope(t *testing.T) {
	t.Run("a field path proof is dropped", func(t *testing.T) {
		sc := newScope()
		sc.proven["o.p"] = true

		out := goroutineScope(sc)

		if out.proven["o.p"] {
			t.Fatal("goroutineScope() kept a field path proof")
		}
	})

	t.Run("a star path proof is dropped", func(t *testing.T) {
		sc := newScope()
		sc.proven["(*pp)"] = true

		out := goroutineScope(sc)

		if out.proven["(*pp)"] {
			t.Fatal("goroutineScope() kept a star path proof")
		}
	})

	t.Run("a bare local proof is kept", func(t *testing.T) {
		sc := newScope()
		sc.proven["x"] = true

		out := goroutineScope(sc)

		if !out.proven["x"] {
			t.Fatal("goroutineScope() dropped a bare local proof")
		}
	})

	t.Run("nilable state is kept", func(t *testing.T) {
		sc := newScope()
		sc.nilable["i"] = isNil

		out := goroutineScope(sc)

		if got := out.nilable["i"]; got != isNil {
			t.Fatalf("goroutineScope() nilable[\"i\"] = %v, want %v", got, isNil)
		}
	})

	t.Run("an alias of a proven path is dropped", func(t *testing.T) {
		sc := newScope()
		sc.proven["o.p"] = true
		sc.alias["p"] = "o.p"

		out := goroutineScope(sc)

		if _, ok := out.alias["p"]; ok {
			t.Fatal("goroutineScope() kept an alias of a proven path")
		}
	})

	t.Run("an alias of an unproven path is kept", func(t *testing.T) {
		sc := newScope()
		sc.alias["p"] = "o.p"

		out := goroutineScope(sc)

		if got := out.alias["p"]; got != "o.p" {
			t.Fatalf("goroutineScope() alias[\"p\"] = %q, want %q", got, "o.p")
		}
	})
}
