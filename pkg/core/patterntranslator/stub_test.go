package patterntranslator

import (
	"context"
	"errors"
	"testing"
)

func TestStub(t *testing.T) {
	stub := Stub{}
	ctx := context.Background()

	t.Run("text naming an issue gets two issues", func(t *testing.T) {
		got, err := stub.Translate(ctx, Request{Text: "show me an Issue", Zone: zone, Now: now})
		if err != nil {
			t.Fatalf("Translate: %v", err)
		}
		if got.Pattern != "" || len(got.Issues) != 2 || got.Issues[0].Kind != Missing || got.Issues[1].Kind != Ambiguous {
			t.Fatalf("got %+v, want a missing and an ambiguous issue", got)
		}
	})

	t.Run("text naming a failure is unavailable", func(t *testing.T) {
		_, err := stub.Translate(ctx, Request{Text: "please FAIL", Zone: zone, Now: now})
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("got %v, want ErrUnavailable", err)
		}
	})

	t.Run("anything else is Sundays from the next one", func(t *testing.T) {
		got, err := stub.Translate(ctx, Request{Text: "whatever", Zone: zone, Now: now})
		if err != nil {
			t.Fatalf("Translate: %v", err)
		}
		// now is a Sunday, so the next Sunday on or after today is today.
		if got.Pattern != sundays {
			t.Fatalf("got %q, want %q", got.Pattern, sundays)
		}
		if problems := check(got.Pattern, zone); len(problems) != 0 {
			t.Fatalf("the stub's pattern does not read: %v", problems)
		}
	})

	t.Run("the limit holds", func(t *testing.T) {
		long := make([]rune, MaxText+1)
		for i := range long {
			long[i] = 'a'
		}
		if _, err := stub.Translate(ctx, Request{Text: string(long), Zone: zone, Now: now}); !errors.Is(err, ErrTooLong) {
			t.Fatalf("got %v, want ErrTooLong", err)
		}
	})
}
