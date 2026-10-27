
package model
import ("encoding/json"; "strings"; "testing")
func TestCHK_CFG_07_ViberForExportJSON(t *testing.T) {
  v := DefaultViberSettings()
  v.HasToken = true
  b, _ := json.Marshal(v)
  if !strings.Contains(string(b), `"hasToken":true`) { t.Fatalf("view %s", b) }
  b, _ = json.Marshal(v.ForExport())
  if strings.Contains(string(b), "hasToken") || strings.Contains(string(b), "status") {
    t.Fatalf("export %s", b)
  }
}
