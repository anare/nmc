package main

import "testing"

func TestSplitCommandLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "simple", input: "vim", want: []string{"vim"}},
		{name: "whitespace", input: "  vim   file  ", want: []string{"vim", "file"}},
		{name: "double quoted arg", input: "code --goto \"a b.txt:1\"", want: []string{"code", "--goto", "a b.txt:1"}},
		{name: "single quoted arg", input: "editor 'my file.txt'", want: []string{"editor", "my file.txt"}},
		{name: "escaped space", input: "my\\ editor --wait", want: []string{"my editor", "--wait"}},
		{name: "mixed quotes", input: "cmd \"two words\" 'three words'", want: []string{"cmd", "two words", "three words"}},
		{name: "unterminated double", input: "code \"oops", wantErr: true},
		{name: "unterminated single", input: "code 'oops", wantErr: true},
		{name: "trailing escape", input: "code \\", wantErr: true},
		{name: "empty", input: "   ", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitCommandLine(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil with result %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d (%#v), want %d (%#v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("arg %d mismatch: got %q, want %q (all got=%#v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
