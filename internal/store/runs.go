// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrRunActive is returned when an account that already has an active run
// asks for a new one.
var ErrRunActive = errors.New("an active run exists")

// Gateway is the player's computer.
type Gateway struct {
	CPUMHz    int
	RAMMB     int
	ModemMbps int
	StorageMB int
	NICCount  int
	Security  int
}

// Installed names one installed version of a program.
type Installed struct {
	Software string
	Version  int
}

// Known is a server whose address the run has, and whether it has been
// probed: until it is, the player sees the address and the name and nothing
// else about it.
type Known struct {
	Server string
	Probed bool
}

// BankAccount is one of the player's bank accounts.
type BankAccount struct {
	Server  string
	Clean   int64
	Dirty   int64
	Primary bool
}

// NewRun is what a new run starts with.
type NewRun struct {
	AccountID int64
	Gateway   Gateway
	Bank      string
	Balance   int64
	Software  []Installed
	Known     []string
}

// Run is the state of an active run that the client is shown.
type Run struct {
	ID              int64
	Reputation      int
	ExposureSeconds int64
	CreatedAt       time.Time
	Gateway         Gateway
	Accounts        []BankAccount
	Software        []Installed
	Known           []Known
	// Root are the servers this run holds root on. The layers of a
	// connection close when it ends; root does not, for the life of the run.
	Root []string
}

// HasRoot reports whether the run holds root on the given server.
func (r *Run) HasRoot(server string) bool {
	for _, s := range r.Root {
		if s == server {
			return true
		}
	}
	return false
}

// Knows reports whether the run has the address of the given server.
func (r *Run) Knows(server string) bool {
	for _, k := range r.Known {
		if k.Server == server {
			return true
		}
	}
	return false
}

