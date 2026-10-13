package sqlcheck

import (
	"context"
	"errors"
	"time"

	"github.com/alexal46372-maker/ExchangeChecker/internal/model"
)

// Phase 9 (PLAN §12 p. 106, CHK-SQL-07): the database list of the
// connection wizard. Read-only, no session SET; SQL Server 2005/2008
// compatible (CHK-SQL-05). Only user databases (database_id > 4) that are
// ONLINE (state 0) and that the login may open (HAS_DBACCESS = 1).
const databasesSQL = `SELECT TOP (@p1) d.name, d.compatibility_level, d.state_desc, d.database_id
FROM sys.databases AS d
WHERE d.database_id > 4 AND d.state = 0 AND HAS_DBACCESS(d.name) = 1
ORDER BY d.name`

var errNotInteractive = errors.New("not an interactive SQL store")

// databaseSizesSQL: size per database in MB (8 KB pages); rows are only
// visible with VIEW ANY DEFINITION / for the login's own databases —
// missing rows or an error give SizeMB 0.
const databaseSizesSQL = `SELECT f.database_id, SUM(CAST(f.size AS bigint)) / 128
FROM sys.master_files AS f
GROUP BY f.database_id`

// dbProbeSQL is the read-only 1C probe of one database (three-part names,
// no USE, no SET; catalog views only). db is validated and quoted.
func dbProbeSQL(qdb string) string {
	return `SELECT
 (SELECT COUNT(*) FROM ` + qdb + `.sys.tables AS t WHERE t.name IN (N'Config', N'Params', N'DBSchema')),
 CASE WHEN EXISTS (SELECT 1 FROM ` + qdb + `.sys.tables AS t WHERE t.name LIKE N'[_]Node%') THEN 1 ELSE 0 END`
}

// DefaultDatabasesProbeLimit: databases probed for 1C tables at most.
const DefaultDatabasesProbeLimit = 64

// DatabasesOptions configures Databases.
type DatabasesOptions struct {
	// ProbeLimit: databases probed for Is1C / HasNodeTables (0 =
	// DefaultDatabasesProbeLimit, < 0 = no probe).
	ProbeLimit int
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
type dbRow struct {
	info model.SQLDatabaseInfo
	id   int64
}

func (s *SQLStore) databases(ctx context.Context, limit int) ([]dbRow, error) {
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, databasesSQL, limit+1)
	if err != nil {
		return nil, classify("databases", err, false)
	}
	defer rows.Close()
	var out []dbRow
	for rows.Next() {
		var d dbRow
		var compat int64
		if err := rows.Scan(&d.info.Name, &compat, &d.info.State, &d.id); err != nil {
			return nil, classify("databases", err, false)
		}
		d.info.CompatLevel = int(compat)
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
	rows, err := guarded(st, ctx, "databases", st.qbudget(), func(ctx context.Context) ([]dbRow, error) {
		return st.SQLStore.databases(ctx, limit)
	})
	if err != nil {
		return res, ctxErr("databases", err)
	}
	res.ConnectMs, res.QueryMs = t1.Sub(t0).Milliseconds(), time.Since(t1).Milliseconds()
	if len(rows) > limit {
		rows, res.Truncated = rows[:limit], true
	}
	st.enrichDatabases(ctx, rows, opt)
	list := make([]model.SQLDatabaseInfo, len(rows))
	for i := range rows {
		list[i] = rows[i].info
	}
	if err := ctx.Err(); err != nil {
		return model.SQLDatabasesResult{}, ctxErr("databases", err)
	}
	var cut bool
	res.Databases, cut = model.CapSQLList(list, limit)
	res.Truncated = res.Truncated || cut
	if res.Databases == nil {
		res.Databases = []model.SQLDatabaseInfo{}
	}
	return res, nil
}

// enrichDatabases adds SizeMB and the 1C probe; every error except the
// context is ignored (permission: the fields stay 0 / false).
func (st *interactiveStore) enrichDatabases(ctx context.Context, list []dbRow, opt DatabasesOptions) {
	sizes, _ := guarded(st, ctx, "database sizes", st.qbudget(), func(ctx context.Context) (map[int64]int64, error) {
		return st.SQLStore.databaseSizes(ctx)
	})
	probe := opt.ProbeLimit
	if probe == 0 {
		probe = DefaultDatabasesProbeLimit
	}
	for i := range list {
		if ctx.Err() != nil {
			return
		}
		d := &list[i].info
		d.SizeMB = sizes[list[i].id]
		if i < probe {
			n1c, node, err := st.probeDatabase(ctx, d.Name)
			if err == nil {
				d.Is1C, d.HasNodeTables, d.Probed = n1c >= 2, node, true
			}
		}
	}
}

func (st *interactiveStore) probeDatabase(ctx context.Context, name string) (int64, bool, error) {
	q, err := quoteChecked(name)
	if err != nil {
		return 0, false, err
	}
	type pr struct {
		n    int64
		node int64
	}
	v, err := guarded(st, ctx, "database probe", st.qbudget(), func(ctx context.Context) (pr, error) {
		ctx, cancel := st.SQLStore.qctx(ctx)
		defer cancel()
		var p pr
		err := st.SQLStore.db.QueryRowContext(ctx, dbProbeSQL(q)).Scan(&p.n, &p.node)
		return p, classify("database probe", err, false)
	})
	return v.n, v.node == 1, err
}

func (s *SQLStore) databaseSizes(ctx context.Context) (map[int64]int64, error) {
	ctx, cancel := s.qctx(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, databaseSizesSQL)
	if err != nil {
		return nil, classify("database sizes", err, false)
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, mb int64
		if err := rows.Scan(&id, &mb); err != nil {
			return nil, classify("database sizes", err, false)
		}
		out[id] = mb
	}
	return out, classify("database sizes", rows.Err(), false)
}

// ListDatabases opens an interactive reader for the (unsaved) profile p,
// lists its databases and closes it (EC Tray naming).
func ListDatabases(ctx context.Context, p model.Profile, password string, opt DatabasesOptions) (model.SQLDatabasesResult, error) {
	r, err := NewInteractiveReader(p, password, Options{})
	if err != nil {
		return model.SQLDatabasesResult{}, err
	}
	defer r.Close()
	return r.Databases(ctx, opt)
}

// AutoPick decides whether the wizard may apply a discovery candidate
// without asking (model.SQLAutoPickFor; CHK-SQL-08).
func AutoPick(cands []model.DiscoveryCandidate) model.SQLAutoPick {
	return model.SQLAutoPickFor(cands)
}
