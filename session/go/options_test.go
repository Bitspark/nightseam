package session

import (
	"errors"
	"testing"
	"time"
)

func TestNewRejectsNegativeLimits(t *testing.T) {
	for _, test := range []struct {
		name    string
		options Options
	}{
		{"MaxAttachments", Options{MaxAttachments: -1}},
		{"MaxInflight", Options{MaxInflight: -1}},
		{"SendTimeout", Options{SendTimeout: -time.Nanosecond}},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := New(test.options)
			if registry != nil {
				t.Fatal("a negative limit created a registry")
			}
			var refusal *Error
			if !errors.As(err, &refusal) || refusal.Code != ErrorInvalidOptions {
				t.Fatalf("negative limit: got %v, want *Error with code %q", err, ErrorInvalidOptions)
			}
			if !errors.Is(err, &Error{Code: ErrorInvalidOptions}) {
				t.Fatal("errors.Is does not match the refusal by code")
			}
			if refusal.Message == "" {
				t.Fatal("the refusal has no explanation")
			}
		})
	}
}

func TestNewDefaultsOnlyZeroLimits(t *testing.T) {
	for _, test := range []struct {
		name    string
		options Options
		want    Options
	}{
		{"defaults", Options{}, Options{MaxAttachments: 64, MaxInflight: 256, SendTimeout: 10 * time.Second}},
		{"attachments", Options{MaxAttachments: 1}, Options{MaxAttachments: 1, MaxInflight: 256, SendTimeout: 10 * time.Second}},
		{"inflight", Options{MaxInflight: 1}, Options{MaxAttachments: 64, MaxInflight: 1, SendTimeout: 10 * time.Second}},
		{"timeout", Options{SendTimeout: time.Nanosecond}, Options{MaxAttachments: 64, MaxInflight: 256, SendTimeout: time.Nanosecond}},
		{"explicit", Options{MaxAttachments: 3, MaxInflight: 7, SendTimeout: time.Second}, Options{MaxAttachments: 3, MaxInflight: 7, SendTimeout: time.Second}},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := New(test.options)
			if err != nil {
				t.Fatal(err)
			}
			if registry.options != test.want {
				t.Fatalf("limits: got %+v, want %+v", registry.options, test.want)
			}
		})
	}
}
