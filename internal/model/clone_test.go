package model

import "testing"

func TestCHK_CFG_08_DefaultCloneName(t *testing.T) {
	if DefaultCloneName("Склад") != "Склад (копия)" {
		t.Fatal(DefaultCloneName("Склад"))
	}
	if DefaultCloneName("Warehouse") != "Warehouse (копия)" {
		t.Fatal(DefaultCloneName("Warehouse"))
	}
	if DefaultCloneName("  ") != "(копия)" {
		t.Fatal(DefaultCloneName("  "))
	}
}