// CreateRun starts a run. It fails with ErrRunActive when the account has one.
func (s *Store) CreateRun(ctx context.Context, n NewRun) (int64, error) {
	var id int64
	err := s.tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "INSERT INTO run (account_id) VALUES ($1) RETURNING id", n.AccountID).Scan(&id)
		if isUniqueViolation(err, "run_one_active_idx") {
			return ErrRunActive
		}
		if err != nil {
			return err
		}
		g := n.Gateway
		if _, err := tx.Exec(ctx, `INSERT INTO gateway (run_id, cpu_mhz, ram_mb, modem_mbps, storage_mb, nic_count, security)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, g.CPUMHz, g.RAMMB, g.ModemMbps, g.StorageMB, g.NICCount, g.Security); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO bank_account (run_id, bank_server, balance_clean, is_primary)
			VALUES ($1, $2, $3, true)`, id, n.Bank, n.Balance); err != nil {
			return err
		}
		for _, sw := range n.Software {
			if _, err := tx.Exec(ctx, "INSERT INTO run_software (run_id, software, version) VALUES ($1, $2, $3)",
				id, sw.Software, sw.Version); err != nil {
				return err
			}
		}
		for _, server := range n.Known {
			if _, err := tx.Exec(ctx, "INSERT INTO known_server (run_id, server) VALUES ($1, $2)", id, server); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// ActiveRun returns the account's active run, or nil when it has none.
func (s *Store) ActiveRun(ctx context.Context, accountID int64) (*Run, error) {
	r := &Run{}
	err := s.pool.QueryRow(ctx, `SELECT r.id, r.reputation, r.exposure_seconds, r.created_at,
			g.cpu_mhz, g.ram_mb, g.modem_mbps, g.storage_mb, g.nic_count, g.security
		FROM run r JOIN gateway g ON g.run_id = r.id
		WHERE r.account_id = $1 AND r.active`, accountID).
		Scan(&r.ID, &r.Reputation, &r.ExposureSeconds, &r.CreatedAt,
			&r.Gateway.CPUMHz, &r.Gateway.RAMMB, &r.Gateway.ModemMbps, &r.Gateway.StorageMB,
			&r.Gateway.NICCount, &r.Gateway.Security)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	r.Accounts, err = collect(ctx, s, `SELECT bank_server, balance_clean, balance_dirty, is_primary
		FROM bank_account WHERE run_id = $1 ORDER BY is_primary DESC, id`, r.ID,
		func(row pgx.CollectableRow) (BankAccount, error) {
			var b BankAccount
			err := row.Scan(&b.Server, &b.Clean, &b.Dirty, &b.Primary)
			return b, err
		})
	if err != nil {
		return nil, err
	}
	r.Software, err = collect(ctx, s, `SELECT software, version FROM run_software
		WHERE run_id = $1 ORDER BY software, version`, r.ID,
		func(row pgx.CollectableRow) (Installed, error) {
			var i Installed
			err := row.Scan(&i.Software, &i.Version)
			return i, err
		})
	if err != nil {
		return nil, err
	}
	r.Root, err = collect(ctx, s, "SELECT server FROM run_root WHERE run_id = $1 ORDER BY server", r.ID,
		func(row pgx.CollectableRow) (string, error) {
			var server string
			err := row.Scan(&server)
			return server, err
		})
	if err != nil {
		return nil, err
	}
	r.Known, err = collect(ctx, s, "SELECT server, probed FROM known_server WHERE run_id = $1 ORDER BY server", r.ID,
		func(row pgx.CollectableRow) (Known, error) {
			var k Known
			err := row.Scan(&k.Server, &k.Probed)
			return k, err
		})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// MarkProbed records that the run has probed the server, which is what makes
// its type, its security level and its layers visible to the player. It
// reports whether the run knows that address at all.
func (s *Store) MarkProbed(ctx context.Context, runID int64, server string) (bool, error) {
	tag, err := s.pool.Exec(ctx, "UPDATE known_server SET probed = true WHERE run_id = $1 AND server = $2",
		runID, server)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// LogEntry is one line a run left in a server's log.
type LogEntry struct {
	ID        int64
	Server    string
	HopIndex  int
	WorldTime time.Time
	Action    string
	// SourceIP is the node the connection came from, empty for the hop the
	// player's own gateway talked to.
	SourceIP string
	// WipedWith is the version of the LogWiper that cleared the line, or 0
	// while it stands.
	WipedWith int
	// Severity is how heavy the act was, which is what a fine is weighed
	// against.
	Severity int
}

// Wiped reports whether the line has been cleared.
func (e LogEntry) Wiped() bool { return e.WipedWith > 0 }

// AddLogs writes the lines a run left, in one statement.
func (s *Store) AddLogs(ctx context.Context, runID int64, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []any{runID, e.Server, e.HopIndex, e.WorldTime, e.Action, e.SourceIP, e.Severity})
	}
	_, err := s.pool.CopyFrom(ctx, pgx.Identifier{"run_log"},
		[]string{"run_id", "server", "hop_index", "world_time", "action", "source_ip", "severity"},
		pgx.CopyFromRows(rows))
	return err
}

// LogsOn returns the lines this run left on one server, oldest first.
func (s *Store) LogsOn(ctx context.Context, runID int64, server string) ([]LogEntry, error) {
	return collect2(ctx, s, `SELECT id, server, hop_index, world_time, action, source_ip, wiped_with, severity
		FROM run_log WHERE run_id = $1 AND server = $2 ORDER BY id`, runID, server, scanLog)
}

// LogsOfRun returns every line this run left, oldest first. An investigation
// reads them to walk the route back.
func (s *Store) LogsOfRun(ctx context.Context, runID int64) ([]LogEntry, error) {
	return collect(ctx, s, `SELECT id, server, hop_index, world_time, action, source_ip, wiped_with, severity
		FROM run_log WHERE run_id = $1 ORDER BY id`, runID, scanLog)
}

func scanLog(row pgx.CollectableRow) (LogEntry, error) {
	var e LogEntry
	err := row.Scan(&e.ID, &e.Server, &e.HopIndex, &e.WorldTime, &e.Action, &e.SourceIP, &e.WipedWith, &e.Severity)
	return e, err
}

// WipeLogs clears the lines this run left on one server and reports how many
// it cleared. A line already clear is left as it was: the first wipe is the
// one that counts, and a worse program cannot undo a better one.
func (s *Store) WipeLogs(ctx context.Context, runID int64, server string, version int) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE run_log SET wiped_with = $3
		WHERE run_id = $1 AND server = $2 AND wiped_with = 0`, runID, server, version)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// The states an investigation can be in.
const (
	// InvestigationRunning is a walk still coming.
	InvestigationRunning = "RUNNING"
	// InvestigationStalled is a walk that found a cleared log and has
	// nothing left to follow.
	InvestigationStalled = "STALLED"
	// InvestigationAtGateway is a walk that reached the player.
	InvestigationAtGateway = "REACHED_GATEWAY"
)

// Investigation is an organisation walking a route back after a break-in.
type Investigation struct {
	ID            int64
	RunID         int64
	AccountID     int64
	Target        string
	Route         []string
	Severity      int
	Position      int
	State         string
	Due           int64
	Reconstructed bool
	Fined, Seized bool
	StartedAt     time.Time
}

// StartInvestigation writes down a walk that is about to begin.
func (s *Store) StartInvestigation(ctx context.Context, in Investigation) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO investigation
		(run_id, target, route, severity, state, due, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		in.RunID, in.Target, in.Route, in.Severity, in.State, in.Due, in.StartedAt).Scan(&id)
	return id, err
}

// DueInvestigations returns the walks whose next step has come: the run's
// online time has passed what the step was waiting for. The account is
// carried along, because what happens next has to reach the player's browser.
func (s *Store) DueInvestigations(ctx context.Context) ([]Investigation, error) {
	rows, err := s.pool.Query(ctx, `SELECT i.id, i.run_id, r.account_id, i.target, i.route,
			i.severity, i.position, i.state, i.due, i.reconstructed, i.fined, i.seized, i.started_at
		FROM investigation i JOIN run r ON r.id = i.run_id
		WHERE i.state = $1 AND r.active AND i.due <= r.exposure_seconds
		ORDER BY i.id`, InvestigationRunning)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanInvestigation)
}

// InvestigationsOfRun returns every walk against this run, oldest first.
func (s *Store) InvestigationsOfRun(ctx context.Context, runID int64) ([]Investigation, error) {
	return collect(ctx, s, `SELECT i.id, i.run_id, r.account_id, i.target, i.route,
			i.severity, i.position, i.state, i.due, i.reconstructed, i.fined, i.seized, i.started_at
		FROM investigation i JOIN run r ON r.id = i.run_id
		WHERE i.run_id = $1 ORDER BY i.id`, runID, scanInvestigation)
}

func scanInvestigation(row pgx.CollectableRow) (Investigation, error) {
	var in Investigation
	err := row.Scan(&in.ID, &in.RunID, &in.AccountID, &in.Target, &in.Route, &in.Severity,
		&in.Position, &in.State, &in.Due, &in.Reconstructed, &in.Fined, &in.Seized, &in.StartedAt)
	return in, err
}

// SaveInvestigation writes back where a walk has got to.
func (s *Store) SaveInvestigation(ctx context.Context, in Investigation) error {
	_, err := s.pool.Exec(ctx, `UPDATE investigation
		SET position = $2, state = $3, due = $4, reconstructed = $5, fined = $6, seized = $7
		WHERE id = $1`, in.ID, in.Position, in.State, in.Due, in.Reconstructed, in.Fined, in.Seized)
	return err
}

// Fine takes money whether the account holds it or not: an obligation is not
// a purchase, and what is left of the balance is a debt. It returns the
// balance afterwards.
func (s *Store) Fine(ctx context.Context, runID int64, amount int64) (int64, error) {
	var left int64
	err := s.pool.QueryRow(ctx, `UPDATE bank_account SET balance_clean = balance_clean - $2
		WHERE run_id = $1 AND is_primary RETURNING balance_clean`, runID, amount).Scan(&left)
	return left, err
}

// SeizeSoftware takes one program off the gateway and reports which. The
// dearest version goes: a seizure is meant to hurt.
func (s *Store) SeizeSoftware(ctx context.Context, runID int64, software string, version int) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM run_software WHERE run_id = $1 AND software = $2 AND version = $3",
		runID, software, version)
	return err
}

// Debtor is a run that owes money, or has been owing it.
type Debtor struct {
	RunID     int64
	AccountID int64
	Exposure  int64
	Balance   int64
	Since     *int64
	Charged   int
	Missed    int
}

// Debtors returns the active runs whose balance has fallen below the
// threshold, and those still inside a window they have not climbed out of.
// One query serves the whole instance.
func (s *Store) Debtors(ctx context.Context, threshold int64) ([]Debtor, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.account_id, r.exposure_seconds,
			b.balance_clean, r.debt_since, r.debt_charged, r.debt_missed
		FROM run r JOIN bank_account b ON b.run_id = r.id AND b.is_primary
		WHERE r.active AND (b.balance_clean < $1 OR r.debt_since IS NOT NULL)
		ORDER BY r.id`, threshold)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Debtor, error) {
		var d Debtor
		err := row.Scan(&d.RunID, &d.AccountID, &d.Exposure, &d.Balance, &d.Since, &d.Charged, &d.Missed)
		return d, err
	})
}

