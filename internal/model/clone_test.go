package model

import "testing"

func TestCHK_CFG_08_DefaultCloneName(t *testing.T) {
	if DefaultCloneName("Склад", LangRU) != "Склад (копия)" {
		t.Fatal(DefaultCloneName("Склад", LangRU))
	}
	if DefaultCloneName("Warehouse", LangEN) != "Warehouse (copy)" {
		t.Fatal(DefaultCloneName("Warehouse", LangEN))
	}
	if DefaultCloneName("  ", LangEN) != "(copy)" {
		t.Fatal(DefaultCloneName("  ", LangEN))
	}
}
