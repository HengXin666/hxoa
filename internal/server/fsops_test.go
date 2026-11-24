package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HengXin666/hxoa/internal/proto"
)

func code(err error) string {
	if ae, ok := err.(*apiErr); ok {
		return ae.code
	}
	return ""
}

func TestVersionedEdits(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\nb\nc\n"), 0o644)

	r, err := doRead(root, "f.txt", 0, 0)
	if err != nil || r.Lines != 3 {
		t.Fatal(err, r)
	}
	v0 := r.Version

	// line patch with the version just read
	w, err := doPatch(root, &proto.PatchReq{Path: "f.txt", IfMatch: v0, Edits: []proto.Edit{{Op: "lines", Start: 2, End: 2, Lines: []string{"B"}}}})
	if err != nil {
		t.Fatal(err)
	}
	// stale version must conflict
	if _, err := doPatch(root, &proto.PatchReq{Path: "f.txt", IfMatch: v0, Edits: []proto.Edit{{Op: "lines", Start: 1, End: 1, Lines: []string{"x"}}}}); code(err) != proto.CodeConflict {
		t.Fatal("want conflict, got", err)
	}
	// replace with current version
	if _, err := doPatch(root, &proto.PatchReq{Path: "f.txt", IfMatch: w.Version, Edits: []proto.Edit{{Op: "replace", Old: "c", New: "C"}}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(b) != "a\nB\nC\n" {
		t.Fatalf("got %q", b)
	}
	// range guard: survives an edit elsewhere, fails when the range changed
	rr, _ := doRead(root, "f.txt", 3, 3)
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\nB\nC\nappended\n"), 0o644)
	if _, err := doPatch(root, &proto.PatchReq{Path: "f.txt", RangeMatch: rr.RangeVersion, Edits: []proto.Edit{{Op: "lines", Start: 3, End: 3, Lines: []string{"c3"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := doPatch(root, &proto.PatchReq{Path: "f.txt", RangeMatch: rr.RangeVersion, Edits: []proto.Edit{{Op: "lines", Start: 3, End: 3, Lines: []string{"again"}}}}); code(err) != proto.CodeConflict {
		t.Fatal("want range conflict, got", err)
	}
	// multiple line edits use original numbering
	r, _ = doRead(root, "f.txt", 0, 0)
	if _, err := doPatch(root, &proto.PatchReq{Path: "f.txt", IfMatch: r.Version, Edits: []proto.Edit{
		{Op: "lines", Start: 1, End: 1, Lines: []string{"1a", "1b"}},
		{Op: "lines", Start: 4, End: 4, Lines: nil},
		{Op: "lines", Start: 3, End: 2, Lines: []string{"ins"}},
	}}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(root, "f.txt"))
	if string(b) != "1a\n1b\nB\nins\nc3\n" {
		t.Fatalf("got %q", b)
	}
	// create-only and escapes
	if _, err := doWrite(root, &proto.WriteReq{Path: "f.txt", Content: "x", IfMatch: "new"}); code(err) != proto.CodeExists {
		t.Fatal("want exists", err)
	}
	if _, err := doWrite(root, &proto.WriteReq{Path: "../x", Content: "x", IfMatch: "new"}); code(err) != proto.CodeDenied {
		t.Fatal("want denied", err)
	}
	os.Symlink("/etc", filepath.Join(root, "link"))
	if _, err := doRead(root, "link/hostname", 0, 0); code(err) != proto.CodeDenied {
		t.Fatal("want denied through symlink", err)
	}
	// ambiguous replace
	r, _ = doRead(root, "f.txt", 0, 0)
	if _, err := doPatch(root, &proto.PatchReq{Path: "f.txt", IfMatch: r.Version, Edits: []proto.Edit{{Op: "replace", Old: "1", New: "z"}}}); code(err) != proto.CodeAmbiguous {
		t.Fatal("want ambiguous", err)
	}
}
