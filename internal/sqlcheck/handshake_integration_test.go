package sqlcheck

import (
	"context"
	"testing"
	"time"
)

// TestCHK_SQL_01_IntegrationSessionOutlivesHandshake: a session whose
// connect context ends (cancel, or its deadline expires) right after login,
// and whose handshake budget (ConnectTimeoutSec = 1) has long passed, keeps
// working for a query longer than both (the deadline is cleared after a
// successful login, poll queries are not cut).
func TestCHK_SQL_01_IntegrationSessionOutlivesHandshake(t *testing.T) {
	p, pw := itProfile(t, "ec_1c_plans_off")
	p.ConnectTimeoutSec = 1
	st, err := Open(p, pw)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, mode := range []string{"cancel", "deadline"} {
		var ctx context.Context
		var cancel context.CancelFunc
		if mode == "cancel" {
			ctx, cancel = context.WithCancel(context.Background())
		} else {
			ctx, cancel = context.WithTimeout(context.Background(), 1500*time.Millisecond)
		}
		conn, err := st.DB().Conn(ctx)
		if err != nil {
			cancel()
			t.Fatalf("%s: connect: %v", mode, err)
		}
		if mode == "cancel" {
			cancel()
		} else {
			<-ctx.Done() // the connect deadline expires after the login
			cancel()
		}
		time.Sleep(1200 * time.Millisecond) // past the 1 s handshake budget
		qctx, qcancel := context.WithTimeout(context.Background(), 20*time.Second)
		var one int
		start := time.Now()
		err = conn.QueryRowContext(qctx, "WAITFOR DELAY '00:00:02'; SELECT 1").Scan(&one)
		qcancel()
		conn.Close()
		// the essence: the session survives the handshake deadline and the
		// long query completes without error. The duration is only logged:
		// CI runners measured 1.95 s and even 1.40 s on the client clock for
		// WAITFOR DELAY '00:00:02' (the SQL container's timer drifts).
		t.Logf("%s: long query took %v", mode, time.Since(start))
		if err != nil || one != 1 {
			t.Errorf("%s: long query after the handshake: %v (%d, %v)", mode, err, one, time.Since(start))
		}
	}
}
