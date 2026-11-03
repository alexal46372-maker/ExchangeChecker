package wininstall

import (
	"strings"
	"testing"
)

// TestCHK_SETUP_15_FolderHintLayout — title is clean; upgrade explanation
// lives under the path field as a multiline hint (CHK-SETUP-15).
func TestCHK_SETUP_15_FolderHintLayout(t *testing.T) {
	if IDFolderHint != 115 {
		t.Fatalf("IDFolderHint=%d want 115", IDFolderHint)
	}
	hx, hy, hw, hh := FolderHintRect()
	if hx != ContentLeft() || hw != ContentWidth() {
		t.Fatalf("hint x/w %d/%d", hx, hw)
	}
	if hy <= ContentTop()+FolderEditOffset {
		t.Fatalf("hint y %d must be below the path EDIT", hy)
	}
	if hh < FolderHintMinH {
		t.Fatalf("hint min height %d", hh)
	}
	ru := CatalogKeys("ru")
	title := ru["setup.folder.label"]
	if title != "Выбор папки установки" {
		t.Fatalf("folder title %q", title)
	}
	hint := ru["setup.folder.upgrade"]
	if hint == "" || strings.Contains(hint, "\r") {
		t.Fatalf("upgrade hint empty or has CR: %q", hint)
	}
	if len([]rune(hint)) < 20 {
		t.Fatalf("upgrade hint too short to need wrapping room: %q", hint)
	}
	// Title must not be the place that embeds the upgrade note.
	if strings.Contains(title, "обновлен") || strings.Contains(title, "не меняется") {
		t.Fatalf("title must not carry the upgrade note: %q", title)
	}
}