// SaveDebt writes back where a debt stands. A nil since clears it: the player
// has climbed out.
func (s *Store) SaveDebt(ctx context.Context, runID int64, since *int64, charged, missed int) error {
	_, err := s.pool.Exec(ctx, `UPDATE run SET debt_since = $2, debt_charged = $3, debt_missed = $4
		WHERE id = $1`, runID, since, charged, missed)
	return err
}

// Spend takes clean money from the run's primary account and reports whether
// there was enough. A purchase never makes a player poorer than nothing; an
// obligation can, which is why a fine is not a purchase.
func (s *Store) Spend(ctx context.Context, runID int64, amount int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE bank_account SET balance_clean = balance_clean - $2
		WHERE run_id = $1 AND is_primary AND balance_clean >= $2`, runID, amount)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// Credit pays money into the run's primary account. Clean money is what work
// through the board pays; dirty is what is taken.
func (s *Store) Credit(ctx context.Context, runID int64, clean, dirty int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE bank_account
		SET balance_clean = balance_clean + $2, balance_dirty = balance_dirty + $3
		WHERE run_id = $1 AND is_primary`, runID, clean, dirty)
	return err
}

// InstallSoftware puts a version of a program on the gateway. Owning it twice
// is owning it once.
func (s *Store) InstallSoftware(ctx context.Context, runID int64, id string, version int) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO run_software (run_id, software, version)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, runID, id, version)
	return err
}

