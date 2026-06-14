package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/config"
)

// runBackup writes a tar.gz archive containing a consistent SQLite
// snapshot plus metadata. Postgres deployments fall back to pg_dump —
// the command refuses early so operators don't end up with an
// incomplete archive on the wrong driver. The snapshot is captured
// via "VACUUM INTO" which is the SQLite-recommended way to take a
// consistent online backup without quiescing writers.
func runBackup(args []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	out := flags.String("out", "", "Output path for the .tar.gz archive (required)")
	flags.Usage = func() {
		// Best-effort writes to the flag output; failure to print usage
		// text is not actionable, callers will see no usage but the
		// command still returns the parse error.
		_, _ = fmt.Fprintf(flags.Output(), "Usage: panvex-control-plane backup -out <path.tar.gz> [flags]\n\n")
		_, _ = fmt.Fprintf(flags.Output(), "Captures a SQLite-only backup. Postgres operators should use pg_dump.\n")
		_, _ = fmt.Fprintf(flags.Output(), "Restore: tar -xzf <archive>, then point the panel at the extracted .db file.\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*out) == "" {
		return errors.New("backup: -out is required")
	}

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}
	if storageConfig.Driver != config.StorageDriverSQLite {
		return fmt.Errorf("backup: only sqlite is supported (got %q); use pg_dump for postgres", storageConfig.Driver)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return writeSQLiteBackup(ctx, storageConfig.DSN, *out)
}

// writeSQLiteBackup is the testable core of runBackup. Splitting this
// out lets tests drive a real temp DB without going through flag
// parsing or signal wiring.
func writeSQLiteBackup(ctx context.Context, sourceDSN, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		return fmt.Errorf("backup: create output dir: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "panvex-backup-*")
	if err != nil {
		return fmt.Errorf("backup: tempdir: %w", err)
	}
	defer func() {
		// Best-effort tempdir cleanup. A leftover snapshot DB in
		// /tmp/panvex-backup-* is harmless; the archive (if produced)
		// already contains a sealed copy of it.
		_ = os.RemoveAll(tmpDir)
	}()

	snapshotPath := filepath.Join(tmpDir, "panvex.db")
	schemaVersion, err := vacuumIntoSQLite(ctx, sourceDSN, snapshotPath)
	if err != nil {
		return err
	}

	metadata := backupMetadata{
		FormatVersion:             1,
		PanelVersion:              Version,
		PanelCommit:               CommitSHA,
		StorageDriver:             config.StorageDriverSQLite,
		SchemaVersion:             schemaVersion,
		EncryptionKeyFingerprint:  encryptionKeyFingerprint(),
		CreatedAt:                 time.Now().UTC().Format(time.RFC3339),
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: marshal metadata: %w", err)
	}

	return writeBackupArchive(outPath, snapshotPath, metadataBytes)
}

// backupMetadata is the JSON sidecar embedded in the archive. The
// fingerprint is one-way (SHA256 prefix) so the archive is safe to
// store alongside the .db without leaking the actual key. Operators
// cross-check the fingerprint at restore time before pointing the
// panel at the file.
type backupMetadata struct {
	FormatVersion            int    `json:"format_version"`
	PanelVersion             string `json:"panel_version"`
	PanelCommit              string `json:"panel_commit"`
	StorageDriver            string `json:"storage_driver"`
	SchemaVersion            int64  `json:"schema_version"`
	EncryptionKeyFingerprint string `json:"encryption_key_fingerprint"`
	CreatedAt                string `json:"created_at"`
}

// vacuumIntoSQLite runs VACUUM INTO against sourceDSN and writes the
// result to dest. Returns the highest applied goose version observed
// in the source, which is captured into the archive metadata so a
// restore-time mismatch can be flagged before the panel boots.
//
// VACUUM INTO is the official online-backup recipe: SQLite acquires
// a shared lock for the duration of the copy, so concurrent readers
// keep working and the snapshot is internally consistent without
// requiring the panel to be stopped.
func vacuumIntoSQLite(ctx context.Context, sourceDSN, dest string) (int64, error) {
	if _, err := os.Stat(dest); err == nil {
		// VACUUM INTO refuses to overwrite. Be defensive — the dest
		// is in our own tempdir but a future caller might pass a
		// pre-existing path.
		return 0, fmt.Errorf("backup: destination %q already exists", dest)
	}

	db, err := sql.Open("sqlite", sourceDSN)
	if err != nil {
		return 0, fmt.Errorf("backup: open source: %w", err)
	}
	defer db.Close()

	// VACUUM INTO refuses to bind the destination via a parameter
	// placeholder. The path comes from our own tempdir, never from
	// operator-controlled input, so the literal string interpolation
	// is safe; we still escape single quotes defensively.
	escaped := strings.ReplaceAll(dest, "'", "''")
	//nolint:gosec // G201: VACUUM INTO requires literal path; dest is a tempdir we control.
	stmt := fmt.Sprintf("VACUUM INTO '%s'", escaped)
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return 0, fmt.Errorf("backup: VACUUM INTO: %w", err)
	}

	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version); err != nil {
		// "no such table" is the only legitimate swallow case
		// (operator backed up a never-migrated DB). Anything else
		// — corruption, permission, disk error — would silently
		// produce an archive labelled schema_version=0 and defeat
		// the version-mismatch check at restore time.
		if isSQLiteMissingTable(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("backup: read schema version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return version.Int64, nil
}

// isSQLiteMissingTable reports whether err comes from a SELECT against
// a table that does not exist. modernc.org/sqlite wraps the SQLite C
// "no such table:" message verbatim; matching by substring keeps this
// driver-agnostic without taking a hard dependency on the sqlite
// package's error types.
func isSQLiteMissingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table")
}

// encryptionKeyFingerprint returns the first 8 hex chars of the SHA256
// hash of PANVEX_ENCRYPTION_KEY, or an empty string when the env var
// is unset. NEVER logs or returns the key itself (constraint: one-way).
func encryptionKeyFingerprint() string {
	key := strings.TrimSpace(os.Getenv("PANVEX_ENCRYPTION_KEY"))
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:8]
}

// writeBackupArchive writes a tar.gz containing the snapshot DB and
// the metadata JSON. The archive is written to <outPath>.tmp first
// and renamed into place on success — a SIGTERM mid-write or a tar
// writer error therefore leaves the previous good archive (or nothing
// at all) at outPath, never a half-written file.
//
// File ownership is dropped (uid/gid 0) so the archive is portable
// across hosts.
func writeBackupArchive(outPath, dbPath string, metadataBytes []byte) (err error) {
	tmpPath := outPath + ".tmp"
	// Clean up any leftover .tmp from a previous interrupted run so
	// the create below isn't surprised by a stale file.
	_ = os.Remove(tmpPath)

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("backup: create archive: %w", err)
	}
	// Two-phase cleanup: close the file handle, then either rename
	// it into place or remove the partial. This matches the standard
	// "atomic write" idiom; do not collapse into one defer.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(tmpPath)
			return
		}
		if rerr := os.Rename(tmpPath, outPath); rerr != nil {
			err = fmt.Errorf("backup: finalize archive: %w", rerr)
			_ = os.Remove(tmpPath)
		}
	}()

	gzw := gzip.NewWriter(f)
	defer func() {
		if cerr := gzw.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	tw := tar.NewWriter(gzw)
	defer func() {
		if cerr := tw.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if err := writeTarBytes(tw, "metadata.json", metadataBytes); err != nil {
		return err
	}
	if err := writeTarFile(tw, "panvex.db", dbPath); err != nil {
		return err
	}
	return nil
}

// writeTarBytes writes content as a regular tar entry with `name`.
func writeTarBytes(tw *tar.Writer, name string, content []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(content)),
		ModTime: time.Now().UTC(),
		Format:  tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: tar header %s: %w", name, err)
	}
	if _, err := tw.Write(content); err != nil {
		return fmt.Errorf("backup: tar write %s: %w", name, err)
	}
	return nil
}

