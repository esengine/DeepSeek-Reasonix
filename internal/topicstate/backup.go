package topicstate

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

// BackupExisting reads an existing database without running schema migrations.
// VACUUM INTO captures a consistent SQLite snapshot including committed WAL
// pages and unknown tables/columns. The destination must not already exist.
func BackupExisting(ctx context.Context, path, destination string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("topic state is not a regular file")
	}
	// diskFileDSN handles Windows drive letters (file:///C:/...); a bare
	// file://C:/... URI fails with "invalid uri authority: C:".
	db, err := sql.Open("sqlite", diskFileDSN(path)+"&mode=ro&_pragma=busy_timeout%285000%29")
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "VACUUM INTO ?", destination)
	return err
}