// GrantRoot records that the run won root on a server. Winning it twice
// changes nothing, so the first time is the one that counts.
func (s *Store) GrantRoot(ctx context.Context, runID int64, server string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO run_root (run_id, server, opened_at) VALUES ($1, $2, $3)
		ON CONFLICT (run_id, server) DO NOTHING`, runID, server, at)
	return err
}

// EndRun closes a run: the trace reached the gateway, or the player is out of
// the game for another reason. A run ends once.
func (s *Store) EndRun(ctx context.Context, runID int64, at time.Time) error {
	_, err := s.pool.Exec(ctx, "UPDATE run SET active = false, ended_at = $2 WHERE id = $1 AND active",
		runID, at)
	return err
}

// AddExposure adds seconds of online time to the active runs of the given accounts.
func (s *Store) AddExposure(ctx context.Context, accountIDs []int64, seconds int64) error {
	if len(accountIDs) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE run SET exposure_seconds = exposure_seconds + $2
		WHERE active AND account_id = ANY($1)`, accountIDs, seconds)
	return err
}

func collect[T any](ctx context.Context, s *Store, sql string, arg any, scan pgx.RowToFunc[T]) ([]T, error) {
	rows, err := s.pool.Query(ctx, sql, arg)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scan)
}

func collect2[T any](ctx context.Context, s *Store, sql string, a, b any, scan pgx.RowToFunc[T]) ([]T, error) {
	rows, err := s.pool.Query(ctx, sql, a, b)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scan)
}
