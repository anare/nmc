package panel

import (
	"context"
	"testing"

	"github.com/anare/nmc/internal/vfs"
)

func TestPanelRestoresTheCursorWhenReturningToAParent(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "alpha", IsDir: true}, {Name: "beta", IsDir: true}})
	fs.Put("/beta", []vfs.Entry{{Name: "inside.txt"}})
	p, err := New(fs, "/", vfs.Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	p.SetRect(0, 0, 80, 24)
	p.MoveTo(2)
	if got, ok := p.Selected(); !ok || got.Name != "beta" {
		t.Fatalf("selected entry = %#v, want beta", got)
	}
	if handled, err := p.Enter(context.Background()); !handled || err != nil {
		t.Fatalf("enter returned handled=%v, err=%v", handled, err)
	}
	if err := p.GoUp(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, ok := p.Selected(); !ok || got.Name != "beta" {
		t.Fatalf("cursor after returning = %#v, want beta", got)
	}
}

func TestPanelCannotMarkTheParentAndMarkingAdvances(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "work", IsDir: true}})
	fs.Put("/work", []vfs.Entry{{Name: "a"}, {Name: "b"}})
	p, err := New(fs, "/work", vfs.Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	p.ToggleMark()
	if len(p.Marked) != 0 || p.Cursor != 1 {
		t.Fatalf("parent entry was marked: %#v", p.Marked)
	}
	p.ToggleMark()
	if len(p.Marked) != 1 || p.Cursor != 2 {
		t.Fatalf("marking did not mark and advance: marks=%#v cursor=%d", p.Marked, p.Cursor)
	}
	if got := p.Targets(); len(got) != 1 || got[0] != "/work/a" {
		t.Fatalf("targets = %#v, want /work/a", got)
	}
}

func TestFailedDirectoryChangeKeepsTheCurrentListing(t *testing.T) {
	fs := vfs.NewMemFS("test")
	fs.Put("/", []vfs.Entry{{Name: "visible.txt"}})
	p, err := New(fs, "/", vfs.Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	before := append([]vfs.Entry(nil), p.Entries...)
	if err := p.SetDir(context.Background(), "/missing", ""); err == nil {
		t.Fatal("SetDir succeeded for a missing directory")
	}
	if len(p.Entries) != len(before) || p.Entries[0].Name != before[0].Name || p.Cwd != "/" || p.Err == nil {
		t.Fatalf("failed SetDir changed visible state: cwd=%q entries=%#v error=%v", p.Cwd, p.Entries, p.Err)
	}
}
