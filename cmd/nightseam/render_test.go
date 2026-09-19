package main

import "testing"

// renderTool renders every family of a checkout with the tool as composed.
func renderTool(t *testing.T, root string) map[string][]byte {
	t.Helper()
	a := &app{root: root, module: module, scope: scope}
	names, err := a.chosen(nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := a.render(names)
	if err != nil {
		t.Fatal(err)
	}
	return files
}
