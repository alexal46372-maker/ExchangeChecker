package sqlcheck

import (
	"context"
	"errors"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 9 (PLAN §12 p. 97, CHK-SQL-07): the database list of the
// connection wizard. Read-only, no session SET; SQL Server 2005/2008
// compatible (CHK-SQL-05). Only user databases (database_id > 4) that are
// ONLINE (state 0) and that the login may open (HAS_DBACCESS = 1).
const databasesSQL = `SELECT TOP (@p1) d.name, d.compatibility_level, d.state_desc
FROM sys.databases AS d
WHERE d.database_id > 4 AND d.state = 0 AND HAS_DBACCESS(d.name) = 1
ORDER BY d.name`

var errNotInteractive = errors.New("not an interactive SQL store")

// DatabasesOptions configures Databases.
type DatabasesOptions struct {
	// Limit: maximum number of databases (0 = model.DefaultSQLDatabasesLimit,
	// at most model.MaxSQLDatabases). The JSON budget
	// model.MaxSQLWizardJSONBytes applies as well; Truncated reports a cut.
	Limit int
}

func (o DatabasesOptions) limit() int {
	switch {
	case o.Limit <= 0:
		return model.DefaultSQLDatabasesLimit
	case o.Limit > model.MaxSQLDatabases:
		return model.MaxSQLDatabases
	}
	return o.Limit
}

// databases runs databasesSQL (limit + 1 rows to detect truncation).
func (s *SQLStore) databases(ctx context.Context, limit int) ([]model.SQLDatabaseInfo, error) {
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, databasesSQL, limit+1)
	if err != nil {
		return nil, classify("databases", err, false)
	}
	defer rows.Close()
	var out []model.SQLDatabaseInfo
	for rows.Next() {
		var d model.SQLDatabaseInfo
		var compat int64
		if err := rows.Scan(&d.Name, &compat, &d.State); err != nil {
			return nil, classify("databases", err, false)
		}
		d.CompatLevel = int(compat)
		out = append(out, d)
	}
	return out, classify("databases", rows.Err(), false)
}

// Databases returns the databases the profile's login can open. ctx
// cancel / expiry gives *Error KindCanceled / KindTimeout. The profile's
// Database may be empty (the login's default database is used).
func (r *InteractiveReader) Databases(ctx context.Context, opt DatabasesOptions) (model.SQLDatabasesResult, error) {
	st, ok := r.st.(*interactiveStore)
	if !ok {
		return model.SQLDatabasesResult{}, &Error{Kind: KindConfig, Op: "databases", Err: errNotInteractive}
	}
	var res model.SQLDatabasesResult
	t0 := time.Now()
	if err := st.Ping(ctx); err != nil {
		return res, ctxErr("databases", refineTLS(err, r.profile))
	}
	t1 := time.Now()
	limit := opt.limit()
	list, err := guarded(st, ctx, "databases", st.qbudget(), func(ctx context.Context) ([]model.SQLDatabaseInfo, error) {
		return st.SQLStore.databases(ctx, limit)
	})
	if err != nil {
		return res, ctxErr("databases", err)
	}
	res.ConnectMs, res.QueryMs = t1.Sub(t0).Milliseconds(), time.Since(t1).Milliseconds()
	res.Databases, res.Truncated = model.CapSQLList(list, limit)
	if res.Databases == nil {
		res.Databases = []model.SQLDatabaseInfo{}
	}
	return res, nil
}

// AutoPick decides whether the wizard may apply a discovery candidate
// without asking (model.SQLAutoPickFor; CHK-SQL-08).
func AutoPick(cands []model.DiscoveryCandidate) model.SQLAutoPick {
	return model.SQLAutoPickFor(cands)
}
