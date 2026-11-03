package addtool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestInvokableRun(t *testing.T) {
	adder, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		args    string
		want    int
		wantErr bool
	}{
		{name: "missing both", args: `{}`, wantErr: true},
		{name: "missing a", args: `{"b":25}`, wantErr: true},
		{name: "missing b", args: `{"a":17}`, wantErr: true},
		{name: "null a", args: `{"a":null,"b":25}`, wantErr: true},
		{name: "null b", args: `{"a":17,"b":null}`, wantErr: true},
		{name: "null both", args: `{"a":null,"b":null}`, wantErr: true},
		{name: "zero a", args: `{"a":0,"b":25}`, want: 25},
		{name: "zero b", args: `{"a":17,"b":0}`, want: 17},
		{name: "zero both", args: `{"a":0,"b":0}`, want: 0},
		{name: "sum 42", args: `{"a":17,"b":25}`, want: 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := adder.InvokableRun(context.Background(), tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("InvokableRun(%s) returned success: %q", tc.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("InvokableRun(%s): %v", tc.args, err)
			}
			var result struct {
				Sum *int `json:"sum"`
			}
			if err := json.Unmarshal([]byte(got), &result); err != nil {
				t.Fatalf("decoding InvokableRun result %q: %v", got, err)
			}
			if result.Sum == nil {
				t.Fatalf("InvokableRun(%s) result lacks a non-null sum: %q", tc.args, got)
			}
			if *result.Sum != tc.want {
				t.Fatalf("InvokableRun(%s) sum = %d, want %d", tc.args, *result.Sum, tc.want)
			}
		})
	}
}