// writeTarFile streams the file at srcPath into the archive under `name`.
// Streams in 32 KiB chunks to keep memory bounded for large DBs.
func writeTarFile(tw *tar.Writer, name, srcPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("backup: stat %s: %w", srcPath, err)
	}
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    info.Size(),
		ModTime: info.ModTime().UTC(),
		Format:  tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: tar header %s: %w", name, err)
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", srcPath, err)
	}
	defer src.Close()
	if _, err := io.Copy(tw, src); err != nil {
		return fmt.Errorf("backup: tar copy %s: %w", name, err)
	}
	return nil
}

// runRestore is a documentation stub extended with optional -archive
// verify mode (C6). Without -archive it prints the manual recipe; with
// -archive it reads metadata.json from the archive and cross-checks the
// encryption-key fingerprint and schema version so operators can confirm
// safety before carrying out the manual steps.
func runRestore(args []string) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	archive := flags.String("archive", "", "Backup archive to VERIFY against the current environment (optional)")
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	flags.Usage = func() {
		// Best-effort writes to the flag output; failure to print usage
		// text is not actionable.
		_, _ = fmt.Fprintf(flags.Output(), "Usage: panvex-control-plane restore [-archive <path.tar.gz>] [flags]\n\n")
		_, _ = fmt.Fprintf(flags.Output(), "Without -archive: prints the manual restore steps (we deliberately do\n")
		_, _ = fmt.Fprintf(flags.Output(), "NOT auto-restore — overwriting a populated DB loses fleets).\n")
		_, _ = fmt.Fprintf(flags.Output(), "With -archive: verifies the archive's encryption-key fingerprint and\n")
		_, _ = fmt.Fprintf(flags.Output(), "schema version against the current environment before you restore.\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*archive) == "" {
		fmt.Println(restoreHelpText())
		return nil
	}
	return verifyRestoreArchive(context.Background(), *archive, *storageDriver, *storageDSN)
}

