package app

import "testing"

// A menu item's icon is the SF Symbol's, made once however many items
// show it, and none where the system has no such symbol.
func TestMenuIcon(t *testing.T) {
	ic := menuIcon("doc.on.doc")
	if ic == nil {
		t.Fatal("no icon of doc.on.doc")
	}
	if menuIcon("doc.on.doc") != ic {
		t.Error("the icon was made again")
	}
	if menuIcon("no.symbol.of.that.name") != nil {
		t.Error("an icon of a symbol the system has not")
	}
}
