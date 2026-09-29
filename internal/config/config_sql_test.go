package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// TestCHK_SQL_04_ProfilesPhase2Fields: phase-1 profiles.json documents keep
// loading, and the phase-2 connection/mapping fields survive a roundtrip.
func TestCHK_SQL_04_ProfilesPhase2Fields(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"version":1,"profiles":[{"id":"p1","name":"x","server":"srv","database":"d","authMode":"sql","login":"sa",
	  "enabled":true,"mapping":{"mode":"auto","table":"_Node123","pointNameColumn":"_Description","lastSendColumn":"_Fld1","lastRecvColumn":"_Fld2"}}]}`
	if err := os.WriteFile(filepath.Join(s.Dir, ProfilesFile), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	ps, err := s.LoadProfiles()
	if err != nil || len(ps) != 1 || ps[0].Encrypt != model.EncryptDefault || ps[0].Mapping.Linked() {
		t.Fatalf("phase-1 document: %+v %v", ps, err)
	}
	ps = []model.Profile{
		{ID: "p1", Server: `10.0.0.5\SQL2005`, Database: "buh", AuthMode: model.AuthSQL, Login: "sa", Enabled: true,
			Encrypt: model.EncryptOn, TrustServerCert: true, TLSMinVersion: model.TLSMin10, ConnectTimeoutSec: 10, QueryTimeoutSec: 60,
			Mapping: model.Mapping{Mode: model.MappingAuto, Table: "_Reference77", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef",
				TimesTable: "_InfoRg88", TimesLinkColumn: "_Fld89RRef", LastSendColumn: "_Fld90", LastRecvColumn: "_Fld91",
				YearOffset: 2000, KeepUnlinkedPoints: true}},
		{ID: "p2", Server: "srv", Database: "trade", AuthMode: model.AuthWindows, Encrypt: model.EncryptDisable,
			Mapping: model.Mapping{Mode: model.MappingCounters, Table: "_Node123", PointNameColumn: "_Description", PointKeyColumn: "_IDRRef"}},
	}
	if err := s.SaveProfiles(ps); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadProfiles()
	if err != nil || !reflect.DeepEqual(got, ps) {
		t.Errorf("roundtrip: %+v %v", got, err)
	}
	bad := ps[:1:1]
	bad[0].Encrypt = "maybe"
	if err := s.SaveProfiles(bad); err == nil {
		t.Error("invalid encrypt mode saved")
	}
}