// verifyRestoreArchive implements `restore -archive` (C6). It reads
// metadata.json from the archive and validates:
//  1. Encryption-key fingerprint matches the current PANVEX_ENCRYPTION_KEY.
//  2. If -storage-dsn is provided (SQLite only), the archive schema version
//     is compatible with the target DB.
func verifyRestoreArchive(ctx context.Context, archivePath, storageDriver, storageDSN string) error {
	meta, err := readBackupMetadata(archivePath)
	if err != nil {
		return err
	}
	fmt.Printf("Archive: %s\n  panel_version: %s (%s)\n  storage_driver: %s\n  schema_version: %d\n  created_at: %s\n",
		archivePath, meta.PanelVersion, meta.PanelCommit, meta.StorageDriver, meta.SchemaVersion, meta.CreatedAt)

	current := encryptionKeyFingerprint()
	switch {
	case meta.EncryptionKeyFingerprint == "":
		fmt.Println("  encryption key: archive was taken WITHOUT PANVEX_ENCRYPTION_KEY — nothing to verify")
	case current == "":
		return fmt.Errorf("restore: archive expects encryption-key fingerprint %s but PANVEX_ENCRYPTION_KEY is not set — the restored DB could not decrypt secrets", meta.EncryptionKeyFingerprint)
	case current != meta.EncryptionKeyFingerprint:
		return fmt.Errorf("restore: encryption-key fingerprint mismatch: archive %s, current env %s — restoring would leave webhook/integration secrets undecryptable; locate the key the backup was taken with", meta.EncryptionKeyFingerprint, current)
	default:
		fmt.Println("  encryption key: fingerprint matches current PANVEX_ENCRYPTION_KEY")
	}

	if strings.TrimSpace(storageDSN) != "" {
		storageConfig, err := config.ResolveStorage(storageDriver, storageDSN)
		if err != nil {
			return err
		}
		if storageConfig.Driver != config.StorageDriverSQLite {
			return fmt.Errorf("restore: schema verification supports sqlite only (got %q); for postgres compare goose_db_version manually — see deploy/backup.md", storageConfig.Driver)
		}
		targetVersion, err := sqliteSchemaVersion(ctx, storageConfig.DSN)
		if err != nil {
			return err
		}
		switch {
		case meta.SchemaVersion > targetVersion:
			fmt.Printf("  schema: archive (v%d) is AHEAD of target (v%d) — run migrate-schema after restoring\n", meta.SchemaVersion, targetVersion)
		case meta.SchemaVersion < targetVersion:
			return fmt.Errorf("restore: archive schema v%d is OLDER than the target DB v%d — goose cannot downgrade; restore onto a fresh path and let migrate-schema bring it forward, do not overwrite the newer DB", meta.SchemaVersion, targetVersion)
		default:
			fmt.Printf("  schema: version v%d matches target\n", meta.SchemaVersion)
		}
	}

	fmt.Println("\nVerification passed. Follow the manual steps to actually restore:")
	fmt.Println(restoreHelpText())
	return nil
}

// readBackupMetadata extracts and parses metadata.json from a backup archive.
func readBackupMetadata(path string) (backupMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return backupMetadata{}, fmt.Errorf("restore: open archive: %w", err)
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return backupMetadata{}, fmt.Errorf("restore: gzip: %w", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return backupMetadata{}, fmt.Errorf("restore: read archive: %w", err)
		}
		if hdr.Name != "metadata.json" {
			continue
		}
		var meta backupMetadata
		if err := json.NewDecoder(tr).Decode(&meta); err != nil {
			return backupMetadata{}, fmt.Errorf("restore: parse metadata.json: %w", err)
		}
		return meta, nil
	}
	return backupMetadata{}, errors.New("restore: archive has no metadata.json — not a panvex backup?")
}

// sqliteSchemaVersion reads the highest applied goose version from the
// database at dsn without copying it. 0 means "never migrated".
func sqliteSchemaVersion(ctx context.Context, dsn string) (int64, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return 0, fmt.Errorf("restore: open target db: %w", err)
	}
	defer db.Close()
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&version); err != nil {
		if isSQLiteMissingTable(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("restore: read target schema version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return version.Int64, nil
}

// restoreHelpText is split out so tests can pin the operator-facing
// string against accidental edits.
func restoreHelpText() string {
	return strings.TrimSpace(`
Panvex backup restore (manual procedure)

1. Stop the panel:
     systemctl stop panvex-control-plane

2. Extract the archive next to (NOT over) the existing DB:
     mkdir -p /var/lib/panvex/restore
     tar -xzf <archive>.tar.gz -C /var/lib/panvex/restore

3. Inspect metadata.json — confirm panel_version and
   encryption_key_fingerprint match the panel you are restoring into.
   A mismatched fingerprint means PANVEX_ENCRYPTION_KEY is wrong;
   the restored DB will refuse to decrypt secrets.

4. Move the snapshot into place (the old DB is preserved as .bak):
     mv /var/lib/panvex/panvex.db /var/lib/panvex/panvex.db.bak
     mv /var/lib/panvex/restore/panvex.db /var/lib/panvex/panvex.db

5. Run any pending schema migrations:
     panvex-control-plane migrate-schema \
       -storage-driver sqlite \
       -storage-dsn /var/lib/panvex/panvex.db

6. Start the panel:
     systemctl start panvex-control-plane
`)
}
