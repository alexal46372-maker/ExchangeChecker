// Package sqlcheck reads 1C 8.x exchange times from MSSQL (PLAN.md §5):
// TestConnection, Discovery of the table/columns holding the exchange
// timestamps, and ListPoints by a saved (or manual) mapping.
//
// Driver: github.com/microsoft/go-mssqldb (pure Go TDS, no CGO). Windows
// authentication uses the driver's SSPI provider (secur32.dll, winsspi);
// no helper processes, no os/exec in the build graph (POLICY.md §4).
// All SQL is built from bracket-quoted identifiers (QuoteIdent); values
// are always passed as parameters.
//
// DB access goes through the small Store interface so that Discovery and
// ListPoints are unit-tested on in-memory schema fixtures without a server.
//
// Check IDs: CHK-SQL-01 (connection), CHK-SQL-02 (Discovery),
// CHK-SQL-03 (ListPoints by mapping), CHK-SQL-04 (manual mapping / SQL).
package sqlcheck
